package project

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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

// newTestECDSAHostKey is the type go's defaults negotiate ahead of ed25519.
func newTestECDSAHostKey(t *testing.T) ssh.Signer {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
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
func startTestSSHD(t *testing.T, signers ...ssh.Signer) string {
	t.Helper()
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	for _, signer := range signers {
		cfg.AddHostKey(signer)
	}

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
	algos, err := pinnedHostKeyAlgorithms(podID, knownHosts)
	if err != nil {
		t.Fatalf("reading pinned algorithms: %v", err)
	}
	return dialWith(addr, cb, algos)
}

func dialWith(addr string, cb ssh.HostKeyCallback, algos []string) error {
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:              "root",
		HostKeyCallback:   cb,
		HostKeyAlgorithms: algos,
		Timeout:           10 * time.Second,
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
	// stop/start is the benign cause users hit, so the error has to name it,
	// and the remedy has to be a command, not a line to delete by hand.
	for _, want := range []string{"pod-abc123", knownHosts, ssh.FingerprintSHA256(impostor.PublicKey()), "stopping and starting a pod", "runpodctl ssh forget pod-abc123"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "delete") {
		t.Errorf("error %q still tells the user to edit the store by hand", err)
	}
	var mismatch *hostKeyMismatchError
	if !errors.As(err, &mismatch) || mismatch.offered.Type() != impostor.PublicKey().Type() || len(mismatch.recorded) != 1 {
		t.Errorf("error is not a typed mismatch carrying both keys: %#v", err)
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

// TestPodHostKeyCallbackGivesUpOnHeldLock guards against a stuck runpodctl
// holding the lock: the wait must end with an error that says why, enroll
// nothing, and succeed once the lock is free.
func TestPodHostKeyCallbackGivesUpOnHeldLock(t *testing.T) {
	path := emptyKnownHosts(t)
	held, err := lockKnownHosts(path)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	previous := hostKeyLockTimeout
	hostKeyLockTimeout = 200 * time.Millisecond
	t.Cleanup(func() { hostKeyLockTimeout = previous })

	callback, err := podHostKeyCallback("pod-one", path)
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40022}
	key := newTestHostKey(t).PublicKey()
	start := time.Now()
	err = callback(remote.String(), remote, key)
	if err == nil || !strings.Contains(err.Error(), "locked by another runpodctl process") {
		t.Fatalf("wanted a held-lock error, got %v", err)
	}
	if waited := time.Since(start); waited > 5*time.Second {
		t.Errorf("waited %s for a 200ms lock timeout", waited)
	}
	if lines := readLines(t, path); len(lines) != 0 {
		t.Fatalf("enrolled while another process held the lock: %v", lines)
	}

	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	if err := callback(remote.String(), remote, key); err != nil {
		t.Fatalf("enrollment after the lock was released: %v", err)
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

// TestReadOnlyStoreStillTrustsKnownPod: verifying a pod that is already trusted
// only reads the store, so a store the user made read-only must not block it.
// a new pod still cannot be recorded there, and must be refused rather than
// trusted without a pin.
func TestReadOnlyStoreStillTrustsKnownPod(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not make a file read-only for its owner on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	addr := startTestSSHD(t, newTestHostKey(t))
	path, err := knownHostsPath()
	if err != nil {
		t.Fatalf("knownHostsPath: %v", err)
	}
	if err := dial(t, addr, "pod-abc123", path); err != nil {
		t.Fatalf("first connection: %v", err)
	}

	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, knownHostsFilePerm) })

	if path, err = knownHostsPath(); err != nil {
		t.Fatalf("knownHostsPath on a read-only store: %v", err)
	}
	if err := dial(t, addr, "pod-abc123", path); err != nil {
		t.Fatalf("trusted pod with a read-only store: %v", err)
	}

	other := startTestSSHD(t, newTestHostKey(t))
	if err := dial(t, other, "pod-new", path); err == nil {
		t.Fatal("new pod accepted although its key could not be recorded")
	}
}

// TestPinnedHostKeyAlgorithms covers the lookup behind HostKeyAlgorithms:
// only this pod's recorded types lead, rsa expands to its sha-2 names, and the
// fallback adds plain-key algorithms without repeating or certificates.
func TestPinnedHostKeyAlgorithms(t *testing.T) {
	path := emptyKnownHosts(t)
	if algos, err := pinnedHostKeyAlgorithms("pod-one", path); err != nil || algos != nil {
		t.Fatalf("empty store: got %v, %v; want go's defaults (nil)", algos, err)
	}

	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaKey, err := ssh.NewPublicKey(&rsaPriv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendKnownHost(path, "runpod-pod-other", newTestHostKey(t).PublicKey()); err != nil {
		t.Fatal(err)
	}
	if err := appendKnownHost(path, "runpod-pod-one", rsaKey); err != nil {
		t.Fatal(err)
	}

	recorded, err := recordedHostKeyAlgorithms("pod-one", path)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}; !slices.Equal(recorded, want) {
		t.Errorf("recorded = %v, want %v (another pod's ed25519 must not leak in)", recorded, want)
	}

	algos, err := pinnedHostKeyAlgorithms("pod-one", path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(algos[:2], recorded) {
		t.Errorf("pinned = %v, want the recorded %v first", algos, recorded)
	}
	for i, algo := range algos {
		if strings.Contains(algo, "-cert-") || slices.Index(algos, algo) != i {
			t.Errorf("pinned %v has a certificate or repeated algorithm %q", algos, algo)
		}
	}
	if !slices.Contains(algos, ssh.KeyAlgoED25519) {
		t.Errorf("pinned %v has no fallback for a pod that changed key type", algos)
	}
}

// TestGoClientNegotiatesRecordedKeyType is the pin surviving a key type go
// does not prefer. the pod offers ecdsa and ed25519 like runpod's images do,
// and an ed25519 pin must hold even though go's defaults pick ecdsa.
func TestGoClientNegotiatesRecordedKeyType(t *testing.T) {
	ed := newTestHostKey(t)
	addr := startTestSSHD(t, newTestECDSAHostKey(t), ed)
	path := emptyKnownHosts(t)
	if err := appendKnownHost(path, "runpod-pod-one", ed.PublicKey()); err != nil {
		t.Fatal(err)
	}

	// control: without pinning, go picks ecdsa and calls a trusted pod intercepted.
	cb, err := podHostKeyCallback("pod-one", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := dialWith(addr, cb, nil); err == nil || !strings.Contains(err.Error(), "host key mismatch") {
		t.Fatalf("control dial with go's defaults: wanted a mismatch, got %v", err)
	}

	if err := dial(t, addr, "pod-one", path); err != nil {
		t.Fatalf("pinned dial rejected the recorded ed25519 key: %v", err)
	}
	if lines := readLines(t, path); len(lines) != 1 {
		t.Errorf("known_hosts has %d lines, want the one pin: %v", len(lines), lines)
	}

	// a pod that no longer offers the recorded type must still get the
	// mismatch error and its remedy, not a bare negotiation failure.
	ecdsaOnly := startTestSSHD(t, newTestECDSAHostKey(t))
	if err := dial(t, ecdsaOnly, "pod-one", path); err == nil || !strings.Contains(err.Error(), "host key mismatch") {
		t.Fatalf("pod without the recorded type: wanted a mismatch, got %v", err)
	}
}

func TestGetSshOptionsPinsHostKeyChecking(t *testing.T) {
	conn := &SSHConnection{
		podId:             "pod-abc123",
		podPort:           40022,
		sshKeyPath:        "/tmp/key",
		knownHostsPath:    "/tmp/known_hosts",
		hostKeyAlgorithms: []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256},
	}
	opts := strings.Join(conn.getSshOptions(), " ")

	for _, want := range []string{
		"StrictHostKeyChecking=yes",
		"UserKnownHostsFile=/tmp/known_hosts",
		"HostKeyAlias=runpod-pod-abc123",
		"CheckHostIP=no",
		"UpdateHostKeys=no",
		"HostKeyAlgorithms=rsa-sha2-512,rsa-sha2-256",
	} {
		if !strings.Contains(opts, want) {
			t.Errorf("ssh options %q missing %q", opts, want)
		}
	}
	if strings.Contains(opts, "StrictHostKeyChecking=no") {
		t.Errorf("ssh options still disable host key checking: %q", opts)
	}
}

// writeClientKey puts a usable private key in dir so the openssh invocation
// below gets the real option list, -i included, rather than a filtered one.
func writeClientKey(t *testing.T, dir string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating client key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshalling client key: %v", err)
	}
	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("writing client key: %v", err)
	}
	return path
}

func runTestOpenSSH(t *testing.T, conn *SSHConnection) ([]byte, error) {
	t.Helper()
	return runTestOpenSSHWithConfig(t, conn, os.DevNull)
}

// runTestOpenSSHWithConfig stands in for the user's ~/.ssh/config, which
// rsync's ssh reads and command-line options override.
func runTestOpenSSHWithConfig(t *testing.T, conn *SSHConnection, config string) ([]byte, error) {
	t.Helper()
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("no ssh binary available")
	}
	args := append([]string{"-F", config, "-o", "GlobalKnownHostsFile=none", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}, conn.getSshOptions()...)
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
		sshKeyPath:     writeClientKey(t, t.TempDir()),
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

// TestOpenSSHNegotiatesVerifiedKeyType covers a user ssh config preferring a
// type go did not record. openssh under strict checking refuses an unknown
// type, so without the HostKeyAlgorithms option rsync would fail on a pod go
// had just verified.
func TestOpenSSHNegotiatesVerifiedKeyType(t *testing.T) {
	addr := startTestSSHD(t, newTestECDSAHostKey(t), newTestHostKey(t))
	knownHosts := emptyKnownHosts(t)
	if err := dial(t, addr, "pod-abc123", knownHosts); err != nil {
		t.Fatalf("go enrollment: %v", err)
	}
	verified, err := recordedHostKeyAlgorithms("pod-abc123", knownHosts)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(verified, []string{ssh.KeyAlgoECDSA256}) {
		t.Fatalf("go recorded %v; this test needs go to prefer ecdsa over the config's ed25519", verified)
	}

	userConfig := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(userConfig, []byte("HostKeyAlgorithms ssh-ed25519\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := &SSHConnection{
		podId:          "pod-abc123",
		sshKeyPath:     writeClientKey(t, t.TempDir()),
		knownHostsPath: knownHosts,
	}
	setTestSSHAddress(t, conn, addr)

	// control: the user's preference alone reaches a type openssh cannot verify.
	if out, err := runTestOpenSSHWithConfig(t, conn, userConfig); err == nil {
		t.Fatalf("control: openssh accepted an unrecorded key type (%s)", out)
	}

	conn.hostKeyAlgorithms = verified
	if out, err := runTestOpenSSHWithConfig(t, conn, userConfig); err != nil {
		t.Fatalf("openssh did not negotiate the verified type: %v (%s)", err, out)
	}
	if lines := readLines(t, knownHosts); len(lines) != 1 {
		t.Errorf("known_hosts has %d lines, want the one pin: %v", len(lines), lines)
	}
}

// TestRsyncHandlesSpacedPathsAndReportsRefusal runs the real rsync and openssh
// binaries with a key and store under a directory holding a space and a quote.
// both have to reach openssh as one argument each through rsync's -e split and
// openssh's config parser, and openssh's refusal has to reach the caller.
func TestRsyncHandlesSpacedPathsAndReportsRefusal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a posix rsync and openssh")
	}
	for _, bin := range []string{"rsync", "ssh"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("no %s binary available", bin)
		}
	}
	addr := startTestSSHD(t, newTestHostKey(t))
	spaced := filepath.Join(t.TempDir(), "o'brien docs")
	if err := os.Mkdir(spaced, 0o700); err != nil {
		t.Fatal(err)
	}
	knownHosts := filepath.Join(spaced, "known_hosts")
	if err := os.WriteFile(knownHosts, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	conn := &SSHConnection{
		podId:          "pod-abc123",
		sshKeyPath:     writeClientKey(t, spaced),
		knownHostsPath: knownHosts,
	}
	setTestSSHAddress(t, conn, addr)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "main.py"), []byte("print(1)\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// nothing is enrolled, so openssh must get as far as verifying the key and
	// refuse. a mis-split path fails earlier, naming a fragment as the host.
	err := conn.Rsync(src, "/tmp/dst", true)
	if err == nil || !strings.Contains(err.Error(), "Host key verification failed") {
		t.Fatalf("wanted openssh's refusal in the error, got %v", err)
	}
	// ssh exits 255 for a refused key and a dropped connection alike; only the
	// latter may be explained as a restart.
	if strings.Contains(err.Error(), "restarting") {
		t.Fatalf("a refused host key was explained as a restarting pod: %v", err)
	}

	// once go has enrolled the pod, openssh passes verification. the test
	// sshd runs no rsync, so the transfer itself still fails, but not on the key.
	if err := dial(t, addr, "pod-abc123", knownHosts); err != nil {
		t.Fatalf("go enrollment: %v", err)
	}
	err = conn.Rsync(src, "/tmp/dst", true)
	if err == nil || strings.Contains(err.Error(), "Host key verification failed") || strings.Contains(err.Error(), "Could not resolve") {
		t.Fatalf("wanted a transfer failure past host key verification, got %v", err)
	}
}

func TestRsyncRemoteShellQuotesArguments(t *testing.T) {
	got := rsyncRemoteShell([]string{"ssh", "-i", "/home/o'brien/my key", "-o", `UserKnownHostsFile="/a b/kh"`, "-p", "22"})
	want := `ssh -i '/home/o''brien/my key' -o 'UserKnownHostsFile="/a b/kh"' -p 22`
	if got != want {
		t.Errorf("rsyncRemoteShell =\n%s\nwant\n%s", got, want)
	}
}

// withHostKeyPrompt routes the re-trust prompt through buffers so the terminal
// and non-terminal paths can both be driven without a tty.
func withHostKeyPrompt(t *testing.T, terminal bool, answer string) *strings.Builder {
	t.Helper()
	prevTerminal, prevIn, prevOut := stdinIsTerminal, promptInput, promptOutput
	t.Cleanup(func() { stdinIsTerminal, promptInput, promptOutput = prevTerminal, prevIn, prevOut })
	out := &strings.Builder{}
	stdinIsTerminal = func() bool { return terminal }
	promptInput = strings.NewReader(answer)
	promptOutput = out
	return out
}

// enrollThenChangeKey trusts one key for pod-abc123, then stands up a pod
// offering another under the same id: the shape of a stop/start.
func enrollThenChangeKey(t *testing.T) (knownHosts, addr string, trusted, changed ssh.Signer) {
	t.Helper()
	trusted = newTestHostKey(t)
	knownHosts = emptyKnownHosts(t)
	if err := dial(t, startTestSSHD(t, trusted), "pod-abc123", knownHosts); err != nil {
		t.Fatalf("first connection: %v", err)
	}
	changed = newTestHostKey(t)
	return knownHosts, startTestSSHD(t, changed), trusted, changed
}

func dialPodForTest(addr, podID, knownHosts string) error {
	client, err := dialPod(podID, addr, nil, knownHosts)
	if err == nil {
		client.Close()
	}
	return err
}

func assertSoleEntry(t *testing.T, knownHosts, alias string, key ssh.PublicKey) {
	t.Helper()
	lines := readLines(t, knownHosts)
	if want := knownhosts.Line([]string{alias}, key); len(lines) != 1 || lines[0] != want {
		t.Fatalf("known_hosts = %v, want only %q", lines, want)
	}
}

// TestDialPodRetrustsAfterConfirmation is the terminal flow after a stop/start:
// both fingerprints are shown, a yes replaces only that pod's entry with the
// key that was shown, and the same run goes on to connect.
func TestDialPodRetrustsAfterConfirmation(t *testing.T) {
	knownHosts, addr, trusted, changed := enrollThenChangeKey(t)
	prompt := withHostKeyPrompt(t, true, "y\n")

	if err := dialPodForTest(addr, "pod-abc123", knownHosts); err != nil {
		t.Fatalf("confirmed new key was refused: %v", err)
	}
	for _, want := range []string{
		"pod-abc123",
		ssh.FingerprintSHA256(trusted.PublicKey()),
		ssh.FingerprintSHA256(changed.PublicKey()),
		"stopping and starting",
		"[y/N]",
	} {
		if !strings.Contains(prompt.String(), want) {
			t.Errorf("prompt %q does not show %q", prompt.String(), want)
		}
	}
	assertSoleEntry(t, knownHosts, "runpod-pod-abc123", changed.PublicKey())

	// the replaced pin holds: the next connection verifies without asking.
	prompt = withHostKeyPrompt(t, true, "")
	if err := dialPodForTest(addr, "pod-abc123", knownHosts); err != nil {
		t.Fatalf("connection after re-trust: %v", err)
	}
	if prompt.Len() != 0 {
		t.Errorf("a verified connection prompted: %q", prompt.String())
	}
}

func TestDialPodKeepsRefusalWithoutConsent(t *testing.T) {
	for _, answer := range []string{"n\n", "\n", "", "yes please\n", "Y es\n"} {
		t.Run(fmt.Sprintf("answer=%q", answer), func(t *testing.T) {
			knownHosts, addr, trusted, _ := enrollThenChangeKey(t)
			withHostKeyPrompt(t, true, answer)

			err := dialPodForTest(addr, "pod-abc123", knownHosts)
			var mismatch *hostKeyMismatchError
			if !errors.As(err, &mismatch) {
				t.Fatalf("wanted the mismatch error, got %v", err)
			}
			if strings.Contains(err.Error(), "restarting") {
				t.Errorf("a refused key was explained as a restart: %v", err)
			}
			assertSoleEntry(t, knownHosts, "runpod-pod-abc123", trusted.PublicKey())
		})
	}
}

// TestDialPodDoesNotPromptWithoutTerminal: a script, ci job or agent cannot
// answer, so it must get the error and its `ssh forget` remedy, and a yes
// waiting on a piped stdin must not be read as consent.
func TestDialPodDoesNotPromptWithoutTerminal(t *testing.T) {
	knownHosts, addr, trusted, _ := enrollThenChangeKey(t)
	prompt := withHostKeyPrompt(t, false, "y\n")

	err := dialPodForTest(addr, "pod-abc123", knownHosts)
	if err == nil || !strings.Contains(err.Error(), "runpodctl ssh forget pod-abc123") {
		t.Fatalf("wanted the mismatch error naming the forget command, got %v", err)
	}
	if prompt.Len() != 0 {
		t.Errorf("prompted without a terminal: %q", prompt.String())
	}
	assertSoleEntry(t, knownHosts, "runpod-pod-abc123", trusted.PublicKey())
}

// TestForgetKnownHostRemovesOnlyThatPod: every line for the pod goes,
// including a second key for it, and nothing else moves: not another pod,
// not a pod whose id merely starts the same way, not a comment or blank line.
func TestForgetKnownHostRemovesOnlyThatPod(t *testing.T) {
	path := emptyKnownHosts(t)
	first := newTestHostKey(t).PublicKey()
	second := newTestHostKey(t).PublicKey()
	other := newTestHostKey(t).PublicKey()
	similar := newTestHostKey(t).PublicKey()
	content := "# hand-written comment\n" +
		knownhosts.Line([]string{"runpod-pod-one"}, first) + "\n" +
		knownhosts.Line([]string{"runpod-pod-two"}, other) + "\n" +
		"\n" +
		knownhosts.Line([]string{"runpod-pod-one-dev"}, similar) + "\n" +
		knownhosts.Line([]string{"runpod-pod-one"}, second) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := forgetKnownHost(path, "runpod-pod-one")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, key := range removed {
		got = append(got, ssh.FingerprintSHA256(key))
	}
	if want := []string{ssh.FingerprintSHA256(first), ssh.FingerprintSHA256(second)}; !slices.Equal(got, want) {
		t.Errorf("removed %v, want %v", got, want)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# hand-written comment\n" +
		knownhosts.Line([]string{"runpod-pod-two"}, other) + "\n" +
		"\n" +
		knownhosts.Line([]string{"runpod-pod-one-dev"}, similar) + "\n"
	if string(raw) != want {
		t.Errorf("known_hosts =\n%q\nwant\n%q", raw, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != knownHostsFilePerm {
			t.Errorf("rewritten store mode = %#o, want %#o", got, knownHostsFilePerm)
		}
	}

	// forgetting again finds nothing and leaves the file alone.
	removed, err = forgetKnownHost(path, "runpod-pod-one")
	if err != nil || len(removed) != 0 {
		t.Fatalf("second forget removed %v (%v)", removed, err)
	}
	if again, err := os.ReadFile(path); err != nil || string(again) != want {
		t.Errorf("a no-op forget rewrote the store: %q (%v)", again, err)
	}
}

func TestForgetHostKeyWithoutStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	path, fingerprints, err := ForgetHostKey("pod-one")
	if err != nil || len(fingerprints) != 0 {
		t.Fatalf("ForgetHostKey on a missing store: %v, %v", fingerprints, err)
	}
	if want := filepath.Join(home, ".runpod", "ssh", "known_hosts"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("forget created the store: %v", err)
	}
}

// TestForgetHostKeyThenReenroll is the scripted recovery end to end: forget
// reports the removed fingerprint, and the next connection trusts the pod's
// current key on first use again.
func TestForgetHostKeyThenReenroll(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	signer := newTestHostKey(t)
	addr := startTestSSHD(t, signer)
	path, err := knownHostsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := dial(t, addr, "pod-one", path); err != nil {
		t.Fatalf("enrollment: %v", err)
	}

	_, fingerprints, err := ForgetHostKey("pod-one")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{ssh.FingerprintSHA256(signer.PublicKey())}; !slices.Equal(fingerprints, want) {
		t.Errorf("fingerprints = %v, want %v", fingerprints, want)
	}
	if lines := readLines(t, path); len(lines) != 0 {
		t.Fatalf("known_hosts still has %v", lines)
	}

	changed := newTestHostKey(t)
	if err := dial(t, startTestSSHD(t, changed), "pod-one", path); err != nil {
		t.Fatalf("re-enrollment after forget: %v", err)
	}
	assertSoleEntry(t, path, "runpod-pod-one", changed.PublicKey())
}

// TestForgetKnownHostRefusesReadOnlyStore: a store the user made read-only
// refuses enrollment, so it must refuse forgetting too, and the rename that
// rewrites the file must not get around its mode.
func TestForgetKnownHostRefusesReadOnlyStore(t *testing.T) {
	path := emptyKnownHosts(t)
	if err := appendKnownHost(path, "runpod-pod-one", newTestHostKey(t).PublicKey()); err != nil {
		t.Fatal(err)
	}
	expected := changeTestTrustStore(t, path, "read-only")

	if _, err := forgetKnownHost(path, "runpod-pod-one"); err == nil {
		t.Fatal("a read-only store was rewritten")
	}
	assertTestTrustStore(t, path, "read-only", expected)
}

// closedPort returns an address nothing listens on.
func closedPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// hangupListener accepts connections and closes them at once, as a
// container's sshd does while it shuts down.
func hangupListener(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	return ln.Addr().String()
}

// TestDialPodExplainsRestartingPod covers the window after `pod start` in
// which the api still reports the previous container's address: the
// connection is refused, or accepted and dropped before the handshake. both
// must say the pod may be restarting, keep the original error, and enroll
// nothing.
func TestDialPodExplainsRestartingPod(t *testing.T) {
	for name, addr := range map[string]string{
		"connection refused": closedPort(t),
		"handshake hangup":   hangupListener(t),
	} {
		t.Run(name, func(t *testing.T) {
			knownHosts := emptyKnownHosts(t)
			err := dialPodForTest(addr, "pod-abc123", knownHosts)
			var unreachable *podUnreachableError
			if !errors.As(err, &unreachable) {
				t.Fatalf("wanted the restarting explanation, got %v", err)
			}
			for _, want := range []string{"pod-abc123", "may still be restarting", "~30 seconds", addr} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
			if lines := readLines(t, knownHosts); len(lines) != 0 {
				t.Errorf("enrolled a key from a dead connection: %v", lines)
			}
		})
	}
}

func TestRsyncConnectionLost(t *testing.T) {
	for output, want := range map[string]bool{
		"ssh: connect to host 1.2.3.4 port 40022: Connection refused\nrsync: connection unexpectedly closed (0 bytes received so far) [sender]": true,
		"kex_exchange_identification: read: Connection reset by peer":                                                                           true,
		"Connection closed by 1.2.3.4 port 40022":                                                                                               true,
		"client_loop: send disconnect: Broken pipe":                                                                                             true,
		"Host key verification failed.\nrsync: connection unexpectedly closed (0 bytes received so far) [sender]":                               false,
		"root@1.2.3.4: Permission denied (publickey).":                                                                                          false,
		"": false,
	} {
		if got := rsyncConnectionLost(output); got != want {
			t.Errorf("rsyncConnectionLost(%q) = %t, want %t", output, got, want)
		}
	}
}

// TestRsyncExplainsRestartingPod runs the real rsync and openssh against a
// closed port, which is what the api's stale address becomes once the old
// container is gone, and expects the restart explanation around rsync's 255.
func TestRsyncExplainsRestartingPod(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a posix rsync and openssh")
	}
	for _, bin := range []string{"rsync", "ssh"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("no %s binary available", bin)
		}
	}
	conn := &SSHConnection{
		podId:          "pod-abc123",
		sshKeyPath:     writeClientKey(t, t.TempDir()),
		knownHostsPath: emptyKnownHosts(t),
	}
	setTestSSHAddress(t, conn, closedPort(t))
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "main.py"), []byte("print(1)\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := conn.Rsync(src, "/tmp/dst", true)
	var unreachable *podUnreachableError
	if !errors.As(err, &unreachable) {
		t.Fatalf("wanted the restarting explanation, got %v", err)
	}
	for _, want := range []string{"pod-abc123", "may still be restarting", "Connection refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
