package sandbox_test

import (
	"testing"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
	_ "github.com/tinkerloft/fleetlift/internal/sandbox/opensandbox"
)

func TestNewProvider_OpenSandbox(t *testing.T) {
	p, err := sandbox.NewProvider(sandbox.ProviderConfig{
		OpenSandboxDomain: "http://localhost:8080",
		OpenSandboxAPIKey: "key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "opensandbox" {
		t.Errorf("Name() = %q, want opensandbox", p.Name())
	}
}

func TestNewProvider_MissingDomain(t *testing.T) {
	_, err := sandbox.NewProvider(sandbox.ProviderConfig{
		OpenSandboxAPIKey: "key",
		// Domain intentionally empty
	})
	if err == nil {
		t.Error("expected error for missing domain")
	}
}
