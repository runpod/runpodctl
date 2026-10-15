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

	client := newV2TestClient(t, server)

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

	client := newV2TestClient(t, server)

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

	client := newV2TestClient(t, server)

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

	client := newV2TestClient(t, server)

	err := client.DeleteContainerRegistryAuth("reg-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListContainerRegistryAuthsFollowsPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			_, _ = w.Write([]byte(`{"registries":[{"id":"reg-1","name":"a"}],"pagination":{"hasNextPage":true,"nextCursor":"c1"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"registries":[{"id":"reg-2","name":"b"}],"pagination":{"hasNextPage":false}}`))
	}))
	defer server.Close()

	auths, err := newV2TestClient(t, server).ListContainerRegistryAuths()
	if err != nil {
		t.Fatal(err)
	}
	if len(auths) != 2 || auths[1].ID != "reg-2" {
		t.Fatalf("auths = %+v, want both pages", auths)
	}
}
