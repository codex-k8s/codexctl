package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/env"
	"github.com/codex-k8s/codexctl/internal/hooks"
)

// newDoctorCommand creates the "doctor" subcommand that runs environment preflight checks.
func newDoctorCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run environment preflight checks",
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

			envName := opts.Env
			if envName == "" {
				envName = "dev"
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

			ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
			defer cancel()

			hookExec := hooks.NewExecutor(logger)
			if err := hookExec.RunPreflightBasic(ctx); err != nil {
				return err
			}

			if err := runDoctorChecks(ctx, logger, stackCfg, ctxData, envCfg, envName); err != nil {
				return err
			}

			logger.Info("doctor checks completed successfully", "env", envName)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment name to validate (dev, staging, ai)")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")

	return cmd
}

func runKubectlVersion(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "kubectl", "version", "--client")
	return cmd.Run()
}

func runKubectlAuthCheck(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "kubectl", "auth", "can-i", "get", "pods")
	return cmd.Run()
}

func runDockerChecks(ctx context.Context) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker binary not found in PATH: %w", err)
	}
	cmd := exec.CommandContext(ctx, "docker", "info")
	return cmd.Run()
}

func runGhChecks(ctx context.Context) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found in PATH: %w", err)
	}
	cmd := exec.CommandContext(ctx, "gh", "--version")
	return cmd.Run()
}

func hasGitHubHooks(cfg *config.StackConfig) bool {
	if cfg == nil {
		return false
	}
	if hasGitHubInSteps(cfg.Hooks.BeforeAll) || hasGitHubInSteps(cfg.Hooks.AfterAll) {
		return true
	}
	for _, infra := range cfg.Infrastructure {
		if hasGitHubInResourceHooks(infra.Hooks) {
			return true
		}
	}
	for _, svc := range cfg.Services {
		if hasGitHubInResourceHooks(svc.Hooks) {
			return true
		}
	}
	return false
}

func hasGitHubInResourceHooks(h config.ResourceHooks) bool {
	return hasGitHubInSteps(h.BeforeApply) ||
		hasGitHubInSteps(h.AfterApply) ||
		hasGitHubInSteps(h.BeforeDestroy) ||
		hasGitHubInSteps(h.AfterDestroy)
}

func hasGitHubInSteps(steps []config.HookStep) bool {
	for _, s := range steps {
		if s.Use == "github.comment" {
			return true
		}
	}
	return false
}

func runDoctorChecks(
	ctx context.Context,
	logger *slog.Logger,
	stackCfg *config.StackConfig,
	ctxData config.TemplateContext,
	envCfg config.Environment,
	envName string,
) error {
	var fatalErrs []error

	if err := runKubectlVersion(ctx); err != nil {
		logger.Error("kubectl version check failed", "error", err)
		fatalErrs = append(fatalErrs, err)
	} else {
		logger.Info("kubectl version check ok")
	}

	if err := runKubectlAuthCheck(ctx); err != nil {
		logger.Error("kubectl auth check failed", "error", err)
		fatalErrs = append(fatalErrs, err)
	} else {
		logger.Info("kubectl auth check ok")
	}

	if envCfg.LocalRegistry != nil && envCfg.LocalRegistry.Enabled {
		if err := runDockerChecks(ctx); err != nil {
			logger.Error("docker checks failed", "error", err)
			fatalErrs = append(fatalErrs, err)
		} else {
			logger.Info("docker checks ok")
		}
	}

	usesGitHub := hasGitHubHooks(stackCfg)
	if usesGitHub {
		if err := runGhChecks(ctx); err != nil {
			logger.Error("GitHub CLI checks failed", "error", err)
			fatalErrs = append(fatalErrs, err)
		} else {
			logger.Info("GitHub CLI checks ok")
		}

		codexToken := ctxData.EnvMap["CODEX_GH_PAT"]
		codexUser := ctxData.EnvMap["CODEX_GH_USERNAME"]
		if codexToken == "" || codexUser == "" {
			err := fmt.Errorf("CODEX_GH_PAT and CODEX_GH_USERNAME must be defined when GitHub hooks are used")
			logger.Error("Codex GitHub credentials missing", "error", err)
			fatalErrs = append(fatalErrs, err)
		} else {
			logger.Info("Codex GitHub credentials present")
		}
	}

	isAIEnv := envName == "ai"
	if isAIEnv {
		openaiKey := ctxData.EnvMap["OPENAI_API_KEY"]
		if openaiKey == "" {
			err := fmt.Errorf("OPENAI_API_KEY must be defined for ai environment")
			logger.Error("OpenAI API key missing", "error", err)
			fatalErrs = append(fatalErrs, err)
		} else {
			logger.Info("OpenAI API key present")
		}

		if ctxData.EnvMap["CONTEXT7_API_KEY"] == "" {
			logger.Warn("Context7 API key is not set; Context7 MCP integration will be unavailable")
		} else {
			logger.Info("Context7 API key present")
		}
	}

	if len(fatalErrs) > 0 {
		return fmt.Errorf("doctor found %d fatal issue(s); see log for details", len(fatalErrs))
	}

	return nil
}
