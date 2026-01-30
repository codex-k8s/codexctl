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
		newManageEnvCleanupCommand(opts),
		newManageEnvSetCommand(opts),
		newManageEnvCommentCommand(opts),
	)

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
		cleanupAll    bool
	)

	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Destroy an environment for the given selector and optionally remove its state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())

			if !cleanupAll && issue <= 0 && pr <= 0 && slot <= 0 {
				return fmt.Errorf("at least one of --issue, --pr or --slot must be specified")
			}

			envName := opts.Env
			if envName == "" {
				envName = "ai"
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
			defer cancel()

			// Load stack/state store once to enumerate known environments.
			envStore, err := loadEnvSlotStore(opts.ConfigPath, envName, config.LoadOptions{Env: envName}, logger)
			if err != nil {
				return err
			}

			records, err := envStore.store.List(ctx)
			if err != nil {
				return err
			}

			stateNS := envStore.stackCfg.State.ConfigMapNamespace

			for _, rec := range records {
				if envName != "" && rec.Env != envName {
					continue
				}
				if !cleanupAll {
					if slot > 0 && rec.Slot != slot {
						continue
					}
					if issue > 0 && rec.Issue != issue {
						continue
					}
					if pr > 0 && rec.PR != pr {
						continue
					}
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

				if err := destroyStack(ctx, logger, stackSlot, ctxData, envStore.envCfg, envName); err != nil {
					return err
				}
				if err := handleDataPaths(logger, stackSlot, dataPathDelete); err != nil {
					logger.Warn("failed to delete data paths for environment", "slot", rec.Slot, "namespace", rec.Namespace, "error", err)
				}

				if withConfigMap && stateNS != "" && rec.ConfigName != "" {
					_, _ = envStore.kubeClient.RunAndCapture(ctx, nil,
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
	cmd.Flags().BoolVar(&cleanupAll, "all", false, "Cleanup all matching environments for the selected env")

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

// copyDir performs a naive recursive file copy when rsync is unavailable.
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

			envStore, err := loadEnvSlotStore(opts.ConfigPath, envName, config.LoadOptions{Env: envName}, logger)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			return envStore.store.UpdateAttributes(ctx, slot, issue, pr)
		},
	}
	cmd.Flags().StringVar(&opts.Env, "env", "ai", "Environment type (default: ai)")
	cmd.Flags().IntVar(&slot, "slot", 0, "Slot number to update (required)")
	_ = cmd.MarkFlagRequired("slot")
	cmd.Flags().IntVar(&issue, "issue", 0, "Issue number to set")
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number to set")
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

// envSlotStore bundles stack configuration, template context, environment config and state store for slot operations.
type envSlotStore struct {
	// stackCfg is the loaded stack configuration.
	stackCfg *config.StackConfig
	// templateCtx is the template context used for rendering.
	templateCtx config.TemplateContext
	// envCfg is the resolved environment configuration.
	envCfg config.Environment
	// kubeClient is the Kubernetes client for slot operations.
	kubeClient *kube.Client
	// store manages slot state persistence.
	store *state.Store
}

// loadEnvSlotStore loads stack configuration, resolves the target environment, constructs a Kubernetes client
// and initializes the state store for slot management.
func loadEnvSlotStore(
	configPath string,
	envName string,
	loadOpts config.LoadOptions,
	logger *slog.Logger,
) (*envSlotStore, error) {
	stackCfg, ctxData, err := config.LoadStackConfig(configPath, loadOpts)
	if err != nil {
		return nil, err
	}

	envCfg, err := config.ResolveEnvironment(stackCfg, envName)
	if err != nil {
		return nil, err
	}

	applyKubeconfigOverride(&envCfg)
	kubeClient := kube.NewClient(envCfg.Kubeconfig, envCfg.Context)
	store, err := state.NewStore(stackCfg, kubeClient, logger)
	if err != nil {
		return nil, err
	}

	return &envSlotStore{
		stackCfg:    stackCfg,
		templateCtx: ctxData,
		envCfg:      envCfg,
		kubeClient:  kubeClient,
		store:       store,
	}, nil
}

// allocateSlotWithRetry encapsulates the common allocation loop for helpers
// that need to allocate a new slot (e.g. ensure-slot).
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

// findMatchingEnvRecord lists environment records in the store and returns the first record
// matching the provided selector (envName/slot/issue/pr). When no record is found, it returns (nil, nil).
func findMatchingEnvRecord(
	ctx context.Context,
	store *state.Store,
	envName string,
	slot int,
	issue int,
	pr int,
) (*state.EnvRecord, error) {
	records, err := store.List(ctx)
	if err != nil {
		return nil, err
	}

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
		return &records[i], nil
	}

	return nil, nil
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
	case "kv":
		fmt.Printf("slot=%d\nnamespace=%s\nenv=%s\n", rec.Slot, rec.Namespace, rec.Env)
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
