package opensandbox_test

import (
	"context"
	"os"
	"testing"

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
