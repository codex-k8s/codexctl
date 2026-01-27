package promptctx

// IssueComment represents a GitHub Issue comment attached to a prompt context.
type IssueComment struct {
	IssueNumber int
	ID          int
	Author      string
	URL         string
	Body        string
	CreatedAt   string
}

// ReviewComment represents a GitHub PR review comment attached to a prompt context.
type ReviewComment struct {
	PRNumber  int
	ID        int
	Author    string
	URL       string
	Body      string
	ThreadID  string
	CreatedAt string
}
