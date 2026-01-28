// Package githubapi provides a simple GitHub API client using the GitHub CLI.
// TODO: Replace this lightweight GraphQL client with a maintained Go GitHub SDK.
package githubapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

type Client struct {
	logger *slog.Logger
	token  string
	repo   string
	owner  string
	name   string
}

func NewClient(logger *slog.Logger, token, repo string) (*Client, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return nil, fmt.Errorf("repository is empty")
	}
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return nil, fmt.Errorf("invalid repository slug %q, expected owner/repo", repo)
	}
	return &Client{
		logger: logger,
		token:  token,
		repo:   repo,
		owner:  parts[0],
		name:   parts[1],
	}, nil
}

func (c *Client) FetchIssueComments(ctx context.Context, number int) ([]IssueComment, error) {
	if number <= 0 {
		return nil, fmt.Errorf("issue number must be positive")
	}
	query := `query($owner: String!, $name: String!, $number: Int!, $after: String) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
      comments(first: 100, after: $after) {
        nodes {
          databaseId
          body
          url
          createdAt
          isMinimized
          minimizedReason
          author { login }
        }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}`

	var out []IssueComment
	var after string
	for {
		resp := issueCommentsResponse{}
		vars := map[string]any{
			"owner":  c.owner,
			"name":   c.name,
			"number": number,
		}
		if after != "" {
			vars["after"] = after
		}
		if err := c.runGraphQL(ctx, query, vars, &resp); err != nil {
			return nil, err
		}
		for _, node := range resp.Data.Repository.Issue.Comments.Nodes {
			if node.IsMinimized || strings.TrimSpace(node.MinimizedReason) != "" {
				continue
			}
			out = append(out, IssueComment{
				ID:        node.DatabaseID,
				Author:    strings.TrimSpace(node.Author.Login),
				URL:       strings.TrimSpace(node.URL),
				Body:      strings.TrimSpace(node.Body),
				CreatedAt: strings.TrimSpace(node.CreatedAt),
			})
		}
		if !resp.Data.Repository.Issue.Comments.PageInfo.HasNextPage {
			break
		}
		after = resp.Data.Repository.Issue.Comments.PageInfo.EndCursor
		if after == "" {
			break
		}
	}
	return out, nil
}

func (c *Client) FetchReviewComments(ctx context.Context, number int) ([]ReviewComment, error) {
	if number <= 0 {
		return nil, fmt.Errorf("pr number must be positive")
	}
	query := `query($owner: String!, $name: String!, $number: Int!, $after: String) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviewThreads(first: 50, after: $after) {
        nodes {
          id
          isResolved
          comments(first: 50) {
            nodes {
              databaseId
              body
              url
              createdAt
              isMinimized
              minimizedReason
              author { login }
            }
            pageInfo { hasNextPage endCursor }
          }
        }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}`

	var out []ReviewComment
	var after string
	for {
		resp := reviewThreadsResponse{}
		vars := map[string]any{
			"owner":  c.owner,
			"name":   c.name,
			"number": number,
		}
		if after != "" {
			vars["after"] = after
		}
		if err := c.runGraphQL(ctx, query, vars, &resp); err != nil {
			return nil, err
		}
		for _, thread := range resp.Data.Repository.PullRequest.ReviewThreads.Nodes {
			if thread.IsResolved {
				continue
			}
			for _, node := range thread.Comments.Nodes {
				if node.IsMinimized || strings.TrimSpace(node.MinimizedReason) != "" {
					continue
				}
				out = append(out, ReviewComment{
					ID:        node.DatabaseID,
					Author:    strings.TrimSpace(node.Author.Login),
					URL:       strings.TrimSpace(node.URL),
					Body:      strings.TrimSpace(node.Body),
					ThreadID:  thread.ID,
					CreatedAt: strings.TrimSpace(node.CreatedAt),
				})
			}
		}
		if !resp.Data.Repository.PullRequest.ReviewThreads.PageInfo.HasNextPage {
			break
		}
		after = resp.Data.Repository.PullRequest.ReviewThreads.PageInfo.EndCursor
		if after == "" {
			break
		}
	}
	return out, nil
}

func (c *Client) runGraphQL(ctx context.Context, query string, vars map[string]any, out any) error {
	args := []string{"api", "graphql", "-f", "query=" + query}
	for key, val := range vars {
		if val == nil {
			continue
		}
		switch v := val.(type) {
		case int, int32, int64, uint, uint32, uint64, float32, float64, bool:
			args = append(args, "-F", fmt.Sprintf("%s=%v", key, v))
			continue
		}
		str := fmt.Sprintf("%v", val)
		if str == "" {
			continue
		}
		args = append(args, "-f", fmt.Sprintf("%s=%s", key, str))
	}
	if c.logger != nil {
		c.logger.Debug("github graphql query", "repo", c.repo, "args", args)
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

	env := os.Environ()
	env = append(env, "GITHUB_TOKEN="+c.token, "GH_TOKEN="+c.token)
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh api graphql failed: %w", err)
	}

	if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
		return fmt.Errorf("decode github graphql response: %w", err)
	}
	return nil
}
