package project

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

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
)

// hostKeyAlias is the known_hosts key for a pod, shared by the go client and
// the openssh -o HostKeyAlias option so both maintain one entry per pod.
func hostKeyAlias(podID string) string {
	return hostKeyAliasPrefix + podID
}

// knownHostsPath returns the trust store, creating it if needed.
// knownhosts.New fails outright on a missing file, so it cannot be left to the
// first write.
func knownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}

	dir := filepath.Join(home, ".runpod", "ssh")
	if err := os.MkdirAll(dir, knownHostsDirPerm); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}

	path := filepath.Join(dir, "known_hosts")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, knownHostsFilePerm)
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("creating %s: %w", path, err)
	}
	return path, nil
}

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
	// knownhosts reads an unbracketed pattern as port 22 and then compares
	// ports exactly, so the lookup has to name that port for the bare alias
	// line openssh writes to match. verified against openssh 10.3: with
	// HostKeyAlias set it records the alias with no port bracket even on a
	// non-default port, which is also what knownhosts.Line produces.
	lookup := net.JoinHostPort(alias, "22")

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

		// never auto-heal. a mismatch is either a recreated pod or an
		// interception, and only the user can tell which.
		return fmt.Errorf("host key mismatch for pod %s: it offered %s, which is not the key recorded in %s. "+
			"if the pod was recreated, delete the %q line from that file and retry. "+
			"otherwise the connection is being intercepted: %w",
			podID, ssh.FingerprintSHA256(key), path, alias, err)
	}, nil
}

func lockKnownHosts(path string) (*os.File, error) {
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, knownHostsFilePerm)
	if err != nil {
		return nil, fmt.Errorf("opening host key lock for %s: %w", path, err)
	}
	if err := lockHostKeyFile(file); err != nil {
		file.Close()
		return nil, fmt.Errorf("locking host keys in %s: %w", path, err)
	}
	return file, nil
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
