package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/engine"
	"github.com/codex-k8s/codexctl/internal/env"
	"github.com/codex-k8s/codexctl/internal/hooks"
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

			eng := engine.NewEngine()
			manifests, err := eng.RenderStack(stackCfg, ctxData)
			if err != nil {
				return err
			}

			kubeClient := kube.NewClient(envCfg.Kubeconfig, envCfg.Context)

			hookExec := hooks.NewExecutor(logger)
			hookCtx := hooks.StepContext{
				Stack:      stackCfg,
				Template:   ctxData,
				EnvName:    opts.Env,
				KubeClient: kubeClient,
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
			defer cancel()

			preflight, _ := cmd.Flags().GetBool("preflight")
			if preflight {
				logger.Info("running preflight checks before apply", "env", opts.Env)
				if err := hookExec.RunPreflightBasic(ctx); err != nil {
					return err
				}
				if err := runDoctorChecks(ctx, logger, stackCfg, ctxData, envCfg, opts.Env); err != nil {
					return err
				}
			}

			// Stack-level and infrastructure/service hooks before apply.
			if err := hookExec.RunSteps(ctx, stackCfg.Hooks.BeforeAll, hookCtx); err != nil {
				return err
			}
			for _, infra := range stackCfg.Infrastructure {
				if err := hookExec.RunSteps(ctx, infra.Hooks.BeforeApply, hookCtx); err != nil {
					return err
				}
			}
			for _, svc := range stackCfg.Services {
				if err := hookExec.RunSteps(ctx, svc.Hooks.BeforeApply, hookCtx); err != nil {
					return err
				}
			}

			logger.Info("applying manifests", "env", opts.Env, "namespace", ctxData.Namespace)
			if err := kubeClient.Apply(ctx, manifests); err != nil {
				return err
			}

			// Infrastructure/service hooks and stack-level hooks after apply.
			for _, infra := range stackCfg.Infrastructure {
				if err := hookExec.RunSteps(ctx, infra.Hooks.AfterApply, hookCtx); err != nil {
					return err
				}
			}
			for _, svc := range stackCfg.Services {
				if err := hookExec.RunSteps(ctx, svc.Hooks.AfterApply, hookCtx); err != nil {
					return err
				}
			}
			if err := hookExec.RunSteps(ctx, stackCfg.Hooks.AfterAll, hookCtx); err != nil {
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

			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to apply (dev, staging, ai)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace override for resources")
	cmd.Flags().Bool("wait", false, "Wait for core deployments to become ready")
	cmd.Flags().Bool("preflight", false, "Run preflight checks before applying manifests")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")
	cmd.Flags().Int("slot", 0, "Slot number for slot-based environments (e.g. ai)")
	_ = cmd.MarkFlagRequired("env")

	return cmd
}
