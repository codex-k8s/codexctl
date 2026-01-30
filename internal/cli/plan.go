package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/ghoutput"
)

// newPlanCommand creates the "plan" group command with helpers for AI planning workflows.
func newPlanCommand(opts *Options) *cobra.Command {
	return newGroupCommand(
		"plan",
		"Helpers for AI planning workflows and AI-PLAN-PARENT issue structure",
		newPlanResolveRootCommand(opts),
	)
}

// newPlanResolveRootCommand creates "plan resolve-root" that resolves the root planning issue
// for a given focus issue using the [ai-plan] label or AI-PLAN-PARENT marker in the body.
func newPlanResolveRootCommand(_ *Options) *cobra.Command {
	var (
		issue int
		repo  string
	)

	cmd := &cobra.Command{
		Use:   "resolve-root",
		Short: "Resolve root planning issue for a given focus issue",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())
			envCfg := planEnv{}
			if err := parseEnv(&envCfg); err != nil {
				return err
			}
			if !cmd.Flags().Changed("issue") && envPresent("CODEXCTL_ISSUE_NUMBER") {
				issue = envCfg.Issue
			}
			if !cmd.Flags().Changed("repo") && envPresent("CODEXCTL_REPO") {
				repo = envCfg.Repo
			}

			if issue <= 0 {
				return fmt.Errorf("resolve-root requires a positive --issue number")
			}
			if strings.TrimSpace(repo) == "" {
				repo = strings.TrimSpace(os.Getenv("CODEXCTL_REPO"))
				if repo == "" {
					repo = os.Getenv("GITHUB_REPOSITORY")
				}
			}
			if strings.TrimSpace(repo) == "" {
				return fmt.Errorf("resolve-root requires --repo or CODEXCTL_REPO env")
			}

			token, err := lookupGitHubToken()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			issueData, err := fetchGitHubEntity[ghIssue](ctx, logger, token, repo, "issue", "number,title,state,body,url,labels", issue)
			if err != nil {
				return err
			}

			root := resolveRootIssueNumber(issueData)
			if root == 0 {
				return fmt.Errorf("unable to resolve root planning issue for focus issue %d", issue)
			}
			writeErr := ghoutput.Write(map[string]string{
				"root":  strconv.Itoa(root),
				"focus": strconv.Itoa(issue),
			})
			if writeErr != nil {
				return writeErr
			}
			fmt.Printf("root: %d\nfocus: %d\n", root, issue)
			return nil
		},
	}

	cmd.Flags().IntVar(&issue, "issue", 0, "Focus issue number to resolve the root planner for (required)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository slug owner/repo (defaults to CODEXCTL_REPO)")

	return cmd
}

type ghIssueLabel struct {
	// Name is the GitHub label name.
	Name string `json:"name"`
}

type ghIssue struct {
	// Number is the issue number.
	Number int `json:"number"`
	// Title is the issue title.
	Title string `json:"title"`
	// State is the issue state (open/closed).
	State string `json:"state"`
	// Body is the raw Markdown body.
	Body string `json:"body"`
	// URL is the canonical issue URL.
	URL string `json:"url"`
	// Labels lists attached issue labels.
	Labels []ghIssueLabel `json:"labels"`
}

type ghPR struct {
	Number int            `json:"number"`
	Title  string         `json:"title"`
	State  string         `json:"state"`
	URL    string         `json:"url"`
	Labels []ghIssueLabel `json:"labels"`
}

// lookupGitHubToken finds a token from known environment variables.
func lookupGitHubToken() (string, error) {
	candidates := []string{
		os.Getenv("CODEXCTL_GH_PAT"),
		os.Getenv("GH_TOKEN"),
		os.Getenv("GITHUB_TOKEN"),
	}
	for _, v := range candidates {
		if strings.TrimSpace(v) != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("GitHub token is required; set CODEXCTL_GH_PAT or GH_TOKEN or GITHUB_TOKEN")
}

// runGHJSON executes a gh command and returns its stdout as JSON bytes.
func runGHJSON(ctx context.Context, logger *slog.Logger, token string, args []string, logFields ...any) ([]byte, error) {
	logger.Info("querying GitHub via gh", logFields...)

	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

	envVars := os.Environ()
	envVars = append(envVars, "GITHUB_TOKEN="+token, "GH_TOKEN="+token)
	cmd.Env = envVars

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return stdout.Bytes(), nil
}

// fetchGitHubJSON runs a gh view command and decodes its JSON response.
func fetchGitHubJSON[T any](ctx context.Context, logger *slog.Logger, token, repo, entity string, number int, args []string) (*T, error) {
	payload, err := runGHJSON(ctx, logger, token, args, "repo", repo, entity, number, "args", args)
	if err != nil {
		return nil, fmt.Errorf("gh %s view for %s %d failed: %w", entity, entity, number, err)
	}

	var out T
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("decode gh %s view output: %w", entity, err)
	}

	return &out, nil
}

// fetchGitHubEntity builds gh view args for an entity and decodes the response.
func fetchGitHubEntity[T any](ctx context.Context, logger *slog.Logger, token, repo, entity, fields string, number int) (*T, error) {
	args := []string{
		entity, "view", strconv.Itoa(number),
		"--repo", repo,
		"--json", fields,
	}

	return fetchGitHubJSON[T](ctx, logger, token, repo, entity, number, args)
}

// resolveRootIssueNumber finds the root planning issue via label or body marker.
func resolveRootIssueNumber(issue *ghIssue) int {
	if issue == nil {
		return 0
	}

	// Prefer explicit [ai-plan] label.
	for _, l := range issue.Labels {
		if strings.TrimSpace(l.Name) == "[ai-plan]" {
			return issue.Number
		}
	}

	body := issue.Body
	if strings.TrimSpace(body) == "" {
		return 0
	}

	// Fallback to body marker AI-PLAN-PARENT: #<num>.
	re := regexp.MustCompile(`AI-PLAN-PARENT:\s*#(\d+)`)
	matches := re.FindStringSubmatch(body)
	if len(matches) != 2 {
		return 0
	}

	n, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0
	}
	return n
}
