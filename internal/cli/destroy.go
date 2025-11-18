package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/engine"
	"github.com/codex-k8s/codexctl/internal/env"
	"github.com/codex-k8s/codexctl/internal/kube"
)

// newDestroyCommand creates the "destroy" subcommand that deletes resources for an environment.
func newDestroyCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Delete resources created from services.yaml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())

			inlineVars, err := env.ParseInlineVars(cmd.Flag("vars").Value.String())
			if err != nil {
				return err
			}

			varFile := cmd.Flag("var-file").Value.String()
			varFiles := []string{}
			if varFile != "" {
				varFiles = append(varFiles, varFile)
			}

			loadOpts := config.LoadOptions{
				Env:       opts.Env,
				Namespace: opts.Namespace,
				UserVars:  inlineVars,
				VarFiles:  varFiles,
			}

			stackCfg, ctxData, err := config.LoadStackConfig(opts.ConfigPath, loadOpts)
			if err != nil {
				return err
			}

			envCfg, err := config.ResolveEnvironment(stackCfg, opts.Env)
			if err != nil {
				return err
			}

			eng := engine.NewEngine()
			manifests, err := eng.RenderStack(stackCfg, ctxData)
			if err != nil {
				return err
			}

			kubeClient := kube.NewClient(envCfg.Kubeconfig, envCfg.Context)

			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
			defer cancel()

			logger.Info("deleting manifests", "env", opts.Env, "namespace", ctxData.Namespace)
			if err := kubeClient.Delete(ctx, manifests, true); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to destroy (dev, staging, ai)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace override for resources")
	cmd.Flags().Bool("yes", false, "Do not prompt for confirmation")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")
	_ = cmd.MarkFlagRequired("env")

	return cmd
}
