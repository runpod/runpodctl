package transfer

// These tests lock the croc wire format that `runpodctl send`/`receive` speak.
// Both ends of a transfer must agree on it, including the runpodctl copy the
// platform mounts into the container, so a silent change here breaks transfers
// in the field. We cannot golden freshly encrypted bytes (croc encrypts with a
// random nonce), so we lock what decides compatibility: the croc module the
// build resolves to (version and no replace), the fork's raw handshake strings
// and JSON wire structs, the message envelope/vocabulary, and that a message
// captured from a v9.6.16 peer still decodes. See E-3968 for the longer-term
// version-negotiation fix.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os/exec"
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
	mod := crocModuleInfo(t)
	if mod.Replace != nil {
		t.Fatalf("go.mod replaces %s with %s %s.\n"+
			"A replace swaps the croc wire format underneath the pinned require line. "+
			"Both transfer ends must speak the same croc, including the runpodctl copy "+
			"mounted into the container. Coordinate a version/wire-format handshake first "+
			"(E-3968) and drop the replace.",
			crocModule, mod.Replace.Path, mod.Replace.Version)
	}
	if mod.Version != crocPinnedVersion {
		t.Fatalf("croc is %s in go.mod, want %s.\n"+
			"The croc wire format is tied to this version. Both transfer ends must match, "+
			"including the runpodctl copy mounted into the container. Bumping can break "+
			"interop, and the fork has diverged from upstream's security fix. Coordinate a "+
			"version/wire-format handshake first (E-3968) before changing this.",
			mod.Version, crocPinnedVersion)
	}
}

// goListModule is the subset of `go list -m -json` output the version lock
// reads.
type goListModule struct {
	Version string
	Replace *struct {
		Path    string
		Version string
	}
}

// crocModuleInfo asks the go command which croc the module actually builds
// against. Unlike parsing the require line, this sees replace directives, and
// unlike debug.ReadBuildInfo it works under `go test` (test binaries carry no
// dependency list). `go test` puts its own go on PATH; -m on a module already
// in go.mod reads the module graph and needs no network.
func crocModuleInfo(t *testing.T) goListModule {
	t.Helper()
	// a test binary built with go test -c can run where there is no toolchain.
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH; skipping the croc version pin check")
	}
	out, err := exec.CommandContext(t.Context(), "go", "list", "-m", "-json", crocModule).Output()
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			t.Fatalf("go list -m %s: %v\n%s", crocModule, err, exitErr.Stderr)
		}
		t.Fatalf("go list -m %s: %v", crocModule, err)
	}
	var mod goListModule
	if err := json.Unmarshal(out, &mod); err != nil {
		t.Fatalf("parse go list -m output %q: %v", out, err)
	}
	return mod
}

// TestForkHandshakeStrings locks the raw request strings the fork exchanges
// with the relay and peer before any croc message. They are compared byte for
// byte on the other end, so changing one breaks the handshake.
func TestForkHandshakeStrings(t *testing.T) {
	tests := []struct {
		name string
		got  []byte
		want string
	}{
		{name: "ipRequest", got: ipRequest, want: "ips?"},
		{name: "handshakeRequest", got: handshakeRequest, want: "handshake"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.got) != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
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

// TestRemoteFileRequestWireGolden locks RemoteFileRequest, which the recipient
// marshals into the TypeRecipientReady message to ask the sender for specific
// chunks, and the sender unmarshals on the other end. Like SenderInfo it has no
// json tags, so its wire keys are the Go field names; renaming one silently
// breaks the chunk-request wire.
func TestRemoteFileRequestWireGolden(t *testing.T) {
	b, err := json.Marshal(RemoteFileRequest{
		CurrentFileChunkRanges:    []int64{1, 2},
		FilesToTransferCurrentNum: 3,
		MachineID:                 "mid",
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"CurrentFileChunkRanges":[1,2],"FilesToTransferCurrentNum":3,"MachineID":"mid"}`
	if string(b) != want {
		t.Errorf("RemoteFileRequest wire json =\n  %s\nwant\n  %s", b, want)
	}
}

// Fixed inputs for TestMessageDecodesPinnedCiphertext. capturedMessageHex is
// message.Encode output captured once from croc v9.6.16 with the key derived
// from fixturePassphrase and fixtureSalt (see TestMessageDecodesPinnedCiphertext
// for the message it holds). Never regenerate it to make the test pass: a
// mismatch means this binary can no longer read what a v9.6.16 peer sends.
const (
	fixturePassphrase  = "passphrase"
	fixtureSalt        = "fixed-salt-123456"
	capturedMessageHex = "718d28e22bac367d36f503f1346cc56100779b906a7a28ab3ef9b5dddd0e4764619218e414d015f02c970934bc0fd7160c6fc8b66c0ee500ee4c369256928362e04dab739d93128fab3222b09a4dc0d94ea3de4a16a6b5d6b6fd"
)

// TestMessageDecodesPinnedCiphertext decodes a message encrypted by croc
// v9.6.16, so it fails if this build's key derivation (pbkdf2 parameters),
// cipher (AES-GCM, 12-byte nonce prefix), compression (flate) or JSON envelope
// stops matching what a v9.6.16 peer puts on the wire. A same-binary
// encode/decode round trip cannot catch that, since a change applies to both
// sides at once.
func TestMessageDecodesPinnedCiphertext(t *testing.T) {
	key, _, err := crypt.New([]byte(fixturePassphrase), []byte(fixtureSalt))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := hex.DecodeString(capturedMessageHex)
	if err != nil {
		t.Fatal(err)
	}
	got, err := message.Decode(key, enc)
	if err != nil {
		t.Fatalf("decode v9.6.16 ciphertext: %v", err)
	}
	want := message.Message{Type: message.TypeFileInfo, Message: "meta", Bytes: []byte("payload"), Num: 7}
	if got.Type != want.Type || got.Message != want.Message || !bytes.Equal(got.Bytes, want.Bytes) || got.Bytes2 != nil || got.Num != want.Num {
		t.Errorf("decoded %+v, want %+v", got, want)
	}
}
