package opensandbox

import "github.com/tinkerloft/fleetlift/internal/sandbox"

func init() {
	sandbox.RegisterProvider("opensandbox", func(cfg sandbox.ProviderConfig) (sandbox.AgentProvider, error) {
		return NewProvider(Config{
			Domain:                cfg.OpenSandboxDomain,
			APIKey:                cfg.OpenSandboxAPIKey,
			ExecdAccessToken:      cfg.OpenSandboxExecdAccessToken,
			UseServerProxy:        cfg.OpenSandboxUseServerProxy,
			DefaultTimeoutSeconds: cfg.OpenSandboxDefaultTimeout,
		}), nil
	})
}
