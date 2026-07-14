package model

import (
	"testing"

	"github.com/runpod/runpodctl/api"
)

func TestFindModelVersion(t *testing.T) {
	model := &api.Model{
		Versions: []*api.ModelVersion{
			nil,
			{UUID: "version-uuid", Hash: "version-hash"},
		},
	}

	tests := []struct {
		name    string
		hash    string
		version string
		want    string
	}{
		{name: "hash", hash: "version-hash", want: "version-uuid"},
		{name: "uuid", version: "version-uuid", want: "version-uuid"},
		{name: "missing", hash: "missing", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findModelVersion(model, tt.hash, tt.version)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("expected nil version, got %#v", got)
				}
				return
			}
			if got == nil || got.UUID != tt.want {
				t.Fatalf("expected version %q, got %#v", tt.want, got)
			}
		})
	}
}

func TestRemoveModelVersionUpdatesTargetVersion(t *testing.T) {
	tests := []struct {
		name     string
		model    *api.Model
		hash     string
		version  string
		wantHash string
		wantUUID string
	}{
		{
			name: "hash uses target hash",
			model: &api.Model{
				Owner: "owner",
				Name:  "name",
				Versions: []*api.ModelVersion{
					{UUID: "version-uuid", Hash: "version-hash"},
				},
			},
			hash:     "version-hash",
			wantHash: "version-hash",
		},
		{
			name: "version uses target uuid",
			model: &api.Model{
				Owner: "owner",
				Name:  "name",
				Versions: []*api.ModelVersion{
					{UUID: "version-uuid", Hash: "version-hash"},
				},
			},
			version:  "version-uuid",
			wantUUID: "version-uuid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalGet := getModelForRemove
			originalUpdate := updateModelVersionStatusByIdentifier
			t.Cleanup(func() {
				getModelForRemove = originalGet
				updateModelVersionStatusByIdentifier = originalUpdate
			})

			getModelForRemove = func(input *api.GetModelInput) (*api.Model, error) {
				if input.Owner != "owner" || input.Name != "name" {
					t.Fatalf("unexpected get input: %#v", input)
				}
				return tt.model, nil
			}

			updateModelVersionStatusByIdentifier = func(input *api.UpdateModelVersionStatusInput) (*api.ModelVersion, error) {
				if input.Hash != tt.wantHash || input.UUID != tt.wantUUID || input.Status != api.ModelVersionStatusPodRemoved {
					t.Fatalf("unexpected update input: %#v", input)
				}
				return &api.ModelVersion{UUID: "version-uuid", Hash: "version-hash", Status: api.ModelVersionStatusPodRemoved}, nil
			}

			result, err := removeModelVersion("owner", "name", tt.hash, tt.version)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.Success || result.Version == nil || result.Version.Status != api.ModelVersionStatusPodRemoved {
				t.Fatalf("unexpected result: %#v", result)
			}
			if result.Model == nil || len(result.Model.Versions) != 1 || result.Model.Versions[0].Status != api.ModelVersionStatusPodRemoved {
				t.Fatalf("expected returned model version status to be patched, got %#v", result.Model)
			}
		})
	}
}
