// Package hooks contains the hook execution layer used by codexctl.
package hooks

// Executor is a placeholder for the hook execution engine.
// It will know how to run shell commands and built-in hook types.
type Executor struct{}

// NewExecutor constructs a new Executor instance with default configuration.
func NewExecutor() *Executor {
	return &Executor{}
}
