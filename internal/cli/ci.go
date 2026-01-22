package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/env"
	"github.com/codex-k8s/codexctl/internal/kube"
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
		newCIEnsureSlotCommand(opts),
		newCIEnsureReadyCommand(opts),
	)

	return cmd
}

func newCIImagesCommand(opts *Options) *cobra.Command {
	var mirror bool
	var build bool
	var slot int

	cmd := &cobra.Command{
		Use:   "images",
		Short: "Mirror and build images for CI",
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

			loadOpts := config.LoadOptions{
				Env:       opts.Env,
				Namespace: opts.Namespace,
				Slot:      slot,
				UserVars:  inlineVars,
				VarFiles:  varFiles,
			}

			stackCfg, tmplCtx, err := config.LoadStackConfig(opts.ConfigPath, loadOpts)
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
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")

	return cmd
}

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
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply manifests with retries and optional wait",
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

			attempts := applyRetries
			if attempts <= 0 {
				attempts = 1
			}
			delay := applyBackoff
			if delay <= 0 {
				delay = 5 * time.Second
			}

			var applyErr error
			for attempt := 1; attempt <= attempts; attempt++ {
				ctxApply, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
				applyErr = applyStack(ctxApply, logger, stackCfg, ctxData, opts.Env, envCfg, preflight, false)
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

			client := kube.NewClient(envCfg.Kubeconfig, envCfg.Context)
			for attempt := 1; attempt <= waitAttempts; attempt++ {
				if err := waitForDeployments(cmd.Context(), client, ctxData.Namespace, requestTimeout, waitTimeout); err != nil {
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
	cmd.Flags().StringVar(&waitTimeout, "wait-timeout", "1200s", "kubectl wait timeout")
	cmd.Flags().DurationVar(&requestTimeout, "request-timeout", 600*time.Second, "kubectl request-timeout")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")

	return cmd
}

func newCIEnsureSlotCommand(opts *Options) *cobra.Command {
	var issue, pr, slot, max int
	var output string

	cmd := &cobra.Command{
		Use:   "ensure-slot",
		Short: "Ensure a slot exists for CI workflows",
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

			res, err := ensureSlot(cmd.Context(), logger, opts, ensureSlotRequest{
				envName:    opts.Env,
				issue:      issue,
				pr:         pr,
				slot:       slot,
				maxSlots:   max,
				inlineVars: inlineVars,
				varFiles:   varFiles,
			})
			if err != nil {
				return err
			}
			return printResolveOutput(res.record, output, logger)
		},
	}

	cmd.Flags().IntVar(&slot, "slot", 0, "Explicit slot number")
	cmd.Flags().IntVar(&issue, "issue", 0, "Issue number selector")
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number selector")
	cmd.Flags().IntVar(&max, "max", 0, "Maximum number of slots (0 means unlimited)")
	cmd.Flags().StringVar(&output, "output", "plain", "Output format: plain|json")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")

	return cmd
}

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
		output        string
	)

	cmd := &cobra.Command{
		Use:   "ensure-ready",
		Short: "Ensure an environment slot exists and is ready for CI workflows",
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

			res, err := ensureReady(cmd.Context(), logger, opts, ensureReadyRequest{
				envName:       opts.Env,
				issue:         issue,
				pr:            pr,
				slot:          slot,
				maxSlots:      maxSlots,
				codeRootBase:  codeRootBase,
				source:        source,
				prepareImages: prepareImages,
				doApply:       doApply,
				inlineVars:    inlineVars,
				varFiles:      varFiles,
			})
			if err != nil {
				return err
			}

			switch output {
			case "json":
				type out struct {
					Slot      int    `json:"slot"`
					Namespace string `json:"namespace"`
					Env       string `json:"env"`
					Created   bool   `json:"created,omitempty"`
					Recreated bool   `json:"recreated,omitempty"`
				}
				payload, _ := json.Marshal(out{
					Slot:      res.record.Slot,
					Namespace: res.record.Namespace,
					Env:       res.record.Env,
					Created:   res.created,
					Recreated: res.recreated,
				})
				fmt.Println(string(payload))
			default:
				logger.Info("environment ensured ready",
					"slot", res.record.Slot,
					"namespace", res.record.Namespace,
					"env", res.record.Env,
					"created", res.created,
					"recreated", res.recreated,
				)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&slot, "slot", 0, "Explicit slot number")
	cmd.Flags().IntVar(&issue, "issue", 0, "Issue number selector")
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number selector")
	cmd.Flags().IntVar(&maxSlots, "max", 0, "Maximum number of slots (0 means unlimited)")
	cmd.Flags().StringVar(&codeRootBase, "code-root-base", os.Getenv("CODE_ROOT_BASE"), "Base path for slot workspaces")
	cmd.Flags().StringVar(&source, "source", ".", "Source directory to sync")
	cmd.Flags().BoolVar(&prepareImages, "prepare-images", false, "Mirror external and build local images before apply")
	cmd.Flags().BoolVar(&doApply, "apply", false, "Apply manifests for the ensured environment")
	cmd.Flags().StringVar(&output, "output", "plain", "Output format: plain|json")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")

	return cmd
}

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
