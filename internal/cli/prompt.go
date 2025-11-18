package cli

import "github.com/spf13/cobra"

// newPromptCommand creates the "prompt" group command for AI prompt operations.
func newPromptCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Work with AI prompts and Codex agents",
	}

	cmd.AddCommand(
		newPromptRenderCommand(opts),
	)

	return cmd
}

// newPromptRenderCommand creates the "prompt render" subcommand that renders prompt templates.
func newPromptRenderCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render an AI prompt template using services.yaml context",
		RunE:  notImplemented,
	}

	cmd.Flags().String("template", "", "Path to prompt template file")
	cmd.Flags().String("out", "", "Output file path for rendered prompt")
	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to use for rendering (dev, staging, ai)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace context for rendering")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")
	_ = cmd.MarkFlagRequired("template")

	return cmd
}
