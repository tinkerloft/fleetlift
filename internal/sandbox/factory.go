package sandbox

import (
	"fmt"
)

// ProviderConfig contains configuration for provider construction.
type ProviderConfig struct {
	Namespace      string // K8s namespace (default: sandbox-isolated)
	AgentImage     string // K8s agent init container image
	KubeconfigPath string // K8s kubeconfig path (empty = in-cluster)
}

// ProviderFactory is a function that creates an AgentProvider.
// This allows the factory to be extended without import cycles.
type ProviderFactory func(cfg ProviderConfig) (AgentProvider, error)

// providerFactories maps provider names to their factory functions.
var providerFactories = map[string]ProviderFactory{}

// RegisterProvider registers a provider factory by name.
func RegisterProvider(name string, factory ProviderFactory) {
	providerFactories[name] = factory
}

// NewProvider creates an AgentProvider based on the provider name.
// Empty string or "docker" selects the Docker provider.
// "kubernetes" or "k8s" selects the Kubernetes provider.
func NewProvider(providerName string, cfg ProviderConfig) (AgentProvider, error) {
	var registryKey string
	switch providerName {
	case "", "docker":
		registryKey = "docker"
	case "kubernetes", "k8s":
		registryKey = "kubernetes"
	default:
		return nil, fmt.Errorf("unknown sandbox provider: %q", providerName)
	}

	factory, ok := providerFactories[registryKey]
	if !ok {
		return nil, fmt.Errorf("%s provider not registered", registryKey)
	}
	return factory(cfg)
}
