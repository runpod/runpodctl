package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListContainerRegistryAuths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/registries" {
			t.Errorf("expected /registries, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string][]ContainerRegistryAuth{"registries": {
			{ID: "reg-1", Name: "dockerhub"},
			{ID: "reg-2", Name: "gcr"},
		}})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.v2BaseURL = server.URL

	auths, err := client.ListContainerRegistryAuths()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auths) != 2 {
		t.Errorf("expected 2 auths, got %d", len(auths))
	}
}

func TestGetContainerRegistryAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/registries/reg-123" {
			t.Errorf("expected /registries/reg-123, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(ContainerRegistryAuth{
			ID:       "reg-123",
			Name:     "my-registry",
			Username: "user",
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.v2BaseURL = server.URL

	auth, err := client.GetContainerRegistryAuth("reg-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth.ID != "reg-123" {
		t.Errorf("expected reg-123, got %s", auth.ID)
	}
}

func TestCreateContainerRegistryAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/registries" {
			t.Errorf("expected POST /registries, got %s %s", r.Method, r.URL.Path)
		}
		var req ContainerRegistryAuthCreateRequest
		json.NewDecoder(r.Body).Decode(&req)
		json.NewEncoder(w).Encode(ContainerRegistryAuth{
			ID:       "new-reg-id",
			Name:     req.Name,
			Username: req.Username,
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.v2BaseURL = server.URL

	auth, err := client.CreateContainerRegistryAuth(&ContainerRegistryAuthCreateRequest{
		Name:     "test-registry",
		Username: "user",
		Password: "pass",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth.ID != "new-reg-id" {
		t.Errorf("expected new-reg-id, got %s", auth.ID)
	}
}

func TestDeleteContainerRegistryAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/registries/reg-123" {
			t.Errorf("expected DELETE /registries/reg-123, got %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.v2BaseURL = server.URL

	err := client.DeleteContainerRegistryAuth("reg-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
