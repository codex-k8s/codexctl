package cli

import (
	"context"
	"log/slog"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/engine"
	"github.com/codex-k8s/codexctl/internal/hooks"
	"github.com/codex-k8s/codexctl/internal/kube"
)

// destroyStack runs the core destroy logic shared by higher-level helpers
// such as "manage-env cleanup".
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
