package cli

import "github.com/spf13/cobra"

// newStatusCommand creates the "status" subcommand that shows deployment status.
func newStatusCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status of core deployments and services",
		RunE:  notImplemented,
	}

	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to inspect (dev, staging, ai)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace override for resources")
	cmd.Flags().Bool("watch", false, "Watch status changes continuously")

	return cmd
}
