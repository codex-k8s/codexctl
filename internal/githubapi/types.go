package githubapi

type IssueComment struct {
	ID        int
	Author    string
	URL       string
	Body      string
	CreatedAt string
}

type ReviewComment struct {
	ID        int
	Author    string
	URL       string
	Body      string
	ThreadID  string
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
					Nodes    []issueCommentNode `json:"nodes"`
					PageInfo pageInfo           `json:"pageInfo"`
				} `json:"comments"`
			} `json:"issue"`
		} `json:"repository"`
	} `json:"data"`
}

type issueCommentNode struct {
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
	Nodes    []reviewCommentNode `json:"nodes"`
	PageInfo pageInfo            `json:"pageInfo"`
}

type reviewCommentNode struct {
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
