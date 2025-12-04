package cli

import (
	"context"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/engine"
	"github.com/codex-k8s/codexctl/internal/env"
	"github.com/codex-k8s/codexctl/internal/hooks"
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

			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
			defer cancel()

			return destroyStack(ctx, logger, stackCfg, ctxData, envCfg, opts.Env)
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to destroy (dev, staging, ai)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace override for resources")
	cmd.Flags().Bool("yes", false, "Do not prompt for confirmation")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")
	cmd.Flags().Int("slot", 0, "Slot number for slot-based environments (e.g. ai)")
	_ = cmd.MarkFlagRequired("env")

	return cmd
}

// destroyStack runs the core destroy logic shared between the "destroy" command
// and higher-level helpers such as "manage-env cleanup".
func destroyStack(
	ctx context.Context,
	logger *slog.Logger,
	stackCfg *config.StackConfig,
	ctxData config.TemplateContext,
	envCfg config.Environment,
	envName string,
) error {
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
		EnvName:    envName,
		KubeClient: kubeClient,
	}

	// Stack-level and infrastructure/service hooks before destroy.
	if err := hookExec.RunSteps(ctx, stackCfg.Hooks.BeforeAll, hookCtx); err != nil {
		return err
	}
	for _, infra := range stackCfg.Infrastructure {
		if err := hookExec.RunSteps(ctx, infra.Hooks.BeforeDestroy, hookCtx); err != nil {
			return err
		}
	}
	for _, svc := range stackCfg.Services {
		enabled, err := serviceEnabled(svc, ctxData)
		if err != nil {
			return err
		}
		if !enabled {
			continue
		}
		if err := hookExec.RunSteps(ctx, svc.Hooks.BeforeDestroy, hookCtx); err != nil {
			return err
		}
	}

	logger.Info("deleting manifests", "env", envName, "namespace", ctxData.Namespace)
	if err := kubeClient.Delete(ctx, manifests, true); err != nil {
		return err
	}

	// Infrastructure/service hooks and stack-level hooks after destroy.
	for _, infra := range stackCfg.Infrastructure {
		if err := hookExec.RunSteps(ctx, infra.Hooks.AfterDestroy, hookCtx); err != nil {
			return err
		}
	}
	for _, svc := range stackCfg.Services {
		enabled, err := serviceEnabled(svc, ctxData)
		if err != nil {
			return err
		}
		if !enabled {
			continue
		}
		if err := hookExec.RunSteps(ctx, svc.Hooks.AfterDestroy, hookCtx); err != nil {
			return err
		}
	}
	if err := hookExec.RunSteps(ctx, stackCfg.Hooks.AfterAll, hookCtx); err != nil {
		return err
	}

	return nil
}
