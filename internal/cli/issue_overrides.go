package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/codex-k8s/codexctl/internal/config"
)

var allowedCodexModels = map[string]string{
	"gpt-5.3-codex":      "Latest frontier agentic coding model.",
	"gpt-5.2":            "Latest frontier model with improvements across knowledge, reasoning and coding.",
	"gpt-5.1-codex-max":  "Codex-optimized flagship for deep and fast reasoning.",
	"gpt-5.1-codex-mini": "Optimized for codex. Cheaper, faster, but less capable.",
}

var allowedReasoningEffort = map[string]string{
	"low":    "Fast responses with lighter reasoning.",
	"medium": "Balanced speed and reasoning depth.",
	"high":   "Greater reasoning depth for complex problems.",
	"xhigh":  "Extra high reasoning depth for complex problems.",
}

// applyIssueCodexOverrides updates model/reasoning based on Issue labels, or PR labels when Issue is absent.
func applyIssueCodexOverrides(
	ctx context.Context,
	logger *slog.Logger,
	envName string,
	issue int,
	pr int,
	stackCfg *config.StackConfig,
	ctxData *config.TemplateContext,
) {
	if envName != "ai" || ctxData == nil || (issue <= 0 && pr <= 0) {
		return
	}

	repo := strings.TrimSpace(os.Getenv("CODEXCTL_REPO"))
	if repo == "" {
		repo = strings.TrimSpace(os.Getenv("GITHUB_REPOSITORY"))
	}
	if repo == "" {
		logger.Warn("GitHub repository is not set; skipping codex overrides", "issue", issue, "pr", pr)
		return
	}

	token, err := lookupGitHubToken()
	if err != nil {
		logger.Warn("GitHub token missing; skipping codex overrides", "issue", issue, "pr", pr, "error", err)
		return
	}

	var labels []ghIssueLabel
	if issue > 0 {
		issueData, err := fetchGitHubEntity[ghIssue](ctx, logger, token, repo, "issue", "number,title,state,body,url,labels", issue)
		if err != nil {
			logger.Warn("failed to query issue labels; skipping codex overrides", "issue", issue, "repo", repo, "error", err)
			return
		}
		labels = issueData.Labels
	} else {
		prData, err := fetchGitHubEntity[ghPR](ctx, logger, token, repo, "pr", "number,title,state,url,labels", pr)
		if err != nil {
			logger.Warn("failed to query PR labels; skipping codex overrides", "pr", pr, "repo", repo, "error", err)
			return
		}
		labels = prData.Labels
	}

	if model, ok := resolveModelOverride(labels); ok {
		ctxData.Codex.Model = model
		if stackCfg != nil {
			stackCfg.Codex.Model = model
		}
		logger.Info("overriding codex model from labels", "issue", issue, "pr", pr, "model", model)
	}

	effort, ok := resolveReasoningEffort(labels)
	if !ok {
		return
	}

	ctxData.Codex.ModelReasoningEffort = effort
	if stackCfg != nil {
		stackCfg.Codex.ModelReasoningEffort = effort
	}
	logger.Info("overriding codex model reasoning effort from labels", "issue", issue, "pr", pr, "effort", effort)
}

func normalizeReasoningEffort(input string) (string, error) {
	raw := strings.ToLower(strings.TrimSpace(input))
	raw = strings.ReplaceAll(raw, "_", "-")
	raw = strings.ReplaceAll(raw, " ", "-")
	switch raw {
	case "extra-high":
		raw = "xhigh"
	}
	if _, ok := allowedReasoningEffort[raw]; ok {
		return raw, nil
	}
	return "", fmt.Errorf("unsupported reasoning effort %q", input)
}

func normalizeModel(input string) (string, error) {
	raw := strings.ToLower(strings.TrimSpace(input))
	if _, ok := allowedCodexModels[raw]; ok {
		return raw, nil
	}
	return "", fmt.Errorf("unsupported model %q", input)
}

// resolveModelOverride maps label names to Codex model identifiers.
func resolveModelOverride(labels []ghIssueLabel) (string, bool) {
	if len(labels) == 0 {
		return "", false
	}

	labelSet := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		name := strings.ToLower(strings.TrimSpace(label.Name))
		name = strings.Trim(name, "[]")
		if name == "" {
			continue
		}
		labelSet[name] = struct{}{}
	}

	priority := []string{
		"ai-model-gpt-5.3-codex",
		"ai-model-gpt-5.2",
		"ai-model-gpt-5.1-codex-max",
		"ai-model-gpt-5.1-codex-mini",
	}

	for _, label := range priority {
		if _, ok := labelSet[label]; ok {
			model := strings.TrimPrefix(label, "ai-model-")
			if _, ok := allowedCodexModels[model]; ok {
				return model, true
			}
		}
	}
	return "", false
}

// resolveReasoningEffort maps label names to reasoning effort values.
func resolveReasoningEffort(labels []ghIssueLabel) (string, bool) {
	if len(labels) == 0 {
		return "", false
	}

	labelSet := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		name := strings.ToLower(strings.TrimSpace(label.Name))
		name = strings.Trim(name, "[]")
		if name == "" {
			continue
		}
		labelSet[name] = struct{}{}
	}

	priority := []struct {
		Label  string
		Effort string
	}{
		{Label: "ai-reasoning-extra-high", Effort: "xhigh"},
		{Label: "ai-reasoning-xhigh", Effort: "xhigh"},
		{Label: "ai-reasoning-high", Effort: "high"},
		{Label: "ai-reasoning-medium", Effort: "medium"},
		{Label: "ai-reasoning-low", Effort: "low"},
		{Label: "ai-xhigh", Effort: "xhigh"},
		{Label: "ai-high", Effort: "high"},
		{Label: "ai-medium", Effort: "medium"},
		{Label: "ai-low", Effort: "low"},
	}

	for _, item := range priority {
		if _, ok := labelSet[item.Label]; ok {
			return item.Effort, true
		}
	}
	return "", false
}
