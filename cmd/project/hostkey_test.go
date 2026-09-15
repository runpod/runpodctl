package project

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
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
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return
	}
	serverConn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
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
			defer ch.Close()
			for request := range chReqs {
				if request.WantReply {
					request.Reply(request.Type == "exec", nil) //nolint:errcheck
				}
				if request.Type == "exec" {
					ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0})) //nolint:errcheck
					return
				}
			}
		}()
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
	// HostKeyAlias is bare even on non-default ports; openssh must read this pin.
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

func TestPodHostKeyCallbackReloadsBeforeEnrollment(t *testing.T) {
	for _, sameKey := range []bool{true, false} {
		t.Run(fmt.Sprintf("same-key=%t", sameKey), func(t *testing.T) {
			checkRepeatedEnrollment(t, sameKey)
		})
	}
}

func checkRepeatedEnrollment(t *testing.T, sameKey bool) {
	t.Helper()
	path := emptyKnownHosts(t)
	trusted := newTestHostKey(t).PublicKey()
	offered := trusted
	if !sameKey {
		offered = newTestHostKey(t).PublicKey()
	}
	first, err := podHostKeyCallback("pod-one", path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := podHostKeyCallback("pod-one", path)
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40022}
	if err := first(remote.String(), remote, trusted); err != nil {
		t.Fatal(err)
	}
	err = second(remote.String(), remote, offered)
	if sameKey && err != nil {
		t.Fatalf("same key rejected: %v", err)
	}
	if !sameKey && (err == nil || !strings.Contains(err.Error(), "host key mismatch")) {
		t.Fatalf("wanted mismatch, got %v", err)
	}
	lines := readLines(t, path)
	if len(lines) != 1 || lines[0] != knownhosts.Line([]string{"runpod-pod-one"}, trusted) {
		t.Fatalf("trusted entry changed: %v", lines)
	}
}

func TestPodHostKeyCallbackConcurrentEnrollment(t *testing.T) {
	path := emptyKnownHosts(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	remote := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40022}
	for range 2 {
		callback, err := podHostKeyCallback("pod-one", path)
		if err != nil {
			t.Fatal(err)
		}
		key := newTestHostKey(t).PublicKey()
		go func() {
			<-start
			results <- callback(remote.String(), remote, key)
		}()
	}
	close(start)
	accepted := 0
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				accepted++
			} else if !strings.Contains(err.Error(), "host key mismatch") {
				t.Errorf("unexpected enrollment error: %v", err)
			}
		case <-deadline.C:
			t.Fatal("concurrent enrollment did not finish")
		}
	}
	if accepted != 1 || len(readLines(t, path)) != 1 {
		t.Fatalf("accepted %d conflicting keys; want exactly one trusted entry", accepted)
	}
}

func TestPodHostKeyCallbackRejectsMalformedStore(t *testing.T) {
	path := emptyKnownHosts(t)
	if err := os.WriteFile(path, []byte("invalid host key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := podHostKeyCallback("pod-one", path); err == nil {
		t.Fatal("malformed trust store was accepted")
	}
}

func TestPodHostKeyCallbackFailsClosedDuringEnrollment(t *testing.T) {
	for _, failure := range []string{"missing", "malformed", "unreadable", "unwritable", "lock unavailable"} {
		t.Run(failure, func(t *testing.T) {
			checkEnrollmentFailure(t, failure)
		})
	}
}

func checkEnrollmentFailure(t *testing.T, failure string) {
	t.Helper()
	path := emptyKnownHosts(t)
	callback, err := podHostKeyCallback("pod-one", path)
	if err != nil {
		t.Fatal(err)
	}
	expected := changeTestTrustStore(t, path, failure)
	wantError := "reading "
	if failure == "unwritable" {
		wantError = "recording host key"
	} else if failure == "lock unavailable" {
		wantError = "opening host key lock"
	}
	remote := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40022}
	err = callback(remote.String(), remote, newTestHostKey(t).PublicKey())
	if err == nil || !strings.Contains(err.Error(), wantError) {
		t.Fatalf("wanted %q failure, got %v", wantError, err)
	}
	assertTestTrustStore(t, path, failure, expected)
}

func chmodTestTrustStore(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires posix permissions and a non-root user")
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })
}

func changeTestTrustStore(t *testing.T, path, condition string) string {
	t.Helper()
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	switch condition {
	case "missing":
		err = os.Remove(path)
	case "malformed":
		expected = []byte("runpod-pod-abc123 invalid key\n")
		err = os.WriteFile(path, expected, 0o600)
	case "unreadable":
		chmodTestTrustStore(t, path, 0)
	case "unwritable", "read-only":
		chmodTestTrustStore(t, path, 0o400)
	case "lock unavailable":
		err = os.Mkdir(path+".lock", 0o700)
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(expected)
}

func assertTestTrustStore(t *testing.T, path, condition, expected string) {
	t.Helper()
	if condition == "missing" {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("missing trust store was recreated: %v", err)
		}
		return
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != expected {
		t.Fatalf("trust store changed: %q (%v)", data, err)
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
		"StrictHostKeyChecking=yes",
		"UserKnownHostsFile=/tmp/known_hosts",
		"HostKeyAlias=runpod-pod-abc123",
		"CheckHostIP=no",
		"UpdateHostKeys=no",
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

func runTestOpenSSH(t *testing.T, conn *SSHConnection) ([]byte, error) {
	t.Helper()
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("no ssh binary available")
	}
	args := append([]string{"-F", os.DevNull, "-o", "GlobalKnownHostsFile=none", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}, conn.getSshOptions()...)
	args = append(args, "root@"+conn.podIp, "true")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, sshBin, args...).CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("openssh timed out: %s", out)
	}
	return out, err
}

func TestOpenSSHRequiresGoTrustedKey(t *testing.T) {
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

	if out, err := runTestOpenSSH(t, conn); err == nil || !strings.Contains(string(out), "Host key verification failed") {
		t.Fatalf("openssh must reject an unknown key: %v (%s)", err, out)
	}
	if lines := readLines(t, knownHosts); len(lines) != 0 {
		t.Fatalf("openssh enrolled an unknown key: %v", lines)
	}
	if err := dial(t, addr, "pod-abc123", knownHosts); err != nil {
		t.Fatalf("go enrollment: %v", err)
	}
	if out, err := runTestOpenSSH(t, conn); err != nil {
		t.Fatalf("openssh rejected go's pin: %v (%s)", err, out)
	}

	lines := readLines(t, knownHosts)
	if len(lines) != 1 {
		t.Fatalf("known_hosts has %d lines, want 1: %v", len(lines), lines)
	}
	want := knownhosts.Line([]string{"runpod-pod-abc123"}, signer.PublicKey())
	if lines[0] != want {
		t.Fatalf("trusted entry = %q, want %q", lines[0], want)
	}

	if err := dial(t, addr, "pod-abc123", knownHosts); err != nil {
		t.Errorf("go client rejected the shared pin: %v", err)
	}
	if after := readLines(t, knownHosts); len(after) != 1 {
		t.Errorf("go client added a duplicate entry: %v", after)
	}

	for _, condition := range []string{"different pod", "changed key", "missing", "malformed", "unreadable", "read-only"} {
		t.Run(condition, func(t *testing.T) {
			checkOpenSSHTrust(t, conn, addr, condition)
		})
	}
}

func setTestSSHAddress(t *testing.T, conn *SSHConnection, addr string) {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	conn.podIp = host
	conn.podPort, err = strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
}

func checkOpenSSHTrust(t *testing.T, conn *SSHConnection, addr, condition string) {
	t.Helper()
	connection := *conn
	connection.knownHostsPath = emptyKnownHosts(t)
	if err := dial(t, addr, connection.podId, connection.knownHostsPath); err != nil {
		t.Fatal(err)
	}
	path := connection.knownHostsPath
	expected := changeTestTrustStore(t, path, condition)
	switch condition {
	case "different pod":
		connection.podId = "pod-other"
	case "changed key":
		setTestSSHAddress(t, &connection, startTestSSHD(t, newTestHostKey(t)))
	}
	out, err := runTestOpenSSH(t, &connection)
	if condition == "read-only" {
		if err != nil {
			t.Fatalf("read-only pin rejected: %v (%s)", err, out)
		}
	} else if err == nil || !strings.Contains(string(out), "Host key verification failed") {
		t.Fatalf("wanted host verification failure: %v (%s)", err, out)
	}
	if condition == "changed key" && !strings.Contains(string(out), "REMOTE HOST IDENTIFICATION HAS CHANGED") {
		t.Fatalf("missing changed-key diagnostic: %s", out)
	}
	assertTestTrustStore(t, path, condition, expected)
}
