package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/engine"
	"github.com/codex-k8s/codexctl/internal/env"
	"github.com/codex-k8s/codexctl/internal/kube"
	"github.com/codex-k8s/codexctl/internal/state"
)

type ensureSlotRequest struct {
	// envName is the target environment name.
	envName string
	// issue is the GitHub issue selector.
	issue int
	// pr is the GitHub PR selector.
	pr int
	// slot is an explicit slot override.
	slot int
	// maxSlots limits the allocation search.
	maxSlots int
	// machineOutput toggles machine-readable logging behavior.
	machineOutput bool
	// inlineVars are inline variables for template rendering.
	inlineVars env.Vars
	// varFiles are additional var-file paths.
	varFiles []string
}

type ensureSlotResult struct {
	// record is the resolved environment record.
	record state.EnvRecord
	// created indicates a new slot allocation.
	created bool
	// store holds the resolved store context.
	store *envSlotStore
}

type ensureReadyRequest struct {
	// envName is the target environment name.
	envName string
	// issue is the GitHub issue selector.
	issue int
	// pr is the GitHub PR selector.
	pr int
	// slot is an explicit slot override.
	slot int
	// maxSlots limits the allocation search.
	maxSlots int
	// codeRootBase is the base path for slot workspaces.
	codeRootBase string
	// source is the path to sync into the slot workspace.
	source string
	// prepareImages toggles mirroring/building images.
	prepareImages bool
	// doApply toggles applying manifests after allocation.
	doApply bool
	// forceApply forces apply even for existing envs.
	forceApply bool
	// waitTimeout overrides deployment wait timeout.
	waitTimeout string
	// waitTimeoutSet indicates explicit wait-timeout flag usage.
	waitTimeoutSet bool
	// waitSoftFail allows wait failures to be non-fatal.
	waitSoftFail bool
	// machineOutput toggles machine-readable logging behavior.
	machineOutput bool
	// inlineVars are inline variables for template rendering.
	inlineVars env.Vars
	// varFiles are additional var-file paths.
	varFiles []string
}

type ensureReadyResult struct {
	// record is the resolved environment record.
	record state.EnvRecord
	// created indicates a new slot allocation.
	created bool
	// recreated indicates resources were recreated due to missing namespace.
	recreated bool
	// infraReady indicates whether infra rollout was successful.
	infraReady bool
	// envReady indicates whether the existing environment looks ready to run Codex.
	envReady bool
}

// ensureSlot allocates or resolves an environment slot based on selectors.
func ensureSlot(ctx context.Context, logger *slog.Logger, opts *Options, req ensureSlotRequest) (ensureSlotResult, error) {
	var res ensureSlotResult
	if req.issue <= 0 && req.pr <= 0 && req.slot <= 0 {
		return res, fmt.Errorf("at least one of --issue, --pr or --slot must be specified")
	}

	envName := req.envName
	if envName == "" {
		envName = "ai"
	}

	loadOpts := config.LoadOptions{
		Env:      envName,
		UserVars: req.inlineVars,
		VarFiles: req.varFiles,
	}

	envStore, err := loadEnvSlotStore(opts.ConfigPath, envName, loadOpts, logger)
	if err != nil {
		return res, err
	}
	if envStore.kubeClient != nil {
		envStore.kubeClient.StdoutToStderr = req.machineOutput
	}

	ctxList, cancelList := context.WithTimeout(ctx, 30*time.Second)
	defer cancelList()

	found, err := findMatchingEnvRecord(ctxList, envStore.store, envName, req.slot, req.issue, req.pr)
	if err != nil {
		return res, err
	}

	if found != nil {
		// Recompute namespace for existing records so slot-based envs do not get stuck with "-0"
		// when services.yaml was previously loaded with Slot=0.
		ctxSlot := envStore.templateCtx
		ctxSlot.Slot = found.Slot
		ctxSlot.Namespace = ""
		expectedNS, err := config.ResolveNamespace(envStore.stackCfg, ctxSlot, envName)
		if err == nil && strings.TrimSpace(expectedNS) != "" && strings.TrimSpace(expectedNS) != strings.TrimSpace(found.Namespace) {
			ctxFix, cancelFix := context.WithTimeout(ctx, 15*time.Second)
			if err := envStore.store.UpdateNamespace(ctxFix, found.Slot, expectedNS); err != nil {
				logger.Warn("failed to patch namespace for existing environment record", "slot", found.Slot, "env", envName, "expected", expectedNS, "actual", found.Namespace, "error", err)
			} else {
				found.Namespace = expectedNS
			}
			cancelFix()
		}

		res.record = *found
		res.store = envStore
		return res, nil
	}

	prefer := req.slot
	ctxAlloc, cancelAlloc := context.WithTimeout(ctx, 6*time.Hour)
	defer cancelAlloc()

	rec, err := allocateSlotWithRetry(ctxAlloc, envStore.store, envStore.stackCfg, envStore.templateCtx, envName, req.maxSlots, prefer, req.issue, req.pr, logger)
	if err != nil {
		return res, err
	}

	res.record = rec
	res.created = true
	res.store = envStore
	if err := applySlotBootstrapInfra(ctx, logger, opts, envStore, rec, req); err != nil {
		return res, err
	}
	return res, nil
}

// ensureReady ensures a slot exists and optionally prepares/apply resources.
func ensureReady(ctx context.Context, logger *slog.Logger, opts *Options, req ensureReadyRequest) (ensureReadyResult, error) {
	var res ensureReadyResult
	res.infraReady = true
	if req.issue <= 0 && req.pr <= 0 && req.slot <= 0 {
		return res, fmt.Errorf("at least one of --issue, --pr or --slot must be specified")
	}

	slotRes, err := ensureSlot(ctx, logger, opts, ensureSlotRequest{
		envName:       req.envName,
		issue:         req.issue,
		pr:            req.pr,
		slot:          req.slot,
		maxSlots:      req.maxSlots,
		machineOutput: req.machineOutput,
		inlineVars:    req.inlineVars,
		varFiles:      req.varFiles,
	})
	if err != nil {
		return res, err
	}

	rec := slotRes.record
	created := slotRes.created
	envName := req.envName
	if envName == "" {
		envName = "ai"
	}

	if slotRes.store != nil && slotRes.store.kubeClient != nil {
		slotRes.store.kubeClient.StdoutToStderr = req.machineOutput
	}

	recreated := false
	if !created && strings.TrimSpace(rec.Namespace) != "" {
		ctxNs, cancelNs := context.WithTimeout(ctx, 15*time.Second)
		defer cancelNs()
		nsArgs := []string{"get", "ns", rec.Namespace}
		if _, err := slotRes.store.kubeClient.RunAndCapture(ctxNs, nil, nsArgs...); err != nil {
			logger.Warn("namespace missing for existing environment; resources will be recreated",
				"namespace", rec.Namespace,
				"slot", rec.Slot,
				"env", rec.Env,
				"error", err,
			)
			recreated = true
		}
	}

	if req.codeRootBase != "" && req.source != "" {
		workspaceMount := strings.TrimSpace(os.Getenv("CODEXCTL_WORKSPACE_MOUNT"))
		if workspaceMount == "" {
			workspaceMount = "/workspace"
		}
		workspacePVC := strings.TrimSpace(os.Getenv("CODEXCTL_WORKSPACE_PVC"))
		if workspacePVC == "" && slotRes.store.stackCfg != nil {
			workspacePVC = fmt.Sprintf("%s-workspace", slotRes.store.stackCfg.Project)
		}
		targetRel, err := resolveWorkspaceRelativeTarget(envName, rec.Slot, req.codeRootBase, workspaceMount)
		if err != nil {
			return res, err
		}
		targetPath := filepath.Join(workspaceMount, targetRel)
		syncImg := strings.TrimSpace(os.Getenv("CODEXCTL_SYNC_IMAGE"))
		if err := syncSources(ctx, logger, req.source, syncTarget{
			Namespace:  rec.Namespace,
			PVCName:    workspacePVC,
			MountPath:  workspaceMount,
			TargetRel:  targetRel,
			Image:      syncImg,
			KubeClient: slotRes.store.kubeClient,
		}); err != nil {
			return res, fmt.Errorf("sync sources to %q: %w", targetPath, err)
		}
		logger.Info("slot workspace synced (ensure-ready)",
			"slot", rec.Slot,
			"target", targetPath,
			"source", req.source,
			"env", envName,
			"namespace", rec.Namespace,
		)
	}

	loadOptsSlot := config.LoadOptions{
		Env:       envName,
		Namespace: rec.Namespace,
		Slot:      rec.Slot,
		UserVars:  req.inlineVars,
		VarFiles:  req.varFiles,
	}

	stackCfg, ctxData, err := config.LoadStackConfig(opts.ConfigPath, loadOptsSlot)
	if err != nil {
		return res, err
	}

	if req.prepareImages {
		if created || recreated {
			ctxImages, cancelImages := context.WithTimeout(ctx, 2*time.Hour)
			defer cancelImages()

			if err := mirrorExternalImages(ctxImages, logger, stackCfg); err != nil {
				return res, err
			}
			if err := buildImages(ctxImages, logger, stackCfg, ctxData); err != nil {
				return res, err
			}
		} else {
			logger.Info("skipping image preparation for existing environment",
				"slot", rec.Slot,
				"namespace", rec.Namespace,
				"env", rec.Env,
			)
		}
	}

	if req.doApply {
		applyNeeded := created || recreated || req.forceApply
		if applyNeeded {
			ctxApply, cancelApply := context.WithTimeout(ctx, 10*time.Minute)
			defer cancelApply()

			if err := applyStack(ctxApply, logger, stackCfg, ctxData, envName, slotRes.store.envCfg, true, false, req.machineOutput, engine.RenderOptions{}); err != nil {
				return res, err
			}

			if rec.Namespace == "" {
				logger.Info("skip wait: namespace is empty, resources may be cluster-scoped or namespaced explicitly in manifests")
			} else {
				waitTimeout := resolveDeployWaitTimeout(stackCfg, req.waitTimeout, req.waitTimeoutSet)
				logger.Info("waiting for deployments to become Available", "namespace", rec.Namespace, "timeout", waitTimeout)
				if err := slotRes.store.kubeClient.WaitForDeployments(ctx, rec.Namespace, waitTimeout); err != nil {
					if req.waitSoftFail {
						res.infraReady = false
						logger.Warn("wait for deployments failed, continuing", "namespace", rec.Namespace, "error", err)
					} else {
						return res, err
					}
				}
			}
		} else {
			logger.Info("skipping apply for existing environment",
				"slot", rec.Slot,
				"namespace", rec.Namespace,
				"env", rec.Env,
			)
		}
	}

	res.envReady = false
	if slotRes.store != nil && slotRes.store.kubeClient != nil && strings.TrimSpace(rec.Namespace) != "" {
		ctxReady, cancelReady := context.WithTimeout(ctx, 20*time.Second)
		defer cancelReady()
		ready, err := checkEnvReady(ctxReady, slotRes.store.kubeClient, rec.Namespace, "codex")
		if err != nil {
			logger.Debug("failed to check existing environment readiness", "namespace", rec.Namespace, "slot", rec.Slot, "env", rec.Env, "error", err)
		}
		res.envReady = ready
	}

	res.record = rec
	res.created = created
	res.recreated = recreated
	return res, nil
}

func applySlotBootstrapInfra(
	ctx context.Context,
	logger *slog.Logger,
	opts *Options,
	envStore *envSlotStore,
	rec state.EnvRecord,
	req ensureSlotRequest,
) error {
	if envStore == nil {
		return nil
	}
	infraNames := envStore.envCfg.SlotBootstrapInfra
	if len(infraNames) == 0 {
		return nil
	}

	envName := req.envName
	if envName == "" {
		envName = "ai"
	}

	loadOpts := config.LoadOptions{
		Env:       envName,
		Namespace: rec.Namespace,
		Slot:      rec.Slot,
		UserVars:  req.inlineVars,
		VarFiles:  req.varFiles,
	}
	stackCfg, ctxData, err := config.LoadStackConfig(opts.ConfigPath, loadOpts)
	if err != nil {
		return err
	}

	applyCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	logger.Info("applying slot bootstrap infra", "env", envName, "namespace", rec.Namespace, "infra", infraNames)
	onlyInfra := nameSetFromSlice(infraNames)
	if len(onlyInfra) == 0 {
		return nil
	}
	renderOpts := engine.RenderOptions{
		OnlyInfra: onlyInfra,
	}
	return applyStack(applyCtx, logger, stackCfg, ctxData, envName, envStore.envCfg, false, false, req.machineOutput, renderOpts)
}

func nameSetFromSlice(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		set[trimmed] = struct{}{}
	}
	return set
}

func checkEnvReady(ctx context.Context, client *kube.Client, namespace, deployment string) (bool, error) {
	if client == nil {
		return false, fmt.Errorf("kubernetes client is nil")
	}
	if strings.TrimSpace(namespace) == "" {
		return false, fmt.Errorf("namespace is empty")
	}
	if strings.TrimSpace(deployment) == "" {
		return false, fmt.Errorf("deployment name is empty")
	}
	if _, err := client.RunAndCapture(ctx, nil, "get", "ns", namespace); err != nil {
		return false, err
	}
	if _, err := client.RunAndCapture(ctx, nil, "-n", namespace, "get", "deploy", deployment); err != nil {
		return false, err
	}
	return true, nil
}
