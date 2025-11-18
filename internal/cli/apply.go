package cli

import "github.com/spf13/cobra"

// newApplyCommand creates the "apply" subcommand that renders and applies manifests to a cluster.
func newApplyCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Render and apply manifests to a Kubernetes cluster",
		RunE:  notImplemented,
	}

	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to apply (dev, staging, ai)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace override for resources")
	cmd.Flags().Bool("wait", false, "Wait for core deployments to become ready")
	cmd.Flags().Bool("preflight", false, "Run preflight checks before applying manifests")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")

	return cmd
}
