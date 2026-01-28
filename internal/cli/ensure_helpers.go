package cli

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/env"
	"github.com/codex-k8s/codexctl/internal/state"
)

type ensureSlotRequest struct {
	envName       string
	issue         int
	pr            int
	slot          int
	maxSlots      int
	machineOutput bool
	inlineVars    env.Vars
	varFiles      []string
}

type ensureSlotResult struct {
	record  state.EnvRecord
	created bool
	store   *envSlotStore
}

type ensureReadyRequest struct {
	envName        string
	issue          int
	pr             int
	slot           int
	maxSlots       int
	codeRootBase   string
	source         string
	prepareImages  bool
	doApply        bool
	forceApply     bool
	waitTimeout    string
	waitTimeoutSet bool
	waitSoftFail   bool
	machineOutput  bool
	inlineVars     env.Vars
	varFiles       []string
}

type ensureReadyResult struct {
	record     state.EnvRecord
	created    bool
	recreated  bool
	infraReady bool
}

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
	return res, nil
}

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
		target := fmt.Sprintf("%s/%d/src", strings.TrimSuffix(req.codeRootBase, "/"), rec.Slot)
		if err := syncSources(req.source, target); err != nil {
			return res, fmt.Errorf("sync sources to %q: %w", target, err)
		}
		logger.Info("slot workspace synced (ensure-ready)",
			"slot", rec.Slot,
			"target", target,
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

	if recreated {
		if err := handleDataPaths(logger, stackCfg, dataPathClean); err != nil {
			logger.Warn("failed to clean data paths for recreated environment", "slot", rec.Slot, "namespace", rec.Namespace, "error", err)
		}
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

			if err := applyStack(ctxApply, logger, stackCfg, ctxData, envName, slotRes.store.envCfg, true, false, req.machineOutput); err != nil {
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

	res.record = rec
	res.created = created
	res.recreated = recreated
	return res, nil
}
