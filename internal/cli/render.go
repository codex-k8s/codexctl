package cli

import "github.com/spf13/cobra"

// newRenderCommand creates the "render" subcommand that renders manifests from services.yaml.
func newRenderCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render Kubernetes manifests from services.yaml",
		RunE:  notImplemented,
	}

	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to render (dev, staging, ai)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace override for rendered manifests")
	cmd.Flags().StringP("output", "o", "", "Output directory for rendered manifests (if empty, prints to stdout)")
	cmd.Flags().Bool("stdout", false, "Force output to stdout instead of files")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")

	return cmd
}
