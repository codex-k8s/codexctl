// Package promptctx defines data structures used to enrich prompt templates.
package promptctx

// IssueComment represents a GitHub Issue comment attached to a prompt context.
type IssueComment struct {
	// IssueNumber is the GitHub issue number the comment belongs to.
	IssueNumber int
	// ID is the GitHub comment database ID.
	ID int
	// Author is the GitHub login of the comment author.
	Author string
	// URL is the canonical URL of the comment.
	URL string
	// Body is the raw markdown body of the comment.
	Body string
	// CreatedAt is the ISO timestamp of comment creation.
	CreatedAt string
}

// ReviewComment represents a GitHub PR review comment attached to a prompt context.
type ReviewComment struct {
	// PRNumber is the GitHub pull request number the comment belongs to.
	PRNumber int
	// ID is the GitHub comment database ID.
	ID int
	// Author is the GitHub login of the comment author.
	Author string
	// URL is the canonical URL of the comment.
	URL string
	// Body is the raw markdown body of the comment.
	Body string
	// ThreadID identifies the review thread in GraphQL.
	ThreadID string
	// CreatedAt is the ISO timestamp of comment creation.
	CreatedAt string
}
