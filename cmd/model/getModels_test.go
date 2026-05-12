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
