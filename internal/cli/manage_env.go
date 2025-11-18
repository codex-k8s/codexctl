package cli

import "github.com/spf13/cobra"

// newManageEnvCommand creates the "manage-env" group command for ephemeral environments.
func newManageEnvCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manage-env",
		Short: "Manage ephemeral environments and slots",
	}

	cmd.AddCommand(
		newManageEnvCreateCommand(opts),
		newManageEnvGCCommand(opts),
	)

	return cmd
}

// newManageEnvCreateCommand creates the "manage-env create" subcommand that allocates a slot.
func newManageEnvCreateCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Allocate a new ephemeral environment slot",
		RunE:  notImplemented,
	}

	cmd.Flags().StringVar(&opts.Env, "env", "ai", "Environment type for the slot (default: ai)")
	cmd.Flags().Int("max", 0, "Maximum number of slots (0 means unlimited)")
	cmd.Flags().Int("issue", 0, "GitHub issue number associated with the slot")
	cmd.Flags().Int("pr", 0, "GitHub pull request number associated with the slot")
	cmd.Flags().Int("prefer", 0, "Preferred slot number to reuse if available")

	return cmd
}

// newManageEnvGCCommand creates the "manage-env gc" subcommand that garbage-collects old environments.
func newManageEnvGCCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Garbage-collect stale ephemeral environments",
		RunE:  notImplemented,
	}

	cmd.Flags().StringVar(&opts.Env, "env", "ai", "Environment type to clean (default: ai)")
	cmd.Flags().String("ttl", "", "Time-to-live for environments (e.g. 24h)")

	return cmd
}
