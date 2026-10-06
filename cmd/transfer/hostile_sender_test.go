package transfer

import (
	"archive/zip"
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/schollz/croc/v9/src/models"
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
	relayErr    error
	roomCounter atomic.Int64
)

// startTestRelay runs croc's relay in-process. tcp.Run never returns, so the
// goroutines live for the test binary; there is no shutdown to call. It is
// started once and shared, which is why rooms have to be kept apart below.
//
// A startup failure is kept rather than reported inside the Once: t.Fatal
// there fails only the first test, and every later one would index an empty
// relayPorts and panic without the cause.
func startTestRelay(t *testing.T) []string {
	t.Helper()
	relayOnce.Do(func() { relayPorts, relayErr = runTestRelay() })
	if relayErr != nil {
		t.Fatalf("starting the test relay: %v", relayErr)
	}
	return relayPorts
}

func runTestRelay() ([]string, error) {
	first := 40000 + rand.Intn(20000) //nolint:gosec // test port selection
	open := utils.FindOpenPorts("localhost", first, 4)
	if len(open) < 4 {
		return nil, fmt.Errorf("only found %d open ports", len(open))
	}
	ports := make([]string, 0, len(open))
	for _, p := range open {
		ports = append(ports, strconv.Itoa(p))
	}
	// "localhost" rather than 127.0.0.1: tcp.Run rewrites the latter to
	// 0.0.0.0, which would expose the relay off the machine.
	go tcp.Run("error", "localhost", ports[0], "testpw", strings.Join(ports[1:], ",")) //nolint:errcheck
	for _, p := range ports[1:] {
		go tcp.Run("error", "localhost", p, "testpw") //nolint:errcheck
	}
	for range 100 {
		if err := tcp.PingServer("localhost:" + ports[0]); err == nil {
			return ports, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("relay on port %s never came up", ports[0])
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
	return runPairWith(t, files, folders, dest, nil)
}

// runPairWith is runPair with a hook over the sender's options, for the modes
// a sender can be in that the manifest alone does not express.
func runPairWith(t *testing.T, files, folders []FileInfo, dest string, tweak func(*Options)) (sendErr, recvErr error) {
	t.Helper()
	// croc derives the relay room from SharedSecret[:3], so each pair needs a
	// distinct three-character prefix or they share a room and every transfer
	// after the first reports "room not ready".
	secret := fmt.Sprintf("%03d4-runpodctl-test", roomCounter.Add(1)%1000)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		opts := testOptions(t, true, secret, "")
		if tweak != nil {
			tweak(&opts)
		}
		sender, err := New(opts)
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

// TestHostileSenderCannotLinkThroughAnExistingEscapingSymlink is the forward
// version of the previous test. `pwn -> esc/x` never says "..", so the lexical
// check passed it, but the receiver's own `esc` already points out of the
// destination, and os.Root creates the link without following it.
func TestHostileSenderCannotLinkThroughAnExistingEscapingSymlink(t *testing.T) {
	quietStderr(t)
	base, src, dest := hostileDirs(t)
	if err := os.MkdirAll(filepath.Join(base, "outside"), 0o755); err != nil {
		t.Fatalf("creating outside: %v", err)
	}
	if err := os.Symlink("../outside", filepath.Join(dest, "esc")); err != nil {
		t.Fatalf("seeding symlink: %v", err)
	}

	if err := os.Symlink("esc/x", filepath.Join(src, "pwn")); err != nil {
		t.Fatalf("seeding symlink: %v", err)
	}
	hash, err := utils.HashFile(filepath.Join(src, "pwn"), "xxhash")
	if err != nil {
		t.Fatalf("hashing the link: %v", err)
	}
	entry := FileInfo{
		Name: "pwn", FolderRemote: "./", FolderSource: src,
		Mode: os.ModeSymlink, Hash: hash, ModTime: time.Now(),
	}

	sendErr, recvErr := runPair(t, []FileInfo{entry}, nil, dest)

	if _, err := os.Lstat(filepath.Join(dest, "pwn")); err == nil {
		t.Error("a symlink resolving outside the destination was created")
	}
	if !refusedByReceiver(recvErr) {
		t.Errorf("receiver error = %v, want a refusal", recvErr)
	}
	if sendErr == nil || !strings.Contains(sendErr.Error(), "refusing files") {
		t.Errorf("sender error = %v, want the refusal reported back", sendErr)
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
	// the archive stays, and the error says where, so a user who did not
	// expect a zip is not left to wonder about one.
	if _, err := os.Lstat(filepath.Join(dest, "evil.zip")); err != nil {
		t.Errorf("the refused archive was removed: %v", err)
	}
	if recvErr == nil || !strings.Contains(recvErr.Error(), "kept at") {
		t.Errorf("receiver error = %v, want the kept archive named", recvErr)
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

// TestHostileSenderCannotWriteThroughAnExistingLeafSymlink covers a symlink
// already in the destination at the declared file's own path. os.Root refuses
// to follow it out, but the failure used to land mid-transfer, where it was
// discarded, so neither peer ever heard of it and both waited forever.
func TestHostileSenderCannotWriteThroughAnExistingLeafSymlink(t *testing.T) {
	quietStderr(t)
	base, src, dest := hostileDirs(t)
	if err := os.Symlink("../escaped.txt", filepath.Join(dest, "payload.txt")); err != nil {
		t.Fatalf("seeding symlink: %v", err)
	}

	entry := sendable(t, src, "payload.txt", "owned")
	sendErr, recvErr := runPair(t, []FileInfo{entry}, nil, dest)

	if _, err := os.Lstat(filepath.Join(base, "escaped.txt")); err == nil {
		t.Error("the sender wrote outside the destination")
	}
	if !refusedByReceiver(recvErr) {
		t.Errorf("receiver error = %v, want a refusal", recvErr)
	}
	if sendErr == nil || !strings.Contains(sendErr.Error(), "refusing files") {
		t.Errorf("sender error = %v, want the refusal reported back", sendErr)
	}
}

// TestSymlinkOnlyTransferLeavesItsTargetAlone is `send link` with nothing else
// to transfer. Finishing used to open the current entry anyway, which was the
// link just created: os.Root followed it to the existing in-tree target and
// truncated that to the link's declared size of zero.
func TestSymlinkOnlyTransferLeavesItsTargetAlone(t *testing.T) {
	quietStderr(t)
	_, src, dest := hostileDirs(t)
	if err := os.WriteFile(filepath.Join(dest, "data.bin"), []byte("precious"), 0o644); err != nil {
		t.Fatalf("seeding target: %v", err)
	}
	if err := os.Symlink("data.bin", filepath.Join(src, "link")); err != nil {
		t.Fatalf("seeding symlink: %v", err)
	}
	hash, err := utils.HashFile(filepath.Join(src, "link"), "xxhash")
	if err != nil {
		t.Fatalf("hashing the link: %v", err)
	}
	link := FileInfo{
		Name: "link", FolderRemote: "./", FolderSource: src,
		Mode: os.ModeSymlink, Hash: hash, ModTime: time.Now(),
	}

	sendErr, recvErr := runPair(t, []FileInfo{link}, nil, dest)
	if sendErr != nil || recvErr != nil {
		t.Fatalf("transfer failed: send=%v recv=%v", sendErr, recvErr)
	}

	if got, err := os.Readlink(filepath.Join(dest, "link")); err != nil || got != "data.bin" {
		t.Errorf("link = %q (%v), want data.bin", got, err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "data.bin"))
	if err != nil {
		t.Fatalf("reading the target: %v", err)
	}
	if string(got) != "precious" {
		t.Errorf("target = %q, want it untouched", got)
	}
}

// TestChunkPastTheDeclaredSizeEndsBothSides covers the data goroutine's
// failure paths. The sender declares a smaller size than the file it sends, so
// the first chunk is out of bounds. That goroutine used to just return, which
// left the receive blocked on its control connection and the sender waiting on
// it, for as long as comm's three hour read deadline.
func TestChunkPastTheDeclaredSizeEndsBothSides(t *testing.T) {
	quietStderr(t)
	_, src, dest := hostileDirs(t)

	entry := sendable(t, src, "payload.txt", "0123456789")
	entry.Size = 4

	// runPair fails the test if either side is still running after its bound.
	sendErr, recvErr := runPair(t, []FileInfo{entry}, nil, dest)

	if recvErr == nil || !strings.Contains(recvErr.Error(), "past the declared size") {
		t.Errorf("receiver error = %v, want the bound named", recvErr)
	}
	if sendErr == nil {
		t.Error("sender reported success for a receive that failed")
	}
	got, err := os.ReadFile(filepath.Join(dest, "payload.txt"))
	if err != nil {
		t.Fatalf("reading the received file: %v", err)
	}
	if len(got) > 4 {
		t.Errorf("received file grew to %d bytes past its declared size of 4", len(got))
	}
}

// TestOpenFailureIsReportedToTheSender covers a failure after validation has
// accepted the manifest: the path is there and is a file, but cannot be opened
// for writing. The receiver returned the error but sent nothing, so the sender
// saw only EOF once the connection closed.
func TestOpenFailureIsReportedToTheSender(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens a read-only file for writing regardless")
	}
	quietStderr(t)
	_, src, dest := hostileDirs(t)
	if err := os.WriteFile(filepath.Join(dest, "ro.txt"), []byte("mine"), 0o444); err != nil {
		t.Fatalf("seeding read-only file: %v", err)
	}

	// a different size, so the receiver does not skip it as already present.
	entry := sendable(t, src, "ro.txt", "replaced!")
	sendErr, recvErr := runPair(t, []FileInfo{entry}, nil, dest)

	if recvErr == nil || !strings.Contains(recvErr.Error(), "could not open") {
		t.Errorf("receiver error = %v, want the open failure", recvErr)
	}
	if sendErr == nil || !strings.Contains(sendErr.Error(), "could not open") {
		t.Errorf("sender error = %v, want the receiver's reason reported back", sendErr)
	}
	got, err := os.ReadFile(filepath.Join(dest, "ro.txt"))
	if err != nil {
		t.Fatalf("reading the read-only file: %v", err)
	}
	if string(got) != "mine" {
		t.Errorf("read-only file = %q, want it untouched", got)
	}
}

// TestAbortedTextReceiveRemovesItsFile covers the stdin path's cleanup. A text
// send arrives as a file named croc-stdin-*, which the receiver creates in the
// destination, prints, and removes at the end of transfer(). An abort used to
// return before that cleanup, leaving the file behind with its handle open.
func TestAbortedTextReceiveRemovesItsFile(t *testing.T) {
	quietStderr(t)
	_, src, dest := hostileDirs(t)

	entry := sendable(t, src, "croc-stdin-test", "0123456789")
	entry.Size = 4 // the first chunk is out of bounds, so the receive aborts
	_, recvErr := runPairWith(t, []FileInfo{entry}, nil, dest, func(o *Options) { o.SendingText = true })

	if recvErr == nil || !strings.Contains(recvErr.Error(), "past the declared size") {
		t.Errorf("receiver error = %v, want the abort", recvErr)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "croc-stdin-") {
			t.Errorf("aborted text receive left %s in the destination", e.Name())
		}
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
	const chunk = models.TCP_BUFFER_SIZE / 2
	const size = 3 * chunk // a file sendData reads as exactly three chunks

	for _, ranges := range [][]int64{
		{chunk, 0},          // even length: the panic
		{0, 0, 1},           // zero chunk size
		{-1, 0, 1},          // negative chunk size
		{chunk, -1, 1},      // negative offset
		{chunk, 0, -1},      // negative count
		{chunk, 0, 1 << 40}, // an allocation the peer chose
		{chunk, 0, 4},       // more chunks than the file has
		{chunk, 0, 2, chunk * 2, 2},
		// a chunk size of 1 would make a bound of size/ranges[0] the file's
		// size in bytes, so the peer's chunk size does not count.
		{1, 0, 4},
	} {
		c := &Client{Options: Options{IsSender: true}, mutex: &sync.Mutex{}}
		c.FilesToTransfer = []FileInfo{{Name: "a", Size: size}}

		if _, _, err := c.applyFileRequest(RemoteFileRequest{CurrentFileChunkRanges: ranges}); err == nil {
			t.Errorf("resume request %v accepted", ranges)
		}
	}

	// well-formed requests still work, up to every chunk of the file, so the
	// bound is not simply refusing every resume.
	for _, ranges := range [][]int64{
		{chunk, 0, 1},
		{chunk, 0, 3},
		{chunk, 0, 1, chunk * 2, 1},
	} {
		c := &Client{Options: Options{IsSender: true}, mutex: &sync.Mutex{}}
		c.FilesToTransfer = []FileInfo{{Name: "a", Size: size}}
		applied, _, err := c.applyFileRequest(RemoteFileRequest{CurrentFileChunkRanges: ranges})
		if err != nil || !applied {
			t.Errorf("valid resume request %v refused: applied=%v err=%v", ranges, applied, err)
		}
	}
}

func TestFileChunks(t *testing.T) {
	const chunk = models.TCP_BUFFER_SIZE / 2
	for size, want := range map[int64]int64{
		0:         0,
		1:         1,
		chunk - 1: 1,
		chunk:     1,
		chunk + 1: 2,
		3 * chunk: 3,
	} {
		if got := fileChunks(size); got != want {
			t.Errorf("fileChunks(%d) = %d, want %d", size, got, want)
		}
	}
}

// TestChunkFits pins the bound on a peer-supplied chunk offset. The cases near
// MaxInt64 are the reason it is not written as offset+n > size: that sum wraps
// negative and passes.
func TestChunkFits(t *testing.T) {
	for _, tc := range []struct {
		offset int64
		n      int
		size   int64
		want   bool
	}{
		{offset: 0, n: 4, size: 4, want: true},
		{offset: 2, n: 2, size: 4, want: true},
		{offset: 4, n: 0, size: 4, want: true},
		{offset: 0, n: 0, size: 0, want: true},
		{offset: 1, n: 4, size: 4},
		{offset: 0, n: 5, size: 4},
		{offset: -1, n: 1, size: 4},
		{offset: math.MaxInt64, n: 1, size: 4},
		{offset: math.MaxInt64 - 1, n: 10, size: 4},
		{offset: math.MaxInt64 - 1, n: 10, size: math.MaxInt64},
	} {
		if got := chunkFits(tc.offset, tc.n, tc.size); got != tc.want {
			t.Errorf("chunkFits(%d, %d, %d) = %v, want %v", tc.offset, tc.n, tc.size, got, tc.want)
		}
	}
}
