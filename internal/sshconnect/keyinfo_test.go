package sshconnect

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/runpod/runpodctl/internal/api"
	"golang.org/x/crypto/ssh"
)

type fakeLister struct {
	keys []api.SSHKey
	err  error
}

func (f fakeLister) GetPublicSSHKeys() ([]api.SSHKey, error) { return f.keys, f.err }

func TestResolveKeyInfo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".runpod", "ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, defaultKeyName)
	if err := os.WriteFile(keyPath, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath+".pub", ssh.MarshalAuthorizedKey(sshPub), 0o600); err != nil {
		t.Fatal(err)
	}
	fingerprint := ssh.FingerprintSHA256(sshPub)

	yes, no := true, false
	tests := []struct {
		name   string
		client SSHKeyLister
		want   *bool
	}{
		{"registered", fakeLister{keys: []api.SSHKey{{Fingerprint: "other"}, {Fingerprint: fingerprint}}}, &yes},
		{"not registered", fakeLister{keys: []api.SSHKey{{Fingerprint: "other"}}}, &no},
		{"lister error omits the check", fakeLister{err: errors.New("boom")}, nil},
		{"nil lister omits the check", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := ResolveKeyInfo(tt.client)
			if !info.Exists || info.Path != keyPath || info.Fingerprint != fingerprint {
				t.Fatalf("info = %+v", info)
			}
			switch {
			case tt.want == nil && info.InAccount != nil:
				t.Errorf("in_account = %v, want omitted", *info.InAccount)
			case tt.want != nil && (info.InAccount == nil || *info.InAccount != *tt.want):
				t.Errorf("in_account = %v, want %v", info.InAccount, *tt.want)
			}
		})
	}
}
