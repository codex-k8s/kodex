package github

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	providerrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/provider"
	githubapi "github.com/google/go-github/v88/github"
)

const providerName = "github"

var defaultWebhookEvents = []string{
	"pull_request",
	"pull_request_review",
	"pull_request_review_comment",
	"issue_comment",
	"push",
}

type ProviderConfig struct {
	Token         string
	WebhookURL    string
	WebhookSecret string
	WebhookEvents []string
}

type Provider struct {
	client        *githubapi.Client
	webhookURL    string
	webhookSecret string
	webhookEvents []string
}

type TokenInspector struct{}

var _ providerrepo.RepositoryProvider = (*Provider)(nil)
var _ providerrepo.GitHubAccountInspector = (*TokenInspector)(nil)

func NewProvider(cfg ProviderConfig) (*Provider, error) {
	client, err := githubapi.NewClient(githubapi.WithAuthToken(strings.TrimSpace(cfg.Token)))
	if err != nil {
		return nil, fmt.Errorf("create github client: %w", err)
	}
	events := cfg.WebhookEvents
	if len(events) == 0 {
		events = defaultWebhookEvents
	}
	return &Provider{
		client:        client,
		webhookURL:    strings.TrimSpace(cfg.WebhookURL),
		webhookSecret: strings.TrimSpace(cfg.WebhookSecret),
		webhookEvents: append([]string(nil), events...),
	}, nil
}

func NewTokenInspector() *TokenInspector {
	return &TokenInspector{}
}

func (inspector *TokenInspector) InspectToken(ctx context.Context, token string) (providerrepo.GitHubTokenInspection, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return providerrepo.GitHubTokenInspection{}, fmt.Errorf("github token is required")
	}
	client, err := githubapi.NewClient(githubapi.WithAuthToken(token))
	if err != nil {
		return providerrepo.GitHubTokenInspection{}, fmt.Errorf("create github token client: %w", err)
	}
	user, response, err := client.Users.Get(ctx, "")
	if err != nil {
		return providerrepo.GitHubTokenInspection{}, githubError("github token introspection", err)
	}
	username := strings.TrimSpace(user.GetLogin())
	if username == "" {
		return providerrepo.GitHubTokenInspection{}, fmt.Errorf("github token introspection: username is empty")
	}
	email := strings.TrimSpace(user.GetEmail())
	if email == "" {
		email = inspector.primaryEmail(ctx, client)
	}
	if email == "" {
		email = username + "@users.noreply.github.com"
	}
	return providerrepo.GitHubTokenInspection{
		Username: username,
		Email:    email,
		Scopes:   githubScopesFromResponse(response),
	}, nil
}

func (inspector *TokenInspector) primaryEmail(ctx context.Context, client *githubapi.Client) string {
	emails, _, err := client.Users.ListEmails(ctx, &githubapi.ListOptions{PerPage: 100})
	if err != nil {
		return ""
	}
	for _, email := range emails {
		if email.GetPrimary() && email.GetVerified() && strings.TrimSpace(email.GetEmail()) != "" {
			return strings.TrimSpace(email.GetEmail())
		}
	}
	for _, email := range emails {
		if email.GetPrimary() && strings.TrimSpace(email.GetEmail()) != "" {
			return strings.TrimSpace(email.GetEmail())
		}
	}
	for _, email := range emails {
		if email.GetVerified() && strings.TrimSpace(email.GetEmail()) != "" {
			return strings.TrimSpace(email.GetEmail())
		}
	}
	for _, email := range emails {
		if strings.TrimSpace(email.GetEmail()) != "" {
			return strings.TrimSpace(email.GetEmail())
		}
	}
	return ""
}

func (provider *Provider) CheckRepository(ctx context.Context, owner string, name string) (providerrepo.RepositoryAccess, error) {
	repo, _, err := provider.client.Repositories.Get(ctx, owner, name)
	if err != nil {
		return providerrepo.RepositoryAccess{}, githubError("github repository access", err)
	}
	permissions := repo.GetPermissions()
	return providerrepo.RepositoryAccess{
		Provider:      providerName,
		Owner:         owner,
		Name:          name,
		DefaultBranch: repo.GetDefaultBranch(),
		Private:       repo.GetPrivate(),
		CanPull:       permissions.GetPull(),
		CanPush:       permissions.GetPush(),
		CanMaintain:   permissions.GetMaintain(),
		CanAdmin:      permissions.GetAdmin(),
	}, nil
}

func (provider *Provider) ResolveBranch(ctx context.Context, owner string, name string, branch string) (providerrepo.BranchRef, error) {
	ref, err := provider.getBranchRef(ctx, owner, name, branch)
	if err != nil {
		return providerrepo.BranchRef{}, err
	}
	return providerrepo.BranchRef{
		Provider: providerName,
		Owner:    owner,
		Name:     name,
		Branch:   branch,
		SHA:      ref.GetObject().GetSHA(),
		Created:  false,
	}, nil
}

func (provider *Provider) CreateBranch(ctx context.Context, owner string, name string, branch string, baseBranch string) (providerrepo.BranchRef, error) {
	baseRef, err := provider.getBranchRef(ctx, owner, name, baseBranch)
	if err != nil {
		return providerrepo.BranchRef{}, err
	}
	createdRef, _, err := provider.client.Git.CreateRef(ctx, owner, name, githubapi.CreateRef{
		Ref: "refs/heads/" + branch,
		SHA: baseRef.GetObject().GetSHA(),
	})
	if err != nil {
		return providerrepo.BranchRef{}, githubError("github create branch", err)
	}
	return providerrepo.BranchRef{
		Provider: providerName,
		Owner:    owner,
		Name:     name,
		Branch:   branch,
		SHA:      createdRef.GetObject().GetSHA(),
		Created:  true,
	}, nil
}

func (provider *Provider) PreviewPullRequest(ctx context.Context, input providerrepo.PullRequestInput) (providerrepo.PullRequestPreview, error) {
	headRef, err := provider.getBranchRef(ctx, input.Owner, input.Name, input.Head)
	if err != nil {
		return providerrepo.PullRequestPreview{}, err
	}
	baseRef, err := provider.getBranchRef(ctx, input.Owner, input.Name, input.Base)
	if err != nil {
		return providerrepo.PullRequestPreview{}, err
	}
	return providerrepo.PullRequestPreview{
		Provider: providerName,
		Owner:    input.Owner,
		Name:     input.Name,
		Head:     input.Head,
		Base:     input.Base,
		Title:    input.Title,
		HeadSHA:  headRef.GetObject().GetSHA(),
		BaseSHA:  baseRef.GetObject().GetSHA(),
	}, nil
}

func (provider *Provider) CreatePullRequest(ctx context.Context, input providerrepo.PullRequestInput) (providerrepo.PullRequestSummary, error) {
	pullRequest, _, err := provider.client.PullRequests.Create(ctx, input.Owner, input.Name, &githubapi.NewPullRequest{
		Title: githubapi.Ptr(input.Title),
		Head:  githubapi.Ptr(input.Head),
		Base:  githubapi.Ptr(input.Base),
		Body:  githubapi.Ptr(input.Body),
		Draft: githubapi.Ptr(input.Draft),
	})
	if err != nil {
		return providerrepo.PullRequestSummary{}, githubError("github create pull request", err)
	}
	return summaryFromPullRequest(input.Owner, input.Name, pullRequest), nil
}

func (provider *Provider) GetPullRequest(ctx context.Context, owner string, name string, number int) (providerrepo.PullRequestSummary, error) {
	pullRequest, _, err := provider.client.PullRequests.Get(ctx, owner, name, number)
	if err != nil {
		return providerrepo.PullRequestSummary{}, githubError("github get pull request", err)
	}
	summary := summaryFromPullRequest(owner, name, pullRequest)

	reviews, _, err := provider.client.PullRequests.ListReviews(ctx, owner, name, number, &githubapi.ListOptions{PerPage: 20})
	if err != nil {
		return providerrepo.PullRequestSummary{}, githubError("github list pull request reviews", err)
	}
	summary.ReviewCount = len(reviews)
	for _, review := range latestReviews(reviews, 5) {
		summary.LatestReviews = append(summary.LatestReviews, providerrepo.PullRequestReview{
			State:  review.GetState(),
			Author: review.GetUser().GetLogin(),
		})
	}

	comments, _, err := provider.client.PullRequests.ListComments(ctx, owner, name, number, &githubapi.PullRequestListCommentsOptions{
		ListOptions: githubapi.ListOptions{PerPage: 1},
	})
	if err != nil {
		return providerrepo.PullRequestSummary{}, githubError("github list pull request comments", err)
	}
	summary.ReviewCommentCount = len(comments)
	return summary, nil
}

func (provider *Provider) EnsureRepositoryWebhook(ctx context.Context, owner string, name string) (providerrepo.WebhookRegistration, error) {
	if provider.webhookURL == "" || provider.webhookSecret == "" {
		return providerrepo.WebhookRegistration{}, fmt.Errorf("github webhook config is not complete")
	}
	hook := &githubapi.Hook{
		Config: &githubapi.HookConfig{
			URL:         githubapi.Ptr(provider.webhookURL),
			ContentType: githubapi.Ptr("json"),
			InsecureSSL: githubapi.Ptr("0"),
			Secret:      githubapi.Ptr(provider.webhookSecret),
		},
		Events: append([]string(nil), provider.webhookEvents...),
		Active: githubapi.Ptr(true),
	}

	existing, err := provider.findWebhook(ctx, owner, name)
	if err != nil {
		return providerrepo.WebhookRegistration{}, err
	}
	created := false
	if existing == nil {
		existing, _, err = provider.client.Repositories.CreateHook(ctx, owner, name, hook)
		created = true
	} else {
		existing, _, err = provider.client.Repositories.EditHook(ctx, owner, name, existing.GetID(), hook)
	}
	if err != nil {
		return providerrepo.WebhookRegistration{}, githubError("github ensure repository webhook", err)
	}
	if _, err := provider.client.Repositories.PingHook(ctx, owner, name, existing.GetID()); err != nil {
		return providerrepo.WebhookRegistration{}, githubError("github ping repository webhook", err)
	}
	return providerrepo.WebhookRegistration{
		Provider: providerName,
		Owner:    owner,
		Name:     name,
		ID:       existing.GetID(),
		URL:      provider.webhookURL,
		Events:   append([]string(nil), provider.webhookEvents...),
		Created:  created,
		Active:   existing.GetActive(),
	}, nil
}

func (provider *Provider) findWebhook(ctx context.Context, owner string, name string) (*githubapi.Hook, error) {
	hooks, _, err := provider.client.Repositories.ListHooks(ctx, owner, name, &githubapi.ListOptions{PerPage: 100})
	if err != nil {
		return nil, githubError("github list repository webhooks", err)
	}
	for _, hook := range hooks {
		if hook.GetConfig().GetURL() == provider.webhookURL {
			return hook, nil
		}
	}
	return nil, nil
}

func (provider *Provider) getBranchRef(ctx context.Context, owner string, name string, branch string) (*githubapi.Reference, error) {
	ref, _, err := provider.client.Git.GetRef(ctx, owner, name, "heads/"+branch)
	if err != nil {
		return nil, githubError("github get branch ref", err)
	}
	return ref, nil
}

func githubError(operation string, err error) error {
	var response *githubapi.ErrorResponse
	if errors.As(err, &response) {
		message := strings.TrimSpace(response.Message)
		if message == "" && response.Response != nil {
			message = response.Response.Status
		}
		if message != "" {
			return fmt.Errorf("%s: %s", operation, message)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func githubScopesFromResponse(response *githubapi.Response) []string {
	if response == nil || response.Response == nil {
		return nil
	}
	raw := response.Response.Header.Get("X-OAuth-Scopes")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	scopes := make([]string, 0, len(parts))
	for _, part := range parts {
		scope := strings.TrimSpace(part)
		if scope != "" {
			scopes = append(scopes, scope)
		}
	}
	sort.Strings(scopes)
	return scopes
}

func summaryFromPullRequest(owner string, name string, pullRequest *githubapi.PullRequest) providerrepo.PullRequestSummary {
	return providerrepo.PullRequestSummary{
		Provider:       providerName,
		Owner:          owner,
		Name:           name,
		Number:         pullRequest.GetNumber(),
		Title:          pullRequest.GetTitle(),
		State:          pullRequest.GetState(),
		URL:            pullRequest.GetHTMLURL(),
		Draft:          pullRequest.GetDraft(),
		Merged:         pullRequest.GetMerged(),
		MergeableState: pullRequest.GetMergeableState(),
	}
}

func latestReviews(reviews []*githubapi.PullRequestReview, limit int) []*githubapi.PullRequestReview {
	if len(reviews) <= limit {
		return reviews
	}
	return reviews[len(reviews)-limit:]
}
