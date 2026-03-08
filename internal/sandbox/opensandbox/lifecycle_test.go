package opensandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLifecycleClient_Create(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sandboxes" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("OPEN-SANDBOX-API-KEY") != "test-key" {
			t.Errorf("missing or wrong API key header")
		}
		var req CreateSandboxRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Image.URI != "myimage:latest" {
			t.Errorf("unexpected image: %s", req.Image.URI)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(SandboxResponse{
			ID:     "sb-123",
			Status: sandboxStatusField{State: "Running"},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewLifecycleClient(srv.URL, "test-key")
	resp, err := c.Create(context.Background(), CreateSandboxRequest{
		Image:      SandboxImage{URI: "myimage:latest"},
		Entrypoint: []string{"sh", "-c", "sleep 3600"},
		Timeout:    3600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != "sb-123" {
		t.Errorf("got ID %q, want sb-123", resp.ID)
	}
	if resp.Status.State != "Running" {
		t.Errorf("got State %q, want Running", resp.Status.State)
	}
}

func TestLifecycleClient_GetEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sandboxes/sb-123/endpoints/44772" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewEncoder(w).Encode(EndpointResponse{Endpoint: "http://proxy:8080/sb-123/44772"}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewLifecycleClient(srv.URL, "test-key")
	url, err := c.GetEndpoint(context.Background(), "sb-123", 44772, true)
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://proxy:8080/sb-123/44772" {
		t.Errorf("got %q", url)
	}
}

func TestLifecycleClient_Delete(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/v1/sandboxes/sb-123" {
			called = true
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	c := NewLifecycleClient(srv.URL, "test-key")
	if err := c.Delete(context.Background(), "sb-123"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("DELETE was not called")
	}
}

func TestLifecycleClient_Get_StateMapping(t *testing.T) {
	cases := []struct {
		state     string
		wantPhase string
	}{
		{"Pending", "pending"},
		{"Running", "running"},
		{"Terminated", "succeeded"},
		{"Failed", "failed"},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewEncoder(w).Encode(SandboxResponse{
				ID:     "sb-1",
				Status: sandboxStatusField{State: tc.state},
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		c := NewLifecycleClient(srv.URL, "test-key")
		resp, err := c.Get(context.Background(), "sb-1")
		if err != nil {
			t.Fatalf("state %q: %v", tc.state, err)
		}
		got := resp.SandboxPhase()
		if string(got) != tc.wantPhase {
			t.Errorf("state %q: got phase %q, want %q", tc.state, got, tc.wantPhase)
		}
		srv.Close()
	}
}
