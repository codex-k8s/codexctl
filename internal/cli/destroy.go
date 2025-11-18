package cli

import "github.com/spf13/cobra"

// newDestroyCommand creates the "destroy" subcommand that deletes resources for an environment.
func newDestroyCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Delete resources created from services.yaml",
		RunE:  notImplemented,
	}

	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to destroy (dev, staging, ai)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace override for resources")
	cmd.Flags().Bool("yes", false, "Do not prompt for confirmation")

	return cmd
}
