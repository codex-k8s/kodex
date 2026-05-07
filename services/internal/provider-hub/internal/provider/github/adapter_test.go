package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
	providerclient "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/client"
)

func TestProbeAccountMapsGitHubRateLimits(t *testing.T) {
	t.Parallel()

	accountID := uuid.New()
	observedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rate_limit" {
			t.Fatalf("path = %s, want /rate_limit", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token-value" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"resources":{"core":{"limit":5000,"remaining":4999,"reset":1770000000},"search":{"limit":30,"remaining":29,"reset":1770000100},"graphql":{"limit":5000,"remaining":0,"reset":1770000200}}}`))
	}))
	defer server.Close()

	ids := idQueue([]uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()})
	result, err := New(Config{BaseURL: server.URL, HTTPClient: server.Client(), IDGenerator: &ids}).ProbeAccount(context.Background(), providerclient.AccountProbeRequest{
		Credential: providerclient.AccountCredential{
			ExternalAccountID: accountID,
			ProviderSlug:      enum.ProviderSlugGitHub,
			Token:             "token-value",
		},
		ObservedAt: observedAt,
	})
	if err != nil {
		t.Fatalf("ProbeAccount(): %v", err)
	}
	if result.RuntimeState.Status != enum.ProviderAccountRuntimeStatusLimited {
		t.Fatalf("runtime status = %s, want limited", result.RuntimeState.Status)
	}
	if len(result.LimitSnapshots) != 3 {
		t.Fatalf("limit snapshots = %d, want 3", len(result.LimitSnapshots))
	}
	if result.LimitSnapshots[0].ExternalAccountID != accountID || result.LimitSnapshots[0].CapturedAt != observedAt {
		t.Fatalf("first snapshot = %+v, want account %s and captured_at %s", result.LimitSnapshots[0], accountID, observedAt)
	}
}

func TestProbeAccountMapsUnauthorizedToPrecondition(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := New(Config{BaseURL: server.URL, HTTPClient: server.Client()}).ProbeAccount(context.Background(), providerclient.AccountProbeRequest{
		Credential: providerclient.AccountCredential{ExternalAccountID: uuid.New(), Token: "expired"},
		ObservedAt: time.Now(),
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("ProbeAccount() err = %v, want %v", err, errs.ErrPreconditionFailed)
	}
}

func TestNormalizeWebhookMapsGitHubIssuePayload(t *testing.T) {
	t.Parallel()

	receivedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	facts, ok, err := New(Config{}).NormalizeWebhook(entity.WebhookEvent{
		ProviderSlug: enum.ProviderSlugGitHub,
		EventName:    "issues",
		ReceivedAt:   receivedAt,
		PayloadJSON:  []byte(`{"action":"opened","repository":{"id":101,"full_name":"codex-k8s/kodex"},"issue":{"id":55,"number":7,"html_url":"https://github.com/codex-k8s/kodex/issues/7","title":"Issue title","state":"open","body":"Issue body","labels":[{"name":"type:dev"}],"assignees":[{"login":"kodex-agent"}],"updated_at":"2026-05-07T11:59:00Z"}}`),
	})
	if err != nil {
		t.Fatalf("NormalizeWebhook(): %v", err)
	}
	if !ok {
		t.Fatal("NormalizeWebhook() ok = false, want true")
	}
	if facts.FactKind != value.ProviderWebhookFactKindWorkItem || facts.ProviderWorkItemID != "github:codex-k8s/kodex:issue:7" || facts.RepositoryProviderID != "101" {
		t.Fatalf("facts = %+v, want GitHub issue facts", facts)
	}
	if facts.OccurredAt != receivedAt.Add(-time.Minute) {
		t.Fatalf("occurred_at = %s, want %s", facts.OccurredAt, receivedAt.Add(-time.Minute))
	}
	if facts.WorkItem == nil {
		t.Fatal("work item snapshot is nil, want issue snapshot")
	}
	if facts.WorkItem.Title != "Issue title" || facts.WorkItem.State != "open" || len(facts.WorkItem.Labels) != 1 || facts.WorkItem.Labels[0] != "type:dev" {
		t.Fatalf("work item snapshot = %+v, want title, state and labels", facts.WorkItem)
	}
}

func TestNormalizeWebhookMapsGitHubPRConversationCommentToPullRequestProjection(t *testing.T) {
	t.Parallel()

	receivedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	facts, ok, err := New(Config{}).NormalizeWebhook(entity.WebhookEvent{
		ProviderSlug: enum.ProviderSlugGitHub,
		EventName:    "issue_comment",
		ReceivedAt:   receivedAt,
		PayloadJSON:  []byte(`{"action":"created","repository":{"id":101,"full_name":"codex-k8s/kodex"},"issue":{"id":55,"number":7,"html_url":"https://github.com/codex-k8s/kodex/pull/7","title":"PR title","state":"open","body":"PR body","updated_at":"2026-05-07T11:59:00Z","pull_request":{"html_url":"https://github.com/codex-k8s/kodex/pull/7"}},"comment":{"id":900,"body":"Conversation comment","user":{"login":"reviewer"},"created_at":"2026-05-07T11:58:00Z","updated_at":"2026-05-07T11:59:30Z"}}`),
	})
	if err != nil {
		t.Fatalf("NormalizeWebhook(): %v", err)
	}
	if !ok {
		t.Fatal("NormalizeWebhook() ok = false, want true")
	}
	if facts.ProviderWorkItemID != "github:codex-k8s/kodex:pull_request:7" || facts.Kind != "comment" {
		t.Fatalf("facts = %+v, want PR comment facts", facts)
	}
	if facts.WorkItem == nil || facts.WorkItem.Kind != string(enum.WorkItemKindPullRequest) || facts.WorkItem.ProviderWorkItemID != facts.ProviderWorkItemID {
		t.Fatalf("work item = %+v, want pull request snapshot linked to facts", facts.WorkItem)
	}
	if facts.Comment == nil || facts.Comment.ProviderWorkItemID != facts.ProviderWorkItemID || facts.Comment.Kind != "comment" {
		t.Fatalf("comment = %+v, want comment linked to PR projection", facts.Comment)
	}
}

func TestNormalizeWebhookMapsGitHubReviewState(t *testing.T) {
	t.Parallel()

	receivedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	facts, ok, err := New(Config{}).NormalizeWebhook(entity.WebhookEvent{
		ProviderSlug: enum.ProviderSlugGitHub,
		EventName:    "pull_request_review",
		ReceivedAt:   receivedAt,
		PayloadJSON:  []byte(`{"action":"submitted","repository":{"id":101,"full_name":"codex-k8s/kodex"},"pull_request":{"id":77,"number":7,"html_url":"https://github.com/codex-k8s/kodex/pull/7","title":"PR title","state":"open","body":"PR body","updated_at":"2026-05-07T11:59:00Z"},"review":{"id":901,"body":"Looks good","state":"approved","user":{"login":"owner"},"submitted_at":"2026-05-07T11:59:30Z"}}`),
	})
	if err != nil {
		t.Fatalf("NormalizeWebhook(): %v", err)
	}
	if !ok {
		t.Fatal("NormalizeWebhook() ok = false, want true")
	}
	if facts.ProviderWorkItemID != "github:codex-k8s/kodex:pull_request:7" || facts.Comment == nil || facts.Comment.ReviewState != string(enum.ReviewStateApproved) {
		t.Fatalf("facts = %+v comment = %+v, want approved review linked to PR", facts, facts.Comment)
	}
}

func TestNormalizeWebhookIgnoresUnsupportedGitHubEvent(t *testing.T) {
	t.Parallel()

	_, ok, err := New(Config{}).NormalizeWebhook(entity.WebhookEvent{
		ProviderSlug: enum.ProviderSlugGitHub,
		EventName:    "ping",
		ReceivedAt:   time.Now(),
		PayloadJSON:  []byte(`{"zen":"keep it logically awesome"}`),
	})
	if err != nil {
		t.Fatalf("NormalizeWebhook(): %v", err)
	}
	if ok {
		t.Fatal("NormalizeWebhook() ok = true, want false")
	}
}

func TestNormalizeWebhookReturnsErrorForKnownPayloadWithoutRequiredID(t *testing.T) {
	t.Parallel()

	_, ok, err := New(Config{}).NormalizeWebhook(entity.WebhookEvent{
		ProviderSlug: enum.ProviderSlugGitHub,
		EventName:    "issues",
		ReceivedAt:   time.Now(),
		PayloadJSON:  []byte(`{"repository":{"id":101},"issue":{"number":7}}`),
	})
	if !ok {
		t.Fatal("NormalizeWebhook() ok = false, want true for known event")
	}
	if err == nil {
		t.Fatal("NormalizeWebhook() err = nil, want error")
	}
}

type idQueue []uuid.UUID

func (q *idQueue) New() uuid.UUID {
	if len(*q) == 0 {
		panic("test id sequence is empty")
	}
	id := (*q)[0]
	*q = (*q)[1:]
	return id
}
