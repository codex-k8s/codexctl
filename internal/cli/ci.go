package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/engine"
	"github.com/codex-k8s/codexctl/internal/ghoutput"
	"github.com/codex-k8s/codexctl/internal/kube"
	"github.com/codex-k8s/codexctl/internal/state"
)

// newCICommand creates the "ci" group command for CI helpers.
func newCICommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ci",
		Short: "Helpers for CI workflows",
	}

	cmd.AddCommand(
		newCIImagesCommand(opts),
		newCIApplyCommand(opts),
		newCISyncSourcesCommand(opts),
		newCIEnsureSlotCommand(opts),
		newCIEnsureReadyCommand(opts),
	)

	return cmd
}

// newCISyncSourcesCommand creates "ci sync-sources" to sync sources into a workspace.
func newCISyncSourcesCommand(opts *Options) *cobra.Command {
	var (
		slot         int
		codeRootBase string
		source       string
	)

	cmd := &cobra.Command{
		Use:   "sync-sources",
		Short: "Sync sources into the workspace for the selected environment",
		RunE: func(cmd *cobra.Command, _ []string) error {
			envCfg := ciEnv{}
			if err := parseEnv(&envCfg); err != nil {
				return err
			}
			if !cmd.Flags().Changed("slot") && envPresent("CODEXCTL_SLOT") {
				slot = envCfg.Slot
			}
			if !cmd.Flags().Changed("code-root-base") && envPresent("CODEXCTL_CODE_ROOT_BASE") {
				codeRootBase = envCfg.CodeRootBase
			}
			if !cmd.Flags().Changed("source") && envPresent("CODEXCTL_SOURCE") {
				source = envCfg.Source
			}
			if strings.TrimSpace(codeRootBase) == "" {
				return fmt.Errorf("sync-sources requires --code-root-base or CODEXCTL_CODE_ROOT_BASE env")
			}
			if strings.TrimSpace(source) == "" {
				source = "."
			}

			envName := opts.Env
			if envName == "" {
				envName = "ai"
			}
			target, err := resolveSourceTarget(envName, slot, codeRootBase)
			if err != nil {
				return err
			}
			return syncSources(source, target)
		},
	}

	cmd.Flags().IntVar(&slot, "slot", 0, "Slot number for ai environments")
	cmd.Flags().StringVar(&codeRootBase, "code-root-base", os.Getenv("CODEXCTL_CODE_ROOT_BASE"), "Base path for workspaces")
	cmd.Flags().StringVar(&source, "source", ".", "Source directory to sync")
	return cmd
}

// newCIImagesCommand creates "ci images" to mirror/build images in CI.
func newCIImagesCommand(opts *Options) *cobra.Command {
	var mirror bool
	var build bool
	var slot int

	cmd := &cobra.Command{
		Use:   "images",
		Short: "Mirror and build images for CI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())
			envVars := ciEnv{}
			if err := parseEnv(&envVars); err != nil {
				return err
			}
			if !cmd.Flags().Changed("slot") && envPresent("CODEXCTL_SLOT") {
				slot = envVars.Slot
			}
			if !cmd.Flags().Changed("mirror") && envPresent("CODEXCTL_MIRROR_IMAGES") {
				mirror = envVars.MirrorImages
			}
			if !cmd.Flags().Changed("build") && envPresent("CODEXCTL_BUILD_IMAGES") {
				build = envVars.BuildImages
			}

			stackCfg, tmplCtx, _, _, err := loadStackConfigFromCmd(opts, cmd, slot)
			if err != nil {
				return err
			}

			if mirror {
				if err := mirrorExternalImages(cmd.Context(), logger, stackCfg); err != nil {
					return err
				}
			}
			if build {
				if err := buildImages(cmd.Context(), logger, stackCfg, tmplCtx); err != nil {
					return err
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&mirror, "mirror", true, "Mirror external images into the local registry")
	cmd.Flags().BoolVar(&build, "build", true, "Build and push images declared in services.yaml")
	cmd.Flags().IntVar(&slot, "slot", 0, "Slot number for slot-based environments (e.g. ai)")
	addVarsFlags(cmd)

	return cmd
}

// newCIApplyCommand creates "ci apply" with retries and optional wait.
func newCIApplyCommand(opts *Options) *cobra.Command {
	var (
		slot           int
		preflight      bool
		wait           bool
		applyRetries   int
		waitRetries    int
		applyBackoff   time.Duration
		waitBackoff    time.Duration
		waitTimeout    string
		requestTimeout time.Duration
		onlyServices   string
		skipServices   string
		onlyInfra      string
		skipInfra      string
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply manifests with retries and optional wait",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())
			envVars := ciEnv{}
			if err := parseEnv(&envVars); err != nil {
				return err
			}
			if !cmd.Flags().Changed("slot") && envPresent("CODEXCTL_SLOT") {
				slot = envVars.Slot
			}
			if !cmd.Flags().Changed("preflight") && envPresent("CODEXCTL_PREFLIGHT") {
				preflight = envVars.Preflight
			}
			if !cmd.Flags().Changed("wait") && envPresent("CODEXCTL_WAIT") {
				wait = envVars.Wait
			}
			if !cmd.Flags().Changed("apply-retries") && envPresent("CODEXCTL_APPLY_RETRIES") {
				applyRetries = envVars.ApplyRetries
			}
			if !cmd.Flags().Changed("wait-retries") && envPresent("CODEXCTL_WAIT_RETRIES") {
				waitRetries = envVars.WaitRetries
			}
			if !cmd.Flags().Changed("apply-backoff") && envPresent("CODEXCTL_APPLY_BACKOFF") {
				if d, err := time.ParseDuration(envVars.ApplyBackoff); err != nil {
					return fmt.Errorf("invalid CODEXCTL_APPLY_BACKOFF: %w", err)
				} else {
					applyBackoff = d
				}
			}
			if !cmd.Flags().Changed("wait-backoff") && envPresent("CODEXCTL_WAIT_BACKOFF") {
				if d, err := time.ParseDuration(envVars.WaitBackoff); err != nil {
					return fmt.Errorf("invalid CODEXCTL_WAIT_BACKOFF: %w", err)
				} else {
					waitBackoff = d
				}
			}
			if !cmd.Flags().Changed("wait-timeout") && envPresent("CODEXCTL_WAIT_TIMEOUT") {
				waitTimeout = envVars.WaitTimeout
			}
			if !cmd.Flags().Changed("request-timeout") && envPresent("CODEXCTL_REQUEST_TIMEOUT") {
				if d, err := time.ParseDuration(envVars.RequestTime); err != nil {
					return fmt.Errorf("invalid CODEXCTL_REQUEST_TIMEOUT: %w", err)
				} else {
					requestTimeout = d
				}
			}
			if !cmd.Flags().Changed("only-services") && envPresent("CODEXCTL_ONLY_SERVICES") {
				onlyServices = envVars.OnlyServices
			}
			if !cmd.Flags().Changed("skip-services") && envPresent("CODEXCTL_SKIP_SERVICES") {
				skipServices = envVars.SkipServices
			}
			if !cmd.Flags().Changed("only-infra") && envPresent("CODEXCTL_ONLY_INFRA") {
				onlyInfra = envVars.OnlyInfra
			}
			if !cmd.Flags().Changed("skip-infra") && envPresent("CODEXCTL_SKIP_INFRA") {
				skipInfra = envVars.SkipInfra
			}

			stackCfg, ctxData, _, _, err := loadStackConfigFromCmd(opts, cmd, slot)
			if err != nil {
				return err
			}

			envCfg, err := config.ResolveEnvironment(stackCfg, opts.Env)
			if err != nil {
				return err
			}

			attempts := applyRetries
			if attempts <= 0 {
				attempts = 1
			}
			delay := applyBackoff
			if delay <= 0 {
				delay = 5 * time.Second
			}

			renderOpts := engine.RenderOptions{
				OnlyInfra:    parseNameSet(onlyInfra),
				SkipInfra:    parseNameSet(skipInfra),
				OnlyServices: parseNameSet(onlyServices),
				SkipServices: parseNameSet(skipServices),
			}

			var applyErr error
			for attempt := 1; attempt <= attempts; attempt++ {
				ctxApply, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
				applyErr = applyStack(ctxApply, logger, stackCfg, ctxData, opts.Env, envCfg, preflight, false, false, renderOpts)
				cancel()
				if applyErr == nil {
					break
				}
				if attempt < attempts {
					logger.Warn("codexctl apply failed, retrying", "attempt", attempt, "max", attempts, "delay", delay.String(), "error", applyErr)
					time.Sleep(delay)
					delay *= 2
				}
			}
			if applyErr != nil {
				return applyErr
			}

			if !wait {
				return nil
			}
			if ctxData.Namespace == "" {
				logger.Info("skip wait: namespace is empty, resources may be cluster-scoped or namespaced explicitly in manifests")
				return nil
			}

			waitAttempts := waitRetries
			if waitAttempts <= 0 {
				waitAttempts = 1
			}
			waitDelay := waitBackoff
			if waitDelay <= 0 {
				waitDelay = 5 * time.Second
			}

			applyKubeconfigOverride(&envCfg)
			client := kube.NewClient(envCfg.Kubeconfig, envCfg.Context)
			waitTimeoutResolved := resolveDeployWaitTimeout(stackCfg, waitTimeout, cmd.Flags().Changed("wait-timeout") || envPresent("CODEXCTL_WAIT_TIMEOUT"))
			for attempt := 1; attempt <= waitAttempts; attempt++ {
				if err := waitForDeployments(cmd.Context(), client, ctxData.Namespace, requestTimeout, waitTimeoutResolved); err != nil {
					if attempt == waitAttempts {
						return err
					}
					logger.Warn("kubectl wait failed, retrying", "attempt", attempt, "max", waitAttempts, "delay", waitDelay.String(), "error", err)
					time.Sleep(waitDelay)
					waitDelay *= 2
					continue
				}
				break
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&slot, "slot", 0, "Slot number for slot-based environments (e.g. ai)")
	cmd.Flags().BoolVar(&preflight, "preflight", false, "Run preflight checks before apply")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for deployments to become Available")
	cmd.Flags().IntVar(&applyRetries, "apply-retries", 3, "Number of apply retries")
	cmd.Flags().IntVar(&waitRetries, "wait-retries", 3, "Number of wait retries")
	cmd.Flags().DurationVar(&applyBackoff, "apply-backoff", 5*time.Second, "Initial backoff for apply retries")
	cmd.Flags().DurationVar(&waitBackoff, "wait-backoff", 5*time.Second, "Initial backoff for wait retries")
	cmd.Flags().StringVar(&waitTimeout, "wait-timeout", defaultDeployWaitTimeout, "kubectl wait timeout")
	cmd.Flags().DurationVar(&requestTimeout, "request-timeout", 600*time.Second, "kubectl request-timeout")
	addRenderFilterFlags(cmd, &onlyServices, &skipServices, &onlyInfra, &skipInfra, "Apply", "Skip")
	addVarsFlags(cmd)

	return cmd
}

// newCIEnsureSlotCommand creates "ci ensure-slot" for slot allocation in CI.
func newCIEnsureSlotCommand(opts *Options) *cobra.Command {
	var issue, pr, slot, max int

	cmd := &cobra.Command{
		Use:   "ensure-slot",
		Short: "Ensure a slot exists for CI workflows",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())
			envCfg := ciEnv{}
			if err := parseEnv(&envCfg); err != nil {
				return err
			}
			if !cmd.Flags().Changed("slot") && envPresent("CODEXCTL_SLOT") {
				slot = envCfg.Slot
			}
			if !cmd.Flags().Changed("issue") && envPresent("CODEXCTL_ISSUE_NUMBER") {
				issue = envCfg.Issue
			}
			if !cmd.Flags().Changed("pr") && envPresent("CODEXCTL_PR_NUMBER") {
				pr = envCfg.PR
			}
			if !cmd.Flags().Changed("max") && envPresent("CODEXCTL_DEV_SLOTS_MAX") {
				max = envCfg.MaxSlots
			}

			inlineVars, varFiles, err := parseInlineVarsAndFiles(cmd)
			if err != nil {
				return err
			}

			res, err := ensureSlot(cmd.Context(), logger, opts, ensureSlotRequest{
				envName:       opts.Env,
				issue:         issue,
				pr:            pr,
				slot:          slot,
				maxSlots:      max,
				machineOutput: false,
				inlineVars:    inlineVars,
				varFiles:      varFiles,
			})
			if err != nil {
				return err
			}
			writeErr := ghoutput.Write(map[string]string{
				"slot":      strconv.Itoa(res.record.Slot),
				"namespace": res.record.Namespace,
				"env":       res.record.Env,
			})
			if writeErr != nil {
				return writeErr
			}
			fmt.Printf("slot: %d\nnamespace: %s\nenv: %s\n", res.record.Slot, res.record.Namespace, res.record.Env)
			return nil
		},
	}

	cmd.Flags().IntVar(&slot, "slot", 0, "Explicit slot number")
	cmd.Flags().IntVar(&issue, "issue", 0, "Issue number selector")
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number selector")
	cmd.Flags().IntVar(&max, "max", 0, "Maximum number of slots (0 means unlimited)")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")

	return cmd
}

// newCIEnsureReadyCommand creates "ci ensure-ready" for provisioning envs in CI.
func newCIEnsureReadyCommand(opts *Options) *cobra.Command {
	var (
		issue         int
		pr            int
		slot          int
		maxSlots      int
		codeRootBase  string
		source        string
		prepareImages bool
		doApply       bool
		forceApply    bool
		waitTimeout   string
		waitSoftFail  bool
	)

	cmd := &cobra.Command{
		Use:   "ensure-ready",
		Short: "Ensure an environment slot exists and is ready for CI workflows",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())
			envCfg := ciEnv{}
			if err := parseEnv(&envCfg); err != nil {
				return err
			}
			if !cmd.Flags().Changed("slot") && envPresent("CODEXCTL_SLOT") {
				slot = envCfg.Slot
			}
			if !cmd.Flags().Changed("issue") && envPresent("CODEXCTL_ISSUE_NUMBER") {
				issue = envCfg.Issue
			}
			if !cmd.Flags().Changed("pr") && envPresent("CODEXCTL_PR_NUMBER") {
				pr = envCfg.PR
			}
			if !cmd.Flags().Changed("max") && envPresent("CODEXCTL_DEV_SLOTS_MAX") {
				maxSlots = envCfg.MaxSlots
			}
			if !cmd.Flags().Changed("code-root-base") && envPresent("CODEXCTL_CODE_ROOT_BASE") {
				codeRootBase = envCfg.CodeRootBase
			}
			if !cmd.Flags().Changed("source") && envPresent("CODEXCTL_SOURCE") {
				source = envCfg.Source
			}
			if !cmd.Flags().Changed("prepare-images") && envPresent("CODEXCTL_PREPARE_IMAGES") {
				prepareImages = envCfg.PrepareImages
			}
			if !cmd.Flags().Changed("apply") && envPresent("CODEXCTL_APPLY") {
				doApply = envCfg.Apply
			}
			if !cmd.Flags().Changed("force-apply") && envPresent("CODEXCTL_FORCE_APPLY") {
				forceApply = envCfg.ForceApply
			}
			if !cmd.Flags().Changed("wait-timeout") && envPresent("CODEXCTL_WAIT_TIMEOUT") {
				waitTimeout = envCfg.WaitTimeout
			}
			if !cmd.Flags().Changed("wait-soft-fail") && envPresent("CODEXCTL_WAIT_SOFT_FAIL") {
				waitSoftFail = envCfg.WaitSoftFail
			}

			inlineVars, varFiles, err := parseInlineVarsAndFiles(cmd)
			if err != nil {
				return err
			}

			res, err := ensureReady(cmd.Context(), logger, opts, ensureReadyRequest{
				envName:        opts.Env,
				issue:          issue,
				pr:             pr,
				slot:           slot,
				maxSlots:       maxSlots,
				codeRootBase:   codeRootBase,
				source:         source,
				prepareImages:  prepareImages,
				doApply:        doApply,
				forceApply:     forceApply,
				waitTimeout:    waitTimeout,
				waitTimeoutSet: cmd.Flags().Changed("wait-timeout") || envPresent("CODEXCTL_WAIT_TIMEOUT"),
				waitSoftFail:   waitSoftFail,
				machineOutput:  false,
				inlineVars:     inlineVars,
				varFiles:       varFiles,
			})
			if err != nil {
				return err
			}
			newEnv := res.created || res.recreated
			infraUnhealthy := strconv.FormatBool(!res.infraReady)
			writeErr := ghoutput.Write(map[string]string{
				"slot":               strconv.Itoa(res.record.Slot),
				"namespace":          res.record.Namespace,
				"env":                res.record.Env,
				"created":            strconv.FormatBool(res.created),
				"recreated":          strconv.FormatBool(res.recreated),
				"infra_ready":        strconv.FormatBool(res.infraReady),
				"codexctl_env_ready": strconv.FormatBool(res.envReady),
				"infra_unhealthy":    infraUnhealthy,
				"codexctl_new_env":   strconv.FormatBool(newEnv),
				"codexctl_run_args":  buildCodexctlRunArgs(res.record, issue, pr, opts.Env),
			})
			if writeErr != nil {
				return writeErr
			}
			fmt.Printf(
				"slot: %d\nnamespace: %s\nenv: %s\ncreated: %t\nrecreated: %t\ninfra_ready: %t\ncodexctl_env_ready: %t\ninfra_unhealthy: %s\ncodexctl_new_env: %t\n",
				res.record.Slot,
				res.record.Namespace,
				res.record.Env,
				res.created,
				res.recreated,
				res.infraReady,
				res.envReady,
				infraUnhealthy,
				newEnv,
			)
			return nil
		},
	}

	cmd.Flags().IntVar(&slot, "slot", 0, "Explicit slot number")
	cmd.Flags().IntVar(&issue, "issue", 0, "Issue number selector")
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number selector")
	cmd.Flags().IntVar(&maxSlots, "max", 0, "Maximum number of slots (0 means unlimited)")
	cmd.Flags().StringVar(&codeRootBase, "code-root-base", os.Getenv("CODEXCTL_CODE_ROOT_BASE"), "Base path for slot workspaces")
	cmd.Flags().StringVar(&source, "source", ".", "Source directory to sync")
	cmd.Flags().BoolVar(&prepareImages, "prepare-images", false, "Mirror external and build local images before apply")
	cmd.Flags().BoolVar(&doApply, "apply", false, "Apply manifests for the ensured environment")
	cmd.Flags().BoolVar(&forceApply, "force-apply", false, "Apply manifests even for existing environments")
	cmd.Flags().StringVar(&waitTimeout, "wait-timeout", defaultDeployWaitTimeout, "kubectl wait timeout for deployments")
	cmd.Flags().BoolVar(&waitSoftFail, "wait-soft-fail", false, "Do not fail when deployment wait times out")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")

	return cmd
}

func buildCodexctlRunArgs(rec state.EnvRecord, issue, pr int, envName string) string {
	env := rec.Env
	if env == "" {
		env = strings.TrimSpace(envName)
	}
	if env == "" {
		env = "ai"
	}
	args := []string{"--env", env, "--slot", strconv.Itoa(rec.Slot)}
	if rec.Namespace != "" {
		args = append(args, "--namespace", rec.Namespace)
	}
	if issue > 0 {
		args = append(args, "--issue", strconv.Itoa(issue))
	}
	if pr > 0 {
		args = append(args, "--pr", strconv.Itoa(pr))
	}
	return strings.Join(args, " ")
}

// waitForDeployments runs kubectl wait with a request timeout wrapper.
func waitForDeployments(ctx context.Context, client *kube.Client, namespace string, requestTimeout time.Duration, waitTimeout string) error {
	if client == nil {
		return fmt.Errorf("kubernetes client is nil")
	}
	args := []string{"wait", "--for=condition=Available", "deployment", "--all", fmt.Sprintf("--timeout=%s", waitTimeout)}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	if requestTimeout > 0 {
		args = append(args, fmt.Sprintf("--request-timeout=%s", requestTimeout.String()))
	}
	ctxWait := ctx
	if waitTimeout != "" {
		if d, err := time.ParseDuration(waitTimeout); err == nil {
			var cancel context.CancelFunc
			ctxWait, cancel = context.WithTimeout(ctx, d+time.Minute)
			defer cancel()
		}
	}
	return client.RunRaw(ctxWait, nil, args...)
}
