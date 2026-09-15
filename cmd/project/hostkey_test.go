package project

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func newTestHostKey(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("building signer: %v", err)
	}
	return signer
}

// startTestSSHD runs an in-process ssh server that accepts any client and runs
// nothing. Host key verification happens during the handshake, before auth, so
// this is enough to exercise a real callback rather than calling it directly.
func startTestSSHD(t *testing.T, signer ssh.Signer) string {
	t.Helper()
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed by cleanup
			}
			go serveTestSSHD(conn, cfg)
		}
	}()
	return ln.Addr().String()
}

func serveTestSSHD(conn net.Conn, cfg *ssh.ServerConfig) {
	serverConn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		conn.Close()
		return
	}
	defer serverConn.Close()
	go ssh.DiscardRequests(reqs)
	for newChannel := range chans {
		ch, chReqs, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go func() {
			for r := range chReqs {
				if r.WantReply {
					r.Reply(true, nil) //nolint:errcheck // test server
				}
			}
		}()
		// report success so an openssh client exits cleanly rather than
		// complaining about the channel, which would muddy the assertions.
		ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0})) //nolint:errcheck
		ch.Close()
	}
}

func emptyKnownHosts(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("creating known hosts: %v", err)
	}
	return path
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	trimmed := strings.TrimRight(string(raw), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// dial exercises the callback the way PodSSHConnection does, through a real
// handshake, so the alias substitution and the alias:22 lookup form are both
// covered rather than assumed.
func dial(t *testing.T, addr, podID, knownHosts string) error {
	t.Helper()
	cb, err := podHostKeyCallback(podID, knownHosts)
	if err != nil {
		t.Fatalf("building callback: %v", err)
	}
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            "root",
		HostKeyCallback: cb,
		Timeout:         10 * time.Second,
	})
	if err == nil {
		client.Close()
	}
	return err
}

func TestPodHostKeyCallbackTrustsOnFirstUse(t *testing.T) {
	signer := newTestHostKey(t)
	addr := startTestSSHD(t, signer)
	knownHosts := emptyKnownHosts(t)

	if err := dial(t, addr, "pod-abc123", knownHosts); err != nil {
		t.Fatalf("first connection should be trusted: %v", err)
	}

	lines := readLines(t, knownHosts)
	if len(lines) != 1 {
		t.Fatalf("known_hosts has %d lines, want 1: %v", len(lines), lines)
	}
	// pinned format: this is what openssh writes for -o HostKeyAlias, verified
	// against openssh 10.3p1. if the two ever diverge they would maintain
	// separate trust inside one file, each accepting keys the other never saw.
	want := knownhosts.Line([]string{"runpod-pod-abc123"}, signer.PublicKey())
	if lines[0] != want {
		t.Errorf("entry =\n%q\nwant\n%q", lines[0], want)
	}
}

func TestPodHostKeyCallbackAcceptsKnownKey(t *testing.T) {
	signer := newTestHostKey(t)
	addr := startTestSSHD(t, signer)
	knownHosts := emptyKnownHosts(t)

	if err := dial(t, addr, "pod-abc123", knownHosts); err != nil {
		t.Fatalf("first connection: %v", err)
	}
	before := readLines(t, knownHosts)

	if err := dial(t, addr, "pod-abc123", knownHosts); err != nil {
		t.Fatalf("second connection with the same key: %v", err)
	}
	if after := readLines(t, knownHosts); len(after) != len(before) {
		t.Errorf("known_hosts grew to %d lines, want %d: %v", len(after), len(before), after)
	}
}

// TestPodHostKeyCallbackRefusesChangedKey is the whole point of the ticket: a
// substituted key must not be accepted, and must not be healed into the store.
func TestPodHostKeyCallbackRefusesChangedKey(t *testing.T) {
	trusted := newTestHostKey(t)
	knownHosts := emptyKnownHosts(t)

	firstAddr := startTestSSHD(t, trusted)
	if err := dial(t, firstAddr, "pod-abc123", knownHosts); err != nil {
		t.Fatalf("first connection: %v", err)
	}
	before := readLines(t, knownHosts)

	// same pod id, different host key: an interception, or a recreated pod.
	impostor := newTestHostKey(t)
	secondAddr := startTestSSHD(t, impostor)
	err := dial(t, secondAddr, "pod-abc123", knownHosts)
	if err == nil {
		t.Fatal("a changed host key was accepted")
	}
	for _, want := range []string{"pod-abc123", knownHosts, ssh.FingerprintSHA256(impostor.PublicKey())} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if after := readLines(t, knownHosts); len(after) != len(before) || after[0] != before[0] {
		t.Errorf("known_hosts was rewritten to %v, want %v left intact", after, before)
	}
}

func TestPodHostKeyCallbackSeparatesPods(t *testing.T) {
	signer := newTestHostKey(t)
	addr := startTestSSHD(t, signer)
	knownHosts := emptyKnownHosts(t)

	if err := dial(t, addr, "pod-one", knownHosts); err != nil {
		t.Fatalf("pod-one: %v", err)
	}
	// the same key under a different pod id is a different entry: trust is
	// per pod, so one pod's key never silently authorises another's.
	if err := dial(t, addr, "pod-two", knownHosts); err != nil {
		t.Fatalf("pod-two: %v", err)
	}
	if lines := readLines(t, knownHosts); len(lines) != 2 {
		t.Errorf("known_hosts has %d lines, want one per pod: %v", len(lines), lines)
	}
}

// TestAppendKnownHostRepairsMissingNewline guards a hand-edited file: appending
// to a file whose last line has no newline would splice the two entries
// together and corrupt both.
func TestAppendKnownHostRepairsMissingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	existing := "runpod-other ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatalf("seeding known hosts: %v", err)
	}

	signer := newTestHostKey(t)
	if err := appendKnownHost(path, "runpod-new", signer.PublicKey()); err != nil {
		t.Fatalf("appendKnownHost: %v", err)
	}

	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %v", len(lines), lines)
	}
	if lines[0] != existing {
		t.Errorf("existing entry became %q, want %q", lines[0], existing)
	}
	if want := knownhosts.Line([]string{"runpod-new"}, signer.PublicKey()); lines[1] != want {
		t.Errorf("appended entry = %q, want %q", lines[1], want)
	}
}

func TestKnownHostsPathCreatesPrivateStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	path, err := knownHostsPath()
	if err != nil {
		t.Fatalf("knownHostsPath: %v", err)
	}
	if want := filepath.Join(home, ".runpod", "ssh", "known_hosts"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if runtime.GOOS == "windows" {
		return
	}
	for p, want := range map[string]os.FileMode{
		filepath.Join(home, ".runpod", "ssh"): knownHostsDirPerm,
		path:                                  knownHostsFilePerm,
	} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %#o, want %#o", p, got, want)
		}
	}
}

func TestGetSshOptionsPinsHostKeyChecking(t *testing.T) {
	conn := &SSHConnection{
		podId:          "pod-abc123",
		podPort:        40022,
		sshKeyPath:     "/tmp/key",
		knownHostsPath: "/tmp/known_hosts",
	}
	opts := strings.Join(conn.getSshOptions(), " ")

	for _, want := range []string{
		"StrictHostKeyChecking=accept-new",
		"UserKnownHostsFile=/tmp/known_hosts",
		"HostKeyAlias=runpod-pod-abc123",
		"CheckHostIP=no",
		"HashKnownHosts=no",
	} {
		if !strings.Contains(opts, want) {
			t.Errorf("ssh options %q missing %q", opts, want)
		}
	}
	if strings.Contains(opts, "StrictHostKeyChecking=no") {
		t.Errorf("ssh options still disable host key checking: %q", opts)
	}
}

// writeClientKey puts a usable private key on disk so the openssh invocation
// below gets the real option list, -i included, rather than a filtered one.
func writeClientKey(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating client key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshalling client key: %v", err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("writing client key: %v", err)
	}
	return path
}

// TestOpenSSHWritesTheSameEntry is the interop guard. The go client and the
// openssh invocation rsync shells out to share one file, so if their entry
// formats ever diverge they would each accept keys the other had never seen,
// inside one file, with nothing to show it. Skipped where ssh is unavailable.
func TestOpenSSHWritesTheSameEntry(t *testing.T) {
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("no ssh binary available")
	}

	signer := newTestHostKey(t)
	addr := startTestSSHD(t, signer)
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("splitting %s: %v", addr, err)
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("parsing port %s: %v", port, err)
	}

	knownHosts := emptyKnownHosts(t)
	// the real connection shape, so getSshOptions goes to ssh unmodified.
	conn := &SSHConnection{
		podId:          "pod-abc123",
		podIp:          host,
		podPort:        portNum,
		sshKeyPath:     writeClientKey(t),
		knownHostsPath: knownHosts,
	}

	// -F /dev/null so a developer's ~/.ssh/config cannot change the outcome.
	// the exit status is ignored on purpose: the assertion is on the file, not
	// on the session, and the test server runs no command.
	args := append([]string{"-F", "/dev/null", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}, conn.getSshOptions()...)
	args = append(args, "root@"+host, "true")
	out, _ := exec.Command(sshBin, args...).CombinedOutput()

	lines := readLines(t, knownHosts)
	if len(lines) != 1 {
		t.Fatalf("openssh wrote %d lines, want 1: %v (ssh said: %s)", len(lines), lines, out)
	}
	want := knownhosts.Line([]string{"runpod-pod-abc123"}, signer.PublicKey())
	if lines[0] != want {
		t.Fatalf("openssh entry =\n%q\ngo client would write\n%q", lines[0], want)
	}

	// and the go client must accept the entry openssh just wrote, without
	// appending a second one.
	if err := dial(t, addr, "pod-abc123", knownHosts); err != nil {
		t.Errorf("go client rejected openssh's entry: %v", err)
	}
	if after := readLines(t, knownHosts); len(after) != 1 {
		t.Errorf("go client added a duplicate entry: %v", after)
	}
}
