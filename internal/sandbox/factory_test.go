package sandbox_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tinkerloft/fleetlift/internal/sandbox"
	_ "github.com/tinkerloft/fleetlift/internal/sandbox/docker"
	_ "github.com/tinkerloft/fleetlift/internal/sandbox/k8s"
)

func TestNewProvider_DockerAliases(t *testing.T) {
	tests := []struct {
		name         string
		providerName string
	}{
		{"empty defaults to docker", ""},
		{"explicit docker", "docker"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := sandbox.NewProvider(tt.providerName, sandbox.ProviderConfig{})
			assert.NoError(t, err)
			assert.Equal(t, "docker", provider.Name())
		})
	}
}

func TestNewProvider_KubernetesAliases(t *testing.T) {
	tests := []string{"k8s", "kubernetes"}

	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := sandbox.NewProvider(name, sandbox.ProviderConfig{})
			// Will error without kubeconfig, but shouldn't be "not registered"
			if err != nil {
				assert.NotContains(t, err.Error(), "not registered")
			}
		})
	}
}

func TestNewProvider_Unknown(t *testing.T) {
	_, err := sandbox.NewProvider("unknown", sandbox.ProviderConfig{})
	assert.ErrorContains(t, err, "unknown sandbox provider")
}
