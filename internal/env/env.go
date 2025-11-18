// Package env contains helpers for loading and merging environment variables from multiple sources.
package env

// Loader is a placeholder for environment and envFiles loader.
// It will merge OS environment, .env files and project-specific files like VERSIONS.
type Loader struct{}

// NewLoader constructs a new Loader instance with default behavior.
func NewLoader() *Loader {
	return &Loader{}
}
