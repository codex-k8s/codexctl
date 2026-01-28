package cli

import (
	"context"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/hooks"
)

type infraHookSelector func(config.InfraItem) []config.HookStep
type serviceHookSelector func(config.Service) []config.HookStep

type hookStage struct {
	infra    infraHookSelector
	services serviceHookSelector
}

type hookStageContext struct {
	stackCfg *config.StackConfig
	ctxData  config.TemplateContext
	hookCtx  hooks.StepContext
}

func runInfrastructureHooks(ctx context.Context, hookExec *hooks.Executor, infra []config.InfraItem, hookCtx hooks.StepContext, selector infraHookSelector) error {
	for _, item := range infra {
		if err := hookExec.RunSteps(ctx, selector(item), hookCtx); err != nil {
			return err
		}
	}
	return nil
}

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

func runHookStage(ctx context.Context, hookExec *hooks.Executor, stageCtx hookStageContext, stage hookStage) error {
	if err := runInfrastructureHooks(ctx, hookExec, stageCtx.stackCfg.Infrastructure, stageCtx.hookCtx, stage.infra); err != nil {
		return err
	}
	if err := runServiceHooks(ctx, hookExec, stageCtx.stackCfg.Services, stageCtx.ctxData, stageCtx.hookCtx, stage.services); err != nil {
		return err
	}
	return nil
}

func infraBeforeApply(item config.InfraItem) []config.HookStep   { return item.Hooks.BeforeApply }
func infraAfterApply(item config.InfraItem) []config.HookStep    { return item.Hooks.AfterApply }
func infraBeforeDestroy(item config.InfraItem) []config.HookStep { return item.Hooks.BeforeDestroy }
func infraAfterDestroy(item config.InfraItem) []config.HookStep  { return item.Hooks.AfterDestroy }

func serviceBeforeApply(svc config.Service) []config.HookStep   { return svc.Hooks.BeforeApply }
func serviceAfterApply(svc config.Service) []config.HookStep    { return svc.Hooks.AfterApply }
func serviceBeforeDestroy(svc config.Service) []config.HookStep { return svc.Hooks.BeforeDestroy }
func serviceAfterDestroy(svc config.Service) []config.HookStep  { return svc.Hooks.AfterDestroy }
