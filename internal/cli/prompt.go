package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/env"
	"github.com/codex-k8s/codexctl/internal/kube"
	"github.com/codex-k8s/codexctl/internal/prompt"
)

// newPromptCommand creates the "prompt" group command for AI prompt operations.
func newPromptCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Work with AI prompts and Codex agents",
	}

	cmd.AddCommand(
		newPromptRenderCommand(opts),
		newPromptConfigCommand(opts),
		newPromptRunCommand(opts),
	)

	return cmd
}

// newPromptRunCommand creates the "prompt run" subcommand that executes a Codex agent
// inside a Kubernetes pod using the rendered configuration and prompt.
func newPromptRunCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a Codex agent inside a Kubernetes environment",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())

			inlineVars, err := env.ParseInlineVars(cmd.Flag("vars").Value.String())
			if err != nil {
				return err
			}

			issue, _ := cmd.Flags().GetInt("issue")
			pr, _ := cmd.Flags().GetInt("pr")
			if issue > 0 {
				inlineVars["ISSUE_NUMBER"] = fmt.Sprintf("%d", issue)
			}
			if pr > 0 {
				inlineVars["PR_NUMBER"] = fmt.Sprintf("%d", pr)
			}

			varFile := cmd.Flag("var-file").Value.String()
			varFiles := []string{}
			if varFile != "" {
				varFiles = append(varFiles, varFile)
			}

			langFlag := cmd.Flag("lang").Value.String()
			lang := langFlag
			if lang == "" {
				lang = os.Getenv("CODEX_PROMPT_LANG")
			}
			if lang == "" {
				lang = "en"
			}

			if _, ok := inlineVars["CODEX_PROMPT_LANG"]; !ok && lang != "" {
				inlineVars["CODEX_PROMPT_LANG"] = lang
			}
			if _, ok := inlineVars["PROMPT_LANG"]; !ok && lang != "" {
				inlineVars["PROMPT_LANG"] = lang
			}

			slot, err := cmd.Flags().GetInt("slot")
			if err != nil {
				return err
			}
			if slot <= 0 {
				return fmt.Errorf("slot must be a positive integer")
			}

			envName := opts.Env
			if envName == "" {
				envName = "ai"
			}

			loadOpts := config.LoadOptions{
				Env:       envName,
				Namespace: opts.Namespace,
				Slot:      slot,
				UserVars:  inlineVars,
				VarFiles:  varFiles,
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

			r := prompt.NewRenderer(stackCfg, ctxData)

			// Render Codex config and write it into ~/.codex/config.toml inside the Codex pod.
			configBytes, err := r.RenderCodexConfig()
			if err != nil {
				return err
			}

			ns := ctxData.Namespace
			if ns == "" {
				return fmt.Errorf("namespace is empty for env=%q slot=%d; ensure namespace.patterns are configured", envName, slot)
			}

			execTimeout := 60 * time.Minute
			if t := ctxData.Codex.Timeouts.Exec; t != "" {
				if d, parseErr := time.ParseDuration(t); parseErr == nil {
					execTimeout = d
				} else {
					logger.Warn("invalid codex.exec timeout, using default", "value", t, "default", execTimeout.String(), "error", parseErr)
				}
			}

			ctxExec, cancel := context.WithTimeout(cmd.Context(), execTimeout)
			defer cancel()

			logger.Info("waiting for codex deployment to be ready", "namespace", ns)
			rolloutTimeout := ctxData.Codex.Timeouts.Rollout
			if rolloutTimeout == "" {
				rolloutTimeout = "300s"
			}
			if err := kubeClient.RunRaw(
				ctxExec,
				nil,
				"-n", ns,
				"rollout", "status", "deploy/codex",
				"--timeout="+rolloutTimeout,
			); err != nil {
				return err
			}

			logger.Info("uploading Codex config into pod", "namespace", ns)
			if err := kubeClient.RunRaw(
				ctxExec,
				configBytes,
				"-n", ns,
				"exec", "deploy/codex",
				"--", "sh", "-lc",
				"mkdir -p ~/.codex && cat > ~/.codex/config.toml",
			); err != nil {
				return fmt.Errorf("write Codex config inside pod: %w", err)
			}

			// Configure git and GitHub CLI inside the Codex container.
			if err := kubeClient.RunRaw(
				ctxExec,
				nil,
				"-n", ns,
				"exec", "deploy/codex",
				"--", "sh", "-lc",
				"git config --global --add safe.directory /workspace || true; "+
					"git config --global user.name \"codex-bot\"; "+
					"git config --global user.email \"codex-bot@codex-k8s.local\" || true",
			); err != nil {
				logger.Warn("failed to configure git inside Codex pod", "namespace", ns, "error", err)
			}
			if err := kubeClient.RunRaw(
				ctxExec,
				nil,
				"-n", ns,
				"exec", "deploy/codex",
				"--", "sh", "-lc",
				"if [ -n \"$CODEX_GH_PAT\" ]; then printf %s \"$CODEX_GH_PAT\" | gh auth login --with-token >/dev/null 2>&1 || true; fi",
			); err != nil {
				logger.Warn("failed to authenticate gh inside Codex pod", "namespace", ns, "error", err)
			}
			if err := kubeClient.RunRaw(
				ctxExec,
				nil,
				"-n", ns,
				"exec", "deploy/codex",
				"--", "sh", "-lc",
				"printenv OPENAI_API_KEY | npx -y @openai/codex login --with-api-key >/dev/null 2>&1 || true",
			); err != nil {
				logger.Warn("failed to login Codex CLI with OPENAI_API_KEY", "namespace", ns, "error", err)
			}

			kind := cmd.Flag("kind").Value.String()
			templatePath := cmd.Flag("template").Value.String()
			if kind == "" && templatePath == "" {
				kind = prompt.PromptKindDevIssue
			}

			var promptText []byte
			if kind != "" && templatePath == "" {
				renderedPrompt, usedFallback, err := r.RenderBuiltinPrompt(kind, lang)
				if err != nil {
					return err
				}
				if usedFallback {
					logger.Warn("prompt language not found, falling back to default", "lang", lang, "default", "en", "kind", kind)
				}
				promptText = renderedPrompt
			} else {
				renderedPrompt, err := r.RenderPrompt(templatePath)
				if err != nil {
					return err
				}
				promptText = renderedPrompt
			}

			logger.Info("uploading prompt into Codex pod", "namespace", ns)
			if err := kubeClient.RunRaw(
				ctxExec,
				promptText,
				"-n", ns,
				"exec", "deploy/codex",
				"--", "sh", "-lc",
				"cat > /tmp/codex_prompt.txt",
			); err != nil {
				return fmt.Errorf("upload prompt into pod: %w", err)
			}

			resume, _ := cmd.Flags().GetBool("resume")
			execArgs := "npx -y @openai/codex exec \"$(cat /tmp/codex_prompt.txt)\" --json"
			if resume {
				execArgs = execArgs + " resume --last"
			}

			logger.Info("starting Codex execution", "namespace", ns, "slot", slot, "kind", kind)
			if err := kubeClient.RunRaw(
				ctxExec,
				nil,
				"-n", ns,
				"exec", "deploy/codex",
				"--", "sh", "-lc",
				"cd /workspace && "+execArgs,
			); err != nil {
				return fmt.Errorf("run Codex exec inside pod: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "ai", "Environment to use for Codex run (default: ai)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace override (normally derived from services.yaml patterns)")
	cmd.Flags().Int("slot", 0, "Slot number to use for Codex environment (required)")
	cmd.Flags().Int("issue", 0, "GitHub issue number associated with this run")
	cmd.Flags().Int("pr", 0, "GitHub pull request number associated with this run")
	cmd.Flags().Bool("resume", false, "Resume Codex session instead of starting a new one")
	cmd.Flags().String("kind", "", "Builtin prompt kind (e.g. dev_issue, review_fix)")
	cmd.Flags().String("template", "", "Path to prompt template file (overrides --kind when set)")
	cmd.Flags().String("lang", "", "Prompt language (e.g. en, ru); overrides CODEX_PROMPT_LANG and defaults to en")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")

	return cmd
}

// newPromptConfigCommand creates the "prompt config" subcommand that renders
// the Codex config template defined in services.yaml (codex.configTemplate).
func newPromptConfigCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Render Codex config template defined in services.yaml",
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

			langFlag := cmd.Flag("lang").Value.String()
			lang := langFlag
			if lang == "" {
				lang = os.Getenv("CODEX_PROMPT_LANG")
			}
			if lang == "" {
				lang = "en"
			}

			if _, ok := inlineVars["CODEX_PROMPT_LANG"]; !ok && lang != "" {
				inlineVars["CODEX_PROMPT_LANG"] = lang
			}
			if _, ok := inlineVars["PROMPT_LANG"]; !ok && lang != "" {
				inlineVars["PROMPT_LANG"] = lang
			}

			slot, err := cmd.Flags().GetInt("slot")
			if err != nil {
				return err
			}

			envName := opts.Env
			if envName == "" {
				envName = "ai"
			}

			loadOpts := config.LoadOptions{
				Env:       envName,
				Namespace: opts.Namespace,
				Slot:      slot,
				UserVars:  inlineVars,
				VarFiles:  varFiles,
			}

			stackCfg, ctx, err := config.LoadStackConfig(opts.ConfigPath, loadOpts)
			if err != nil {
				return err
			}

			r := prompt.NewRenderer(stackCfg, ctx)
			rendered, err := r.RenderCodexConfig()
			if err != nil {
				return err
			}

			outPath := cmd.Flag("out").Value.String()
			if outPath == "" {
				_, writeErr := os.Stdout.Write(rendered)
				return writeErr
			}

			dir := filepath.Dir(outPath)
			if dir != "" && dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("create output directory %q: %w", dir, err)
				}
			}

			if err := os.WriteFile(outPath, rendered, 0o644); err != nil {
				return fmt.Errorf("write Codex config to %q: %w", outPath, err)
			}

			logger.Info("rendered Codex config", "output", outPath, "lang", lang, "env", envName, "slot", slot, "namespace", ctx.Namespace)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to use for rendering (dev, staging, ai)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace context for rendering")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")
	cmd.Flags().Int("slot", 0, "Slot number for ai environments")
	cmd.Flags().String("lang", "", "Prompt language (e.g. en, ru); overrides CODEX_PROMPT_LANG and defaults to en")
	cmd.Flags().String("out", "", "Output file path for rendered Codex config (default: stdout)")

	return cmd
}

// newPromptRenderCommand creates the "prompt render" subcommand that renders prompt templates.
func newPromptRenderCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render an AI prompt template using services.yaml context",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())

			templatePath := cmd.Flag("template").Value.String()
			kind := cmd.Flag("kind").Value.String()
			if templatePath == "" && kind == "" {
				return fmt.Errorf("either --template or --kind must be specified")
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

			langFlag := cmd.Flag("lang").Value.String()
			lang := langFlag
			if lang == "" {
				lang = os.Getenv("CODEX_PROMPT_LANG")
			}
			if lang == "" {
				lang = "en"
			}

			if _, ok := inlineVars["CODEX_PROMPT_LANG"]; !ok && lang != "" {
				inlineVars["CODEX_PROMPT_LANG"] = lang
			}
			if _, ok := inlineVars["PROMPT_LANG"]; !ok && lang != "" {
				inlineVars["PROMPT_LANG"] = lang
			}

			slot, err := cmd.Flags().GetInt("slot")
			if err != nil {
				return err
			}

			envName := opts.Env
			if envName == "" {
				envName = "ai"
			}

			loadOpts := config.LoadOptions{
				Env:       envName,
				Namespace: opts.Namespace,
				Slot:      slot,
				UserVars:  inlineVars,
				VarFiles:  varFiles,
			}

			stackCfg, ctx, err := config.LoadStackConfig(opts.ConfigPath, loadOpts)
			if err != nil {
				return err
			}

			r := prompt.NewRenderer(stackCfg, ctx)
			var rendered []byte
			if kind != "" && templatePath == "" {
				var usedFallback bool
				rendered, usedFallback, err = r.RenderBuiltinPrompt(kind, lang)
				if err != nil {
					return err
				}
				if usedFallback {
					logger.Warn("prompt language not found, falling back to default", "lang", lang, "default", "en", "kind", kind)
				}
			} else {
				rendered, err = r.RenderPrompt(templatePath)
				if err != nil {
					return err
				}
			}

			outPath := cmd.Flag("out").Value.String()
			if outPath == "" {
				_, writeErr := os.Stdout.Write(rendered)
				return writeErr
			}

			dir := filepath.Dir(outPath)
			if dir != "" && dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("create output directory %q: %w", dir, err)
				}
			}

			if err := os.WriteFile(outPath, rendered, 0o644); err != nil {
				return fmt.Errorf("write rendered prompt to %q: %w", outPath, err)
			}

			logger.Info("rendered prompt", "template", templatePath, "output", outPath, "lang", lang, "env", envName, "slot", slot, "namespace", ctx.Namespace)
			return nil
		},
	}

	cmd.Flags().String("template", "", "Path to prompt template file")
	cmd.Flags().String("out", "", "Output file path for rendered prompt")
	cmd.Flags().String("kind", "", "Builtin prompt kind (e.g. dev_issue, review_fix)")
	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to use for rendering (dev, staging, ai)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace context for rendering")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")
	cmd.Flags().Int("slot", 0, "Slot number for ai environments")
	cmd.Flags().String("lang", "", "Prompt language (e.g. en, ru); overrides CODEX_PROMPT_LANG and defaults to en")

	return cmd
}
