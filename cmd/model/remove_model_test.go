package model

import (
	"errors"
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
			originalList := listDependentEndpoints
			t.Cleanup(func() {
				getModelForRemove = originalGet
				updateModelVersionStatusByIdentifier = originalUpdate
				listDependentEndpoints = originalList
			})

			getModelForRemove = func(input *api.GetModelInput) (*api.Model, error) {
				if input.Owner != "owner" || input.Name != "name" {
					t.Fatalf("unexpected get input: %#v", input)
				}
				return tt.model, nil
			}

			listDependentEndpoints = func(owner, name, hash string) ([]DependentEndpoint, error) {
				return nil, nil
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

func TestModelReferenceMatches(t *testing.T) {
	tests := []struct {
		name  string
		ref   string
		owner string
		model string
		hash  string
		want  bool
	}{
		{
			name:  "exact match lowercase scheme",
			ref:   "https://local/owner/name:hash",
			owner: "owner",
			model: "name",
			hash:  "hash",
			want:  true,
		},
		{
			name:  "exact match uppercase scheme+host as persisted by the backend",
			ref:   "https://LOCAL/owner/name:hash",
			owner: "owner",
			model: "name",
			hash:  "hash",
			want:  true,
		},
		{
			name:  "whole-model match ignores hash when hash is empty",
			ref:   "https://LOCAL/owner/name:some-other-hash",
			owner: "owner",
			model: "name",
			hash:  "",
			want:  true,
		},
		{
			name:  "different hash does not match when hash is pinned",
			ref:   "https://LOCAL/owner/name:some-other-hash",
			owner: "owner",
			model: "name",
			hash:  "hash",
			want:  false,
		},
		{
			name:  "different owner does not match",
			ref:   "https://LOCAL/other-owner/name:hash",
			owner: "owner",
			model: "name",
			hash:  "hash",
			want:  false,
		},
		{
			name:  "huggingface passthrough reference never matches (not a model repo entry)",
			ref:   "https://huggingface.co/owner/name:hash",
			owner: "owner",
			model: "name",
			hash:  "hash",
			want:  false,
		},
		{
			name:  "name prefix collision does not match (name vs name-2)",
			ref:   "https://LOCAL/owner/name-2:hash",
			owner: "owner",
			model: "name",
			hash:  "",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := modelReferenceMatches(tt.ref, tt.owner, tt.model, tt.hash)
			if got != tt.want {
				t.Fatalf("modelReferenceMatches(%q, %q, %q, %q) = %v, want %v", tt.ref, tt.owner, tt.model, tt.hash, got, tt.want)
			}
		})
	}
}

func TestCheckDependentEndpoints(t *testing.T) {
	t.Run("no dependents proceeds silently", func(t *testing.T) {
		originalList := listDependentEndpoints
		t.Cleanup(func() { listDependentEndpoints = originalList })
		listDependentEndpoints = func(owner, name, hash string) ([]DependentEndpoint, error) {
			return nil, nil
		}

		if err := checkDependentEndpoints("owner", "name", "hash"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("dependents found always blocks with a coded error and no override", func(t *testing.T) {
		originalList := listDependentEndpoints
		t.Cleanup(func() { listDependentEndpoints = originalList })
		listDependentEndpoints = func(owner, name, hash string) ([]DependentEndpoint, error) {
			return []DependentEndpoint{{ID: "ep-1", Name: "my-endpoint"}}, nil
		}

		err := checkDependentEndpoints("owner", "name", "hash")
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		var coder interface{ ErrorCode() string }
		if !errors.As(err, &coder) {
			t.Fatalf("expected error to implement ErrorCode(), got %#v", err)
		}
		if coder.ErrorCode() != "dependent_endpoints" {
			t.Fatalf("expected code %q, got %q", "dependent_endpoints", coder.ErrorCode())
		}
	})

	t.Run("check failure blocks too — cannot verify safety means cannot proceed", func(t *testing.T) {
		originalList := listDependentEndpoints
		t.Cleanup(func() { listDependentEndpoints = originalList })
		listDependentEndpoints = func(owner, name, hash string) ([]DependentEndpoint, error) {
			return nil, errors.New("boom")
		}

		if err := checkDependentEndpoints("owner", "name", "hash"); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})
}
