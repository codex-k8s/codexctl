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
)

// newPlanCommand creates the "plan" group command with helpers for AI planning workflows.
func newPlanCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Helpers for AI planning workflows and AI-PLAN-PARENT issue structure",
	}

	cmd.AddCommand(
		newPlanResolveRootCommand(opts),
	)

	return cmd
}

// newPlanResolveRootCommand creates "plan resolve-root" that resolves the root planning issue
// for a given focus issue using the [ai-plan] label or AI-PLAN-PARENT marker in the body.
func newPlanResolveRootCommand(_ *Options) *cobra.Command {
	var (
		issue  int
		repo   string
		output string
	)

	cmd := &cobra.Command{
		Use:   "resolve-root",
		Short: "Resolve root planning issue for a given focus issue",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())

			if issue <= 0 {
				return fmt.Errorf("resolve-root requires a positive --issue number")
			}
			if strings.TrimSpace(repo) == "" {
				repo = os.Getenv("GITHUB_REPOSITORY")
			}
			if strings.TrimSpace(repo) == "" {
				return fmt.Errorf("resolve-root requires --repo or GITHUB_REPOSITORY env")
			}

			token, err := lookupGitHubToken()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			issueData, err := fetchIssueJSON(ctx, logger, token, repo, issue)
			if err != nil {
				return err
			}

			root := resolveRootIssueNumber(issueData)
			if root == 0 {
				return fmt.Errorf("unable to resolve root planning issue for focus issue %d", issue)
			}

			switch strings.ToLower(output) {
			case "json":
				type out struct {
					Root  int `json:"root"`
					Focus int `json:"focus"`
				}
				payload, _ := json.Marshal(out{Root: root, Focus: issue})
				fmt.Println(string(payload))
			default:
				logger.Info("resolved root planning issue",
					"focus", issue,
					"root", root,
					"repo", repo,
				)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&issue, "issue", 0, "Focus issue number to resolve the root planner for (required)")
	_ = cmd.MarkFlagRequired("issue")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository slug owner/repo (defaults to GITHUB_REPOSITORY)")
	cmd.Flags().StringVar(&output, "output", "plain", "Output format: plain|json")

	return cmd
}

type ghIssueLabel struct {
	Name string `json:"name"`
}

type ghIssue struct {
	Number int            `json:"number"`
	Title  string         `json:"title"`
	State  string         `json:"state"`
	Body   string         `json:"body"`
	URL    string         `json:"url"`
	Labels []ghIssueLabel `json:"labels"`
}

func lookupGitHubToken() (string, error) {
	candidates := []string{
		os.Getenv("CODEX_GH_PAT"),
		os.Getenv("GH_TOKEN"),
		os.Getenv("GITHUB_TOKEN"),
	}
	for _, v := range candidates {
		if strings.TrimSpace(v) != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("GitHub token is required; set CODEX_GH_PAT or GH_TOKEN or GITHUB_TOKEN")
}

func fetchIssueJSON(ctx context.Context, logger *slog.Logger, token, repo string, number int) (*ghIssue, error) {
	args := []string{
		"issue", "view", strconv.Itoa(number),
		"--repo", repo,
		"--json", "number,title,state,body,url,labels",
	}

	logger.Info("querying GitHub issue via gh", "repo", repo, "issue", number, "args", args)

	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

	envVars := os.Environ()
	envVars = append(envVars, "GITHUB_TOKEN="+token, "GH_TOKEN="+token)
	cmd.Env = envVars

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh issue view for issue %d failed: %w", number, err)
	}

	var issue ghIssue
	if err := json.Unmarshal(stdout.Bytes(), &issue); err != nil {
		return nil, fmt.Errorf("decode gh issue view output: %w", err)
	}

	return &issue, nil
}

func resolveRootIssueNumber(issue *ghIssue) int {
	if issue == nil {
		return 0
	}

	for _, l := range issue.Labels {
		if strings.TrimSpace(l.Name) == "[ai-plan]" {
			return issue.Number
		}
	}

	body := issue.Body
	if strings.TrimSpace(body) == "" {
		return 0
	}

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
