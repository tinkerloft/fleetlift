package opensandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExecdClient_RunCommand_Success(t *testing.T) {
	exitCode := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/command":
			if r.Header.Get(execdAccessTokenHeader) != "tok-abc" {
				t.Errorf("missing or wrong access token header, got %q", r.Header.Get(execdAccessTokenHeader))
			}
			var req execdCommandRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode request: %v", err)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			// Events are raw JSON separated by double newlines (no "data:" prefix).
			fmt.Fprintf(w, "%s\n\n", mustJSON(execdCommandEvent{Type: "init", Text: "cmd-1"}))
			fmt.Fprintf(w, "%s\n\n", mustJSON(execdCommandEvent{Type: "stdout", Text: "hello\n"}))
			fmt.Fprintf(w, "%s\n\n", mustJSON(execdCommandEvent{Type: "stderr", Text: "warn\n"}))
			fmt.Fprintf(w, "%s\n\n", mustJSON(execdCommandEvent{Type: "execution_complete"}))
		case r.Method == http.MethodGet && r.URL.Path == "/command/status/cmd-1":
			if err := json.NewEncoder(w).Encode(commandStatusResponse{ID: "cmd-1", ExitCode: &exitCode}); err != nil {
				t.Logf("encode status: %v", err)
			}
		default:
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewExecdClient(srv.URL, "tok-abc")
	result, err := c.RunCommand(context.Background(), "bash", []string{"-c", "echo hello"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code %d", result.ExitCode)
	}
	if result.Stdout != "hello\n" {
		t.Errorf("stdout %q", result.Stdout)
	}
	if result.Stderr != "warn\n" {
		t.Errorf("stderr %q", result.Stderr)
	}
}

func TestExecdClient_RunCommand_NonZeroExit(t *testing.T) {
	exitCode := 2
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/command":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "%s\n\n", mustJSON(execdCommandEvent{Type: "init", Text: "cmd-2"}))
			fmt.Fprintf(w, "%s\n\n", mustJSON(execdCommandEvent{Type: "execution_complete"}))
		case r.Method == http.MethodGet && r.URL.Path == "/command/status/cmd-2":
			if err := json.NewEncoder(w).Encode(commandStatusResponse{ID: "cmd-2", ExitCode: &exitCode}); err != nil {
				t.Logf("encode status: %v", err)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewExecdClient(srv.URL, "tok-abc")
	result, err := c.RunCommand(context.Background(), "bash", []string{"-c", "exit 2"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 2 {
		t.Errorf("want exit code 2, got %d", result.ExitCode)
	}
}

func TestExecdClient_WriteFile(t *testing.T) {
	var gotPath, gotContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/files/upload" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		// Parse metadata field (sent as a file attachment via CreateFormFile).
		metaFile, _, err := r.FormFile("metadata")
		if err != nil {
			t.Fatalf("form file metadata: %v", err)
		}
		defer metaFile.Close()
		metaBytes, err := io.ReadAll(metaFile)
		if err != nil {
			t.Fatalf("read metadata: %v", err)
		}
		var meta fileMetadata
		if err := json.Unmarshal(metaBytes, &meta); err != nil {
			t.Fatalf("decode metadata: %v", err)
		}
		gotPath = meta.Path
		// Parse file field.
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		gotContent = string(data)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewExecdClient(srv.URL, "tok-abc")
	if err := c.WriteFile(context.Background(), "/workspace/.fleetlift/manifest.json", []byte(`{"task_id":"t1"}`)); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/workspace/.fleetlift/manifest.json" {
		t.Errorf("path %q", gotPath)
	}
	if gotContent != `{"task_id":"t1"}` {
		t.Errorf("content %q", gotContent)
	}
}

func TestExecdClient_ReadFile_Exists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/download" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("path") != "/workspace/.fleetlift/status.json" {
			t.Errorf("wrong path query: %s", r.URL.RawQuery)
		}
		if _, err := w.Write([]byte(`{"phase":"executing"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewExecdClient(srv.URL, "tok-abc")
	data, err := c.ReadFile(context.Background(), "/workspace/.fleetlift/status.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"phase":"executing"}` {
		t.Errorf("data %q", data)
	}
}

func TestExecdClient_ReadFile_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewExecdClient(srv.URL, "tok-abc")
	data, err := c.ReadFile(context.Background(), "/no/such/file")
	if err != nil {
		t.Fatal(err)
	}
	if data != nil {
		t.Errorf("expected nil for missing file, got %q", data)
	}
}

func TestExecdClient_MakeDir(t *testing.T) {
	var gotBody map[string]permission
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/directories" {
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Errorf("decode body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewExecdClient(srv.URL, "tok-abc")
	if err := c.MakeDir(context.Background(), "/workspace/.fleetlift"); err != nil {
		t.Fatal(err)
	}
	if _, ok := gotBody["/workspace/.fleetlift"]; !ok {
		t.Errorf("expected path key in body, got %v", gotBody)
	}
}

// mustJSON is a test helper to marshal to JSON string.
func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

