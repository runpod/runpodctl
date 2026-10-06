package project

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	// trust is keyed on the pod, not on its address: runpod recycles pod ssh
	// addresses aggressively, so an address-keyed store would report a mismatch
	// for users who did nothing wrong, and the natural workaround -- deleting
	// the entry -- trains people to ignore the one warning this exists to
	// raise. HostKeyAlias is client-side only, so there is no protocol change,
	// and both clients can look up the same string.
	hostKeyAliasPrefix = "runpod-"

	knownHostsDirPerm  = 0o700
	knownHostsFilePerm = 0o600

	hostKeyLockRetry = 50 * time.Millisecond
)

// hostKeyLockTimeout bounds the wait for another runpodctl's lock. the lock is
// only held across one read/check/append, so a longer wait means that process
// is stuck, and blocking on it would hang this one with no message.
var hostKeyLockTimeout = 10 * time.Second

// hostKeyAlias is the known_hosts key for a pod, shared by the go client and
// the openssh -o HostKeyAlias option so both maintain one entry per pod.
func hostKeyAlias(podID string) string {
	return hostKeyAliasPrefix + podID
}

// hostKeyLookup is the address knownhosts must be asked about for podID.
// knownhosts reads an unbracketed pattern as port 22 and then compares ports
// exactly, so the lookup has to name that port for the bare alias line openssh
// writes to match. verified against openssh 10.3: with HostKeyAlias set it
// records the alias with no port bracket even on a non-default port, which is
// also what knownhosts.Line produces.
func hostKeyLookup(podID string) string {
	return net.JoinHostPort(hostKeyAlias(podID), "22")
}

// knownHostsFile is the trust store's path, whether or not it exists yet.
func knownHostsFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".runpod", "ssh", "known_hosts"), nil
}

// knownHostsPath returns the trust store, creating it if needed.
// knownhosts.New fails outright on a missing file, so it cannot be left to the
// first write. it opens read-only: checking an already-trusted pod only reads
// the store, so a read-only store must not block that. appendKnownHost opens
// for writing only when there is a new key to record.
func knownHostsPath() (string, error) {
	path, err := knownHostsFile()
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, knownHostsDirPerm); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, knownHostsFilePerm)
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("creating %s: %w", path, err)
	}
	return path, nil
}

// hostKeyMismatchError is the callback refusing a key other than the pod's
// recorded one. it carries both keys so dialPod can show them to the user,
// and its message carries the scripted remedy for a run that cannot ask.
type hostKeyMismatchError struct {
	podID    string
	path     string
	offered  ssh.PublicKey
	recorded []knownhosts.KnownKey
	cause    error
}

func (e *hostKeyMismatchError) Error() string {
	return fmt.Sprintf("host key mismatch for pod %s: it offered %s, which is not the key recorded in %s. "+
		"stopping and starting a pod, or updating it, clears its container disk, and most images then generate a new host key. "+
		"if that happened since your last connection, run `runpodctl ssh forget %s` and retry. "+
		"otherwise the connection may be intercepted: %v",
		e.podID, ssh.FingerprintSHA256(e.offered), e.path, e.podID, e.cause)
}

func (e *hostKeyMismatchError) Unwrap() error { return e.cause }

// podHostKeyCallback verifies a pod's host key against path, recording the key
// on first contact and refusing a changed one. openssh requires this pin
// before connecting; both clients share the file and the entry format.
//
// trust on first use rejects a substituted key on later connections, unlike
// StrictHostKeyChecking=no, but it cannot detect a machine-in-the-middle on
// the very first connection. Closing that needs the host key delivered through
// an authenticated runpod channel and is separate work.
func podHostKeyCallback(podID, path string) (ssh.HostKeyCallback, error) {
	if _, err := knownhosts.New(path); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	alias := hostKeyAlias(podID)
	lookup := hostKeyLookup(podID)

	return func(_ string, remote net.Addr, key ssh.PublicKey) (resultErr error) {
		lock, err := lockKnownHosts(path)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := lock.Close(); resultErr == nil && closeErr != nil {
				resultErr = fmt.Errorf("releasing host key lock for %s: %w", path, closeErr)
			}
		}()

		verify, err := knownhosts.New(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		// the dialed address is deliberately discarded in favour of the alias.
		err = verify(lookup, remote, key)
		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
			if addErr := appendKnownHost(path, alias, key); addErr != nil {
				return fmt.Errorf("recording host key for pod %s: %w", podID, addErr)
			}
			fmt.Fprintf(os.Stderr, "trusting new host key for pod %s (%s)\n", podID, ssh.FingerprintSHA256(key))
			return nil
		}

		// never heal silently. a mismatch is either a new container or an
		// interception, and only the user can tell which. the pod id survives a
		// stop/start but the host key usually does not: the container disk is
		// cleared on stop, and runpod's images run ssh-keygen at boot for any
		// missing /etc/ssh/ssh_host_* key (runpod/containers start.sh).
		var recorded []knownhosts.KnownKey
		if keyErr != nil {
			recorded = sortedByLine(keyErr.Want)
		}
		return &hostKeyMismatchError{podID: podID, path: path, offered: key, recorded: recorded, cause: err}
	}, nil
}

// recordedKnownKeys returns the keys recorded for podID in file order, or nil
// when none are. knownhosts has no lookup, so it is offered a key that cannot
// match and the recorded ones are read off the mismatch (golang/go#29286).
func recordedKnownKeys(podID, path string) ([]knownhosts.KnownKey, error) {
	check, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	err = check(hostKeyLookup(podID), &net.TCPAddr{IP: net.IPv4zero}, placeholderHostKey{})
	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) {
		return nil, nil
	}
	return sortedByLine(keyErr.Want), nil
}

// sortedByLine puts keys in file order; KeyError.Want is built from a map.
func sortedByLine(keys []knownhosts.KnownKey) []knownhosts.KnownKey {
	return slices.SortedFunc(slices.Values(keys), func(a, b knownhosts.KnownKey) int {
		return cmp.Compare(a.Line, b.Line)
	})
}

// recordedHostKeyAlgorithms returns the host key algorithms of the keys
// recorded for podID, in file order, or nil when none are. knownhosts treats a
// key of another type as a mismatch, so a client must negotiate one of these
// or a trusted pod reads as an interception.
func recordedHostKeyAlgorithms(podID, path string) ([]string, error) {
	recorded, err := recordedKnownKeys(podID, path)
	if err != nil {
		return nil, err
	}
	var algos []string
	for _, known := range recorded {
		for _, algo := range signingAlgorithms(known.Key.Type()) {
			if !slices.Contains(algos, algo) {
				algos = append(algos, algo)
			}
		}
	}
	return algos, nil
}

// pinnedHostKeyAlgorithms is the go client's HostKeyAlgorithms for podID: the
// recorded key's algorithms first, so the pin holds even though go's default
// order (ecdsa before ed25519) is not openssh's and may change. the remaining
// plain-key algorithms follow so a pod that no longer offers the recorded type
// still reaches the callback, which reports the mismatch, rather than failing
// negotiation with no remedy. nil, meaning go's defaults, until a key is recorded.
func pinnedHostKeyAlgorithms(podID, path string) ([]string, error) {
	algos, err := recordedHostKeyAlgorithms(podID, path)
	if err != nil || len(algos) == 0 {
		return nil, err
	}
	for _, algo := range ssh.SupportedAlgorithms().HostKeys {
		if !strings.Contains(algo, "-cert-") && !slices.Contains(algos, algo) {
			algos = append(algos, algo)
		}
	}
	return algos, nil
}

// signingAlgorithms maps a recorded key type to the host key algorithms that
// negotiate it. an rsa key signs under several names; the sha-1 ssh-rsa is
// left out, as go's secure defaults leave it out.
func signingAlgorithms(keyType string) []string {
	if keyType == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
	}
	return []string{keyType}
}

// placeholderHostKey is a key no known_hosts line can match.
type placeholderHostKey struct{}

func (placeholderHostKey) Type() string    { return "runpodctl-placeholder" }
func (placeholderHostKey) Marshal() []byte { return []byte("runpodctl-placeholder") }
func (placeholderHostKey) Verify([]byte, *ssh.Signature) error {
	return errors.New("placeholder host key")
}

func lockKnownHosts(path string) (*os.File, error) {
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, knownHostsFilePerm)
	if err != nil {
		return nil, fmt.Errorf("opening host key lock for %s: %w", path, err)
	}
	deadline := time.Now().Add(hostKeyLockTimeout)
	for {
		locked, err := tryLockHostKeyFile(file)
		if err != nil {
			file.Close()
			return nil, fmt.Errorf("locking host keys in %s: %w", path, err)
		}
		if locked {
			return file, nil
		}
		if time.Now().After(deadline) {
			file.Close()
			return nil, fmt.Errorf("host key store %s is locked by another runpodctl process (waited %s); retry once it finishes", path, hostKeyLockTimeout)
		}
		time.Sleep(hostKeyLockRetry)
	}
}

// appendKnownHost adds one trust line in the format openssh writes for a
// HostKeyAlias, so an entry from either client is read by both.
func appendKnownHost(path, alias string, key ssh.PublicKey) (err error) {
	// O_RDWR, not O_WRONLY: lacksTrailingNewline reads the last byte, which
	// fails with EBADF on a write-only descriptor. O_APPEND still forces every
	// write to the end regardless of the read offset.
	f, openErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, knownHostsFilePerm)
	if openErr != nil {
		return openErr
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	// a hand-edited file can end without a newline, and appending to that would
	// splice this entry onto the previous one, silently corrupting both.
	needsNewline, err := lacksTrailingNewline(f)
	if err != nil {
		return err
	}
	line := knownhosts.Line([]string{alias}, key) + "\n"
	if needsNewline {
		line = "\n" + line
	}
	_, err = f.WriteString(line)
	return err
}

// lacksTrailingNewline reports whether f is non-empty and its last byte is not
// a newline. ReadAt leaves f's offset unchanged.
func lacksTrailingNewline(f *os.File) (bool, error) {
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() == 0 {
		return false, nil
	}
	var last [1]byte
	if _, err := f.ReadAt(last[:], info.Size()-1); err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return last[0] != '\n', nil
}

// ForgetHostKey drops the host key recorded for podID and returns the store
// path and the fingerprints it removed, so the next exec or project connection
// enrolls the key the pod then offers. a pod with nothing recorded is not an
// error: a script can forget and retry without checking first.
func ForgetHostKey(podID string) (string, []string, error) {
	if strings.TrimSpace(podID) == "" {
		return "", nil, errors.New("a pod id is required")
	}
	path, err := knownHostsFile()
	if err != nil {
		return "", nil, err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return path, []string{}, nil
		}
		return path, nil, err
	}
	removed, err := forgetKnownHost(path, hostKeyAlias(podID))
	if err != nil {
		return path, nil, err
	}
	fingerprints := make([]string, 0, len(removed))
	for _, key := range removed {
		fingerprints = append(fingerprints, ssh.FingerprintSHA256(key))
	}
	return path, fingerprints, nil
}

// forgetKnownHost removes every line recording alias and returns the keys
// those lines held.
func forgetKnownHost(path, alias string) (removed []ssh.PublicKey, err error) {
	lock, err := lockKnownHosts(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := lock.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("releasing host key lock for %s: %w", path, closeErr)
		}
	}()
	return rewriteKnownHosts(path, alias, nil)
}

// replaceKnownHost records key for alias in place of whatever was recorded,
// as one locked rewrite so a concurrent enrollment cannot interleave.
func replaceKnownHost(path, alias string, key ssh.PublicKey) (err error) {
	lock, err := lockKnownHosts(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := lock.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("releasing host key lock for %s: %w", path, closeErr)
		}
	}()
	_, err = rewriteKnownHosts(path, alias, key)
	return err
}

// rewriteKnownHosts drops alias's lines, appends key when given, and lands the
// result through a rename rather than truncating in place: a crash mid-write
// would otherwise leave an empty store, and an empty store re-enrolls every
// pod on next contact. every other byte of the file is kept as it was.
func rewriteKnownHosts(path, alias string, key ssh.PublicKey) ([]ssh.PublicKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	var removed []ssh.PublicKey
	changed := false
	lines := strings.Split(string(raw), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !knownHostsLineNames(line, alias) {
			kept = append(kept, line)
			continue
		}
		changed = true
		if key, ok := parseKnownHostsKey(line); ok {
			removed = append(removed, key)
		}
		// a line that also names other hosts keeps them: forgetting this pod
		// must not drop trust for anything else.
		if rest, ok := knownHostsLineWithout(line, alias); ok {
			kept = append(kept, rest)
		}
	}
	if !changed && key == nil {
		return nil, nil
	}

	// a store the user made read-only refuses enrollment, so it must refuse
	// this too; the rename below would otherwise bypass its mode. checked only
	// once there is something to write, so forgetting an unrecorded pod stays a
	// no-op on a read-only store.
	probe, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	if err := probe.Close(); err != nil {
		return nil, err
	}

	content := strings.Join(kept, "\n")
	if key != nil {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += knownhosts.Line([]string{alias}, key) + "\n"
	}
	if err := writeKnownHostsAtomically(path, content, info.Mode().Perm()); err != nil {
		return nil, err
	}
	return removed, nil
}

// knownHostsLineNames reports whether a known_hosts line records alias: its
// host field, after any @marker, lists alias as one of its comma-separated
// patterns. hashed and wildcard patterns are not matched; neither client
// writes them, and the lookup only ever asks about the plain alias.
func knownHostsLineNames(line, alias string) bool {
	fields := knownHostsFields(line)
	if len(fields) < 3 {
		return false
	}
	return slices.Contains(strings.Split(fields[0], ","), alias)
}

// knownHostsLineWithout returns line with alias dropped from its host field,
// and false when alias was the only host it named.
func knownHostsLineWithout(line, alias string) (string, bool) {
	fields := strings.Fields(line)
	hostsAt := 0
	if len(fields) > 0 && strings.HasPrefix(fields[0], "@") {
		hostsAt = 1
	}
	hosts := slices.DeleteFunc(strings.Split(fields[hostsAt], ","), func(h string) bool { return h == alias })
	if len(hosts) == 0 {
		return "", false
	}
	fields[hostsAt] = strings.Join(hosts, ",")
	return strings.Join(fields, " "), true
}

// knownHostsFields splits a line into hosts, key type, key and comment,
// dropping a leading @cert-authority or @revoked marker.
func knownHostsFields(line string) []string {
	fields := strings.Fields(line)
	if len(fields) > 0 && strings.HasPrefix(fields[0], "@") {
		fields = fields[1:]
	}
	return fields
}

func parseKnownHostsKey(line string) (ssh.PublicKey, bool) {
	fields := knownHostsFields(line)
	if len(fields) < 3 {
		return nil, false
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.Join(fields[1:], " ")))
	return key, err == nil
}

func writeKnownHostsAtomically(path, content string, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".known_hosts.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	discard := func(err error) error {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		return discard(err)
	}
	if _, err := tmp.WriteString(content); err != nil {
		return discard(err)
	}
	if err := tmp.Sync(); err != nil {
		return discard(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
