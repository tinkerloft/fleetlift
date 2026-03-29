package sandbox

import "context"

// Client is the interface for sandbox operations (create, exec, file I/O, lifecycle).
type Client interface {
	Create(ctx context.Context, opts CreateOpts) (string, error) // returns sandbox ID
	ExecStream(ctx context.Context, id, cmd, workDir string, onLine func(string)) error
	Exec(ctx context.Context, id, cmd, workDir string) (stdout, stderr string, err error)
	WriteFile(ctx context.Context, id, path, content string) error
	WriteBytes(ctx context.Context, id, path string, data []byte) error
	ReadFile(ctx context.Context, id, path string) (string, error)
	ReadBytes(ctx context.Context, id, path string) ([]byte, error)
	Kill(ctx context.Context, id string) error
	RenewExpiration(ctx context.Context, id string) error
}

// CreateOpts configures sandbox creation.
type CreateOpts struct {
	Image         string
	Env           map[string]string
	TimeoutMins   int
	Resources     *ResourceLimits // nil = provider defaults
	NetworkPolicy *NetworkPolicy  // nil = no egress restrictions
}

// ResourceLimits specifies CPU and memory for a sandbox container.
type ResourceLimits struct {
	CPU    string // Kubernetes-style, e.g. "1000m", "2"
	Memory string // Kubernetes-style, e.g. "2Gi", "512Mi"
}

// NetworkPolicy controls sandbox egress network access.
type NetworkPolicy struct {
	DefaultAction string        // "allow" or "deny"
	Egress        []NetworkRule // evaluated first-match-wins
}

// NetworkRule is a single egress allow/deny entry.
type NetworkRule struct {
	Action string // "allow" or "deny"
	Target string // FQDN, wildcard (*.example.com), or IP/CIDR
}
