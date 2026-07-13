package model

import (
	"testing"

	"github.com/runpod/runpodctl/api"
)

func TestModelVersionHash(t *testing.T) {
	tests := []struct {
		name  string
		model *api.Model
		want  string
	}{
		{
			name: "first hash",
			model: &api.Model{Versions: []*api.ModelVersion{
				{Hash: ""},
				{Hash: "  "},
				{Hash: "hash-1"},
				{Hash: "hash-2"},
			}},
			want: "hash-1",
		},
		{
			name: "skips nil version entries",
			model: &api.Model{Versions: []*api.ModelVersion{
				nil,
				{Hash: "hash-after-nil"},
			}},
			want: "hash-after-nil",
		},
		{
			name: "all hashes blank or whitespace",
			model: &api.Model{Versions: []*api.ModelVersion{
				{Hash: ""},
				{Hash: "   "},
				{Hash: "\t\n"},
			}},
			want: "",
		},
		{
			name:  "no versions",
			model: &api.Model{},
			want:  "",
		},
		{
			name:  "nil model",
			model: nil,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := modelVersionHash(tt.model); got != tt.want {
				t.Fatalf("modelVersionHash() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModelCommandFlags(t *testing.T) {
	if addCmd.Flags().Lookup("owner") == nil {
		t.Fatal("expected model add --owner flag")
	}
	if addCmd.Flags().Lookup("wait-for-hash") == nil {
		t.Fatal("expected model add --wait-for-hash flag")
	}
	if addCmd.Flags().Lookup("hash-timeout") == nil {
		t.Fatal("expected model add --hash-timeout flag")
	}
	if addCmd.Flags().Lookup("verbose") == nil {
		t.Fatal("expected model add --verbose flag")
	}
	if addCmd.Flags().Lookup("version-status") != nil {
		t.Fatal("did not expect model add --version-status flag")
	}
	if listCmd.Flags().Lookup("all") != nil {
		t.Fatal("did not expect model list --all flag")
	}
}
