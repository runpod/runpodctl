package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListContainerRegistryAuths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containerregistryauth" {
			t.Errorf("expected /containerregistryauth, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]ContainerRegistryAuth{
			{ID: "reg-1", Name: "dockerhub"},
			{ID: "reg-2", Name: "gcr"},
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

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
		json.NewEncoder(w).Encode(ContainerRegistryAuth{
			ID:       "reg-123",
			Name:     "my-registry",
			Username: "user",
		})
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

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
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
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
	client.baseURL = server.URL

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
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("RUNPOD_API_KEY", "test-key")

	client, _ := NewClient()
	client.baseURL = server.URL

	err := client.DeleteContainerRegistryAuth("reg-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
