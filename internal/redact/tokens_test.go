package redact

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// fake credentials, not real ones.
const ( //nolint:gosec // G101: fake credential fixtures
	fakeRPAKey     = "rpa_ABCDEF0123456789abcdef-_XYZ"
	fakeBearer     = "eyJhbGciOiJIUzI1NiJ9.payload.sig-nature"
	fakeConfigured = "cfgkey-0123456789-abcdef"
)

func isolateAPIKey(t *testing.T, key string) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("RUNPOD_API_KEY", key)
}

func TestTextMasksTokens(t *testing.T) {
	isolateAPIKey(t, fakeConfigured)
	cases := map[string]struct {
		in, want string
		gone     []string
	}{
		"authorization header with rpa key": {
			in:   "request failed\nAuthorization: Bearer " + fakeRPAKey + "\nHost: api.runpod.io",
			want: "request failed\nAuthorization: Bearer [REDACTED]\nHost: api.runpod.io",
			gone: []string{fakeRPAKey, "ABCDEF"},
		},
		"bare bearer": {
			in:   "invalid token Bearer " + fakeBearer + " rejected",
			want: "invalid token Bearer [REDACTED] rejected",
			gone: []string{fakeBearer, "payload"},
		},
		"bearer case variants": {
			in:   "bearer abc123 BEARER def456 BeArEr\tghi789",
			want: "Bearer [REDACTED] Bearer [REDACTED] Bearer [REDACTED]",
			gone: []string{"abc123", "def456", "ghi789"},
		},
		"bearer in json stops at the quote": {
			in:   `{"headers":{"authorization":"Bearer ` + fakeBearer + `"},"ok":false}`,
			want: `{"headers":{"authorization":"Bearer [REDACTED]"},"ok":false}`,
			gone: []string{fakeBearer},
		},
		"bare rpa key": {
			in:   "api key " + fakeRPAKey + " is invalid",
			want: "api key rpa_[REDACTED] is invalid",
			gone: []string{fakeRPAKey, "ABCDEF"},
		},
		"rpa key in json": {
			in:   `{"error":"bad key ` + fakeRPAKey + `","code":401}`,
			want: `{"error":"bad key rpa_[REDACTED]","code":401}`,
			gone: []string{fakeRPAKey},
		},
		"configured key in the middle of json": {
			in:   `{"error":"denied for ` + fakeConfigured + ` here","code":403}`,
			want: `{"error":"denied for [REDACTED] here","code":403}`,
			gone: []string{fakeConfigured, "0123456789"},
		},
		"configured key repeated": {
			in:   fakeConfigured + "/" + fakeConfigured,
			want: "[REDACTED]/[REDACTED]",
			gone: []string{fakeConfigured},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := Text(tc.in)
			if got != tc.want {
				t.Errorf("Text(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
			for _, secret := range tc.gone {
				if strings.Contains(got, secret) {
					t.Errorf("output still contains %q: %q", secret, got)
				}
			}
		})
	}
}

func TestTextMasksConfiguredRPAKey(t *testing.T) {
	isolateAPIKey(t, fakeRPAKey)
	got := Text("key " + fakeRPAKey + " and rpa_other_KEY123")
	if got != "key [REDACTED] and rpa_[REDACTED]" {
		t.Errorf("got %q", got)
	}
}

func TestTextConfiguredKeyMaskedFromConfigFile(t *testing.T) {
	isolateAPIKey(t, "")
	viper.Set("apiKey", fakeConfigured)
	if got := Text("denied " + fakeConfigured); got != "denied [REDACTED]" {
		t.Errorf("got %q", got)
	}
}

func TestTextShortConfiguredKeyIsNotMasked(t *testing.T) {
	isolateAPIKey(t, "abc")
	in := "abc is part of the abcdef alphabet"
	if got := Text(in); got != in {
		t.Errorf("a key shorter than %d bytes must not be masked, got %q", minAPIKeyMaskLen, got)
	}
	isolateAPIKey(t, "1234567")
	if got := Text("id 1234567"); got != "id 1234567" {
		t.Errorf("7-byte key must not be masked, got %q", got)
	}
	isolateAPIKey(t, "12345678")
	if got := Text("id 12345678"); got != "id [REDACTED]" {
		t.Errorf("8-byte key must be masked, got %q", got)
	}
}

func TestTextLeavesOrdinaryTextAlone(t *testing.T) {
	isolateAPIKey(t, fakeConfigured)
	for _, in := range []string{
		"",
		"no secrets here",
		`{"errors":[{"message":"pod not found","path":["pod"]}]}`,
		"statuscode 502: bad gateway from https://api.runpod.io/graphql",
		"the bearerless header and a carrier rpa are fine",
		"my_rpa_ other rpa_",
		"Bearer",
		"unauthorized: missing Authorization header",
	} {
		if got := Text(in); got != in {
			t.Errorf("Text(%q) changed ordinary text to %q", in, got)
		}
	}
}

func TestErrorMasksTokens(t *testing.T) {
	isolateAPIKey(t, fakeConfigured)
	err := Error(&wrapped{msg: "server said Authorization: Bearer " + fakeBearer + " key " + fakeRPAKey + " " + fakeConfigured})
	for _, secret := range []string{fakeBearer, fakeRPAKey, fakeConfigured} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error still contains %q: %s", secret, err)
		}
	}
}

type wrapped struct{ msg string }

func (e *wrapped) Error() string { return e.msg }

// TestTextTokensStraddlingACut pins that a token is masked before any later cut
// at a fixed byte offset, so a cut inside the original token cannot leave a
// prefix of it behind. The padding is computed from the lengths so the cut
// (byte 4096 of the raw text) falls in the middle of each token.
func TestTextTokensStraddlingACut(t *testing.T) {
	const cut = 4096
	isolateAPIKey(t, fakeConfigured)
	for name, tc := range map[string]struct{ prefix, token string }{
		"bearer":     {"Authorization: Bearer ", fakeBearer},
		"rpa key":    {"key=", fakeRPAKey},
		"config key": {"key=", fakeConfigured},
	} {
		t.Run(name, func(t *testing.T) {
			padding := cut - len(tc.prefix) - len(tc.token)/2
			raw := strings.Repeat("a", padding) + tc.prefix + tc.token + strings.Repeat("b", 100)
			start := bytes.Index([]byte(raw), []byte(tc.token))
			if start >= cut || start+len(tc.token) <= cut {
				t.Fatalf("test premise: token [%d,%d) must cross the cut at %d", start, start+len(tc.token), cut)
			}
			got := Text(raw)
			if len(got) > cut {
				got = got[:cut]
			}
			for _, fragment := range []string{tc.token[:6], tc.token[len(tc.token)-6:], tc.token[:len(tc.token)/2]} {
				if strings.Contains(got, fragment) {
					t.Errorf("cut text leaks token fragment %q: %q", fragment, got[len(got)-60:])
				}
			}
			if strings.Count(Text(raw), "REDACTED") != 1 {
				t.Errorf("expected exactly one mask in the output")
			}
		})
	}
}
