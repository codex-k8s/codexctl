package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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
		newManageEnvCleanupPRCommand(opts),
		newManageEnvCleanupIssueCommand(opts),
		newManageEnvCloseLinkedIssueCommand(opts),
		newManageEnvSetCommand(opts),
		newManageEnvCommentCommand(opts),
		newManageEnvCommentPRCommand(opts),
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
			envCfg := manageEnvEnv{}
			if err := parseEnv(&envCfg); err != nil {
				return err
			}
			if !cmd.Flags().Changed("slot") && envPresent("CODEXCTL_SLOT") {
				slot = envCfg.Slot
			}
			if !cmd.Flags().Changed("issue") && envPresent("CODEXCTL_ISSUE_NUMBER") {
				issue = envCfg.Issue
			}
			if !cmd.Flags().Changed("pr") && envPresent("CODEXCTL_PR_NUMBER") {
				pr = envCfg.PR
			}
			if !cmd.Flags().Changed("all") && envPresent("CODEXCTL_ALL") {
				cleanupAll = envCfg.All.Bool()
			}
			if !cmd.Flags().Changed("with-configmap") && envPresent("CODEXCTL_WITH_CONFIGMAP") {
				withConfigMap = envCfg.WithConfigMap.Bool()
			}

			envName := opts.Env
			if envName == "" {
				envName = "ai"
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
			defer cancel()
			return runEnvCleanup(ctx, logger, opts, envName, issue, pr, slot, cleanupAll, withConfigMap)
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

// newManageEnvCleanupPRCommand creates a helper that cleans up environments for a closed PR
// and optionally deletes the branch or closes a linked issue.
func newManageEnvCleanupPRCommand(opts *Options) *cobra.Command {
	var (
		pr            int
		branch        string
		repo          string
		withConfigMap bool
		deleteBranch  bool
		closeIssue    bool
		includeRepair bool
	)

	cmd := &cobra.Command{
		Use:   "cleanup-pr",
		Short: "Cleanup environments for a PR and optionally delete the branch/close linked issue",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())
			envCfg := cleanupGHEnv{}
			if err := parseEnv(&envCfg); err != nil {
				return err
			}
			if !cmd.Flags().Changed("pr") && envPresent("CODEXCTL_PR_NUMBER") {
				pr = envCfg.PR
			}
			if !cmd.Flags().Changed("branch") && envPresent("CODEXCTL_BRANCH") {
				branch = strings.TrimSpace(envCfg.Branch)
			}
			if !cmd.Flags().Changed("repo") && envPresent("CODEXCTL_REPO") {
				repo = strings.TrimSpace(envCfg.Repo)
			}
			if !cmd.Flags().Changed("with-configmap") && envPresent("CODEXCTL_WITH_CONFIGMAP") {
				withConfigMap = envCfg.WithConfigMap.Bool()
			}
			if !cmd.Flags().Changed("delete-branch") && envPresent("CODEXCTL_DELETE_BRANCH") {
				deleteBranch = envCfg.DeleteBranch.Bool()
			}
			if !cmd.Flags().Changed("close-issue") && envPresent("CODEXCTL_CLOSE_ISSUE") {
				closeIssue = envCfg.CloseIssue.Bool()
			}
			if !cmd.Flags().Changed("include-ai-repair") && envPresent("CODEXCTL_INCLUDE_AI_REPAIR") {
				includeRepair = envCfg.IncludeAIRepair.Bool()
			}
			if pr <= 0 {
				return fmt.Errorf("pr number must be specified")
			}

			repo = resolveGitHubRepo(repo)
			branchKind, branchIssue, branchOK := parseCodexBranch(branch)
			repair := includeRepair || (branchOK && branchKind == "ai-repair")

			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
			defer cancel()

			if err := runEnvCleanup(ctx, logger, opts, "ai", 0, pr, 0, false, withConfigMap); err != nil {
				return err
			}
			if repair {
				if err := runEnvCleanup(ctx, logger, opts, "ai-repair", 0, pr, 0, false, withConfigMap); err != nil {
					return err
				}
			}

			if (deleteBranch || closeIssue) && repo == "" {
				logger.Warn("repository is empty; skipping GitHub operations", "branch", branch)
				return nil
			}

			token, err := lookupGitHubToken()
			if err != nil {
				logger.Warn("GitHub token missing; skipping GitHub operations", "error", err)
				return nil
			}

			if deleteBranch && branchOK {
				if err := deleteGitBranch(ctx, logger, token, repo, branch); err != nil {
					logger.Warn("failed to delete branch", "branch", branch, "error", err)
				}
			}
			if closeIssue && branchOK && branchIssue > 0 {
				if err := closeGitHubIssue(ctx, logger, token, repo, branchIssue); err != nil {
					logger.Warn("failed to close linked issue", "issue", branchIssue, "error", err)
				}
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&pr, "pr", 0, "PR number to clean up")
	cmd.Flags().StringVar(&branch, "branch", "", "Branch ref (used for cleanup hints and issue linking)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository slug owner/repo (defaults to CODEXCTL_REPO)")
	cmd.Flags().BoolVar(&withConfigMap, "with-configmap", false, "Remove state ConfigMap for the selected environment")
	cmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "Delete the PR branch (if it matches codex/*)")
	cmd.Flags().BoolVar(&closeIssue, "close-issue", false, "Close a linked issue based on the branch name")
	cmd.Flags().BoolVar(&includeRepair, "include-ai-repair", false, "Also clean ai-repair env even if the branch is not ai-repair")

	return cmd
}

// newManageEnvCleanupIssueCommand cleans up environments for a closed Issue and optionally deletes branches.
func newManageEnvCleanupIssueCommand(opts *Options) *cobra.Command {
	var (
		issue         int
		repo          string
		withConfigMap bool
		deleteBranch  bool
	)

	cmd := &cobra.Command{
		Use:   "cleanup-issue",
		Short: "Cleanup environments and branches for a closed issue",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())
			envCfg := cleanupGHEnv{}
			if err := parseEnv(&envCfg); err != nil {
				return err
			}
			if !cmd.Flags().Changed("issue") && envPresent("CODEXCTL_ISSUE_NUMBER") {
				issue = envCfg.Issue
			}
			if !cmd.Flags().Changed("repo") && envPresent("CODEXCTL_REPO") {
				repo = strings.TrimSpace(envCfg.Repo)
			}
			if !cmd.Flags().Changed("with-configmap") && envPresent("CODEXCTL_WITH_CONFIGMAP") {
				withConfigMap = envCfg.WithConfigMap.Bool()
			}
			if !cmd.Flags().Changed("delete-branch") && envPresent("CODEXCTL_DELETE_BRANCH") {
				deleteBranch = envCfg.DeleteBranch.Bool()
			}
			if issue <= 0 {
				return fmt.Errorf("issue number must be specified")
			}

			repo = resolveGitHubRepo(repo)

			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Minute)
			defer cancel()

			if err := runEnvCleanup(ctx, logger, opts, "ai", issue, 0, 0, false, withConfigMap); err != nil {
				return err
			}
			if err := runEnvCleanup(ctx, logger, opts, "ai-repair", issue, 0, 0, false, withConfigMap); err != nil {
				return err
			}

			if !deleteBranch {
				return nil
			}
			if repo == "" {
				logger.Warn("repository is empty; skipping branch deletion", "issue", issue)
				return nil
			}
			token, err := lookupGitHubToken()
			if err != nil {
				logger.Warn("GitHub token missing; skipping branch deletion", "error", err)
				return nil
			}

			issueBranch := fmt.Sprintf("codex/issue-%d", issue)
			if err := deleteGitBranch(ctx, logger, token, repo, issueBranch); err != nil {
				logger.Warn("failed to delete branch", "branch", issueBranch, "error", err)
			}
			repairBranch := fmt.Sprintf("codex/ai-repair-%d", issue)
			if err := deleteGitBranch(ctx, logger, token, repo, repairBranch); err != nil {
				logger.Warn("failed to delete branch", "branch", repairBranch, "error", err)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&issue, "issue", 0, "Issue number to clean up")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository slug owner/repo (defaults to CODEXCTL_REPO)")
	cmd.Flags().BoolVar(&withConfigMap, "with-configmap", false, "Remove state ConfigMap for the selected environment")
	cmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "Delete codex/* branches associated with the issue")

	return cmd
}

// newManageEnvCloseLinkedIssueCommand closes an issue linked via codex/issue-* or codex/ai-repair-* branch name.
func newManageEnvCloseLinkedIssueCommand(opts *Options) *cobra.Command {
	var (
		branch     string
		repo       string
		closeIssue bool
	)

	cmd := &cobra.Command{
		Use:   "close-linked-issue",
		Short: "Close a linked issue based on a codex/* branch name",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())
			envCfg := cleanupGHEnv{}
			if err := parseEnv(&envCfg); err != nil {
				return err
			}
			if !cmd.Flags().Changed("branch") && envPresent("CODEXCTL_BRANCH") {
				branch = strings.TrimSpace(envCfg.Branch)
			}
			if !cmd.Flags().Changed("repo") && envPresent("CODEXCTL_REPO") {
				repo = strings.TrimSpace(envCfg.Repo)
			}
			if !cmd.Flags().Changed("close-issue") && envPresent("CODEXCTL_CLOSE_ISSUE") {
				closeIssue = envCfg.CloseIssue.Bool()
			}
			if !closeIssue {
				return nil
			}
			if branch == "" {
				return fmt.Errorf("branch must be specified")
			}

			repo = resolveGitHubRepo(repo)
			if repo == "" {
				logger.Warn("repository is empty; skipping issue close", "branch", branch)
				return nil
			}
			_, issue, ok := parseCodexBranch(branch)
			if !ok || issue <= 0 {
				logger.Info("branch does not map to a linked issue; skipping close", "branch", branch)
				return nil
			}
			token, err := lookupGitHubToken()
			if err != nil {
				logger.Warn("GitHub token missing; skipping issue close", "error", err)
				return nil
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			if err := closeGitHubIssue(ctx, logger, token, repo, issue); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&branch, "branch", "", "Branch ref (codex/issue-* or codex/ai-repair-*)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository slug owner/repo (defaults to CODEXCTL_REPO)")
	cmd.Flags().BoolVar(&closeIssue, "close-issue", false, "Close the linked issue")
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
			envCfg := manageEnvEnv{}
			if err := parseEnv(&envCfg); err != nil {
				return err
			}
			if !cmd.Flags().Changed("slot") && envPresent("CODEXCTL_SLOT") {
				slot = envCfg.Slot
			}
			if !cmd.Flags().Changed("issue") && envPresent("CODEXCTL_ISSUE_NUMBER") {
				issue = envCfg.Issue
			}
			if !cmd.Flags().Changed("pr") && envPresent("CODEXCTL_PR_NUMBER") {
				pr = envCfg.PR
			}
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
			envCfg := manageEnvEnv{}
			if err := parseEnv(&envCfg); err != nil {
				return err
			}
			if !cmd.Flags().Changed("slot") && envPresent("CODEXCTL_SLOT") {
				slot = envCfg.Slot
			}
			if !cmd.Flags().Changed("lang") && envPresent("CODEXCTL_LANG") {
				lang = envCfg.Lang
			}
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

// newManageEnvCommentPRCommand renders and posts a comment with env links to a PR.
func newManageEnvCommentPRCommand(opts *Options) *cobra.Command {
	var (
		lang string
		slot int
		pr   int
		repo string
	)

	cmd := &cobra.Command{
		Use:   "comment-pr",
		Short: "Post environment links to a Pull Request",
		RunE: func(cmd *cobra.Command, _ []string) error {
			envCfg := commentPREnv{}
			if err := parseEnv(&envCfg); err != nil {
				return err
			}
			if !cmd.Flags().Changed("slot") && envPresent("CODEXCTL_SLOT") {
				slot = envCfg.Slot
			}
			if !cmd.Flags().Changed("pr") && envPresent("CODEXCTL_PR_NUMBER") {
				pr = envCfg.PR
			}
			if !cmd.Flags().Changed("repo") && envPresent("CODEXCTL_REPO") {
				repo = strings.TrimSpace(envCfg.Repo)
			}
			if !cmd.Flags().Changed("lang") && envPresent("CODEXCTL_LANG") {
				lang = envCfg.Lang
			}

			if slot <= 0 {
				return fmt.Errorf("slot must be >0")
			}
			if pr <= 0 {
				return fmt.Errorf("pr number must be >0")
			}

			repo = resolveGitHubRepo(repo)
			if strings.TrimSpace(repo) == "" {
				return fmt.Errorf("repository is empty")
			}
			if lang == "" {
				lang = "en"
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

			f, err := os.CreateTemp("", "codexctl-comment-*.md")
			if err != nil {
				return err
			}
			defer os.Remove(f.Name())
			if _, err := f.WriteString(body); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}

			token, err := lookupGitHubToken()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			args := []string{"pr", "comment", strconv.Itoa(pr), "--repo", repo, "--body-file", f.Name()}
			return runGH(ctx, token, args...)
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "ai", "Environment (default: ai)")
	cmd.Flags().IntVar(&slot, "slot", 0, "Slot number (required)")
	_ = cmd.MarkFlagRequired("slot")
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number (required)")
	_ = cmd.MarkFlagRequired("pr")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository slug owner/repo (defaults to CODEXCTL_REPO)")
	cmd.Flags().StringVar(&lang, "lang", "en", "Language for the comment (en|ru)")
	return cmd
}

var codexBranchRE = regexp.MustCompile(`^codex/(issue|ai-repair)-([0-9]+)$`)

func parseCodexBranch(branch string) (string, int, bool) {
	if branch == "" {
		return "", 0, false
	}
	m := codexBranchRE.FindStringSubmatch(strings.TrimSpace(branch))
	if len(m) != 3 {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n <= 0 {
		return "", 0, false
	}
	return m[1], n, true
}

func resolveGitHubRepo(repo string) string {
	if strings.TrimSpace(repo) != "" {
		return strings.TrimSpace(repo)
	}
	if v := strings.TrimSpace(os.Getenv("CODEXCTL_REPO")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("GITHUB_REPOSITORY"))
}

func resolveSourceTarget(envName string, slot int, codeRootBase string) (string, error) {
	base := strings.TrimSuffix(codeRootBase, string(os.PathSeparator))
	if strings.TrimSpace(base) == "" {
		return "", fmt.Errorf("code root base is empty")
	}
	if envName == "" {
		envName = "ai"
	}
	switch envName {
	case "ai":
		if slot <= 0 {
			return "", fmt.Errorf("slot must be >0 for env=%q", envName)
		}
		return filepath.Join(base, strconv.Itoa(slot), "src"), nil
	case "ai-repair":
		return filepath.Join(base, "staging", "src"), nil
	default:
		return filepath.Join(base, envName, "src"), nil
	}
}

func deleteGitBranch(ctx context.Context, logger *slog.Logger, token, repo, branch string) error {
	if strings.TrimSpace(branch) == "" || strings.TrimSpace(repo) == "" {
		return nil
	}
	args := []string{"api", "-X", "DELETE", fmt.Sprintf("repos/%s/git/refs/heads/%s", repo, branch)}
	logger.Debug("deleting GitHub branch", "repo", repo, "branch", branch)
	return runGH(ctx, token, args...)
}

func closeGitHubIssue(ctx context.Context, logger *slog.Logger, token, repo string, issue int) error {
	if issue <= 0 || strings.TrimSpace(repo) == "" {
		return nil
	}
	args := []string{"issue", "close", strconv.Itoa(issue), "--repo", repo, "--reason", "completed"}
	logger.Debug("closing GitHub issue", "repo", repo, "issue", issue)
	return runGH(ctx, token, args...)
}

func runGH(ctx context.Context, token string, args ...string) error {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	envVars := os.Environ()
	envVars = append(envVars, "GITHUB_TOKEN="+token, "GH_TOKEN="+token)
	cmd.Env = envVars
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

func runEnvCleanup(
	ctx context.Context,
	logger *slog.Logger,
	opts *Options,
	envName string,
	issue int,
	pr int,
	slot int,
	cleanupAll bool,
	withConfigMap bool,
) error {
	if !cleanupAll && issue <= 0 && pr <= 0 && slot <= 0 {
		return fmt.Errorf("at least one of --issue, --pr or --slot must be specified")
	}
	if envName == "" {
		envName = "ai"
	}

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
