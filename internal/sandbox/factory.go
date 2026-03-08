package sandbox

import "fmt"

// ProviderConfig contains configuration for the OpenSandbox provider.
type ProviderConfig struct {
	// OpenSandboxDomain is the lifecycle server URL (e.g. "http://localhost:8080").
	OpenSandboxDomain string
	// OpenSandboxAPIKey is the OPEN-SANDBOX-API-KEY.
	OpenSandboxAPIKey string
	// OpenSandboxUseServerProxy routes execd calls through the lifecycle server.
	// Set true when the worker cannot reach sandbox containers directly.
	OpenSandboxUseServerProxy bool
	// OpenSandboxDefaultTimeout is the sandbox TTL in seconds (60–86400). Default: 3600.
	OpenSandboxDefaultTimeout int
	// OpenSandboxExecdAccessToken is sent as X-EXECD-ACCESS-TOKEN to execd.
	// Leave empty if execd runs without authentication (the default).
	OpenSandboxExecdAccessToken string
}

// ProviderFactory is a function that creates an AgentProvider from config.
type ProviderFactory func(cfg ProviderConfig) (AgentProvider, error)

var providerFactory ProviderFactory

// RegisterProvider registers the single provider factory.
// Called by internal/sandbox/opensandbox init().
func RegisterProvider(_ string, factory ProviderFactory) {
	providerFactory = factory
}

// NewProvider creates the AgentProvider from config.
func NewProvider(cfg ProviderConfig) (AgentProvider, error) {
	if cfg.OpenSandboxDomain == "" {
		return nil, fmt.Errorf("OpenSandboxDomain is required")
	}
	if providerFactory == nil {
		return nil, fmt.Errorf("no provider registered (import _ \"github.com/tinkerloft/fleetlift/internal/sandbox/opensandbox\")")
	}
	return providerFactory(cfg)
}
