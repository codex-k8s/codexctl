// Package state defines the backend used to persist environment and slot state.
package state

// Store is a placeholder for the environment and slot state backend.
// It will abstract over ConfigMap or CRD based persistence.
type Store struct{}

// NewStore constructs a new Store instance.
func NewStore() *Store {
	return &Store{}
}
