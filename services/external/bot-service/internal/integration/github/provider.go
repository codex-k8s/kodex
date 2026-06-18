package github

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	providerrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/provider"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
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

type AccountProvider struct {
	tokenReader   runtimerepo.GitHubTokenSecretReader
	webhookURL    string
	webhookSecret string
	webhookEvents []string
}

type TokenInspector struct{}

var _ providerrepo.RepositoryProvider = (*Provider)(nil)
var _ providerrepo.GitHubAccountRepositoryProvider = (*AccountProvider)(nil)
var _ providerrepo.GitHubAccountInspector = (*TokenInspector)(nil)

func NewProvider(cfg ProviderConfig) (*Provider, error) {
	client, err := githubapi.NewClient(githubapi.WithAuthToken(strings.TrimSpace(cfg.Token)))
	if err != nil {
		return nil, fmt.Errorf("create github client: %w", err)
	}
	return &Provider{
		client:        client,
		webhookURL:    strings.TrimSpace(cfg.WebhookURL),
		webhookSecret: strings.TrimSpace(cfg.WebhookSecret),
		webhookEvents: normalizedWebhookEvents(cfg.WebhookEvents),
	}, nil
}

func NewAccountProvider(reader runtimerepo.GitHubTokenSecretReader, cfg ProviderConfig) *AccountProvider {
	return &AccountProvider{
		tokenReader:   reader,
		webhookURL:    strings.TrimSpace(cfg.WebhookURL),
		webhookSecret: strings.TrimSpace(cfg.WebhookSecret),
		webhookEvents: normalizedWebhookEvents(cfg.WebhookEvents),
	}
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
	return checkRepository(ctx, provider.client, owner, name)
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
	return ensureRepositoryWebhook(ctx, provider.client, provider.webhookURL, provider.webhookSecret, provider.webhookEvents, owner, name)
}

func (provider *AccountProvider) ListRepositories(ctx context.Context, input providerrepo.RepositoryListInput) ([]providerrepo.RepositoryCandidate, error) {
	client, err := provider.client(ctx, input.Account)
	if err != nil {
		return nil, err
	}
	owner := strings.TrimSpace(input.Owner)
	if owner != "" {
		repositories, err := listOwnerRepositories(ctx, client, owner, input.OwnerType, input.Limit)
		if err != nil {
			return nil, githubError("github list owner repositories", err)
		}
		return repositoryCandidatesForOwner(repositories, owner), nil
	}
	repositories, _, err := client.Repositories.ListByAuthenticatedUser(ctx, &githubapi.RepositoryListByAuthenticatedUserOptions{
		Visibility:  "all",
		Affiliation: "owner,collaborator,organization_member",
		Sort:        "pushed",
		Direction:   "desc",
		ListOptions: githubapi.ListOptions{PerPage: githubPageLimit(input.Limit)},
	})
	if err != nil {
		return nil, githubError("github list account repositories", err)
	}
	return repositoryCandidates(repositories), nil
}

func (provider *AccountProvider) SearchRepositories(ctx context.Context, input providerrepo.RepositorySearchInput) ([]providerrepo.RepositoryCandidate, error) {
	client, err := provider.client(ctx, input.Account)
	if err != nil {
		return nil, err
	}
	owner := strings.TrimSpace(input.Owner)
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return provider.ListRepositories(ctx, providerrepo.RepositoryListInput{Account: input.Account, Owner: owner, OwnerType: input.OwnerType, Limit: input.Limit})
	}
	if owner, name, ok := parseFullRepositoryName(query); ok {
		repo, _, err := client.Repositories.Get(ctx, owner, name)
		if err == nil && repositoryOwnerMatches(repositoryCandidate(repo), input.Owner) {
			return []providerrepo.RepositoryCandidate{repositoryCandidate(repo)}, nil
		}
	}
	if owner != "" && normalizedGitHubOwnerType(input.OwnerType) == "" {
		repositories, err := listOwnerRepositories(ctx, client, owner, input.OwnerType, input.Limit)
		if err != nil {
			return nil, githubError("github search owner repositories", err)
		}
		return filterRepositoryCandidates(repositoryCandidatesForOwner(repositories, owner), query), nil
	}
	result, _, err := client.Search.Repositories(ctx, repositoryOwnerSearchQuery(owner, input.OwnerType, query), &githubapi.SearchOptions{
		ListOptions: githubapi.ListOptions{PerPage: githubPageLimit(input.Limit)},
	})
	if err != nil {
		return nil, githubError("github search repositories", err)
	}
	return repositoryCandidatesForOwner(result.Repositories, owner), nil
}

func (provider *AccountProvider) ListBranches(ctx context.Context, account providerrepo.GitHubAccountRef, owner string, name string, limit int) ([]providerrepo.BranchCandidate, error) {
	client, err := provider.client(ctx, account)
	if err != nil {
		return nil, err
	}
	branches, _, err := client.Repositories.ListBranches(ctx, owner, name, &githubapi.BranchListOptions{
		ListOptions: githubapi.ListOptions{PerPage: githubPageLimit(limit)},
	})
	if err != nil {
		return nil, githubError("github list repository branches", err)
	}
	items := make([]providerrepo.BranchCandidate, 0, len(branches))
	for _, branch := range branches {
		items = append(items, providerrepo.BranchCandidate{
			Name:      branch.GetName(),
			Protected: branch.GetProtected(),
		})
	}
	return items, nil
}

func (provider *AccountProvider) CheckRepository(ctx context.Context, account providerrepo.GitHubAccountRef, owner string, name string) (providerrepo.RepositoryAccess, error) {
	client, err := provider.client(ctx, account)
	if err != nil {
		return providerrepo.RepositoryAccess{}, err
	}
	return checkRepository(ctx, client, owner, name)
}

func (provider *AccountProvider) EnsureRepositoryWebhook(ctx context.Context, account providerrepo.GitHubAccountRef, owner string, name string) (providerrepo.WebhookRegistration, error) {
	client, err := provider.client(ctx, account)
	if err != nil {
		return providerrepo.WebhookRegistration{}, err
	}
	return ensureRepositoryWebhook(ctx, client, provider.webhookURL, provider.webhookSecret, provider.webhookEvents, owner, name)
}

func (provider *AccountProvider) client(ctx context.Context, account providerrepo.GitHubAccountRef) (*githubapi.Client, error) {
	if provider == nil || provider.tokenReader == nil {
		return nil, fmt.Errorf("github account provider is not configured")
	}
	account.Name = strings.TrimSpace(account.Name)
	account.SecretRef = strings.TrimSpace(account.SecretRef)
	if account.Name == "" {
		return nil, fmt.Errorf("github account name is required")
	}
	if account.SecretRef == "" {
		return nil, fmt.Errorf("github account secret is required")
	}
	credential, err := provider.tokenReader.GetGitHubTokenSecret(ctx, account.Name, account.SecretRef)
	if err != nil {
		return nil, err
	}
	client, err := githubapi.NewClient(githubapi.WithAuthToken(credential.Token))
	if err != nil {
		return nil, fmt.Errorf("create github account client: %w", err)
	}
	return client, nil
}

func checkRepository(ctx context.Context, client *githubapi.Client, owner string, name string) (providerrepo.RepositoryAccess, error) {
	repo, _, err := client.Repositories.Get(ctx, owner, name)
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

func ensureRepositoryWebhook(ctx context.Context, client *githubapi.Client, webhookURL string, webhookSecret string, webhookEvents []string, owner string, name string) (providerrepo.WebhookRegistration, error) {
	if webhookURL == "" || webhookSecret == "" {
		return providerrepo.WebhookRegistration{}, fmt.Errorf("github webhook config is not complete")
	}
	hook := &githubapi.Hook{
		Config: &githubapi.HookConfig{
			URL:         githubapi.Ptr(webhookURL),
			ContentType: githubapi.Ptr("json"),
			InsecureSSL: githubapi.Ptr("0"),
			Secret:      githubapi.Ptr(webhookSecret),
		},
		Events: append([]string(nil), webhookEvents...),
		Active: githubapi.Ptr(true),
	}

	existing, err := findWebhook(ctx, client, owner, name, webhookURL)
	if err != nil {
		return providerrepo.WebhookRegistration{}, err
	}
	created := false
	if existing == nil {
		existing, _, err = client.Repositories.CreateHook(ctx, owner, name, hook)
		created = true
	} else {
		existing, _, err = client.Repositories.EditHook(ctx, owner, name, existing.GetID(), hook)
	}
	if err != nil {
		return providerrepo.WebhookRegistration{}, githubError("github ensure repository webhook", err)
	}
	if _, err := client.Repositories.PingHook(ctx, owner, name, existing.GetID()); err != nil {
		return providerrepo.WebhookRegistration{}, githubError("github ping repository webhook", err)
	}
	return providerrepo.WebhookRegistration{
		Provider: providerName,
		Owner:    owner,
		Name:     name,
		ID:       existing.GetID(),
		URL:      webhookURL,
		Events:   append([]string(nil), webhookEvents...),
		Created:  created,
		Active:   existing.GetActive(),
	}, nil
}

func findWebhook(ctx context.Context, client *githubapi.Client, owner string, name string, webhookURL string) (*githubapi.Hook, error) {
	hooks, _, err := client.Repositories.ListHooks(ctx, owner, name, &githubapi.ListOptions{PerPage: 100})
	if err != nil {
		return nil, githubError("github list repository webhooks", err)
	}
	for _, hook := range hooks {
		if hook.GetConfig().GetURL() == webhookURL {
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

func normalizedWebhookEvents(events []string) []string {
	if len(events) == 0 {
		return append([]string(nil), defaultWebhookEvents...)
	}
	normalized := make([]string, 0, len(events))
	for _, event := range events {
		event = strings.TrimSpace(event)
		if event != "" {
			normalized = append(normalized, event)
		}
	}
	if len(normalized) == 0 {
		return append([]string(nil), defaultWebhookEvents...)
	}
	return normalized
}

func githubPageLimit(limit int) int {
	if limit <= 0 {
		return 10
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func listOwnerRepositories(ctx context.Context, client *githubapi.Client, owner string, ownerType string, limit int) ([]*githubapi.Repository, error) {
	switch normalizedGitHubOwnerType(ownerType) {
	case "org":
		repositories, _, err := client.Repositories.ListByOrg(ctx, owner, &githubapi.RepositoryListByOrgOptions{
			Type:        "all",
			ListOptions: githubapi.ListOptions{PerPage: githubPageLimit(limit)},
		})
		return repositories, err
	case "user":
		repositories, _, err := client.Repositories.List(ctx, owner, &githubapi.RepositoryListOptions{
			Type:        "all",
			Sort:        "pushed",
			Direction:   "desc",
			ListOptions: githubapi.ListOptions{PerPage: githubPageLimit(limit)},
		})
		return repositories, err
	default:
		repositories, _, err := client.Repositories.ListByOrg(ctx, owner, &githubapi.RepositoryListByOrgOptions{
			Type:        "all",
			ListOptions: githubapi.ListOptions{PerPage: githubPageLimit(limit)},
		})
		if err == nil {
			return repositories, nil
		}
		if !githubNotFound(err) {
			return nil, err
		}
		repositories, _, err = client.Repositories.List(ctx, owner, &githubapi.RepositoryListOptions{
			Type:        "all",
			Sort:        "pushed",
			Direction:   "desc",
			ListOptions: githubapi.ListOptions{PerPage: githubPageLimit(limit)},
		})
		return repositories, err
	}
}

func repositorySearchQuery(query string) string {
	query = strings.Join(strings.Fields(query), " ")
	if query == "" {
		return "archived:false"
	}
	return query + " archived:false"
}

func repositoryOwnerSearchQuery(owner string, ownerType string, query string) string {
	query = repositorySearchQuery(query)
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return query
	}
	ownerFilter := ""
	switch normalizedGitHubOwnerType(ownerType) {
	case "org":
		ownerFilter = "org:" + owner
	case "user":
		ownerFilter = "user:" + owner
	}
	if ownerFilter == "" {
		return query
	}
	return ownerFilter + " " + query
}

func normalizedGitHubOwnerType(ownerType string) string {
	switch strings.ToLower(strings.TrimSpace(ownerType)) {
	case "org", "user":
		return strings.ToLower(strings.TrimSpace(ownerType))
	default:
		return ""
	}
}

func parseFullRepositoryName(value string) (string, string, bool) {
	owner, name, ok := strings.Cut(strings.TrimSpace(value), "/")
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", false
	}
	return owner, name, true
}

func repositoryCandidates(repositories []*githubapi.Repository) []providerrepo.RepositoryCandidate {
	items := make([]providerrepo.RepositoryCandidate, 0, len(repositories))
	for _, repository := range repositories {
		item := repositoryCandidate(repository)
		if item.Owner != "" && item.Name != "" {
			items = append(items, item)
		}
	}
	return items
}

func repositoryCandidatesForOwner(repositories []*githubapi.Repository, owner string) []providerrepo.RepositoryCandidate {
	items := repositoryCandidates(repositories)
	if strings.TrimSpace(owner) == "" {
		return items
	}
	filtered := items[:0]
	for _, item := range items {
		if repositoryOwnerMatches(item, owner) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterRepositoryCandidates(candidates []providerrepo.RepositoryCandidate, query string) []providerrepo.RepositoryCandidate {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return candidates
	}
	filtered := candidates[:0]
	for _, candidate := range candidates {
		haystack := strings.ToLower(strings.Join([]string{
			candidate.Owner,
			candidate.Name,
			candidate.FullName,
			candidate.Description,
		}, " "))
		if strings.Contains(haystack, query) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func repositoryOwnerMatches(repository providerrepo.RepositoryCandidate, owner string) bool {
	owner = strings.TrimSpace(owner)
	return owner == "" || strings.EqualFold(repository.Owner, owner)
}

func repositoryCandidate(repository *githubapi.Repository) providerrepo.RepositoryCandidate {
	if repository == nil {
		return providerrepo.RepositoryCandidate{}
	}
	owner := repository.GetOwner().GetLogin()
	name := repository.GetName()
	fullName := repository.GetFullName()
	if fullName == "" && owner != "" && name != "" {
		fullName = owner + "/" + name
	}
	return providerrepo.RepositoryCandidate{
		Provider:      providerName,
		Owner:         owner,
		Name:          name,
		FullName:      fullName,
		DefaultBranch: repository.GetDefaultBranch(),
		Private:       repository.GetPrivate(),
		Description:   repository.GetDescription(),
		URL:           repository.GetHTMLURL(),
	}
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

func githubNotFound(err error) bool {
	var response *githubapi.ErrorResponse
	if errors.As(err, &response) && response.Response != nil {
		return response.Response.StatusCode == 404
	}
	return false
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
