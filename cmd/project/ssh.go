package project

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/runpod/runpodctl/api"
	sshpkg "github.com/runpod/runpodctl/cmd/ssh"
	"github.com/runpod/runpodctl/internal/waitfor"

	"github.com/fatih/color"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

const (
	pollInterval = 1 * time.Second
	maxPollTime  = 5 * time.Minute // Adjusted for clarity
)

func getPodSSHInfo(podID string) (string, int, error) {
	pods, err := api.GetPods()
	if err != nil {
		return "", 0, fmt.Errorf("getting pods: %w", err)
	}

	for _, pod := range pods {
		if pod.Id != podID {
			continue
		}

		if pod.DesiredStatus != "RUNNING" {
			return "", 0, fmt.Errorf("pod desired status not RUNNING")
		}
		if pod.Runtime == nil {
			return "", 0, fmt.Errorf("pod runtime is missing")
		}
		if pod.Runtime.Ports == nil {
			return "", 0, fmt.Errorf("pod runtime ports are missing")
		}
		for _, port := range pod.Runtime.Ports {
			if port.PrivatePort == 22 {
				return port.Ip, port.PublicPort, nil
			}
		}

	}
	return "", 0, fmt.Errorf("no SSH port exposed on pod %s", podID)
}

type SSHConnection struct {
	podId      string
	podIp      string
	podPort    int
	client     *ssh.Client
	sshKeyPath string
	// knownHostsPath is shared with the go client's host key callback, so a pod
	// trusted by one path is trusted by the other.
	knownHostsPath string
	// hostKeyAlgorithms are those of the key the go client verified. openssh
	// under strict checking refuses any other type, so it must not negotiate
	// one because the user's ssh config prefers it.
	hostKeyAlgorithms []string
}

func (sshConn *SSHConnection) getSshOptions() []string {
	opts := []string{
		// the go connection records the first key before rsync runs. require
		// that pin here so a missing or unreadable store cannot disable trust.
		"-o", "StrictHostKeyChecking=yes",
		// openssh splits this value on whitespace into several files, so a
		// path with a space has to be quoted for its config parser.
		"-o", "UserKnownHostsFile=" + quoteSSHConfigValue(sshConn.knownHostsPath),
		// key the entry on the pod rather than its address: runpod recycles pod
		// ssh addresses, and an address-keyed entry would report a mismatch for
		// users who did nothing wrong.
		"-o", "HostKeyAlias=" + hostKeyAlias(sshConn.podId),
		// CheckHostIP defaulted to yes before openssh 8.5, which would pin the
		// recycled ip alongside the alias and reintroduce that false mismatch.
		"-o", "CheckHostIP=no",
		"-o", "UpdateHostKeys=no",
		"-o", "LogLevel=ERROR",
		"-p", fmt.Sprint(sshConn.podPort),
		"-i", sshConn.sshKeyPath,
	}
	if len(sshConn.hostKeyAlgorithms) > 0 {
		opts = append(opts, "-o", "HostKeyAlgorithms="+strings.Join(sshConn.hostKeyAlgorithms, ","))
	}
	return opts
}

// quoteSSHConfigValue double-quotes a value holding whitespace, which is how
// openssh's config parser keeps it one token.
func quoteSSHConfigValue(value string) string {
	if !strings.ContainsAny(value, " \t") {
		return value
	}
	return `"` + value + `"`
}

// rsyncRemoteShell joins args into the command string for rsync's -e. rsync
// splits that string on spaces itself, keeping quoted runs together, and reads
// a doubled quote inside one as a literal quote (verified against macos's
// openrsync; the rsync manpage documents the same). so every argument holding
// a space or a quote is single-quoted.
func rsyncRemoteShell(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if strings.ContainsAny(arg, ` '"`) {
			arg = "'" + strings.ReplaceAll(arg, "'", "''") + "'"
		}
		quoted[i] = arg
	}
	return strings.Join(quoted, " ")
}

func (sshConn *SSHConnection) Rsync(localDir string, remoteDir string, quiet bool) error {
	rsyncCmdArgs := []string{"--compress", "--archive", "--verbose", "--no-owner", "--no-group"}

	// Retrieve and apply ignore patterns
	patterns, err := GetIgnoreList()
	if err != nil {
		return fmt.Errorf("getting ignore list: %w", err)
	}
	for _, pat := range patterns {
		rsyncCmdArgs = append(rsyncCmdArgs, "--exclude", pat)
	}

	// Filter from .runpodignore
	rsyncCmdArgs = append(rsyncCmdArgs, "--filter=:- .runpodignore")

	// Prepare SSH options for rsync
	sshOptions := rsyncRemoteShell(append([]string{"ssh"}, sshConn.getSshOptions()...))
	rsyncCmdArgs = append(rsyncCmdArgs, "-e", sshOptions, localDir, fmt.Sprintf("root@%s:%s", sshConn.podIp, remoteDir))

	// Perform a dry run to check if files need syncing
	dryRunArgs := append(rsyncCmdArgs, "--dry-run")
	dryRunCmd := exec.Command("rsync", dryRunArgs...)
	var dryRunBuf bytes.Buffer
	dryRunCmd.Stdout = &dryRunBuf
	dryRunCmd.Stderr = &dryRunBuf
	if err := dryRunCmd.Run(); err != nil {
		err = fmt.Errorf("running rsync dry run: %w", err)
		// the output holds openssh's own diagnostic, such as a refused or
		// changed host key, which is the part of this failure a user can act on.
		output := strings.TrimSpace(dryRunBuf.String())
		if output != "" {
			err = fmt.Errorf("%w: %s", err, output)
		}
		return sshConn.explainRsyncFailure(err, output)
	}
	dryRunOutput := dryRunBuf.String()

	// Parse the dry run output to determine if files need syncing
	filesNeedSyncing := false
	scanner := bufio.NewScanner(strings.NewReader(dryRunOutput))
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" || strings.Contains(line, "sending incremental file list") || strings.Contains(line, "total size is") || strings.Contains(line, "bytes/sec") || strings.Contains(line, "building file list") {
			continue
		}

		filename := filepath.Base(line)
		if filename == "" || filename == "." || strings.HasSuffix(line, "/") {
			continue
		}

		filesNeedSyncing = true
		break
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanning dry run output: %w", err)
	}

	// Add quiet flag if requested
	if quiet {
		rsyncCmdArgs = append(rsyncCmdArgs, "--quiet")
	}

	if filesNeedSyncing {
		fmt.Println("Syncing files...")

		cmd := exec.Command("rsync", rsyncCmdArgs...)
		cmd.Stdout = os.Stdout
		// stderr still streams to the user; the copy is read only on failure,
		// to tell a dropped connection from any other exit 255.
		var stderrBuf bytes.Buffer
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
		if err := cmd.Run(); err != nil {
			return sshConn.explainRsyncFailure(fmt.Errorf("executing rsync command: %w", err), stderrBuf.String())
		}
	}

	return nil
}

// explainRsyncFailure wraps err as a restarting pod when output shows the
// connection was refused or dropped, and returns it unchanged otherwise.
func (sshConn *SSHConnection) explainRsyncFailure(err error, output string) error {
	if rsyncConnectionLost(output) {
		return &podUnreachableError{podID: sshConn.podId, cause: err}
	}
	return err
}

// rsyncConnectionLost reports whether openssh's or rsync's output shows the
// connection to the pod being refused or dropped. ssh exits 255 for that and
// for a refused host key alike, so the status alone cannot tell them apart.
func rsyncConnectionLost(output string) bool {
	if strings.Contains(output, "Host key verification failed") {
		return false
	}
	for _, marker := range []string{
		"Connection refused",
		"actively refused",
		"Connection reset",
		"Connection closed by",
		"connection unexpectedly closed",
		"Broken pipe",
		"kex_exchange_identification",
	} {
		if strings.Contains(output, marker) {
			return true
		}
	}
	return false
}

// podUnreachableError explains a connection failure that is the normal result
// of connecting right after `pod start`. for up to about 30 seconds the api
// still reports the previous container's address (measured live), so the
// connection reaches a container that is shutting down, or nothing at all.
type podUnreachableError struct {
	podID string
	cause error
}

func (e *podUnreachableError) Error() string {
	return fmt.Sprintf("could not reach pod %s: it may still be restarting.\n"+
		"this is expected for up to ~30 seconds after a pod starts. try again shortly.\n"+
		"(%v)", e.podID, e.cause)
}

func (e *podUnreachableError) Unwrap() error { return e.cause }

// looksLikeUnreachablePod reports whether err is one of the failures a dial
// right after `pod start` produces: the previous container refusing or
// resetting the connection, or closing it during the handshake.
func looksLikeUnreachablePod(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	for _, dropped := range droppedConnectionErrors {
		if errors.Is(err, dropped) {
			return true
		}
	}
	return false
}

// hasChanges checks if there are any modified files in localDir since lastSyncTime.
func hasChanges(localDir string, lastSyncTime time.Time) (bool, string) {
	var firstModifiedFile string = ""

	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				// Handle the case where a file has been removed
				fmt.Printf("Detected a removed file at: %s\n", path)
				return errors.New("change detected") // Stop walking
			}
			return err
		}

		// Check if the file was modified after the last sync time
		if info.ModTime().After(lastSyncTime) {
			firstModifiedFile = path
			return filepath.SkipDir // Skip the rest of the directory if a change is found
		}

		return nil
	})
	if err != nil {
		fmt.Printf("Error walking through directory: %v\n", err)
		return false, ""
	}

	return firstModifiedFile != "", firstModifiedFile
}

func (sshConn *SSHConnection) SyncDir(localDir string, remoteDir string) {
	syncFiles := func() {
		// fmt.Println("Syncing files...")
		err := sshConn.Rsync(localDir, remoteDir, true)
		if err != nil {
			fmt.Printf(" error: %v\n", err)
			return
		}
	}

	// Start listening for events in a separate goroutine.
	go func() {
		lastSyncTime := time.Now()
		for {
			time.Sleep(100 * time.Millisecond)
			hasChanged, firstModifiedFile := hasChanges(localDir, lastSyncTime)
			if hasChanged {
				fmt.Printf("Local changes detected in %s\n", firstModifiedFile)
				syncFiles()
				lastSyncTime = time.Now()
			}
		}
	}()

	<-make(chan struct{})
}

// RunCommand runs a command on the remote pod.
func (conn *SSHConnection) RunCommand(command string) error {
	return conn.RunCommands([]string{command})
}

// RunCommands runs a list of commands on the remote pod.
func (sshConn *SSHConnection) RunCommands(commands []string) error {
	stdoutColor, stderrColor := color.New(color.FgGreen), color.New(color.FgRed)

	for _, command := range commands {
		session, err := sshConn.client.NewSession()
		if err != nil {
			return fmt.Errorf("failed to create SSH session: %w", err)
		}
		defer session.Close()

		// Set up pipes for stdout and stderr
		stdout, err := session.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to get stdout pipe: %w", err)
		}
		go scanAndPrint(stdout, stdoutColor, sshConn.podId, showPrefixInPodLogs)

		stderr, err := session.StderrPipe()
		if err != nil {
			return fmt.Errorf("failed to get stderr pipe: %w", err)
		}
		go scanAndPrint(stderr, stderrColor, sshConn.podId, showPrefixInPodLogs)

		// Run the command
		fullCommand := strings.Join([]string{
			"source /root/.bashrc",
			"source /etc/rp_environment",
			"while IFS= read -r -d '' line; do export \"$line\"; done < /proc/1/environ",
			command,
		}, " && ")

		if err := session.Run(fullCommand); err != nil {
			return fmt.Errorf("failed to run command %q: %w", command, err)
		}
	}
	return nil
}

// Utility function to scan and print output from SSH sessions.
func scanAndPrint(pipe io.Reader, color *color.Color, podID string, showPodIdPrefix bool) {
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		if showPodIdPrefix {
			color.Printf("[%s] ", podID)
		}
		fmt.Println(scanner.Text())
	}
}

// stdinIsTerminal reports whether stdin is a terminal we can prompt on, as
// opposed to a pipe or a redirected file.
var stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// promptInput and promptOutput are the host key prompt's ends. the prompt goes
// to stderr so legacy exec stdout stays exactly as it was.
var (
	promptInput  io.Reader = os.Stdin
	promptOutput io.Writer = os.Stderr
)

// dialPod connects to a pod, verifying its host key against the trust store.
// a changed key on a terminal is put to the user with both fingerprints; on
// yes the pod's recorded key is replaced with the offered one and the dial is
// made once more. a dropped connection is explained as a pod still restarting.
func dialPod(podID, addr string, auth []ssh.AuthMethod, hostKeys string) (*ssh.Client, error) {
	client, err := dialPodOnce(podID, addr, auth, hostKeys)
	var mismatch *hostKeyMismatchError
	if errors.As(err, &mismatch) && confirmHostKeyChange(mismatch) {
		if err := replaceKnownHost(hostKeys, hostKeyAlias(podID), mismatch.offered); err != nil {
			return nil, fmt.Errorf("recording the new host key for pod %s: %w", podID, err)
		}
		client, err = dialPodOnce(podID, addr, auth, hostKeys)
	}
	if err != nil && looksLikeUnreachablePod(err) {
		return nil, &podUnreachableError{podID: podID, cause: err}
	}
	return client, err
}

// dialPodOnce makes one verified connection attempt. the callback enrolls an
// unknown pod and refuses a changed key; the pinned algorithms keep the
// negotiated key type the recorded one.
func dialPodOnce(podID, addr string, auth []ssh.AuthMethod, hostKeys string) (*ssh.Client, error) {
	hostKeyCallback, err := podHostKeyCallback(podID, hostKeys)
	if err != nil {
		return nil, fmt.Errorf("preparing host key verification: %w", err)
	}
	hostKeyAlgorithms, err := pinnedHostKeyAlgorithms(podID, hostKeys)
	if err != nil {
		return nil, fmt.Errorf("preparing host key verification: %w", err)
	}
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:              "root",
		Auth:              auth,
		HostKeyCallback:   hostKeyCallback,
		HostKeyAlgorithms: hostKeyAlgorithms,
	})
	if err != nil {
		return nil, fmt.Errorf("establishing SSH connection to %s: %w", addr, err)
	}
	return client, nil
}

// confirmHostKeyChange asks whether to trust the key a pod now offers. it asks
// only on a terminal: a script, ci job or agent gets the mismatch error, whose
// remedy is `runpodctl ssh forget`, rather than a prompt nobody will answer,
// and stdin is left unread so queued input is not taken as consent.
func confirmHostKeyChange(mismatch *hostKeyMismatchError) bool {
	if !stdinIsTerminal() {
		return false
	}
	fmt.Fprintf(promptOutput, "host key mismatch for pod %s\n", mismatch.podID)
	for _, known := range mismatch.recorded {
		fmt.Fprintf(promptOutput, "  recorded: %s (%s)\n", ssh.FingerprintSHA256(known.Key), known.Key.Type())
	}
	fmt.Fprintf(promptOutput, "  offered:  %s (%s)\n", ssh.FingerprintSHA256(mismatch.offered), mismatch.offered.Type())
	fmt.Fprintln(promptOutput, "stopping and starting a pod, or updating it, usually gives it a new host key. if that did not happen, the connection may be intercepted.")
	fmt.Fprint(promptOutput, "trust the new key and continue? [y/N] ")

	scanner := bufio.NewScanner(promptInput)
	if !scanner.Scan() {
		fmt.Fprintln(promptOutput)
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}

func PodSSHConnection(podId string) (*SSHConnection, error) {
	sshKeyPath, err := sshpkg.ResolvePrivateKeyPath()
	if err != nil {
		return nil, fmt.Errorf("resolving ssh key path: %w", err)
	}

	privateKeyBytes, err := os.ReadFile(sshKeyPath)
	if err != nil {
		return nil, fmt.Errorf("reading private SSH key from %s: %w", sshKeyPath, err)
	}

	privateKey, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private SSH key: %w", err)
	}

	// loop until pod ready
	//
	// this stdout line is legacy behaviour (CON-816) and is left exactly as it
	// was; the shared wait loop below writes no progress of its own, so this
	// command's output does not change. The loop it replaced re-polled inside its
	// own condition and then second-guessed the result with a
	// `time.Since(start) >= maxPollTime` check that could report a timeout for a
	// poll that had just succeeded.
	fmt.Print("Waiting for Pod to come online... ")
	// look up ip and ssh port for pod id
	var podIp string
	var podPort int

	// the two failure messages below are the legacy ones, verbatim, and the poll
	// error is wrapped rather than flattened into a string so errors.Is/As still
	// reaches the typed no_credentials sentinel.
	var lastInfoErr error
	if _, err := waitfor.Until(context.Background(), func(context.Context) (waitfor.State, error) {
		ip, port, infoErr := getPodSSHInfo(podId)
		if infoErr != nil {
			lastInfoErr = infoErr
			return waitfor.State{Detail: infoErr.Error()}, nil
		}
		podIp, podPort = ip, port
		return waitfor.State{Ready: true}, nil
	}, waitfor.Options{
		Label:    fmt.Sprintf("pod %s to come online", podId),
		Timeout:  maxPollTime,
		Interval: pollInterval,
		Progress: nil,
	}); err != nil {
		if lastInfoErr != nil {
			return nil, fmt.Errorf("failed to get SSH info for pod %s: %w", podId, lastInfoErr)
		}
		return nil, fmt.Errorf("timeout waiting for pod %s to come online", podId)
	}

	hostKeys, err := knownHostsPath()
	if err != nil {
		return nil, fmt.Errorf("resolving known hosts file: %w", err)
	}
	host := fmt.Sprintf("%s:%d", podIp, podPort)
	client, err := dialPod(podId, host, []ssh.AuthMethod{ssh.PublicKeys(privateKey)}, hostKeys)
	if err != nil {
		return nil, err
	}

	// read after the dial, which recorded the key on first contact.
	verifiedAlgorithms, err := recordedHostKeyAlgorithms(podId, hostKeys)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("reading verified host key: %w", err)
	}

	return &SSHConnection{podId: podId, client: client, podIp: podIp, podPort: podPort, sshKeyPath: sshKeyPath, knownHostsPath: hostKeys, hostKeyAlgorithms: verifiedAlgorithms}, nil
}
