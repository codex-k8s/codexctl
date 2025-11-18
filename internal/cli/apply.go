package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/engine"
	"github.com/codex-k8s/codexctl/internal/env"
	"github.com/codex-k8s/codexctl/internal/kube"
)

// newApplyCommand creates the "apply" subcommand that renders and applies manifests to a cluster.
func newApplyCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Render and apply manifests to a Kubernetes cluster",
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

			logger.Info("applying manifests", "env", opts.Env, "namespace", ctxData.Namespace)
			if err := kubeClient.Apply(ctx, manifests); err != nil {
				return err
			}

			wait, _ := cmd.Flags().GetBool("wait")
			if wait {
				if ctxData.Namespace == "" {
					logger.Info("skip wait: namespace is empty, resources may be cluster-scoped or namespaced explicitly in manifests")
				} else {
					logger.Info("waiting for deployments to become Available", "namespace", ctxData.Namespace)
					if err := kubeClient.WaitForDeployments(ctx, ctxData.Namespace, "300s"); err != nil {
						return err
					}
				}
			}

			preflight, _ := cmd.Flags().GetBool("preflight")
			if preflight {
				fmt.Fprintln(os.Stderr, "warn: --preflight flag is acknowledged but detailed checks are not implemented yet")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to apply (dev, staging, ai)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace override for resources")
	cmd.Flags().Bool("wait", false, "Wait for core deployments to become ready")
	cmd.Flags().Bool("preflight", false, "Run preflight checks before applying manifests")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")
	_ = cmd.MarkFlagRequired("env")

	return cmd
}
