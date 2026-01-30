package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/kube"
	"github.com/codex-k8s/codexctl/internal/prompt"
)

// newPromptCommand creates the "prompt" group command for AI prompt operations.
func newPromptCommand(opts *Options) *cobra.Command {
	return newGroupCommand(
		"prompt",
		"Work with AI prompts and Codex agents",
		newPromptRunCommand(opts),
	)
}

// newPromptRunCommand creates the "prompt run" subcommand that executes a Codex agent
// inside a Kubernetes pod using the rendered configuration and prompt.
func newPromptRunCommand(opts *Options) *cobra.Command {
	var infraUnhealthy bool
	var modelOverride string
	var reasoningOverride string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a Codex agent inside a Kubernetes environment",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())

			envVars := promptEnv{}
			if err := parseEnv(&envVars); err != nil {
				return err
			}

			inlineVars, varFiles, err := parseInlineVarsAndFiles(cmd)
			if err != nil {
				return err
			}

			issue, _ := cmd.Flags().GetInt("issue")
			pr, _ := cmd.Flags().GetInt("pr")
			if !cmd.Flags().Changed("issue") && envPresent("CODEXCTL_ISSUE_NUMBER") {
				issue = envVars.Issue
			}
			if !cmd.Flags().Changed("pr") && envPresent("CODEXCTL_PR_NUMBER") {
				pr = envVars.PR
			}
			if issue > 0 {
				inlineVars["CODEXCTL_ISSUE_NUMBER"] = fmt.Sprintf("%d", issue)
			}
			if pr > 0 {
				inlineVars["CODEXCTL_PR_NUMBER"] = fmt.Sprintf("%d", pr)
			}
			if envPresent("CODEXCTL_FOCUS_ISSUE_NUMBER") && envVars.FocusIssue > 0 {
				if _, ok := inlineVars["CODEXCTL_FOCUS_ISSUE_NUMBER"]; !ok {
					inlineVars["CODEXCTL_FOCUS_ISSUE_NUMBER"] = fmt.Sprintf("%d", envVars.FocusIssue)
				}
			}

			if !cmd.Flags().Changed("infra-unhealthy") && envPresent("CODEXCTL_INFRA_UNHEALTHY") {
				infraUnhealthy = envVars.InfraUnhealthy
			}
			if infraUnhealthy {
				inlineVars["CODEXCTL_INFRA_UNHEALTHY"] = "1"
			}

			slot, err := cmd.Flags().GetInt("slot")
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("slot") && envPresent("CODEXCTL_SLOT") {
				slot = envVars.Slot
			}
			if slot <= 0 {
				return fmt.Errorf("slot must be a positive integer")
			}
			// Expose slot both via template context and as CODEXCTL_SLOT env-style variable for templates.
			inlineVars["CODEXCTL_SLOT"] = fmt.Sprintf("%d", slot)

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
			langFlag := strings.TrimSpace(cmd.Flag("lang").Value.String())
			if langFlag == "" && !cmd.Flags().Changed("lang") && envPresent("CODEXCTL_LANG") {
				langFlag = strings.TrimSpace(envVars.Lang)
			}
			lang := langFlag
			if lang == "" {
				lang = strings.TrimSpace(ctxData.EnvMap["CODEXCTL_LANG"])
			}
			if lang == "" {
				lang = strings.TrimSpace(stackCfg.Codex.PromptLang)
			}
			if lang == "" {
				lang = "en"
			}
			if strings.TrimSpace(ctxData.EnvMap["CODEXCTL_LANG"]) == "" {
				ctxData.EnvMap["CODEXCTL_LANG"] = lang
			}

			envConfig, err := config.ResolveEnvironment(stackCfg, envName)
			if err != nil {
				return err
			}

			kubeClient := kube.NewClient(envConfig.Kubeconfig, envConfig.Context)

			if raw := strings.TrimSpace(ctxData.EnvMap["CODEXCTL_MODEL"]); raw != "" {
				model, err := normalizeModel(raw)
				if err != nil {
					return err
				}
				ctxData.Codex.Model = model
				stackCfg.Codex.Model = model
			}
			if raw := strings.TrimSpace(ctxData.EnvMap["CODEXCTL_MODEL_REASONING_EFFORT"]); raw != "" {
				effort, err := normalizeReasoningEffort(raw)
				if err != nil {
					return err
				}
				ctxData.Codex.ModelReasoningEffort = effort
				stackCfg.Codex.ModelReasoningEffort = effort
			}

			applyIssueCodexOverrides(cmd.Context(), logger, envName, issue, pr, stackCfg, &ctxData)
			applyIssueContext(cmd.Context(), logger, envName, issue, pr, ctxData.EnvMap["CODEXCTL_FOCUS_ISSUE_NUMBER"], &ctxData)
			if !cmd.Flags().Changed("model") && envPresent("CODEXCTL_MODEL") {
				modelOverride = envVars.Model
			}
			if !cmd.Flags().Changed("reasoning-effort") && envPresent("CODEXCTL_MODEL_REASONING_EFFORT") {
				reasoningOverride = envVars.ReasoningEffort
			}
			if strings.TrimSpace(modelOverride) != "" {
				model, err := normalizeModel(modelOverride)
				if err != nil {
					return err
				}
				ctxData.Codex.Model = model
				stackCfg.Codex.Model = model
			}
			if strings.TrimSpace(reasoningOverride) != "" {
				effort, err := normalizeReasoningEffort(reasoningOverride)
				if err != nil {
					return err
				}
				ctxData.Codex.ModelReasoningEffort = effort
				stackCfg.Codex.ModelReasoningEffort = effort
			}
			if strings.TrimSpace(ctxData.Codex.Model) != "" {
				model, err := normalizeModel(ctxData.Codex.Model)
				if err != nil {
					return err
				}
				ctxData.Codex.Model = model
				stackCfg.Codex.Model = model
			}
			if strings.TrimSpace(ctxData.Codex.ModelReasoningEffort) != "" {
				effort, err := normalizeReasoningEffort(ctxData.Codex.ModelReasoningEffort)
				if err != nil {
					return err
				}
				ctxData.Codex.ModelReasoningEffort = effort
				stackCfg.Codex.ModelReasoningEffort = effort
			}

			resumeFlag, _ := cmd.Flags().GetBool("resume")
			if !cmd.Flags().Changed("resume") && envPresent("CODEXCTL_RESUME") {
				resumeFlag = envVars.Resume
			}
			promptMode := strings.TrimSpace(ctxData.EnvMap["CODEXCTL_PROMPT_MODE"])
			if promptMode == "" {
				if resumeFlag {
					promptMode = "short"
				} else {
					promptMode = "full"
				}
			}
			if strings.TrimSpace(ctxData.EnvMap["CODEXCTL_PROMPT_CONTINUATION"]) != "" {
				promptMode = "full"
			}
			if promptMode != "" {
				ctxData.EnvMap["CODEXCTL_PROMPT_MODE"] = promptMode
			}

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
				rolloutTimeout = "1200s"
			}
			if err := kubeClient.RunRaw(
				ctxExec,
				nil,
				"-n", ns,
				"rollout", "status", "deploy/codex",
				"--timeout="+rolloutTimeout,
			); err != nil {
				if infraUnhealthy {
					logger.Warn("codex rollout not ready; continuing due to infra-unhealthy", "namespace", ns, "error", err)
				} else {
					return err
				}
			}

			logger.Info("uploading Codex config into pod", "namespace", ns)
			const maxConfigPreview = 1024
			configPreview := string(configBytes)
			if len(configPreview) > maxConfigPreview {
				configPreview = configPreview[:maxConfigPreview] + "...(truncated)"
			}
			logger.Debug("codex config preview", "config", configPreview)
			if err := kubeClient.RunRaw(
				ctxExec,
				configBytes,
				"-n", ns,
				"exec", "-i", "deploy/codex",
				"--", "sh", "-lc",
				"mkdir -p ~/.codex && cat > ~/.codex/config.toml",
			); err != nil {
				if infraUnhealthy {
					logger.Warn("failed to upload Codex config; continuing due to infra-unhealthy", "namespace", ns, "error", err)
					return nil
				}
				return fmt.Errorf("write Codex config inside pod: %w", err)
			}

			// Configure git and GitHub CLI inside the Codex container.
			if err := runCodexPodShell(
				ctxExec,
				kubeClient,
				ns,
				"git config --global --add safe.directory /workspace || true; "+
					"git config --global user.name \"codex-bot\"; "+
					"git config --global user.email \"codex-bot@codex-k8s.local\" || true",
			); err != nil {
				logger.Warn("failed to configure git inside Codex pod", "namespace", ns, "error", err)
			}
			if err := runCodexPodShell(
				ctxExec,
				kubeClient,
				ns,
				"if [ -n \"$CODEXCTL_GH_PAT\" ]; then printf %s \"$CODEXCTL_GH_PAT\" | gh auth login --with-token >/dev/null 2>&1 || true; fi",
			); err != nil {
				logger.Warn("failed to authenticate gh inside Codex pod", "namespace", ns, "error", err)
			}
			if err := runCodexPodShell(
				ctxExec,
				kubeClient,
				ns,
				"printenv OPENAI_API_KEY | npx -y @openai/codex login --with-api-key >/dev/null 2>&1 || true",
			); err != nil {
				logger.Warn("failed to login Codex CLI with OPENAI_API_KEY", "namespace", ns, "error", err)
			}

			kind := cmd.Flag("kind").Value.String()
			if kind == "" && !cmd.Flags().Changed("kind") && envPresent("CODEXCTL_KIND") {
				kind = strings.TrimSpace(envVars.Kind)
			}
			templatePath := cmd.Flag("template").Value.String()
			switch kind {
			case "":
				kind = prompt.KindDevIssue
			case prompt.KindDevIssue, prompt.KindDevReview, prompt.KindPlanIssue, prompt.KindPlanReview, prompt.KindStagingRepairIssue, prompt.KindStagingRepairReview:
				// valid
			default:
				return fmt.Errorf("unknown prompt kind %q", kind)
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
			logger.Debug("prompt stats", "length_bytes", len(promptText))
			lines := strings.Split(string(promptText), "\n")
			for i, line := range lines {
				logger.Debug("prompt line", "index", i, "text", line)
			}
			if err := kubeClient.RunRaw(
				ctxExec,
				promptText,
				"-n", ns,
				"exec", "-i", "deploy/codex",
				"--", "sh", "-lc",
				"cat > /tmp/codex_prompt.txt",
			); err != nil {
				if infraUnhealthy {
					logger.Warn("failed to upload prompt; continuing due to infra-unhealthy", "namespace", ns, "error", err)
					return nil
				}
				return fmt.Errorf("upload prompt into pod: %w", err)
			}

			execCmd := "" +
				"if [ ! -s /tmp/codex_prompt.txt ]; then echo 'error: /tmp/codex_prompt.txt is empty' >&2; exit 1; fi; " +
				"PROMPT_B64=$(base64 -w0 /tmp/codex_prompt.txt); " +
				"PROMPT=$(printf %s \"$PROMPT_B64\" | base64 -d); " +
				"echo \"debug: prompt length bytes=${#PROMPT}\" >&2; " +
				"npx -y @openai/codex exec \"$PROMPT\" --cd /workspace --json"
			if ctxData.Codex.Model != "" {
				execCmd = execCmd + fmt.Sprintf(" -m %s", ctxData.Codex.Model)
			}
			if ctxData.Codex.ModelReasoningEffort != "" {
				execCmd = execCmd + fmt.Sprintf(" --config model_reasoning_effort=%q", ctxData.Codex.ModelReasoningEffort)
			}
			if resumeFlag {
				execCmd = execCmd + " resume --last"
			}

			logger.Info("starting Codex execution", "namespace", ns, "slot", slot, "kind", kind)
			if err := kubeClient.RunRaw(
				ctxExec,
				nil,
				"-n", ns,
				"exec", "deploy/codex",
				"--", "sh", "-lc",
				execCmd,
			); err != nil {
				if infraUnhealthy {
					logger.Warn("failed to run Codex exec; continuing due to infra-unhealthy", "namespace", ns, "error", err)
					return nil
				}
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
	cmd.Flags().String("kind", "", "Builtin prompt kind (e.g. dev_issue, dev_review)")
	cmd.Flags().String("template", "", "Path to prompt template file (overrides --kind when set)")
	cmd.Flags().String("lang", "", "Prompt language (e.g. en, ru); overrides CODEXCTL_LANG and defaults to en")
	cmd.Flags().BoolVar(&infraUnhealthy, "infra-unhealthy", false, "Mark infrastructure as unhealthy in prompt context")
	cmd.Flags().StringVar(&modelOverride, "model", "", "Override Codex model (gpt-5.2-codex|gpt-5.2|gpt-5.1-codex-max|gpt-5.1-codex-mini)")
	cmd.Flags().StringVar(&reasoningOverride, "reasoning-effort", "", "Override model reasoning effort (low|medium|high|extra-high)")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")

	return cmd
}
