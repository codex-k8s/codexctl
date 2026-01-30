// Package githubapi provides minimal GitHub API models for GraphQL responses.
package githubapi

type IssueComment struct {
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

type ReviewComment struct {
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

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type issueCommentsResponse struct {
	Data struct {
		Repository struct {
			Issue struct {
				Comments struct {
					Nodes    []commentNode `json:"nodes"`
					PageInfo pageInfo      `json:"pageInfo"`
				} `json:"comments"`
			} `json:"issue"`
		} `json:"repository"`
	} `json:"data"`
}

type commentNode struct {
	DatabaseID      int    `json:"databaseId"`
	Body            string `json:"body"`
	URL             string `json:"url"`
	CreatedAt       string `json:"createdAt"`
	IsMinimized     bool   `json:"isMinimized"`
	MinimizedReason string `json:"minimizedReason"`
	Author          struct {
		Login string `json:"login"`
	} `json:"author"`
}

type reviewThreadsResponse struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes    []reviewThreadNode `json:"nodes"`
					PageInfo pageInfo           `json:"pageInfo"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

type reviewThreadNode struct {
	ID         string             `json:"id"`
	IsResolved bool               `json:"isResolved"`
	Comments   reviewCommentBlock `json:"comments"`
}

type reviewCommentBlock struct {
	Nodes    []commentNode `json:"nodes"`
	PageInfo pageInfo      `json:"pageInfo"`
}
