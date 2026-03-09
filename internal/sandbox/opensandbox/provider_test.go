package opensandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	fleetproto "github.com/tinkerloft/fleetlift/internal/agent/fleetproto"
	"github.com/tinkerloft/fleetlift/internal/sandbox"
)

// newTestServers returns wired lifecycle + execd test servers and a provider pointing at them.
// The caller must call close() when done.
func newTestServers(t *testing.T) (*Provider, func()) {
	t.Helper()

	exitCode := 0
	execdSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/directories":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/files/upload":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/files/download":
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/command":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "%s\n\n", mustJSON(execdCommandEvent{Type: "init", Text: "cmd-x"}))
			fmt.Fprintf(w, "%s\n\n", mustJSON(execdCommandEvent{Type: "execution_complete"}))
		case r.Method == http.MethodGet && r.URL.Path == "/command/status/cmd-x":
			if err := json.NewEncoder(w).Encode(commandStatusResponse{ID: "cmd-x", ExitCode: &exitCode}); err != nil {
				t.Logf("encode status: %v", err)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	lifecycleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sandboxes":
			if err := json.NewEncoder(w).Encode(SandboxResponse{
				ID:     "sb-test",
				Status: sandboxStatusField{State: "Running"},
			}); err != nil {
				t.Logf("encode response: %v", err)
			}
		case r.URL.Path == "/v1/sandboxes/sb-test/endpoints/44772":
			if err := json.NewEncoder(w).Encode(EndpointResponse{Endpoint: execdSrv.URL}); err != nil {
				t.Logf("encode response: %v", err)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/sandboxes/sb-test":
			if err := json.NewEncoder(w).Encode(SandboxResponse{
				ID:     "sb-test",
				Status: sandboxStatusField{State: "Running"},
			}); err != nil {
				t.Logf("encode response: %v", err)
			}
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/sandboxes/sb-test":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	p := NewProvider(Config{
		Domain:         lifecycleSrv.URL,
		APIKey:         "test-key",
		UseServerProxy: false,
	})
	return p, func() {
		lifecycleSrv.Close()
		execdSrv.Close()
	}
}

func TestProvider_Name(t *testing.T) {
	p, close := newTestServers(t)
	defer close()
	if p.Name() != "opensandbox" {
		t.Errorf("Name() = %q, want opensandbox", p.Name())
	}
}

func TestProvider_Provision(t *testing.T) {
	p, close := newTestServers(t)
	defer close()

	sb, err := p.Provision(context.Background(), sandbox.ProvisionOptions{
		TaskID: "task-1",
		Image:  "myimage:latest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sb.ID != "sb-test" {
		t.Errorf("ID = %q, want sb-test", sb.ID)
	}
	if sb.Provider != "opensandbox" {
		t.Errorf("Provider = %q", sb.Provider)
	}
}

func TestProvider_Status(t *testing.T) {
	p, close := newTestServers(t)
	defer close()

	_, err := p.Provision(context.Background(), sandbox.ProvisionOptions{TaskID: "t1", Image: "img:1"})
	if err != nil {
		t.Fatal(err)
	}
	status, err := p.Status(context.Background(), "sb-test")
	if err != nil {
		t.Fatal(err)
	}
	if status.Phase != sandbox.SandboxPhaseRunning {
		t.Errorf("phase %q, want running", status.Phase)
	}
}

func TestProvider_Cleanup(t *testing.T) {
	p, close := newTestServers(t)
	defer close()

	_, err := p.Provision(context.Background(), sandbox.ProvisionOptions{TaskID: "t1", Image: "img:1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Cleanup(context.Background(), "sb-test"); err != nil {
		t.Fatal(err)
	}
}

func TestProvider_AgentProtocol(t *testing.T) {
	var writtenFiles = map[string]string{}

	exitCode := 0
	execdSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/directories":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/files/upload":
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				t.Logf("parse multipart: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			// Metadata is sent as a file attachment via CreateFormFile.
			metaFile, _, err := r.FormFile("metadata")
			if err != nil {
				t.Logf("form file metadata: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			defer metaFile.Close()
			metaBytes, err := io.ReadAll(metaFile)
			if err != nil {
				t.Logf("read metadata: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var meta fileMetadata
			if err := json.Unmarshal(metaBytes, &meta); err != nil {
				t.Logf("decode metadata: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Logf("form file: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil {
				t.Logf("read file: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			writtenFiles[meta.Path] = string(data)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/files/download":
			path := r.URL.Query().Get("path")
			switch path {
			case "/workspace/.fleetlift/status.json":
				if _, err := w.Write([]byte(`{"phase":"executing"}`)); err != nil {
					t.Logf("write response: %v", err)
				}
			case "/workspace/.fleetlift/result.json":
				if _, err := w.Write([]byte(`{"status":"complete"}`)); err != nil {
					t.Logf("write response: %v", err)
				}
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/command":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "%s\n\n", mustJSON(execdCommandEvent{Type: "init", Text: "cmd-y"}))
			fmt.Fprintf(w, "%s\n\n", mustJSON(execdCommandEvent{Type: "execution_complete"}))
		case r.Method == http.MethodGet && r.URL.Path == "/command/status/cmd-y":
			if err := json.NewEncoder(w).Encode(commandStatusResponse{ID: "cmd-y", ExitCode: &exitCode}); err != nil {
				t.Logf("encode status: %v", err)
			}
		}
	}))
	defer execdSrv.Close()

	lifecycleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sandboxes":
			if err := json.NewEncoder(w).Encode(SandboxResponse{
				ID:     "sb-x",
				Status: sandboxStatusField{State: "Running"},
			}); err != nil {
				t.Logf("encode response: %v", err)
			}
		case r.URL.Path == "/v1/sandboxes/sb-x/endpoints/44772":
			if err := json.NewEncoder(w).Encode(EndpointResponse{Endpoint: execdSrv.URL}); err != nil {
				t.Logf("encode response: %v", err)
			}
		}
	}))
	defer lifecycleSrv.Close()

	p := NewProvider(Config{Domain: lifecycleSrv.URL, APIKey: "k"})
	_, err := p.Provision(context.Background(), sandbox.ProvisionOptions{TaskID: "t", Image: "i"})
	if err != nil {
		t.Fatal(err)
	}

	if err := p.SubmitManifest(context.Background(), "sb-x", []byte(`{"task_id":"t"}`)); err != nil {
		t.Fatalf("SubmitManifest: %v", err)
	}
	if writtenFiles["/workspace/.fleetlift/manifest.json"] != `{"task_id":"t"}` {
		t.Errorf("manifest not written correctly, got %q", writtenFiles["/workspace/.fleetlift/manifest.json"])
	}

	raw, err := p.PollStatus(context.Background(), "sb-x")
	if err != nil {
		t.Fatalf("PollStatus: %v", err)
	}
	var agentStatus fleetproto.AgentStatus
	if err := json.Unmarshal(raw, &agentStatus); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if string(agentStatus.Phase) != "executing" {
		t.Errorf("phase %q, want executing", agentStatus.Phase)
	}

	result, err := p.ReadResult(context.Background(), "sb-x")
	if err != nil {
		t.Fatalf("ReadResult: %v", err)
	}
	if string(result) != `{"status":"complete"}` {
		t.Errorf("result %q", result)
	}
}
