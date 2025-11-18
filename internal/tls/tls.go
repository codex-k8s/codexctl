// Package tls contains helpers for TLS-related resources such as certificates and secrets.
package tls

// Manager is a placeholder for the TLS helper component.
// It will manage host checks, certificate resources and secret caching.
type Manager struct{}

// NewManager constructs a new Manager instance.
func NewManager() *Manager {
	return &Manager{}
}
