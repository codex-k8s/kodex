package github

import (
	"context"
	"fmt"
	"strings"

	providerrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/provider"
	githubapi "github.com/google/go-github/v88/github"
)

const providerName = "github"

type Provider struct {
	client *githubapi.Client
}

var _ providerrepo.RepositoryProvider = (*Provider)(nil)

func NewProvider(token string) (*Provider, error) {
	client, err := githubapi.NewClient(githubapi.WithAuthToken(strings.TrimSpace(token)))
	if err != nil {
		return nil, fmt.Errorf("create github client: %w", err)
	}
	return &Provider{client: client}, nil
}

func (provider *Provider) CheckRepository(ctx context.Context, owner string, name string) (providerrepo.RepositoryAccess, error) {
	repo, _, err := provider.client.Repositories.Get(ctx, owner, name)
	if err != nil {
		return providerrepo.RepositoryAccess{}, fmt.Errorf("github repository access: %w", err)
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
		return providerrepo.BranchRef{}, fmt.Errorf("github create branch: %w", err)
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
		return providerrepo.PullRequestSummary{}, fmt.Errorf("github create pull request: %w", err)
	}
	return summaryFromPullRequest(input.Owner, input.Name, pullRequest), nil
}

func (provider *Provider) GetPullRequest(ctx context.Context, owner string, name string, number int) (providerrepo.PullRequestSummary, error) {
	pullRequest, _, err := provider.client.PullRequests.Get(ctx, owner, name, number)
	if err != nil {
		return providerrepo.PullRequestSummary{}, fmt.Errorf("github get pull request: %w", err)
	}
	summary := summaryFromPullRequest(owner, name, pullRequest)

	reviews, _, err := provider.client.PullRequests.ListReviews(ctx, owner, name, number, &githubapi.ListOptions{PerPage: 20})
	if err != nil {
		return providerrepo.PullRequestSummary{}, fmt.Errorf("github list pull request reviews: %w", err)
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
		return providerrepo.PullRequestSummary{}, fmt.Errorf("github list pull request comments: %w", err)
	}
	summary.ReviewCommentCount = len(comments)
	return summary, nil
}

func (provider *Provider) getBranchRef(ctx context.Context, owner string, name string, branch string) (*githubapi.Reference, error) {
	ref, _, err := provider.client.Git.GetRef(ctx, owner, name, "heads/"+branch)
	if err != nil {
		return nil, fmt.Errorf("github get branch ref: %w", err)
	}
	return ref, nil
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
