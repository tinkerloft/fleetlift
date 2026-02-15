package docker

import "github.com/tinkerloft/fleetlift/internal/sandbox"

func init() {
	sandbox.RegisterProvider("docker", func(_ sandbox.ProviderConfig) (sandbox.AgentProvider, error) {
		return NewProvider()
	})
}
