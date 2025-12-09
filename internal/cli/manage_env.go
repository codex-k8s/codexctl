package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/codexctl/internal/prompt"
	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/engine"
	"github.com/codex-k8s/codexctl/internal/env"
	"github.com/codex-k8s/codexctl/internal/hooks"
	"github.com/codex-k8s/codexctl/internal/kube"
	"github.com/codex-k8s/codexctl/internal/state"
)

// newManageEnvCommand creates the "manage-env" group command for ephemeral environments.
func newManageEnvCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manage-env",
		Short: "Manage ephemeral environments and slots",
	}

	cmd.AddCommand(
		newManageEnvCreateCommand(opts),
		newManageEnvEnsureSlotCommand(opts),
		newManageEnvEnsureReadyCommand(opts),
		newManageEnvCleanupCommand(opts),
		newManageEnvGCCommand(opts),
		newManageEnvResolveCommand(opts),
		newManageEnvSetCommand(opts),
		newManageEnvSyncCodeCommand(opts),
		newManageEnvCommentCommand(opts),
	)

	return cmd
}

// newManageEnvCreateCommand creates the "manage-env create" subcommand that allocates a slot.
func newManageEnvCreateCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Allocate a new ephemeral environment slot",
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

			envName := opts.Env
			if envName == "" {
				envName = "ai"
			}

			loadOpts := config.LoadOptions{
				Env:      envName,
				UserVars: inlineVars,
				VarFiles: varFiles,
			}

			stackCfg, ctxData, err := config.LoadStackConfig(opts.ConfigPath, loadOpts)
			if err != nil {
				return err
			}

			envCfg, err := config.ResolveEnvironment(stackCfg, envName)
			if err != nil {
				return err
			}

			kubeClient := kube.NewClient(envCfg.Kubeconfig, envCfg.Context)
			store, err := state.NewStore(stackCfg, kubeClient, logger)
			if err != nil {
				return err
			}

			maxSlots, _ := cmd.Flags().GetInt("max")
			issueNum, _ := cmd.Flags().GetInt("issue")
			prNum, _ := cmd.Flags().GetInt("pr")
			prefer, _ := cmd.Flags().GetInt("prefer")

			ctx, cancel := context.WithTimeout(cmd.Context(), 6*time.Hour)
			defer cancel()

			record, err := allocateSlotWithRetry(ctx, store, stackCfg, ctxData, envName, maxSlots, prefer, issueNum, prNum, logger)
			if err != nil {
				return err
			}

			logger.Info("slot allocated",
				"slot", record.Slot,
				"namespace", record.Namespace,
				"env", record.Env,
				"issue", record.Issue,
				"pr", record.PR,
			)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "ai", "Environment type for the slot (default: ai)")
	cmd.Flags().Int("max", 0, "Maximum number of slots (0 means unlimited)")
	cmd.Flags().Int("issue", 0, "GitHub issue number associated with the slot")
	cmd.Flags().Int("pr", 0, "GitHub pull request number associated with the slot")
	cmd.Flags().Int("prefer", 0, "Preferred slot number to reuse if available")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")

	return cmd
}

// newManageEnvEnsureSlotCommand creates the "manage-env ensure-slot" subcommand that
// returns an existing slot for a given selector (issue/pr/slot) or allocates a new one.
func newManageEnvEnsureSlotCommand(opts *Options) *cobra.Command {
	var issue, pr, slot, max int
	var output string

	cmd := &cobra.Command{
		Use:   "ensure-slot",
		Short: "Ensure a slot exists for the given selector (issue/pr/slot)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())

			if issue <= 0 && pr <= 0 && slot <= 0 {
				return fmt.Errorf("at least one of --issue, --pr or --slot must be specified")
			}

			inlineVars, err := env.ParseInlineVars(cmd.Flag("vars").Value.String())
			if err != nil {
				return err
			}

			varFile := cmd.Flag("var-file").Value.String()
			varFiles := []string{}
			if varFile != "" {
				varFiles = append(varFiles, varFile)
			}

			envName := opts.Env
			if envName == "" {
				envName = "ai"
			}

			loadOpts := config.LoadOptions{
				Env:      envName,
				UserVars: inlineVars,
				VarFiles: varFiles,
			}

			stackCfg, ctxData, err := config.LoadStackConfig(opts.ConfigPath, loadOpts)
			if err != nil {
				return err
			}

			envCfg, err := config.ResolveEnvironment(stackCfg, envName)
			if err != nil {
				return err
			}

			kubeClient := kube.NewClient(envCfg.Kubeconfig, envCfg.Context)
			store, err := state.NewStore(stackCfg, kubeClient, logger)
			if err != nil {
				return err
			}

			// Try to find an existing record first.
			ctxList, cancelList := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancelList()

			records, err := store.List(ctxList)
			if err != nil {
				return err
			}

			var found *state.EnvRecord
			for i := range records {
				rec := records[i]
				if envName != "" && rec.Env != envName {
					continue
				}
				if slot > 0 && rec.Slot != slot {
					continue
				}
				if issue > 0 && rec.Issue != issue {
					continue
				}
				if pr > 0 && rec.PR != pr {
					continue
				}
				found = &rec
				break
			}

			var rec state.EnvRecord
			if found != nil {
				rec = *found
			} else {
				prefer := slot
				ctxAlloc, cancelAlloc := context.WithTimeout(cmd.Context(), 6*time.Hour)
				defer cancelAlloc()

				rec, err = allocateSlotWithRetry(ctxAlloc, store, stackCfg, ctxData, envName, max, prefer, issue, pr, logger)
				if err != nil {
					return err
				}
			}

			return printResolveOutput(rec, output, logger)
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "ai", "Environment type (default: ai)")
	cmd.Flags().IntVar(&slot, "slot", 0, "Explicit slot number")
	cmd.Flags().IntVar(&issue, "issue", 0, "Issue number selector")
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number selector")
	cmd.Flags().IntVar(&max, "max", 0, "Maximum number of slots (0 means unlimited)")
	cmd.Flags().StringVar(&output, "output", "plain", "Output format: plain|json")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")

	return cmd
}

// newManageEnvEnsureReadyCommand creates the "manage-env ensure-ready" subcommand that
// ensures a slot exists, syncs code, prepares images and applies manifests.
func newManageEnvEnsureReadyCommand(opts *Options) *cobra.Command {
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
		Short: "Ensure an environment slot exists, code is synced and manifests are applied",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())

			if issue <= 0 && pr <= 0 && slot <= 0 {
				return fmt.Errorf("at least one of --issue, --pr or --slot must be specified")
			}

			inlineVars, err := env.ParseInlineVars(cmd.Flag("vars").Value.String())
			if err != nil {
				return err
			}

			varFile := cmd.Flag("var-file").Value.String()
			varFiles := []string{}
			if varFile != "" {
				varFiles = append(varFiles, varFile)
			}

			envName := opts.Env
			if envName == "" {
				envName = "ai"
			}

			loadOptsBase := config.LoadOptions{
				Env:      envName,
				UserVars: inlineVars,
				VarFiles: varFiles,
			}

			stackCfgBase, ctxBase, err := config.LoadStackConfig(opts.ConfigPath, loadOptsBase)
			if err != nil {
				return err
			}

			envCfg, err := config.ResolveEnvironment(stackCfgBase, envName)
			if err != nil {
				return err
			}

			kubeClient := kube.NewClient(envCfg.Kubeconfig, envCfg.Context)
			store, err := state.NewStore(stackCfgBase, kubeClient, logger)
			if err != nil {
				return err
			}

			// Ensure slot exists (reuse selector logic from ensure-slot).
			ctxList, cancelList := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancelList()

			records, err := store.List(ctxList)
			if err != nil {
				return err
			}

			var found *state.EnvRecord
			for i := range records {
				rec := records[i]
				if envName != "" && rec.Env != envName {
					continue
				}
				if slot > 0 && rec.Slot != slot {
					continue
				}
				if issue > 0 && rec.Issue != issue {
					continue
				}
				if pr > 0 && rec.PR != pr {
					continue
				}
				found = &rec
				break
			}

			var rec state.EnvRecord
			created := false
			if found != nil {
				rec = *found
			} else {
				prefer := slot
				ctxAlloc, cancelAlloc := context.WithTimeout(cmd.Context(), 6*time.Hour)
				defer cancelAlloc()

				rec, err = allocateSlotWithRetry(ctxAlloc, store, stackCfgBase, ctxBase, envName, maxSlots, prefer, issue, pr, logger)
				if err != nil {
					return err
				}
				created = true
			}

			recreated := false
			if !created && strings.TrimSpace(rec.Namespace) != "" {
				ctxNs, cancelNs := context.WithTimeout(cmd.Context(), 15*time.Second)
				defer cancelNs()
				nsArgs := []string{"get", "ns", rec.Namespace}
				if err := kubeClient.RunRaw(ctxNs, nil, nsArgs...); err != nil {
					logger.Warn("namespace missing for existing environment; resources will be recreated",
						"namespace", rec.Namespace,
						"slot", rec.Slot,
						"env", rec.Env,
						"error", err,
					)
					recreated = true
				}
			}

			// Optionally sync code into workspace.
			if codeRootBase != "" && source != "" {
				target := fmt.Sprintf("%s/%d/src", strings.TrimSuffix(codeRootBase, "/"), rec.Slot)
				if err := syncSources(source, target); err != nil {
					return fmt.Errorf("sync sources to %q: %w", target, err)
				}
				logger.Info("slot workspace synced (ensure-ready)",
					"slot", rec.Slot,
					"target", target,
					"source", source,
					"env", envName,
					"namespace", rec.Namespace,
				)
			}

			// Reload stack config for the concrete slot.
			loadOptsSlot := config.LoadOptions{
				Env:       envName,
				Namespace: rec.Namespace,
				Slot:      rec.Slot,
				UserVars:  inlineVars,
				VarFiles:  varFiles,
			}

			stackCfg, ctxData, err := config.LoadStackConfig(opts.ConfigPath, loadOptsSlot)
			if err != nil {
				return err
			}

			// Optionally prepare images. For existing environments with a live namespace
			// we skip heavy image work and only run it when the environment was just
			// created or needs full recreation.
			if prepareImages {
				if created || recreated {
					ctxImages, cancelImages := context.WithTimeout(cmd.Context(), 2*time.Hour)
					defer cancelImages()

					if err := mirrorExternalImages(ctxImages, logger, stackCfg); err != nil {
						return err
					}
					if err := buildImages(ctxImages, logger, stackCfg, ctxData); err != nil {
						return err
					}
				} else {
					logger.Info("skipping image preparation for existing environment",
						"slot", rec.Slot,
						"namespace", rec.Namespace,
						"env", rec.Env,
					)
				}
			}

			// Optionally apply manifests. Similar to image preparation, we only apply
			// manifests for freshly created or recreated environments to avoid
			// unnecessary redeploys and migrations on every ensure-ready call.
			if doApply {
				if created || recreated {
					ctxApply, cancelApply := context.WithTimeout(cmd.Context(), 10*time.Minute)
					defer cancelApply()

					if err := applyStack(ctxApply, logger, stackCfg, ctxData, envName, envCfg, true, true); err != nil {
						return err
					}
				} else {
					logger.Info("skipping apply for existing environment",
						"slot", rec.Slot,
						"namespace", rec.Namespace,
						"env", rec.Env,
					)
				}
			}

			if strings.ToLower(output) == "json" {
				type out struct {
					Slot      int    `json:"slot"`
					Namespace string `json:"namespace"`
					Env       string `json:"env"`
					Created   bool   `json:"created,omitempty"`
					Recreated bool   `json:"recreated,omitempty"`
				}
				payload, _ := json.Marshal(out{
					Slot:      rec.Slot,
					Namespace: rec.Namespace,
					Env:       rec.Env,
					Created:   created,
					Recreated: recreated,
				})
				fmt.Println(string(payload))
				return nil
			}

			logger.Info("environment ensured ready",
				"slot", rec.Slot,
				"namespace", rec.Namespace,
				"env", rec.Env,
				"created", created,
				"recreated", recreated,
			)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "ai", "Environment type (default: ai)")
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

// newManageEnvCleanupCommand creates the "manage-env cleanup" subcommand that destroys
// an environment slot by selector and optionally removes its state configmap.
func newManageEnvCleanupCommand(opts *Options) *cobra.Command {
	var (
		issue         int
		pr            int
		slot          int
		withConfigMap bool
	)

	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Destroy an environment for the given selector and optionally remove its state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())

			if issue <= 0 && pr <= 0 && slot <= 0 {
				return fmt.Errorf("at least one of --issue, --pr or --slot must be specified")
			}

			envName := opts.Env
			if envName == "" {
				envName = "ai"
			}

			// We first load stack config without slot/namespace to obtain state config
			// and environment connection details.
			stackCfg, _, err := config.LoadStackConfig(opts.ConfigPath, config.LoadOptions{Env: envName})
			if err != nil {
				return err
			}
			envCfg, err := config.ResolveEnvironment(stackCfg, envName)
			if err != nil {
				return err
			}

			kubeClient := kube.NewClient(envCfg.Kubeconfig, envCfg.Context)
			store, err := state.NewStore(stackCfg, kubeClient, logger)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()

			records, err := store.List(ctx)
			if err != nil {
				return err
			}

			stateNS := stackCfg.State.ConfigMapNamespace

			for _, rec := range records {
				if envName != "" && rec.Env != envName {
					continue
				}
				if slot > 0 && rec.Slot != slot {
					continue
				}
				if issue > 0 && rec.Issue != issue {
					continue
				}
				if pr > 0 && rec.PR != pr {
					continue
				}

				logger.Info("destroying environment for selector", "slot", rec.Slot, "namespace", rec.Namespace, "env", rec.Env, "issue", rec.Issue, "pr", rec.PR)

				// Load stack config for this particular slot/namespace.
				loadOptsSlot := config.LoadOptions{
					Env:       envName,
					Namespace: rec.Namespace,
					Slot:      rec.Slot,
				}
				stackSlot, ctxData, err := config.LoadStackConfig(opts.ConfigPath, loadOptsSlot)
				if err != nil {
					return err
				}

				if err := destroyStack(ctx, logger, stackSlot, ctxData, envCfg, envName); err != nil {
					return err
				}

				if withConfigMap && stateNS != "" && rec.ConfigName != "" {
					_, _ = kubeClient.RunAndCapture(ctx, nil,
						"-n", stateNS,
						"delete", "configmap", rec.ConfigName, "--ignore-not-found",
					)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "ai", "Environment type (default: ai)")
	cmd.Flags().IntVar(&slot, "slot", 0, "Explicit slot number")
	cmd.Flags().IntVar(&issue, "issue", 0, "Issue number selector")
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number selector")
	cmd.Flags().BoolVar(&withConfigMap, "with-configmap", false, "Remove state ConfigMap for the selected environment")

	return cmd
}

// newManageEnvGCCommand creates the "manage-env gc" subcommand that garbage-collects old environments.
func newManageEnvGCCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Garbage-collect stale ephemeral environments",
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

			envName := opts.Env
			if envName == "" {
				envName = "ai"
			}

			loadOpts := config.LoadOptions{
				Env:      envName,
				UserVars: inlineVars,
				VarFiles: varFiles,
			}

			stackCfg, ctxData, err := config.LoadStackConfig(opts.ConfigPath, loadOpts)
			if err != nil {
				return err
			}

			envCfg, err := config.ResolveEnvironment(stackCfg, envName)
			if err != nil {
				return err
			}

			kubeClient := kube.NewClient(envCfg.Kubeconfig, envCfg.Context)
			store, err := state.NewStore(stackCfg, kubeClient, logger)
			if err != nil {
				return err
			}

			ttlStr, _ := cmd.Flags().GetString("ttl")
			var ttl time.Duration
			if ttlStr != "" {
				ttl, err = time.ParseDuration(ttlStr)
				if err != nil {
					return fmt.Errorf("invalid ttl value %q: %w", ttlStr, err)
				}
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()

			removed, err := store.GarbageCollect(ctx, envName, ttl)
			if err != nil {
				return err
			}

			if len(removed) == 0 {
				logger.Info("no slots to garbage-collect", "env", envName)
				return nil
			}

			eng := engine.NewEngine()
			hookExec := hooks.NewExecutor(logger)

			for _, rec := range removed {
				logger.Info("performing full GC for slot", "slot", rec.Slot, "env", rec.Env, "namespace", rec.Namespace)

				slotCtx := ctxData
				slotCtx.Env = rec.Env
				slotCtx.Namespace = rec.Namespace
				slotCtx.Slot = rec.Slot

				manifests, err := eng.RenderStack(stackCfg, slotCtx)
				if err != nil {
					logger.Error("render stack for slot gc failed", "slot", rec.Slot, "error", err)
				} else {
					if err := kubeClient.Delete(ctx, manifests, true); err != nil {
						logger.Error("delete manifests during slot gc failed", "slot", rec.Slot, "error", err)
					}
				}

				if rec.Namespace != "" {
					if err := kubeClient.RunRaw(ctx, nil, "delete", "ns", rec.Namespace, "--ignore-not-found"); err != nil {
						logger.Error("namespace delete during slot gc failed", "slot", rec.Slot, "namespace", rec.Namespace, "error", err)
					}
				}

				if rec.Issue != 0 || rec.PR != 0 {
					body := "Environment slot {{ .Slot }} (namespace {{ .Namespace }}) was removed due to TTL expiration."
					step := config.HookStep{
						Name: "slot-ttl-comment",
						Use:  "github.comment",
						With: map[string]any{
							"body":  body,
							"issue": rec.Issue,
							"pr":    rec.PR,
						},
					}
					hookCtx := hooks.StepContext{
						Stack:      stackCfg,
						Template:   slotCtx,
						EnvName:    envName,
						KubeClient: kubeClient,
					}
					if err := hookExec.RunSteps(ctx, []config.HookStep{step}, hookCtx); err != nil {
						logger.Error("github.comment during slot gc failed", "slot", rec.Slot, "error", err)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "ai", "Environment type to clean (default: ai)")
	cmd.Flags().String("ttl", "", "Time-to-live for environments (e.g. 24h)")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")

	return cmd
}

// syncSources copies files from source to target using rsync (if available) or simple copy.
func syncSources(source, target string) error {
	// Ensure trailing slash for rsync semantics
	src := source
	if !strings.HasSuffix(src, string(os.PathSeparator)) {
		src += string(os.PathSeparator)
	}
	tgt := target
	if !strings.HasSuffix(tgt, string(os.PathSeparator)) {
		tgt += string(os.PathSeparator)
	}

	// Ensure target directory hierarchy exists before syncing.
	if err := os.MkdirAll(tgt, 0o755); err != nil {
		return fmt.Errorf("create target dir %q: %w", tgt, err)
	}

	if _, err := exec.LookPath("rsync"); err == nil {
		cmd := exec.Command("rsync", "-a", "--delete", "--no-perms", "--no-owner", "--no-group", src, tgt)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("rsync sources: %w", err)
		}
		return nil
	}

	return copyDir(src, tgt)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func fixWorkspacePermissions(ctx context.Context, client *kube.Client, namespace string) error {
	u := os.Getuid()
	g := os.Getgid()
	cmdArgs := []string{
		"-n", namespace,
		"exec", "deploy/codex",
		"--",
		"sh", "-lc",
		fmt.Sprintf("chown -R %d:%d /workspace || true; chmod -R g+rwX /workspace || true", u, g),
	}
	return client.RunRaw(ctx, nil, cmdArgs...)
}

// newManageEnvResolveCommand creates "manage-env resolve" to find slot/namespace by slot/issue/pr.
func newManageEnvResolveCommand(opts *Options) *cobra.Command {
	var issue, pr, slot int
	var output string
	cmd := &cobra.Command{
		Use:   "resolve",
		Short: "Resolve slot and namespace by slot/issue/pr",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())
			envName := opts.Env
			if envName == "" {
				envName = "ai"
			}

			loadOpts := config.LoadOptions{
				Env: envName,
			}

			stackCfg, _, err := config.LoadStackConfig(opts.ConfigPath, loadOpts)
			if err != nil {
				return err
			}
			envCfg, err := config.ResolveEnvironment(stackCfg, envName)
			if err != nil {
				return err
			}

			kubeClient := kube.NewClient(envCfg.Kubeconfig, envCfg.Context)
			store, err := state.NewStore(stackCfg, kubeClient, logger)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			records, err := store.List(ctx)
			if err != nil {
				return err
			}

			var found *state.EnvRecord
			for i := range records {
				rec := records[i]
				if envName != "" && rec.Env != envName {
					continue
				}
				if slot > 0 && rec.Slot != slot {
					continue
				}
				if issue > 0 && rec.Issue != issue {
					continue
				}
				if pr > 0 && rec.PR != pr {
					continue
				}
				found = &rec
				break
			}

			if found == nil {
				return fmt.Errorf("no slot found (env=%s, slot=%d, issue=%d, pr=%d)", envName, slot, issue, pr)
			}

			switch strings.ToLower(output) {
			case "json":
				type out struct {
					Slot      int    `json:"slot"`
					Namespace string `json:"namespace"`
					Env       string `json:"env"`
				}
				payload, _ := json.Marshal(out{Slot: found.Slot, Namespace: found.Namespace, Env: found.Env})
				fmt.Println(string(payload))
			default:
				logger.Info("resolved slot",
					"slot", found.Slot,
					"namespace", found.Namespace,
					"env", found.Env,
					"issue", found.Issue,
					"pr", found.PR,
				)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "ai", "Environment type (default: ai)")
	cmd.Flags().IntVar(&slot, "slot", 0, "Explicit slot number")
	cmd.Flags().IntVar(&issue, "issue", 0, "Issue number to resolve")
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number to resolve")
	cmd.Flags().StringVar(&output, "output", "plain", "Output format: plain|json")

	return cmd
}

// newManageEnvSetCommand creates "manage-env set" to patch issue/pr fields for a slot.
func newManageEnvSetCommand(opts *Options) *cobra.Command {
	var issue, pr, slot int
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Update metadata (issue/pr) for a slot",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())
			if slot <= 0 {
				return fmt.Errorf("slot must be >0")
			}

			envName := opts.Env
			if envName == "" {
				envName = "ai"
			}

			stackCfg, _, err := config.LoadStackConfig(opts.ConfigPath, config.LoadOptions{Env: envName})
			if err != nil {
				return err
			}
			envCfg, err := config.ResolveEnvironment(stackCfg, envName)
			if err != nil {
				return err
			}
			store, err := state.NewStore(stackCfg, kube.NewClient(envCfg.Kubeconfig, envCfg.Context), logger)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			return store.UpdateAttributes(ctx, slot, issue, pr)
		},
	}
	cmd.Flags().StringVar(&opts.Env, "env", "ai", "Environment type (default: ai)")
	cmd.Flags().IntVar(&slot, "slot", 0, "Slot number to update (required)")
	_ = cmd.MarkFlagRequired("slot")
	cmd.Flags().IntVar(&issue, "issue", 0, "Issue number to set")
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number to set")
	return cmd
}

// newManageEnvSyncCodeCommand syncs repository sources into slot workspace with optional chown.
func newManageEnvSyncCodeCommand(opts *Options) *cobra.Command {
	var slot int
	var namespace string
	var codeRootBase string
	var source string
	cmd := &cobra.Command{
		Use:   "sync-code",
		Short: "Sync sources into slot workspace (rsync + optional chmod in codex pod)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())
			if slot <= 0 {
				return fmt.Errorf("slot must be >0")
			}
			if codeRootBase == "" {
				return fmt.Errorf("code-root-base is required")
			}
			if source == "" {
				source = "."
			}

			target := fmt.Sprintf("%s/%d/src", strings.TrimSuffix(codeRootBase, "/"), slot)
			if namespace == "" {
				if err := os.RemoveAll(target); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("cleanup target dir %q: %w", target, err)
				}
				if err := os.MkdirAll(target, 0o755); err != nil {
					return fmt.Errorf("create target dir %q: %w", target, err)
				}
			}
			if err := syncSources(source, target); err != nil {
				return err
			}

			if namespace != "" {
				envName := opts.Env
				if envName == "" {
					envName = "ai"
				}
				stackCfg, _, err := config.LoadStackConfig(opts.ConfigPath, config.LoadOptions{Env: envName})
				if err != nil {
					return err
				}
				envCfg, err := config.ResolveEnvironment(stackCfg, envName)
				if err != nil {
					return err
				}
				kClient := kube.NewClient(envCfg.Kubeconfig, envCfg.Context)
				if err := fixWorkspacePermissions(cmd.Context(), kClient, namespace); err != nil {
					logger.Warn("workspace permission fix failed", "error", err)
				}
			}

			logger.Info("slot workspace synced",
				"slot", slot,
				"target", target,
				"source", source,
				"env", opts.Env,
				"namespace", namespace,
			)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Env, "env", "ai", "Environment type (default: ai)")
	cmd.Flags().IntVar(&slot, "slot", 0, "Slot number to sync (required)")
	_ = cmd.MarkFlagRequired("slot")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Namespace of slot (for permission fix)")
	cmd.Flags().StringVar(&codeRootBase, "code-root-base", os.Getenv("CODE_ROOT_BASE"), "Base path for slot workspaces")
	cmd.Flags().StringVar(&source, "source", ".", "Source directory to sync")
	return cmd
}

// newManageEnvCommentCommand renders a comment with env links.
func newManageEnvCommentCommand(opts *Options) *cobra.Command {
	var lang string
	var slot int
	cmd := &cobra.Command{
		Use:   "comment",
		Short: "Render environment links for PR/Issue comments",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if slot <= 0 {
				return fmt.Errorf("slot must be >0")
			}
			envName := opts.Env
			if envName == "" {
				envName = "ai"
			}
			_, ctxData, err := config.LoadStackConfig(opts.ConfigPath, config.LoadOptions{Env: envName, Slot: slot})
			if err != nil {
				return err
			}
			siteHost := ctxData.BaseDomain[envName]
			if envName == "ai" {
				siteHost = fmt.Sprintf("dev-%d.%s", slot, ctxData.BaseDomain["ai"])
			}

			body, err := prompt.RenderEnvComment(strings.ToLower(lang), siteHost, slot, ctxData.Codex.Links)
			if err != nil {
				return err
			}
			fmt.Println(body)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Env, "env", "ai", "Environment (default: ai)")
	cmd.Flags().IntVar(&slot, "slot", 0, "Slot number (required)")
	_ = cmd.MarkFlagRequired("slot")
	cmd.Flags().StringVar(&lang, "lang", "en", "Language for the comment (en|ru)")
	return cmd
}

// allocateSlotWithRetry encapsulates the common allocation loop for both "create"
// and higher-level helpers that need to allocate a new slot.
func allocateSlotWithRetry(
	ctx context.Context,
	store *state.Store,
	stackCfg *config.StackConfig,
	baseCtx config.TemplateContext,
	envName string,
	maxSlots int,
	prefer int,
	issue int,
	pr int,
	logger *slog.Logger,
) (state.EnvRecord, error) {
	const retryDelay = 30 * time.Second

	for {
		record, err := store.AllocateSlot(ctx, stackCfg, baseCtx, envName, maxSlots, prefer, issue, pr)
		if err == nil {
			return record, nil
		}

		// If the error is not about lack of free slots, return immediately.
		if !state.IsNoFreeSlotError(err) {
			return state.EnvRecord{}, err
		}

		logger.Info("no free slot available yet; waiting before retry",
			"env", envName,
			"maxSlots", maxSlots,
			"retryDelay", retryDelay.String(),
		)

		select {
		case <-ctx.Done():
			return state.EnvRecord{}, fmt.Errorf("timed out waiting for free slot: %w", ctx.Err())
		case <-time.After(retryDelay):
		}
	}
}

// printResolveOutput prints a slot resolution result in plain or JSON format.
func printResolveOutput(rec state.EnvRecord, output string, logger *slog.Logger) error {
	switch strings.ToLower(output) {
	case "json":
		type out struct {
			Slot      int    `json:"slot"`
			Namespace string `json:"namespace"`
			Env       string `json:"env"`
		}
		payload, _ := json.Marshal(out{Slot: rec.Slot, Namespace: rec.Namespace, Env: rec.Env})
		fmt.Println(string(payload))
	default:
		logger.Info("resolved slot",
			"slot", rec.Slot,
			"namespace", rec.Namespace,
			"env", rec.Env,
			"issue", rec.Issue,
			"pr", rec.PR,
		)
	}
	return nil
}
