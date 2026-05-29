package github

import (
	"context"
	"strconv"
	"strings"
	"time"

	githubapi "github.com/google/go-github/v82/github"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
	providerclient "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/client"
)

var _ providerclient.WebhookRefetcher = (*Adapter)(nil)

func (a *Adapter) RefetchWebhook(ctx context.Context, request providerclient.WebhookRefetchRequest) (providerclient.WebhookRefetchResult, error) {
	if request.Credential.Token.Len() == 0 || request.Credential.ProviderSlug != enum.ProviderSlugGitHub {
		return providerclient.WebhookRefetchResult{}, errs.ErrInvalidArgument
	}
	if request.Webhook.ProviderSlug != enum.ProviderSlugGitHub ||
		strings.TrimSpace(request.Webhook.EventName) != "pull_request" ||
		strings.TrimSpace(request.Envelope.Kind) != string(enum.WorkItemKindPullRequest) ||
		request.Envelope.Number <= 0 {
		return providerclient.WebhookRefetchResult{}, nil
	}
	owner, repo, err := parseRepositoryRef(request.Envelope.RepositoryFullName)
	if err != nil {
		return providerclient.WebhookRefetchResult{}, nil
	}
	client, err := a.githubClient(request.Credential.Token)
	if err != nil {
		return providerclient.WebhookRefetchResult{}, err
	}
	pullRequest, _, err := client.PullRequests.Get(ctx, owner, repo, int(request.Envelope.Number))
	if err != nil {
		return providerclient.WebhookRefetchResult{}, classifyGitHubError(err)
	}
	repositoryFullName := owner + "/" + repo
	snapshot := pullRequestAPISnapshot(repositoryFullName, pullRequest)
	if pullRequest.GetMerged() {
		snapshot.State = "merged"
	}
	occurredAt := snapshot.ProviderUpdatedAt
	if occurredAt.IsZero() {
		occurredAt = observedAtOrNow(request.ObservedAt)
	}
	facts := value.ProviderWebhookFacts{
		FactKind:             value.ProviderWebhookFactKindWorkItem,
		ProviderWorkItemID:   snapshot.ProviderWorkItemID,
		Kind:                 string(enum.WorkItemKindPullRequest),
		Number:               snapshot.Number,
		RepositoryFullName:   repositoryFullName,
		RepositoryProviderID: strings.TrimSpace(request.Envelope.RepositoryProviderID),
		OccurredAt:           occurredAt,
		WorkItem:             &snapshot,
	}
	if pullRequest.GetMerged() {
		facts.MergeSignal = pullRequestAPIMergeSignal(pullRequest, observedAtOrNow(request.ObservedAt))
	}
	return providerclient.WebhookRefetchResult{Facts: facts, OK: true}, nil
}

func pullRequestAPIMergeSignal(pullRequest *githubapi.PullRequest, fallback time.Time) *value.ProviderRepositoryMergeSignalSnapshot {
	mergedAt := pullRequest.GetMergedAt().Time
	if mergedAt.IsZero() {
		mergedAt = pullRequest.GetClosedAt().Time
	}
	if mergedAt.IsZero() {
		mergedAt = fallback.UTC()
	}
	return &value.ProviderRepositoryMergeSignalSnapshot{
		PullRequestProviderID: strconv.FormatInt(pullRequest.GetID(), 10),
		PullRequestURL:        strings.TrimSpace(pullRequest.GetHTMLURL()),
		BaseBranch:            strings.TrimSpace(pullRequest.GetBase().GetRef()),
		HeadBranch:            strings.TrimSpace(pullRequest.GetHead().GetRef()),
		MergeCommitSHA:        strings.TrimSpace(pullRequest.GetMergeCommitSHA()),
		SourceRef:             strings.TrimSpace(pullRequest.GetHead().GetRef()),
		MergedAt:              mergedAt.UTC(),
	}
}

func observedAtOrNow(observedAt time.Time) time.Time {
	if observedAt.IsZero() {
		return time.Now().UTC()
	}
	return observedAt.UTC()
}
