package cli

import (
	"context"
	"fmt"
	"time"

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

			ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
			defer cancel()

			record, err := store.AllocateSlot(ctx, stackCfg, ctxData, envName, maxSlots, prefer, issueNum, prNum)
			if err != nil {
				return err
			}

			fmt.Printf("slot: %d\n", record.Slot)
			fmt.Printf("namespace: %s\n", record.Namespace)
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
