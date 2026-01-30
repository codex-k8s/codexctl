package cli

import (
	"context"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/hooks"
)

type infraHookSelector func(config.InfraItem) []config.HookStep
type serviceHookSelector func(config.Service) []config.HookStep

type hookStage struct {
	// infra selects hooks for infrastructure items.
	infra infraHookSelector
	// services selects hooks for services.
	services serviceHookSelector
}

type hookStageContext struct {
	// stackCfg is the loaded stack configuration.
	stackCfg *config.StackConfig
	// ctxData is the template context for rendering.
	ctxData config.TemplateContext
	// hookCtx is the runtime hook execution context.
	hookCtx hooks.StepContext
}

// runInfrastructureHooks executes infra hook steps in order.
func runInfrastructureHooks(ctx context.Context, hookExec *hooks.Executor, infra []config.InfraItem, hookCtx hooks.StepContext, selector infraHookSelector) error {
	for _, item := range infra {
		if err := hookExec.RunSteps(ctx, selector(item), hookCtx); err != nil {
			return err
		}
	}
	return nil
}

// runServiceHooks executes service hook steps for enabled services.
func runServiceHooks(ctx context.Context, hookExec *hooks.Executor, services []config.Service, ctxData config.TemplateContext, hookCtx hooks.StepContext, selector serviceHookSelector) error {
	for _, svc := range services {
		enabled, err := serviceEnabled(svc, ctxData)
		if err != nil {
			return err
		}
		if !enabled {
			continue
		}
		if err := hookExec.RunSteps(ctx, selector(svc), hookCtx); err != nil {
			return err
		}
	}
	return nil
}

// runHookStage executes the infra and service hooks for a stage.
func runHookStage(ctx context.Context, hookExec *hooks.Executor, stageCtx hookStageContext, stage hookStage) error {
	if err := runInfrastructureHooks(ctx, hookExec, stageCtx.stackCfg.Infrastructure, stageCtx.hookCtx, stage.infra); err != nil {
		return err
	}
	if err := runServiceHooks(ctx, hookExec, stageCtx.stackCfg.Services, stageCtx.ctxData, stageCtx.hookCtx, stage.services); err != nil {
		return err
	}
	return nil
}

// infraBeforeApply selects infra before-apply hooks.
func infraBeforeApply(item config.InfraItem) []config.HookStep { return item.Hooks.BeforeApply }

// infraAfterApply selects infra after-apply hooks.
func infraAfterApply(item config.InfraItem) []config.HookStep { return item.Hooks.AfterApply }

// infraBeforeDestroy selects infra before-destroy hooks.
func infraBeforeDestroy(item config.InfraItem) []config.HookStep { return item.Hooks.BeforeDestroy }

// infraAfterDestroy selects infra after-destroy hooks.
func infraAfterDestroy(item config.InfraItem) []config.HookStep { return item.Hooks.AfterDestroy }

// serviceBeforeApply selects service before-apply hooks.
func serviceBeforeApply(svc config.Service) []config.HookStep { return svc.Hooks.BeforeApply }

// serviceAfterApply selects service after-apply hooks.
func serviceAfterApply(svc config.Service) []config.HookStep { return svc.Hooks.AfterApply }

// serviceBeforeDestroy selects service before-destroy hooks.
func serviceBeforeDestroy(svc config.Service) []config.HookStep { return svc.Hooks.BeforeDestroy }

// serviceAfterDestroy selects service after-destroy hooks.
func serviceAfterDestroy(svc config.Service) []config.HookStep { return svc.Hooks.AfterDestroy }
