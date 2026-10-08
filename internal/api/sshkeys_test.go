package api

import (
	"encoding/json"
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var puts [][]string
			server := sshKeyServer(t, tt.existing, &puts)
			defer server.Close()

			if err := newV2TestClient(t, server).AddPublicSSHKey([]byte(tt.add)); err != nil {
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

			err := newV2TestClient(t, server).RemovePublicSSHKey(tt.byName, tt.byPrint)
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
