package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/env"
	"github.com/codex-k8s/codexctl/internal/kube"
)

// newStatusCommand creates the "status" subcommand that shows deployment status.
func newStatusCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status of core deployments and services",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())

			inlineVars, err := env.ParseInlineVars(cmd.Flag("vars").Value.String())
			if err != nil {
				return err
			}

			varFile := cmd.Flag("var-file").Value.String()
			var varFiles []string
			if varFile != "" {
				varFiles = append(varFiles, varFile)
			}

			slot, err := cmd.Flags().GetInt("slot")
			if err != nil {
				return err
			}

			loadOpts := config.LoadOptions{
				Env:       opts.Env,
				Namespace: opts.Namespace,
				Slot:      slot,
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

			kubeClient := kube.NewClient(envCfg.Kubeconfig, envCfg.Context)

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()

			watch, _ := cmd.Flags().GetBool("watch")
			logger.Info("querying status", "env", opts.Env, "namespace", ctxData.Namespace, "watch", watch)
			return kubeClient.Status(ctx, ctxData.Namespace, watch)
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to inspect (dev, staging, ai)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace override for resources")
	cmd.Flags().Bool("watch", false, "Watch status changes continuously")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")
	cmd.Flags().Int("slot", 0, "Slot number for slot-based environments (e.g. ai)")
	_ = cmd.MarkFlagRequired("env")

	return cmd
}
