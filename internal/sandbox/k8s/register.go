package k8s

import "github.com/tinkerloft/fleetlift/internal/sandbox"

func init() {
	sandbox.RegisterProvider("kubernetes", func(cfg sandbox.ProviderConfig) (sandbox.AgentProvider, error) {
		return NewProvider(cfg)
	})
}
