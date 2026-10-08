package transfer

// These tests lock the croc wire format that `runpodctl send`/`receive` speak.
// Both ends of a transfer must agree on it, including the runpodctl copy the
// platform mounts into the container, so a silent change here breaks transfers
// in the field. We cannot golden the actual on-wire bytes (croc encrypts with a
// random nonce), so we lock the three deterministic things that decide
// compatibility: the pinned upstream croc version, the fork's own JSON wire
// structs, and the message envelope/vocabulary. See E-3968 for the longer-term
// version-negotiation fix.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/schollz/croc/v9/src/crypt"
	"github.com/schollz/croc/v9/src/message"
)

// crocModule and crocPinnedVersion must match go.mod. The wire format (framing,
// handshake, crypto) is defined by this exact upstream version; bumping it can
// break interop with an older or container-mounted peer, and the fork has
// diverged from upstream's later security fix. Do not bump without a
// wire-format/version handshake between ends (E-3968).
const (
	crocModule        = "github.com/schollz/croc/v9"
	crocPinnedVersion = "v9.6.16"
)

func TestCrocDependencyVersionPinned(t *testing.T) {
	got := crocVersionFromGoMod(t)
	if got != crocPinnedVersion {
		t.Fatalf("croc is %s in go.mod, want %s.\n"+
			"The croc wire format is tied to this version. Both transfer ends must match, "+
			"including the runpodctl copy mounted into the container. Bumping can break "+
			"interop, and the fork has diverged from upstream's security fix. Coordinate a "+
			"version/wire-format handshake first (E-3968) before changing this.",
			got, crocPinnedVersion)
	}
}

// crocVersionFromGoMod walks up from this test file to the module go.mod and
// returns the required version of crocModule. go.mod is the source of truth and
// is always present in the module, unlike debug.ReadBuildInfo().Deps under
// `go test`.
func crocVersionFromGoMod(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file path")
	}
	dir := filepath.Dir(thisFile)
	for {
		gomod := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(gomod); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				f := strings.Fields(line)
				if len(f) >= 2 && f[0] == crocModule {
					return f[1]
				}
			}
			t.Fatalf("%s not required in %s", crocModule, gomod)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test file")
		}
		dir = parent
	}
}

// TestMessageTypeVocabulary locks the message type strings that go on the wire.
// Renaming any of these upstream silently breaks compatibility with peers on the
// pinned version.
func TestMessageTypeVocabulary(t *testing.T) {
	want := map[message.Type]string{
		message.TypePAKE:           "pake",
		message.TypeExternalIP:     "externalip",
		message.TypeFinished:       "finished",
		message.TypeError:          "error",
		message.TypeCloseRecipient: "close-recipient",
		message.TypeCloseSender:    "close-sender",
		message.TypeRecipientReady: "recipientready",
		message.TypeFileInfo:       "fileinfo",
	}
	for got, str := range want {
		if string(got) != str {
			t.Errorf("message type = %q, want %q", string(got), str)
		}
	}
}

// TestMessageEnvelopeShape locks the JSON envelope keys (t/m/b/b2/n) that wrap
// every message. The receiver on the pinned version expects exactly these.
func TestMessageEnvelopeShape(t *testing.T) {
	b, err := json.Marshal(message.Message{
		Type:    message.TypeFileInfo,
		Message: "hi",
		Bytes:   []byte("x"),
		Bytes2:  []byte("y"),
		Num:     3,
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"t":"fileinfo","m":"hi","b":"eA==","b2":"eQ==","n":3}`
	if string(b) != want {
		t.Errorf("envelope = %s\nwant       %s", b, want)
	}
}

// fixedFileInfo is a fully-populated FileInfo (every field set so every json tag
// is exercised) with a fixed ModTime for deterministic output.
func fixedFileInfo() FileInfo {
	return FileInfo{
		Name:         "f.txt",
		FolderRemote: "fr",
		FolderSource: "fs",
		Hash:         []byte{1, 2, 3},
		Size:         42,
		ModTime:      time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		IsCompressed: true,
		IsEncrypted:  true,
		Symlink:      "sy",
		Mode:         0o644,
		TempFile:     true,
	}
}

// TestFileInfoWireGolden locks the on-wire JSON of FileInfo (the file metadata
// croc sends). A renamed field or changed json tag changes the bytes and breaks
// a peer; this test makes that break loud and intentional.
func TestFileInfoWireGolden(t *testing.T) {
	b, err := json.Marshal(fixedFileInfo())
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"n":"f.txt","fr":"fr","fs":"fs","h":"AQID","s":42,"m":"2026-01-02T03:04:05Z","c":true,"e":true,"sy":"sy","md":420,"tf":true}`
	if string(b) != want {
		t.Errorf("FileInfo wire json =\n  %s\nwant\n  %s", b, want)
	}
}

// TestSenderInfoWireGolden locks SenderInfo, which has no json tags, so its wire
// keys are the Go field names. Renaming a field silently changes the wire.
func TestSenderInfoWireGolden(t *testing.T) {
	b, err := json.Marshal(SenderInfo{
		FilesToTransfer:    []FileInfo{fixedFileInfo()},
		TotalNumberFolders: 1,
		MachineID:          "mid",
		Ask:                true,
		SendingText:        false,
		NoCompress:         true,
		HashAlgorithm:      "xxhash",
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"FilesToTransfer":[{"n":"f.txt","fr":"fr","fs":"fs","h":"AQID","s":42,"m":"2026-01-02T03:04:05Z","c":true,"e":true,"sy":"sy","md":420,"tf":true}],"EmptyFoldersToTransfer":null,"TotalNumberFolders":1,"MachineID":"mid","Ask":true,"SendingText":false,"NoCompress":true,"HashAlgorithm":"xxhash"}`
	if string(b) != want {
		t.Errorf("SenderInfo wire json =\n  %s\nwant\n  %s", b, want)
	}
}

// TestMessageRoundTrip locks that the pinned message+crypt libraries interoperate:
// a message encoded with a key decodes back to the same message. Catches an
// incompatible library change even when the version string still looks right.
func TestMessageRoundTrip(t *testing.T) {
	key, _, err := crypt.New([]byte("passphrase"), []byte("fixed-salt-123456"))
	if err != nil {
		t.Fatal(err)
	}
	orig := message.Message{Type: message.TypeFileInfo, Message: "meta", Bytes: []byte("payload"), Num: 7}
	enc, err := message.Encode(key, orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := message.Decode(key, enc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != orig.Type || got.Message != orig.Message || string(got.Bytes) != string(orig.Bytes) || got.Num != orig.Num {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, orig)
	}
}
