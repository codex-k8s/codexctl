package cli

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/githubapi"
	"github.com/codex-k8s/codexctl/internal/promptctx"
)

func applyIssueContext(
	ctx context.Context,
	logger *slog.Logger,
	envName string,
	issue int,
	pr int,
	focusIssueRaw string,
	ctxData *config.TemplateContext,
) {
	if ctxData == nil || envName != "ai" {
		return
	}

	repo := strings.TrimSpace(os.Getenv("GITHUB_REPOSITORY"))
	if repo == "" {
		logger.Warn("GitHub repository is not set; skipping issue/PR context enrichment")
		return
	}

	token, err := lookupGitHubToken()
	if err != nil {
		logger.Warn("GitHub token missing; skipping issue/PR context enrichment", "error", err)
		return
	}

	client, err := githubapi.NewClient(logger, token, repo)
	if err != nil {
		logger.Warn("failed to initialize GitHub client; skipping issue/PR context enrichment", "error", err)
		return
	}

	var issueNums []int
	if issue > 0 {
		issueNums = append(issueNums, issue)
	}
	if focusIssueRaw != "" {
		if focusID, parseErr := strconv.Atoi(strings.TrimSpace(focusIssueRaw)); parseErr == nil && focusID > 0 {
			if focusID != issue {
				issueNums = append(issueNums, focusID)
			}
		}
	}

	var issueComments []promptctx.IssueComment
	for _, number := range issueNums {
		comments, err := client.FetchIssueComments(ctx, number)
		if err != nil {
			logger.Warn("failed to fetch issue comments", "issue", number, "error", err)
			continue
		}
		for _, c := range comments {
			issueComments = append(issueComments, promptctx.IssueComment{
				IssueNumber: number,
				ID:          c.ID,
				Author:      c.Author,
				URL:         c.URL,
				Body:        c.Body,
				CreatedAt:   c.CreatedAt,
			})
		}
	}
	ctxData.IssueComments = issueComments

	if pr > 0 {
		comments, err := client.FetchReviewComments(ctx, pr)
		if err != nil {
			logger.Warn("failed to fetch PR review comments", "pr", pr, "error", err)
			return
		}
		var reviewComments []promptctx.ReviewComment
		for _, c := range comments {
			reviewComments = append(reviewComments, promptctx.ReviewComment{
				PRNumber:  pr,
				ID:        c.ID,
				Author:    c.Author,
				URL:       c.URL,
				Body:      c.Body,
				ThreadID:  c.ThreadID,
				CreatedAt: c.CreatedAt,
			})
		}
		ctxData.ReviewComments = reviewComments
	}
}
