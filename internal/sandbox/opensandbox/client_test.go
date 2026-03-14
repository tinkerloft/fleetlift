package opensandbox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
	"github.com/tinkerloft/fleetlift/internal/sandbox/opensandbox"
)

func TestClientRoundTrip(t *testing.T) {
	domain := os.Getenv("OPENSANDBOX_DOMAIN")
	if domain == "" {
		t.Skip("OPENSANDBOX_DOMAIN not set")
	}
	apiKey := os.Getenv("OPENSANDBOX_API_KEY")

	ctx := context.Background()
	c := opensandbox.New(domain, apiKey)

	id, err := c.Create(ctx, sandbox.CreateOpts{
		Image: "ubuntu:22.04",
		Env:   map[string]string{"TEST": "1"},
	})
	require.NoError(t, err)
	defer c.Kill(ctx, id) //nolint:errcheck

	// Exec
	stdout, _, err := c.Exec(ctx, id, "echo hello", "/")
	require.NoError(t, err)
	assert.Contains(t, stdout, "hello")

	// WriteFile + ReadFile
	err = c.WriteFile(ctx, id, "/tmp/test.txt", "content")
	require.NoError(t, err)

	content, err := c.ReadFile(ctx, id, "/tmp/test.txt")
	require.NoError(t, err)
	assert.Equal(t, "content", content)

	// ReadBytes
	b, err := c.ReadBytes(ctx, id, "/tmp/test.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("content"), b)

	// RenewExpiration
	err = c.RenewExpiration(ctx, id)
	require.NoError(t, err)
}

// newTestClient creates a sandbox client from env vars, skipping if not set.
func newTestClient(t *testing.T) *opensandbox.Client {
	t.Helper()
	domain := os.Getenv("OPENSANDBOX_DOMAIN")
	if domain == "" {
		t.Skip("OPENSANDBOX_DOMAIN not set")
	}
	return opensandbox.New(domain, os.Getenv("OPENSANDBOX_API_KEY"))
}

// createTestSandbox creates a sandbox for testing and registers cleanup.
func createTestSandbox(t *testing.T, c *opensandbox.Client) string {
	t.Helper()
	ctx := context.Background()
	id, err := c.Create(ctx, sandbox.CreateOpts{
		Image: "ubuntu:22.04",
		Env:   map[string]string{"TEST": "1"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Kill(ctx, id) })
	return id
}

func TestExecStream_IncrementalDelivery(t *testing.T) {
	c := newTestClient(t)
	id := createTestSandbox(t, c)
	ctx := context.Background()

	var timestamps []time.Time
	var lines []string

	err := c.ExecStream(ctx, id,
		`for i in 1 2 3; do echo "line-$i"; sleep 1; done`, "/",
		func(line string) {
			timestamps = append(timestamps, time.Now())
			lines = append(lines, line)
		},
	)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(lines), 3, "expected at least 3 lines, got %d: %v", len(lines), lines)

	// Verify incremental delivery: at least two consecutive callbacks should
	// have a gap > 500ms (the command sleeps 1s between each echo).
	foundGap := false
	for i := 1; i < len(timestamps); i++ {
		if timestamps[i].Sub(timestamps[i-1]) > 500*time.Millisecond {
			foundGap = true
			break
		}
	}
	assert.True(t, foundGap, "no gap > 500ms between consecutive callbacks — ExecStream may be buffering. Timestamps: %v", timestamps)
}

func TestExec_StdoutStderrSeparation(t *testing.T) {
	c := newTestClient(t)
	id := createTestSandbox(t, c)
	ctx := context.Background()

	stdout, stderr, err := c.Exec(ctx, id, `echo out && echo err >&2`, "/")
	require.NoError(t, err)
	assert.Contains(t, stdout, "out")
	assert.Contains(t, stderr, "err")
}

func TestExec_NonZeroExit(t *testing.T) {
	c := newTestClient(t)
	id := createTestSandbox(t, c)
	ctx := context.Background()

	// Document current behavior: ExecStream may not surface non-zero exit codes.
	// The ShellRunner works around this with an exit code sentinel.
	stdout, stderr, err := c.Exec(ctx, id, "exit 1", "/")
	t.Logf("exit 1 result: stdout=%q stderr=%q err=%v", stdout, stderr, err)
	// We don't assert on specific behavior — this test documents what happens.
}

func TestWriteReadFile_EdgeCases(t *testing.T) {
	c := newTestClient(t)
	id := createTestSandbox(t, c)
	ctx := context.Background()

	t.Run("empty file", func(t *testing.T) {
		err := c.WriteFile(ctx, id, "/tmp/empty.txt", "")
		require.NoError(t, err)
		content, err := c.ReadFile(ctx, id, "/tmp/empty.txt")
		require.NoError(t, err)
		assert.Equal(t, "", content)
	})

	t.Run("special characters", func(t *testing.T) {
		special := "line1\nline2\ttab\n日本語テスト\n"
		err := c.WriteFile(ctx, id, "/tmp/special.txt", special)
		require.NoError(t, err)
		content, err := c.ReadFile(ctx, id, "/tmp/special.txt")
		require.NoError(t, err)
		assert.Equal(t, special, content)
	})

	t.Run("large file", func(t *testing.T) {
		large := strings.Repeat("abcdefghij", 100_000)
		err := c.WriteFile(ctx, id, "/tmp/large.txt", large)
		require.NoError(t, err)
		content, err := c.ReadFile(ctx, id, "/tmp/large.txt")
		require.NoError(t, err)
		assert.Equal(t, len(large), len(content))
	})
}

func TestKill_RejectsSubsequentOps(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	id, err := c.Create(ctx, sandbox.CreateOpts{
		Image: "ubuntu:22.04",
	})
	require.NoError(t, err)

	// Verify sandbox works
	stdout, _, err := c.Exec(ctx, id, "echo alive", "/")
	require.NoError(t, err)
	assert.Contains(t, stdout, "alive")

	// Kill it
	err = c.Kill(ctx, id)
	require.NoError(t, err)

	// Subsequent ops should fail
	_, _, err = c.Exec(ctx, id, "echo dead", "/")
	assert.Error(t, err, "expected error when executing on killed sandbox")
}

func TestCreate_EmptySandboxIDError(t *testing.T) {
	// Server returns 200 but with empty ID
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       "",
			"metadata": map[string]string{},
		})
	}))
	defer ts.Close()

	client := opensandbox.New(ts.URL, "test-key")
	_, err := client.Create(context.Background(), sandbox.CreateOpts{
		Image:       "test:latest",
		TimeoutMins: 5,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing sandbox ID")
}
