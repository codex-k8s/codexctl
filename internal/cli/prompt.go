package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/env"
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
	)

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
