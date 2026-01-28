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
	stageCtx := hookStageContext{stackCfg: stackCfg, ctxData: ctxData, hookCtx: hookCtx}
	beforeStage := hookStage{infra: infraBeforeDestroy, services: serviceBeforeDestroy}
	if err := runHookStage(ctx, hookExec, stageCtx, beforeStage); err != nil {
		return err
	}

	logger.Info("deleting manifests", "env", envName, "namespace", ctxData.Namespace)
	if err := kubeClient.Delete(ctx, manifests, true); err != nil {
		return err
	}

	// Infrastructure/service hooks and stack-level hooks after destroy.
	afterStage := hookStage{infra: infraAfterDestroy, services: serviceAfterDestroy}
	if err := runHookStage(ctx, hookExec, stageCtx, afterStage); err != nil {
		return err
	}
	if err := hookExec.RunSteps(ctx, stackCfg.Hooks.AfterAll, hookCtx); err != nil {
		return err
	}

	return nil
}
