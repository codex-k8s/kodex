package provider

import "context"

type RepositoryAccess struct {
	Provider      string
	Owner         string
	Name          string
	DefaultBranch string
	Private       bool
	CanPull       bool
	CanPush       bool
	CanMaintain   bool
	CanAdmin      bool
}

type BranchRef struct {
	Provider string
	Owner    string
	Name     string
	Branch   string
	SHA      string
	Created  bool
}

type PullRequestPreview struct {
	Provider string
	Owner    string
	Name     string
	Head     string
	Base     string
	Title    string
	HeadSHA  string
	BaseSHA  string
}

type PullRequestSummary struct {
	Provider           string
	Owner              string
	Name               string
	Number             int
	Title              string
	State              string
	URL                string
	Draft              bool
	Merged             bool
	MergeableState     string
	ReviewCount        int
	ReviewCommentCount int
	LatestReviews      []PullRequestReview
}

type PullRequestReview struct {
	State  string
	Author string
}

type WebhookRegistration struct {
	Provider string
	Owner    string
	Name     string
	ID       int64
	URL      string
	Events   []string
	Created  bool
	Active   bool
}

type GitHubAccountRef struct {
	Name      string
	SecretRef string
}

type GitHubTokenInspection struct {
	Username string
	Email    string
	Scopes   []string
}

type RepositoryCandidate struct {
	Provider      string
	Owner         string
	Name          string
	FullName      string
	DefaultBranch string
	Private       bool
	Description   string
	URL           string
}

type BranchCandidate struct {
	Name      string
	Protected bool
}

type RepositorySearchInput struct {
	Account GitHubAccountRef
	Query   string
	Limit   int
}

type RepositoryListInput struct {
	Account GitHubAccountRef
	Limit   int
}

type PullRequestInput struct {
	Owner string
	Name  string
	Head  string
	Base  string
	Title string
	Body  string
	Draft bool
}

type RepositoryProvider interface {
	CheckRepository(ctx context.Context, owner string, name string) (RepositoryAccess, error)
	ResolveBranch(ctx context.Context, owner string, name string, branch string) (BranchRef, error)
	CreateBranch(ctx context.Context, owner string, name string, branch string, baseBranch string) (BranchRef, error)
	PreviewPullRequest(ctx context.Context, input PullRequestInput) (PullRequestPreview, error)
	CreatePullRequest(ctx context.Context, input PullRequestInput) (PullRequestSummary, error)
	GetPullRequest(ctx context.Context, owner string, name string, number int) (PullRequestSummary, error)
	EnsureRepositoryWebhook(ctx context.Context, owner string, name string) (WebhookRegistration, error)
}

type GitHubAccountInspector interface {
	InspectToken(ctx context.Context, token string) (GitHubTokenInspection, error)
}

type GitHubAccountRepositoryProvider interface {
	ListRepositories(ctx context.Context, input RepositoryListInput) ([]RepositoryCandidate, error)
	SearchRepositories(ctx context.Context, input RepositorySearchInput) ([]RepositoryCandidate, error)
	ListBranches(ctx context.Context, account GitHubAccountRef, owner string, name string, limit int) ([]BranchCandidate, error)
	CheckRepository(ctx context.Context, account GitHubAccountRef, owner string, name string) (RepositoryAccess, error)
	EnsureRepositoryWebhook(ctx context.Context, account GitHubAccountRef, owner string, name string) (WebhookRegistration, error)
}
