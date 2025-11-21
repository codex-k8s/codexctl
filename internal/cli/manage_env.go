package cli

import (
	"context"
	"encoding/json"
	"fmt"
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

			// Block while waiting for a free slot: when all slots are busy, do not fail immediately,
			// but retry allocation until a global timeout is reached.
			const waitTimeout = 6 * time.Hour
			const retryDelay = 30 * time.Second

			ctx, cancel := context.WithTimeout(cmd.Context(), waitTimeout)
			defer cancel()

			for {
				record, err := store.AllocateSlot(ctx, stackCfg, ctxData, envName, maxSlots, prefer, issueNum, prNum)
				if err == nil {
					logger.Info("slot allocated",
						"slot", record.Slot,
						"namespace", record.Namespace,
						"env", record.Env,
						"issue", record.Issue,
						"pr", record.PR,
					)
					return nil
				}

				// If the error is not about lack of free slots, return immediately.
				if !state.IsNoFreeSlotError(err) {
					return err
				}

				logger.Info("no free slot available yet; waiting before retry",
					"env", envName,
					"maxSlots", maxSlots,
					"retryDelay", retryDelay.String(),
				)

				select {
				case <-ctx.Done():
					return fmt.Errorf("timed out waiting for free slot (waited %s): %w", waitTimeout, ctx.Err())
				case <-time.After(retryDelay):
				}
			}
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
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create target dir %q: %w", target, err)
	}

	// Ensure trailing slash for rsync semantics
	src := source
	if !strings.HasSuffix(src, string(os.PathSeparator)) {
		src += string(os.PathSeparator)
	}
	tgt := target
	if !strings.HasSuffix(tgt, string(os.PathSeparator)) {
		tgt += string(os.PathSeparator)
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

func renderEnvComment(host string, slot int, lang string) string {
	switch lang {
	case "ru":
		return fmt.Sprintf("🧪 Завершён запуск Codex\n\n- Slot: %d\n- Host: https://%s\n- Explorer: https://%s/explorer\n- gRPC Swagger: https://%s/grpc/swagger/\n", slot, host, host, host)
	default:
		return fmt.Sprintf("🧪 Codex run completed\n\n- Slot: %d\n- Host: https://%s\n- Explorer: https://%s/explorer\n- gRPC Swagger: https://%s/grpc/swagger/\n", slot, host, host, host)
	}
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
