package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/prompt"
)

// newPRCommand creates the "pr" group command with helpers for PR review flows.
func newPRCommand(opts *Options) *cobra.Command {
	return newGroupCommand(
		"pr",
		"Helpers for Pull Request workflows (auto-commit and comments)",
		newPRReviewApplyCommand(opts),
	)
}

// newPRReviewApplyCommand creates "pr review-apply" that commits Codex changes for a PR branch
// and posts an environment comment to the PR.
func newPRReviewApplyCommand(opts *Options) *cobra.Command {
	var (
		prNumber     int
		slot         int
		codeRootBase string
		lang         string
	)

	cmd := &cobra.Command{
		Use:   "review-apply",
		Short: "Commit Codex review changes for a PR and post environment links",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())

			if prNumber <= 0 {
				return fmt.Errorf("review-apply requires a positive --pr number")
			}
			if slot <= 0 {
				return fmt.Errorf("review-apply requires a positive --slot number")
			}

			if codeRootBase == "" {
				codeRootBase = os.Getenv("CODE_ROOT_BASE")
			}
			if strings.TrimSpace(codeRootBase) == "" {
				return fmt.Errorf("review-apply requires --code-root-base or CODE_ROOT_BASE env")
			}

			repo := os.Getenv("GITHUB_REPOSITORY")
			if strings.TrimSpace(repo) == "" {
				return fmt.Errorf("review-apply requires GITHUB_REPOSITORY env")
			}

			if lang == "" {
				lang = "en"
			}

			token, err := lookupGitHubToken()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			branch, err := resolvePRHeadBranch(ctx, token, repo, prNumber)
			if err != nil {
				return err
			}
			if branch == "" {
				return fmt.Errorf("cannot determine head branch for PR #%d", prNumber)
			}

			slotRoot := strings.TrimSuffix(codeRootBase, string(os.PathSeparator))
			workdir := filepath.Join(slotRoot, strconv.Itoa(slot), "src")
			if opts.Env == "ai-repair" {
				workdir = filepath.Join(slotRoot, "staging", "src")
			}

			if err := commitAndPushPRChanges(ctx, logger, workdir, branch, prNumber); err != nil {
				return err
			}

			if err := commentPREnvironment(ctx, logger, opts, token, repo, prNumber, slot, lang); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&prNumber, "pr", 0, "Pull Request number to apply review changes for (required)")
	_ = cmd.MarkFlagRequired("pr")
	cmd.Flags().IntVar(&slot, "slot", 0, "AI environment slot number (required)")
	_ = cmd.MarkFlagRequired("slot")
	cmd.Flags().StringVar(&codeRootBase, "code-root-base", "", "Base path for slot workspaces (defaults to CODE_ROOT_BASE env)")
	cmd.Flags().StringVar(&lang, "lang", "en", "Language for environment comment (en|ru)")

	return cmd
}

func resolvePRHeadBranch(ctx context.Context, token, repo string, prNumber int) (string, error) {
	args := []string{
		"pr", "view", strconv.Itoa(prNumber),
		"--repo", repo,
		"--json", "headRefName",
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

	envVars := os.Environ()
	envVars = append(envVars, "GITHUB_TOKEN="+token, "GH_TOKEN="+token)
	cmd.Env = envVars

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh pr view for PR %d failed: %w", prNumber, err)
	}

	type prInfo struct {
		HeadRefName string `json:"headRefName"`
	}
	var info prInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return "", fmt.Errorf("decode gh pr view output: %w", err)
	}

	return strings.TrimSpace(info.HeadRefName), nil
}

func commitAndPushPRChanges(ctx context.Context, logger *slog.Logger, workdir, branch string, prNumber int) error {
	logger.Info("applying review changes in workspace",
		"workdir", workdir,
		"branch", branch,
		"pr", prNumber,
	)

	if err := os.RemoveAll(filepath.Join(workdir, ".bin")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cleanup .bin in workspace: %w", err)
	}

	if err := runGit(ctx, workdir, []string{"checkout", branch}); err != nil {
		return fmt.Errorf("git checkout %q failed: %w", branch, err)
	}

	// Stage changes.
	if err := runGit(ctx, workdir, []string{"add", "-u"}); err != nil {
		return fmt.Errorf("git add -u failed: %w", err)
	}
	if err := runGit(ctx, workdir, []string{"add", "docs", "proto", "services", "libs"}); err != nil {
		logger.Warn("git add docs/proto/services/libs failed, continuing", "error", err)
	}

	// Check if there is anything to commit.
	diffCmd := exec.CommandContext(ctx, "git", "diff", "--cached", "--quiet")
	diffCmd.Dir = workdir
	if err := diffCmd.Run(); err == nil {
		logger.Info("no changes to commit for PR review", "pr", prNumber)
		return nil
	}

	msg := fmt.Sprintf("feat: apply Codex review changes for PR #%d", prNumber)
	if err := runGit(ctx, workdir, []string{"commit", "-m", msg}); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}
	if err := runGit(ctx, workdir, []string{"push", "origin", branch}); err != nil {
		return fmt.Errorf("git push origin %q failed: %w", branch, err)
	}

	logger.Info("review changes committed and pushed",
		"branch", branch,
		"pr", prNumber,
	)
	return nil
}

func runGit(ctx context.Context, dir string, args []string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func commentPREnvironment(ctx context.Context, logger *slog.Logger, opts *Options, token, repo string, prNumber, slot int, lang string) error {
	envName := opts.Env
	if envName == "" {
		envName = "ai"
	}

	_, ctxData, err := config.LoadStackConfig(opts.ConfigPath, config.LoadOptions{
		Env:  envName,
		Slot: slot,
	})
	if err != nil {
		return fmt.Errorf("load stack config for env %q slot %d: %w", envName, slot, err)
	}

	siteHost := ctxData.BaseDomain[envName]
	if envName == "ai" {
		siteHost = fmt.Sprintf("dev-%d.%s", slot, ctxData.BaseDomain["ai"])
	}

	body, err := prompt.RenderEnvComment(strings.ToLower(lang), siteHost, slot, ctxData.Codex.Links)
	if err != nil {
		return fmt.Errorf("render environment comment: %w", err)
	}

	args := []string{
		"pr", "comment", strconv.Itoa(prNumber),
		"--repo", repo,
		"--body", body,
	}

	logger.Info("posting PR environment comment via gh",
		"repo", repo,
		"pr", prNumber,
		"slot", slot,
	)

	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	envVars := os.Environ()
	envVars = append(envVars, "GITHUB_TOKEN="+token, "GH_TOKEN="+token)
	cmd.Env = envVars

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh pr comment for PR %d failed: %w", prNumber, err)
	}

	logger.Info("PR environment comment posted", "pr", prNumber, "slot", slot)
	return nil
}
