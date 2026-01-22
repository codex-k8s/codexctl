package cli

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/codex-k8s/codexctl/internal/config"
)

func applyIssueReasoningEffortOverride(
	ctx context.Context,
	logger *slog.Logger,
	envName string,
	issue int,
	stackCfg *config.StackConfig,
	ctxData *config.TemplateContext,
) {
	if envName != "ai" || issue <= 0 || ctxData == nil {
		return
	}

	repo := strings.TrimSpace(os.Getenv("GITHUB_REPOSITORY"))
	if repo == "" {
		logger.Warn("GitHub repository is not set; skipping model reasoning effort override", "issue", issue)
		return
	}

	token, err := lookupGitHubToken()
	if err != nil {
		logger.Warn("GitHub token missing; skipping model reasoning effort override", "issue", issue, "error", err)
		return
	}

	issueData, err := fetchIssueJSON(ctx, logger, token, repo, issue)
	if err != nil {
		logger.Warn("failed to query issue labels; skipping model reasoning effort override", "issue", issue, "repo", repo, "error", err)
		return
	}

	effort, ok := resolveReasoningEffort(issueData.Labels)
	if !ok {
		return
	}

	ctxData.Codex.ModelReasoningEffort = effort
	if stackCfg != nil {
		stackCfg.Codex.ModelReasoningEffort = effort
	}
	logger.Info("overriding codex model reasoning effort from issue labels", "issue", issue, "effort", effort)
}

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
