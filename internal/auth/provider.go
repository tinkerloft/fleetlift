package auth

import "context"

// ExternalIdentity represents an authenticated identity from an external provider.
type ExternalIdentity struct {
	ProviderID string
	Email      string
	Name       string
	Provider   string
}

// Provider abstracts an OAuth/SSO identity provider.
type Provider interface {
	Name() string
	AuthURL(state string) string
	Exchange(ctx context.Context, code string) (*ExternalIdentity, error)
}
