package cli

import (
	"context"
	"log/slog"
	"strings"
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

			preflight, _ := cmd.Flags().GetBool("preflight")
			wait, _ := cmd.Flags().GetBool("wait")
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
			defer cancel()

			return applyStack(ctx, logger, stackCfg, ctxData, opts.Env, envCfg, preflight, wait)
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

// applyStack runs the core apply logic shared between the "apply" command and
// higher-level helpers such as "ci ensure-ready".
func applyStack(
	ctx context.Context,
	logger *slog.Logger,
	stackCfg *config.StackConfig,
	ctxData config.TemplateContext,
	envName string,
	envCfg config.Environment,
	preflight bool,
	wait bool,
) error {
	kubeClient := kube.NewClient(envCfg.Kubeconfig, envCfg.Context)

	// Ensure target namespace exists before running hooks or applying manifests.
	if ns := strings.TrimSpace(ctxData.Namespace); ns != "" {
		nsCtx, cancelNS := context.WithTimeout(ctx, 2*time.Minute)
		defer cancelNS()
		if err := kubeClient.RunRaw(nsCtx, nil, "get", "ns", ns); err != nil {
			logger.Info("creating namespace before apply", "env", envName, "namespace", ns)
			_ = kubeClient.RunRaw(nsCtx, nil, "create", "ns", ns)
		}
	}

	eng := engine.NewEngine()
	manifests, err := eng.RenderStack(stackCfg, ctxData)
	if err != nil {
		return err
	}

	hookExec := hooks.NewExecutor(logger)
	hookCtx := hooks.StepContext{
		Stack:      stackCfg,
		Template:   ctxData,
		EnvName:    envName,
		KubeClient: kubeClient,
	}

	if preflight {
		logger.Info("running preflight checks before apply", "env", envName)
		if err := hookExec.RunPreflightBasic(ctx); err != nil {
			return err
		}
		if err := runDoctorChecks(ctx, logger, doctorParams{stackCfg: stackCfg, envName: envName}); err != nil {
			return err
		}
	}

	// Stack-level and infrastructure/service hooks before apply.
	if err := hookExec.RunSteps(ctx, stackCfg.Hooks.BeforeAll, hookCtx); err != nil {
		return err
	}
	stageCtx := hookStageContext{stackCfg: stackCfg, ctxData: ctxData, hookCtx: hookCtx}
	beforeStage := hookStage{infra: infraBeforeApply, services: serviceBeforeApply}
	if err := runHookStage(ctx, hookExec, stageCtx, beforeStage); err != nil {
		return err
	}

	logger.Info("applying manifests", "env", envName, "namespace", ctxData.Namespace)

	applyOnce := func(ctx context.Context) error {
		return kubeClient.Apply(ctx, manifests)
	}

	err = applyOnce(ctx)
	if err != nil {
		msg := err.Error()
		// Ingress-nginx admission webhook might not be ready yet; do a bounded retry loop.
		if strings.Contains(msg, "validate.nginx.ingress.kubernetes.io") ||
			strings.Contains(msg, "ingress-nginx-controller-admission") {
			const maxRetries = 18
			for attempt := 1; attempt <= maxRetries; attempt++ {
				logger.Warn("apply failed due to ingress-nginx admission webhook; retrying", "attempt", attempt, "max", maxRetries, "error", err)
				select {
				case <-ctx.Done():
					return err
				case <-time.After(10 * time.Second):
				}
				err = applyOnce(ctx)
				if err == nil {
					break
				}
				msg = err.Error()
				if !strings.Contains(msg, "validate.nginx.ingress.kubernetes.io") &&
					!strings.Contains(msg, "ingress-nginx-controller-admission") {
					return err
				}
			}
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Infrastructure/service hooks and stack-level hooks after apply.
	afterStage := hookStage{infra: infraAfterApply, services: serviceAfterApply}
	if err := runHookStage(ctx, hookExec, stageCtx, afterStage); err != nil {
		return err
	}
	if err := hookExec.RunSteps(ctx, stackCfg.Hooks.AfterAll, hookCtx); err != nil {
		return err
	}

	if wait {
		if ctxData.Namespace == "" {
			logger.Info("skip wait: namespace is empty, resources may be cluster-scoped or namespaced explicitly in manifests")
		} else {
			waitTimeout := resolveDeployWaitTimeout(stackCfg, "", false)
			logger.Info("waiting for deployments to become Available", "namespace", ctxData.Namespace, "timeout", waitTimeout)
			if err := kubeClient.WaitForDeployments(ctx, ctxData.Namespace, waitTimeout); err != nil {
				return err
			}
		}
	}

	return nil
}
