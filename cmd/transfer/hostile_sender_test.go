package transfer

import (
	"archive/zip"
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/schollz/croc/v9/src/tcp"
	"github.com/schollz/croc/v9/src/utils"
)

// These tests run a complete transfer over croc's own relay on loopback, with
// a sender whose manifest is built by hand. Send takes the manifest directly,
// so FolderRemote, Name and Symlink are all injectable: this is a hostile
// sender, not a simulation of one.
//
// Every assertion has two halves, and the second is the load-bearing one:
// "nothing escaped" is satisfied by os.Root even with the validator's checks
// removed, and the transfer then reports success while writing nothing. So the
// refusal is asserted too. Verified by removing the traversal guard and
// confirming these fail for that reason.

var (
	relayOnce   sync.Once
	relayPorts  []string
	roomCounter atomic.Int64
)

// startTestRelay runs croc's relay in-process. tcp.Run never returns, so the
// goroutines live for the test binary; there is no shutdown to call. It is
// started once and shared, which is why rooms have to be kept apart below.
func startTestRelay(t *testing.T) []string {
	t.Helper()
	relayOnce.Do(func() {
		first := 40000 + rand.Intn(20000) //nolint:gosec // test port selection
		open := utils.FindOpenPorts("localhost", first, 4)
		if len(open) < 4 {
			t.Fatalf("only found %d open ports", len(open))
		}
		for _, p := range open {
			relayPorts = append(relayPorts, strconv.Itoa(p))
		}
		// "localhost" rather than 127.0.0.1: tcp.Run rewrites the latter to
		// 0.0.0.0, which would expose the relay off the machine.
		go tcp.Run("error", "localhost", relayPorts[0], "testpw", strings.Join(relayPorts[1:], ",")) //nolint:errcheck
		for _, p := range relayPorts[1:] {
			go tcp.Run("error", "localhost", p, "testpw") //nolint:errcheck
		}
		for range 100 {
			if err := tcp.PingServer("localhost:" + relayPorts[0]); err == nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("test relay never came up")
	})
	return relayPorts
}

func testOptions(t *testing.T, isSender bool, secret, dest string) Options {
	return Options{
		Curve:         "p256",
		HashAlgorithm: "xxhash",
		IsSender:      isSender,
		NoPrompt:      true,
		Overwrite:     true,
		DisableLocal:  true,
		RelayAddress:  "localhost:" + startTestRelay(t)[0],
		RelayPassword: "testpw",
		RelayPorts:    startTestRelay(t),
		SharedSecret:  secret,
		Destination:   dest,
	}
}

// quietStderr keeps croc's progress output off the test log unless the test
// fails, in which case it is worth reading.
func quietStderr(t *testing.T) {
	t.Helper()
	captured, err := os.CreateTemp(t.TempDir(), "stderr-*")
	if err != nil {
		t.Fatalf("creating capture file: %v", err)
	}
	saved := os.Stderr
	os.Stderr = captured
	t.Cleanup(func() {
		os.Stderr = saved
		captured.Close()
		if !t.Failed() {
			return
		}
		if out, err := os.ReadFile(captured.Name()); err == nil && len(out) > 0 {
			t.Logf("transfer output:\n%s", out)
		}
	})
}

// runPair drives one sender and one receiver to completion.
func runPair(t *testing.T, files, folders []FileInfo, dest string) (sendErr, recvErr error) {
	t.Helper()
	// croc derives the relay room from SharedSecret[:3], so each pair needs a
	// distinct three-character prefix or they share a room and every transfer
	// after the first reports "room not ready".
	secret := fmt.Sprintf("%03d4-runpodctl-test", roomCounter.Add(1)%1000)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		sender, err := New(testOptions(t, true, secret, ""))
		if err != nil {
			sendErr = err
			return
		}
		sendErr = sender.Send(files, folders, len(folders))
	}()
	go func() {
		defer wg.Done()
		// the sender creates the room; joining too early is a "room not ready".
		time.Sleep(300 * time.Millisecond)
		receiver, err := New(testOptions(t, false, secret, dest))
		if err != nil {
			recvErr = err
			return
		}
		recvErr = receiver.Receive()
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("transfer did not finish")
	}
	return sendErr, recvErr
}

// sendable puts a real file in the sender's source directory and returns the
// manifest entry for it.
//
// The file has to exist: sendCollectFiles hashes every entry before announcing
// the manifest, so naming one that is not there kills the sender and the
// receiver never sees the manifest at all. A test written that way passes
// against unfixed code, because nothing was ever transferred.
func sendable(t *testing.T, src, name, content string) FileInfo {
	t.Helper()
	full := filepath.Join(src, name)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	hash, err := utils.HashFile(full, "xxhash")
	if err != nil {
		t.Fatalf("hashing %s: %v", name, err)
	}
	return FileInfo{
		Name: name, FolderRemote: "./", FolderSource: src,
		Size: int64(len(content)), Hash: hash, ModTime: time.Now(),
	}
}

// hostileDirs returns a source directory, a destination, and the parent both
// sit in, which is where an escaping write would land.
func hostileDirs(t *testing.T) (base, src, dest string) {
	t.Helper()
	base = t.TempDir()
	src = filepath.Join(base, "src")
	dest = filepath.Join(base, "dest")
	for _, dir := range []string{src, dest} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating %s: %v", dir, err)
		}
	}
	return base, src, dest
}

func refusedByReceiver(err error) bool {
	return err != nil && strings.Contains(err.Error(), "refusing files")
}

func TestHostileSenderCannotTraverseOutOfTheDestination(t *testing.T) {
	quietStderr(t)
	base, src, dest := hostileDirs(t)

	entry := sendable(t, src, "payload.txt", "owned")
	entry.FolderRemote = "../" // the whole attack

	sendErr, recvErr := runPair(t, []FileInfo{entry}, nil, dest)

	if _, err := os.Lstat(filepath.Join(base, "payload.txt")); err == nil {
		t.Error("the sender wrote outside the destination")
	}
	if !refusedByReceiver(recvErr) {
		t.Errorf("receiver error = %v, want a refusal", recvErr)
	}
	// the sender is told why, rather than seeing a bare disconnect.
	if sendErr == nil || !strings.Contains(sendErr.Error(), "refusing files") {
		t.Errorf("sender error = %v, want the refusal reported back", sendErr)
	}
}

func TestHostileSenderCannotPlantAnEscapingSymlink(t *testing.T) {
	quietStderr(t)
	_, src, dest := hostileDirs(t)

	// a real symlink, so the sender reads and transmits its target the way it
	// would for any tree containing one.
	if err := os.Symlink("../../../../etc/passwd", filepath.Join(src, "esc")); err != nil {
		t.Fatalf("seeding symlink: %v", err)
	}
	hash, err := utils.HashFile(filepath.Join(src, "esc"), "xxhash")
	if err != nil {
		t.Fatalf("hashing the link: %v", err)
	}
	entry := FileInfo{
		Name: "esc", FolderRemote: "./", FolderSource: src,
		Mode: os.ModeSymlink, Hash: hash, ModTime: time.Now(),
	}

	_, recvErr := runPair(t, []FileInfo{entry}, nil, dest)

	if _, err := os.Lstat(filepath.Join(dest, "esc")); err == nil {
		t.Error("an escaping symlink was created in the destination")
	}
	if !refusedByReceiver(recvErr) {
		t.Errorf("receiver error = %v, want a refusal", recvErr)
	}
}

// TestSymlinkWithinTheTreeStillTransfers is the other side of the previous
// test: `send` of a directory containing an ordinary relative symlink has to
// keep working.
func TestSymlinkWithinTheTreeStillTransfers(t *testing.T) {
	quietStderr(t)
	_, src, dest := hostileDirs(t)

	target := sendable(t, src, "payload.txt", "owned")
	if err := os.Symlink("payload.txt", filepath.Join(src, "inside")); err != nil {
		t.Fatalf("seeding symlink: %v", err)
	}
	hash, err := utils.HashFile(filepath.Join(src, "inside"), "xxhash")
	if err != nil {
		t.Fatalf("hashing the link: %v", err)
	}
	link := FileInfo{
		Name: "inside", FolderRemote: "./", FolderSource: src,
		Mode: os.ModeSymlink, Hash: hash, ModTime: time.Now(),
	}

	_, recvErr := runPair(t, []FileInfo{target, link}, nil, dest)
	if recvErr != nil {
		t.Fatalf("legitimate transfer refused: %v", recvErr)
	}

	got, err := os.Readlink(filepath.Join(dest, "inside"))
	if err != nil {
		t.Fatalf("reading the received link: %v", err)
	}
	if got != "payload.txt" {
		t.Errorf("link target = %q, want payload.txt", got)
	}
}

// TestHostileArchiveCannotEscape covers the second write path: `send <folder>`
// zips the folder and the receiver unpacks it in place.
func TestHostileArchiveCannotEscape(t *testing.T) {
	quietStderr(t)
	base, src, dest := hostileDirs(t)

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"../escaped-by-zip.txt": "pwned",
		"innocent.txt":          "fine",
	} {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("adding %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing archive: %v", err)
	}

	entry := sendable(t, src, "evil.zip", buf.String())
	entry.TempFile = true // what the receiver unpacks

	_, recvErr := runPair(t, []FileInfo{entry}, nil, dest)

	if _, err := os.Lstat(filepath.Join(base, "escaped-by-zip.txt")); err == nil {
		t.Error("the archive wrote outside the destination")
	}
	// the innocent entry must be absent too: validation precedes the first
	// write, so a hostile archive cannot get half of itself onto disk. this is
	// the assertion that catches a guard removed from the validator, because
	// os.Root alone would have let this one land.
	if _, err := os.Lstat(filepath.Join(dest, "innocent.txt")); err == nil {
		t.Error("the archive was partly extracted")
	}
	if recvErr == nil {
		t.Error("receiver reported success for a traversing archive")
	}
}

func TestLegitimateTransferStillWorks(t *testing.T) {
	quietStderr(t)
	_, src, dest := hostileDirs(t)

	entry := sendable(t, src, "payload.txt", "owned")
	sendErr, recvErr := runPair(t, []FileInfo{entry}, nil, dest)
	if sendErr != nil || recvErr != nil {
		t.Fatalf("transfer failed: send=%v recv=%v", sendErr, recvErr)
	}

	got, err := os.ReadFile(filepath.Join(dest, "payload.txt"))
	if err != nil {
		t.Fatalf("reading the received file: %v", err)
	}
	if string(got) != "owned" {
		t.Errorf("received %q, want owned", got)
	}
}

// TestSendSameFileTwiceStillWorks is the regression the ticket calls out:
// GetFilesInfo never deduped, so `send a.txt a.txt` produces two identical
// entries, and a blanket duplicate refusal would break a working invocation.
func TestSendSameFileTwiceStillWorks(t *testing.T) {
	quietStderr(t)
	_, src, dest := hostileDirs(t)

	entry := sendable(t, src, "payload.txt", "owned")
	_, recvErr := runPair(t, []FileInfo{entry, entry}, nil, dest)
	if recvErr != nil {
		t.Fatalf("duplicate entries refused: %v", recvErr)
	}

	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("destination holds %v, want one file", names)
	}
	got, err := os.ReadFile(filepath.Join(dest, "payload.txt"))
	if err != nil {
		t.Fatalf("reading the received file: %v", err)
	}
	if string(got) != "owned" {
		t.Errorf("received %q, want owned", got)
	}
}

// TestReceiverIgnoresFileRequest pins the guard on TypeRecipientReady, which
// asks a *sender* for a file. A receiver acting on it adopts a peer-chosen
// index that the loops after the transfer use to index the manifest.
func TestReceiverIgnoresFileRequest(t *testing.T) {
	c := &Client{Options: Options{IsSender: false}, mutex: &sync.Mutex{}}
	c.FilesToTransfer = []FileInfo{{Name: "a"}}

	applied, done, err := c.applyFileRequest(RemoteFileRequest{FilesToTransferCurrentNum: 99})
	if err != nil {
		t.Fatalf("applyFileRequest: %v", err)
	}
	if applied {
		t.Error("a receiver applied a file request")
	}
	if done {
		t.Error("a receiver ended the transfer over a file request")
	}
	if c.FilesToTransferCurrentNum != 0 {
		t.Errorf("receiver adopted the peer's index %d", c.FilesToTransferCurrentNum)
	}
}

// TestSenderRejectsOutOfRangeFileRequest covers the other half: a sender does
// act on the request, so the index it carries has to be bounded before it is
// used to index the manifest.
func TestSenderRejectsOutOfRangeFileRequest(t *testing.T) {
	for _, index := range []int{-1, 1, 1 << 30} {
		c := &Client{Options: Options{IsSender: true}, mutex: &sync.Mutex{}}
		c.FilesToTransfer = []FileInfo{{Name: "a"}}

		applied, done, err := c.applyFileRequest(RemoteFileRequest{FilesToTransferCurrentNum: index})
		if err == nil {
			t.Errorf("index %d accepted", index)
		}
		if applied || !done {
			t.Errorf("index %d: applied=%v done=%v, want false/true", index, applied, done)
		}
	}
}

// TestSenderRejectsMalformedResumeRequest pins the bound on the resume ranges.
// utils.ChunkRangesToChunks reads chunkRanges[i+1] while stepping by two from
// index one, so an even-length list is an out-of-range read, and it expands
// each count into that many entries, so a large one is an allocation the peer
// chose.
func TestSenderRejectsMalformedResumeRequest(t *testing.T) {
	for _, ranges := range [][]int64{
		{1024, 0},          // even length: the panic
		{0, 0, 1},          // zero chunk size
		{-1, 0, 1},         // negative chunk size
		{1024, -1, 1},      // negative offset
		{1024, 0, -1},      // negative count
		{1024, 0, 1 << 40}, // an allocation the peer chose
	} {
		c := &Client{Options: Options{IsSender: true}, mutex: &sync.Mutex{}}
		c.FilesToTransfer = []FileInfo{{Name: "a", Size: 4096}}

		if _, _, err := c.applyFileRequest(RemoteFileRequest{CurrentFileChunkRanges: ranges}); err == nil {
			t.Errorf("resume request %v accepted", ranges)
		}
	}

	// a well-formed request still works, so the bound is not simply refusing
	// every resume.
	c := &Client{Options: Options{IsSender: true}, mutex: &sync.Mutex{}}
	c.FilesToTransfer = []FileInfo{{Name: "a", Size: 4096}}
	applied, _, err := c.applyFileRequest(RemoteFileRequest{CurrentFileChunkRanges: []int64{1024, 0, 2}})
	if err != nil || !applied {
		t.Errorf("a valid resume request was refused: applied=%v err=%v", applied, err)
	}
}
