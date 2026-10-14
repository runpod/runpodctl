package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const (
	testKeyFirst  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJ3RR3gea2dfJPbmxyVu03iX2tDYxtu6rZ7AzpENl3Cl first"
	testKeySecond = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINTtSc8R81qF3axjxlXmNVFngfXiPCDZBdiX7qTnIopn second"
	// a second key also named "first"
	testKeyFirstAgain = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBRhsFVRuz95fI7cuRPJ166WccPHm/PmwqCE8Lq51wyO first"
)

// sshKeyServer serves GET /account/ssh-keys from keys and records each PUT.
func sshKeyServer(t *testing.T, keys []string, puts *[][]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/account/ssh-keys" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(sshKeysBody{Keys: keys})
		case http.MethodPut:
			var body sshKeysBody
			_ = json.NewDecoder(r.Body).Decode(&body)
			*puts = append(*puts, body.Keys)
			_ = json.NewEncoder(w).Encode(body)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
}

func TestGetPublicSSHKeys(t *testing.T) {
	var puts [][]string
	server := sshKeyServer(t, []string{testKeyFirst, "not a key", testKeySecond}, &puts)
	defer server.Close()

	keys, err := newV2TestClient(t, server).GetPublicSSHKeys()
	if err != nil {
		t.Fatalf("GetPublicSSHKeys: %v", err)
	}
	if len(keys) != 2 || keys[0].Name != "first" || keys[1].Name != "second" || keys[0].Fingerprint == "" {
		t.Fatalf("keys = %+v, want the two parseable keys", keys)
	}
}

func TestAddPublicSSHKey(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		add      string
		wantPuts [][]string
	}{
		{name: "appends a new key", existing: []string{testKeyFirst}, add: testKeySecond, wantPuts: [][]string{{testKeyFirst, testKeySecond}}},
		// keys compare by fingerprint, so neither a comment nor its absence makes
		// a registered key look new
		{name: "a registered key is a no-op", existing: []string{testKeyFirst}, add: testKeyFirst + "\n", wantPuts: nil},
		{name: "a registered key without its comment is a no-op", existing: []string{testKeyFirst}, add: strings.Join(strings.Fields(testKeyFirst)[:2], " "), wantPuts: nil},
		// v2 stores one key per entry
		{name: "several pasted keys are stored one per entry", existing: []string{testKeyFirst}, add: testKeyFirst + "\n\n" + testKeySecond + "\n", wantPuts: [][]string{{testKeyFirst, testKeySecond}}},
		{name: "first key on an empty account", existing: []string{}, add: testKeyFirst, wantPuts: [][]string{{testKeyFirst}}},
		// comment lines in a pasted authorized_keys block are skipped
		{name: "comments are skipped", existing: []string{testKeyFirst}, add: "# laptop\n" + testKeySecond, wantPuts: [][]string{{testKeyFirst, testKeySecond}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var puts [][]string
			server := sshKeyServer(t, tt.existing, &puts)
			defer server.Close()

			if err := newV2TestClient(t, server).AddPublicSSHKey([]byte(tt.add), false); err != nil {
				t.Fatalf("AddPublicSSHKey: %v", err)
			}
			if !reflect.DeepEqual(puts, tt.wantPuts) {
				t.Fatalf("puts = %v, want %v", puts, tt.wantPuts)
			}
		})
	}
}

func TestRemovePublicSSHKey(t *testing.T) {
	fingerprintOf := func(line string) string {
		key, _ := parseSSHKey(line)
		return key.Fingerprint
	}
	tests := []struct {
		name        string
		existing    []string
		byName      string
		byPrint     string
		wantPut     []string
		wantErrText string
	}{
		{name: "by name", existing: []string{testKeyFirst, testKeySecond}, byName: "second", wantPut: []string{testKeyFirst}},
		{name: "last key leaves an empty list", existing: []string{testKeyFirst}, byName: "first", wantPut: []string{}},
		{name: "ambiguous name needs a fingerprint", existing: []string{testKeyFirst, testKeyFirstAgain}, byName: "first", wantErrText: "use --fingerprint"},
		{name: "fingerprint disambiguates", existing: []string{testKeyFirst, testKeyFirstAgain}, byPrint: fingerprintOf(testKeyFirstAgain), wantPut: []string{testKeyFirst}},
		{name: "no match", existing: []string{testKeyFirst}, byName: "missing", wantErrText: "ssh key not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var puts [][]string
			server := sshKeyServer(t, tt.existing, &puts)
			defer server.Close()

			err := newV2TestClient(t, server).RemovePublicSSHKey(tt.byName, tt.byPrint, false)
			if tt.wantErrText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrText) {
					t.Fatalf("error = %v, want %q", err, tt.wantErrText)
				}
				if len(puts) != 0 {
					t.Fatalf("a failed remove must not write: %v", puts)
				}
				return
			}
			if err != nil {
				t.Fatalf("RemovePublicSSHKey: %v", err)
			}
			if len(puts) != 1 || !reflect.DeepEqual(puts[0], tt.wantPut) {
				t.Fatalf("puts = %v, want [%v]", puts, tt.wantPut)
			}
		})
	}
}

// v2 validates every entry it is sent, so a line that isn't a key is refused
// before any write rather than failing, or polluting, the whole list.
func TestAddPublicSSHKeyRefusesNonKeyLines(t *testing.T) {
	var puts [][]string
	server := sshKeyServer(t, []string{testKeyFirst}, &puts)
	defer server.Close()

	err := newV2TestClient(t, server).AddPublicSSHKey([]byte(testKeySecond+"\nnot a key at all"), false)
	var usage interface{ ErrorCode() string }
	if !errors.As(err, &usage) || usage.ErrorCode() != "usage_error" || !strings.Contains(err.Error(), "not an ssh public key") {
		t.Fatalf("err = %v, want a usage_error naming the bad line", err)
	}
	if len(puts) != 0 {
		t.Fatalf("a refused add must not write: %v", puts)
	}
}

// an entry v2 rejects, saved back when keys were one free-text blob, blocks
// every key change: add and remove stop before writing, with a conflict error
// that names the way out.
func TestStoredInvalidSSHKeysBlockWritesWithAProperError(t *testing.T) {
	stored := []string{testKeyFirst, "my laptop key, see notes", testKeySecond}
	for name, write := range map[string]func(*Client) error{
		"add":    func(c *Client) error { return c.AddPublicSSHKey([]byte(testKeyFirstAgain), false) },
		"remove": func(c *Client) error { return c.RemovePublicSSHKey("second", "", false) },
	} {
		t.Run(name, func(t *testing.T) {
			var puts [][]string
			server := sshKeyServer(t, stored, &puts)
			defer server.Close()

			err := write(newV2TestClient(t, server))
			var invalid *InvalidStoredSSHKeysError
			if !errors.As(err, &invalid) || invalid.ErrorCode() != "conflict" || len(invalid.Entries) != 1 {
				t.Fatalf("err = %v, want an InvalidStoredSSHKeysError for the one bad entry", err)
			}
			if !strings.Contains(err.Error(), "runpodctl ssh remove-key --invalid") {
				t.Errorf("the error must name the fix: %v", err)
			}
			if len(puts) != 0 {
				t.Fatalf("a blocked change must not write: %v", puts)
			}
		})
	}
}

func TestRemoveInvalidSSHKeys(t *testing.T) {
	var puts [][]string
	optionsPrefixed := `from="10.0.0.1" ` + testKeySecond // parses, but v2's pattern rejects it
	server := sshKeyServer(t, []string{testKeyFirst, "not a key", optionsPrefixed, testKeyFirstAgain}, &puts)
	defer server.Close()

	n, err := newV2TestClient(t, server).RemoveInvalidSSHKeys()
	if err != nil || n != 2 {
		t.Fatalf("removed %d, err %v; want 2 removed", n, err)
	}
	if !reflect.DeepEqual(puts, [][]string{{testKeyFirst, testKeyFirstAgain}}) {
		t.Fatalf("puts = %v, want only the valid keys kept", puts)
	}

	// nothing invalid: nothing written
	puts = nil
	clean := sshKeyServer(t, []string{testKeyFirst}, &puts)
	defer clean.Close()
	if n, err := newV2TestClient(t, clean).RemoveInvalidSSHKeys(); err != nil || n != 0 || len(puts) != 0 {
		t.Fatalf("clean list: removed %d, err %v, puts %v", n, err, puts)
	}
}

// a pasted key with authorized_keys options parses, but v2 rejects it
func TestAddPublicSSHKeyRefusesOptionsPrefixedKey(t *testing.T) {
	var puts [][]string
	server := sshKeyServer(t, []string{testKeyFirst}, &puts)
	defer server.Close()
	err := newV2TestClient(t, server).AddPublicSSHKey([]byte(`from="10.0.0.1" `+testKeySecond), false)
	var usage interface{ ErrorCode() string }
	if !errors.As(err, &usage) || usage.ErrorCode() != "usage_error" || len(puts) != 0 {
		t.Fatalf("err = %v, puts = %v; want a usage_error and no write", err, puts)
	}
}

// with removeInvalid, the bad entries go in the same write as the change.
func TestRemoveInvalidInTheSameWrite(t *testing.T) {
	stored := []string{testKeyFirst, "my laptop key, see notes", testKeySecond}
	var puts [][]string
	server := sshKeyServer(t, stored, &puts)
	defer server.Close()
	client := newV2TestClient(t, server)

	if err := client.AddPublicSSHKey([]byte(testKeyFirstAgain), true); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := client.RemovePublicSSHKey("second", "", true); err != nil {
		t.Fatalf("remove: %v", err)
	}
	want := [][]string{{testKeyFirst, testKeySecond, testKeyFirstAgain}, {testKeyFirst}}
	if !reflect.DeepEqual(puts, want) {
		t.Fatalf("puts = %v, want %v", puts, want)
	}
}

// entries go out trimmed and without blanks: v2's per-entry pattern is anchored.
func TestSSHKeyWritesAreTrimmed(t *testing.T) {
	var puts [][]string
	server := sshKeyServer(t, []string{"  " + testKeyFirst, "", "   ", "not a key"}, &puts)
	defer server.Close()
	n, err := newV2TestClient(t, server).RemoveInvalidSSHKeys()
	if err != nil || n != 1 {
		t.Fatalf("removed %d, err %v; want only the one real invalid entry counted", n, err)
	}
	if !reflect.DeepEqual(puts, [][]string{{testKeyFirst}}) {
		t.Fatalf("puts = %q, want the trimmed key alone", puts)
	}

	puts = nil
	padded := sshKeyServer(t, []string{testKeyFirst + "  ", ""}, &puts)
	defer padded.Close()
	if err := newV2TestClient(t, padded).AddPublicSSHKey([]byte(testKeySecond), false); err != nil {
		t.Fatalf("add: %v", err)
	}
	if !reflect.DeepEqual(puts, [][]string{{testKeyFirst, testKeySecond}}) {
		t.Fatalf("puts = %q, want trimmed entries and no blanks", puts)
	}
}

// an options-prefixed copy of a key parses but v2 rejects it: adding the clean
// copy must not be a no-op that leaves every later write blocked.
func TestAddCleanCopyOfInvalidStoredKey(t *testing.T) {
	prefixed := `from="10.0.0.1" ` + testKeySecond
	var puts [][]string
	server := sshKeyServer(t, []string{testKeyFirst, prefixed}, &puts)
	defer server.Close()
	client := newV2TestClient(t, server)

	var invalid *InvalidStoredSSHKeysError
	if err := client.AddPublicSSHKey([]byte(testKeySecond), false); !errors.As(err, &invalid) {
		t.Fatalf("without the flag: err = %v, want the invalid-entry error (so a person is asked)", err)
	}
	if err := client.AddPublicSSHKey([]byte(testKeySecond), true); err != nil {
		t.Fatalf("with the flag: %v", err)
	}
	if !reflect.DeepEqual(puts, [][]string{{testKeyFirst, testKeySecond}}) {
		t.Fatalf("puts = %q, want the clean copy in and the prefixed one gone", puts)
	}
}

// with --remove-invalid and nothing new to add, the invalid entries still go.
func TestAddExistingKeyWithRemoveInvalidStillCleans(t *testing.T) {
	var puts [][]string
	server := sshKeyServer(t, []string{testKeyFirst, "not a key"}, &puts)
	defer server.Close()
	if err := newV2TestClient(t, server).AddPublicSSHKey([]byte(testKeyFirst), true); err != nil {
		t.Fatalf("add: %v", err)
	}
	if !reflect.DeepEqual(puts, [][]string{{testKeyFirst}}) {
		t.Fatalf("puts = %q, want the invalid entry removed", puts)
	}

	// without the flag, re-adding a key that's present stays a no-op
	puts = nil
	if err := newV2TestClient(t, server).AddPublicSSHKey([]byte(testKeyFirst), false); err != nil || len(puts) != 0 {
		t.Fatalf("err %v, puts %q; want a no-op", err, puts)
	}
}

// listing marks which saved entries v2 accepts, so a sync doesn't count an
// options-prefixed copy as registered.
func TestGetPublicSSHKeysMarksAcceptedByV2(t *testing.T) {
	var puts [][]string
	server := sshKeyServer(t, []string{testKeyFirst, `from="10.0.0.1" ` + testKeySecond}, &puts)
	defer server.Close()
	keys, err := newV2TestClient(t, server).GetPublicSSHKeys()
	if err != nil || len(keys) != 2 {
		t.Fatalf("keys %+v, err %v", keys, err)
	}
	if !keys[0].AcceptedByV2 || keys[1].AcceptedByV2 {
		t.Fatalf("AcceptedByV2 = %t, %t; want true, false", keys[0].AcceptedByV2, keys[1].AcceptedByV2)
	}
	if b, _ := json.Marshal(keys[1]); strings.Contains(string(b), "AcceptedByV2") || strings.Contains(string(b), "accepted") {
		t.Fatalf("the flag must not change list-keys output: %s", b)
	}
}

// doctor and config have no --remove-invalid, so their error names the
// commands that clear the entries, and keeps the conflict code
func TestInvalidStoredSSHKeysForSync(t *testing.T) {
	err := (&InvalidStoredSSHKeysError{Entries: []string{"bad"}}).ForSync()
	var coded interface{ ErrorCode() string }
	if !errors.As(err, &coded) || coded.ErrorCode() != "conflict" {
		t.Fatalf("err = %v, want code conflict", err)
	}
	if strings.Contains(err.Error(), "--remove-invalid") || !strings.Contains(err.Error(), "runpodctl ssh remove-key --invalid") {
		t.Fatalf("message = %q, want remove-key --invalid and no --remove-invalid", err)
	}
	if got := TruncateRunes("ééé", 2); got != "éé..." {
		t.Fatalf("TruncateRunes = %q, want whole characters", got)
	}
}
