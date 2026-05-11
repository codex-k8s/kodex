package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
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
	token := secretresolver.NewSecretValue([]byte("token-value"))
	defer token.Clear()
	result, err := New(Config{BaseURL: server.URL, HTTPClient: server.Client(), IDGenerator: &ids}).ProbeAccount(context.Background(), providerclient.AccountProbeRequest{
		Credential: providerclient.AccountCredential{
			ExternalAccountID: accountID,
			ProviderSlug:      enum.ProviderSlugGitHub,
			Token:             token,
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

	token := secretresolver.NewSecretValue([]byte("expired"))
	defer token.Clear()
	_, err := New(Config{BaseURL: server.URL, HTTPClient: server.Client()}).ProbeAccount(context.Background(), providerclient.AccountProbeRequest{
		Credential: providerclient.AccountCredential{ExternalAccountID: uuid.New(), Token: token},
		ObservedAt: time.Now(),
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("ProbeAccount() err = %v, want %v", err, errs.ErrPreconditionFailed)
	}
}

func TestReconcileReadsRepositoryIssues(t *testing.T) {
	t.Parallel()

	accountID := uuid.New()
	observedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	resetAt := observedAt.Add(time.Hour).Unix()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/codex-k8s/kodex/issues" {
			t.Fatalf("path = %s, want repository issues", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token-value" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("per_page") != "50" {
			t.Fatalf("per_page = %q, want 50", r.URL.Query().Get("per_page"))
		}
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4998")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))
		_, _ = w.Write([]byte(`[{"id":100,"number":7,"html_url":"https://github.com/codex-k8s/kodex/issues/7","title":"Issue title","state":"open","body":"<!-- kodex:artifact v1\nkind: issue\nmanaged_by: kodex\nwork_type: dev\n-->","labels":[{"name":"type:dev"}],"assignees":[{"login":"kodex-agent"}],"updated_at":"2026-05-07T11:59:00Z"}]`))
	}))
	defer server.Close()

	token := secretresolver.NewSecretValue([]byte("token-value"))
	defer token.Clear()
	ids := idQueue([]uuid.UUID{uuid.New()})
	result, err := New(Config{BaseURL: server.URL, HTTPClient: server.Client(), IDGenerator: &ids}).Reconcile(context.Background(), providerclient.ReconciliationRequest{
		Credential: providerclient.AccountCredential{ExternalAccountID: accountID, ProviderSlug: enum.ProviderSlugGitHub, Token: token},
		Cursor: entity.SyncCursor{
			ProviderSlug:        enum.ProviderSlugGitHub,
			ScopeType:           enum.SyncCursorScopeRepository,
			ScopeRef:            "codex-k8s/kodex",
			ArtifactKind:        enum.SyncArtifactIssue,
			RateBudgetStateJSON: []byte(`{}`),
		},
		MaxItems:   50,
		ObservedAt: observedAt,
	})
	if err != nil {
		t.Fatalf("Reconcile(): %v", err)
	}
	if len(result.WorkItems) != 1 {
		t.Fatalf("work items = %d, want 1", len(result.WorkItems))
	}
	item := result.WorkItems[0]
	if item.ProviderWorkItemID != "github:codex-k8s/kodex:issue:7" || item.Title != "Issue title" || len(item.Labels) != 1 {
		t.Fatalf("work item = %+v, want normalized issue", item)
	}
	if result.NextCursorValue != "2026-05-07T11:59:00Z" {
		t.Fatalf("next cursor = %q, want provider watermark", result.NextCursorValue)
	}
	if len(result.LimitSnapshots) != 1 || *result.LimitSnapshots[0].Remaining != 4998 {
		t.Fatalf("limit snapshots = %+v, want core remaining 4998", result.LimitSnapshots)
	}
}

func TestReconcileRepositoryCursorUsesFilteredProviderWatermark(t *testing.T) {
	t.Parallel()

	accountID := uuid.New()
	observedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/codex-k8s/kodex/issues" {
			t.Fatalf("path = %s, want repository issues", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":100,"number":7,"html_url":"https://github.com/codex-k8s/kodex/issues/7","title":"Issue title","state":"open","updated_at":"2026-05-07T11:58:00Z"}]`))
	}))
	defer server.Close()

	token := secretresolver.NewSecretValue([]byte("token-value"))
	defer token.Clear()
	result, err := New(Config{BaseURL: server.URL, HTTPClient: server.Client()}).Reconcile(context.Background(), providerclient.ReconciliationRequest{
		Credential: providerclient.AccountCredential{ExternalAccountID: accountID, ProviderSlug: enum.ProviderSlugGitHub, Token: token},
		Cursor: entity.SyncCursor{
			ProviderSlug: enum.ProviderSlugGitHub,
			ScopeType:    enum.SyncCursorScopeRepository,
			ScopeRef:     "codex-k8s/kodex",
			ArtifactKind: enum.SyncArtifactPullRequest,
		},
		MaxItems:   1,
		ObservedAt: observedAt,
	})
	if err != nil {
		t.Fatalf("Reconcile(): %v", err)
	}
	if len(result.WorkItems) != 0 {
		t.Fatalf("work items = %d, want none for filtered issue page", len(result.WorkItems))
	}
	if result.NextCursorValue != "2026-05-07T11:58:00Z" {
		t.Fatalf("next cursor = %q, want filtered provider watermark", result.NextCursorValue)
	}
}

func TestReconcileCommentsCursorUsesLastReturnedComment(t *testing.T) {
	t.Parallel()

	accountID := uuid.New()
	observedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/codex-k8s/kodex/issues/7":
			_, _ = w.Write([]byte(`{"id":100,"number":7,"html_url":"https://github.com/codex-k8s/kodex/issues/7","title":"Issue title","state":"open","updated_at":"2026-05-07T11:00:00Z"}`))
		case "/repos/codex-k8s/kodex/issues/7/comments":
			_, _ = w.Write([]byte(`[{"id":200,"html_url":"https://github.com/codex-k8s/kodex/issues/7#issuecomment-200","body":"first","user":{"login":"reviewer"},"created_at":"2026-05-07T11:01:00Z","updated_at":"2026-05-07T11:01:00Z"},{"id":201,"html_url":"https://github.com/codex-k8s/kodex/issues/7#issuecomment-201","body":"second","user":{"login":"reviewer"},"created_at":"2026-05-07T11:02:00Z","updated_at":"2026-05-07T11:02:00Z"}]`))
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	token := secretresolver.NewSecretValue([]byte("token-value"))
	defer token.Clear()
	result, err := New(Config{BaseURL: server.URL, HTTPClient: server.Client()}).Reconcile(context.Background(), providerclient.ReconciliationRequest{
		Credential: providerclient.AccountCredential{ExternalAccountID: accountID, ProviderSlug: enum.ProviderSlugGitHub, Token: token},
		Cursor: entity.SyncCursor{
			ProviderSlug: enum.ProviderSlugGitHub,
			ScopeType:    enum.SyncCursorScopeWorkItem,
			ScopeRef:     "codex-k8s/kodex#issue:7",
			ArtifactKind: enum.SyncArtifactComment,
		},
		MaxItems:   1,
		ObservedAt: observedAt,
	})
	if err != nil {
		t.Fatalf("Reconcile(): %v", err)
	}
	if len(result.Comments) != 1 || result.Comments[0].Body != "first" {
		t.Fatalf("comments = %+v, want only first comment", result.Comments)
	}
	if result.NextCursorValue != "2026-05-07T11:01:00Z" {
		t.Fatalf("next cursor = %q, want last returned comment watermark", result.NextCursorValue)
	}
}

func TestReconcilePullRequestCommentsReadsReviewPageBeforeAdvancingCursor(t *testing.T) {
	t.Parallel()

	accountID := uuid.New()
	observedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/codex-k8s/kodex/issues/7":
			_, _ = w.Write([]byte(`{"id":100,"number":7,"html_url":"https://github.com/codex-k8s/kodex/pull/7","title":"PR title","state":"open","pull_request":{"url":"https://api.github.com/repos/codex-k8s/kodex/pulls/7"},"updated_at":"2026-05-07T11:00:00Z"}`))
		case "/repos/codex-k8s/kodex/pulls/7":
			_, _ = w.Write([]byte(`{"id":700,"number":7,"html_url":"https://github.com/codex-k8s/kodex/pull/7","title":"PR title","state":"open","updated_at":"2026-05-07T11:00:00Z"}`))
		case "/repos/codex-k8s/kodex/issues/7/comments":
			_, _ = w.Write([]byte(`[{"id":200,"html_url":"https://github.com/codex-k8s/kodex/pull/7#issuecomment-200","body":"later issue comment","user":{"login":"reviewer"},"created_at":"2026-05-07T11:05:00Z","updated_at":"2026-05-07T11:05:00Z"}]`))
		case "/repos/codex-k8s/kodex/pulls/7/reviews":
			if r.URL.Query().Get("page") == "2" {
				_, _ = w.Write([]byte(`[{"id":301,"html_url":"https://github.com/codex-k8s/kodex/pull/7#pullrequestreview-301","body":"first review after cursor","state":"COMMENTED","user":{"login":"reviewer"},"submitted_at":"2026-05-07T11:01:00Z"}]`))
				return
			}
			w.Header().Set("Link", `<`+server.URL+`/repos/codex-k8s/kodex/pulls/7/reviews?page=2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"id":300,"html_url":"https://github.com/codex-k8s/kodex/pull/7#pullrequestreview-300","body":"old review","state":"COMMENTED","user":{"login":"reviewer"},"submitted_at":"2026-05-07T10:00:00Z"}]`))
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	token := secretresolver.NewSecretValue([]byte("token-value"))
	defer token.Clear()
	result, err := New(Config{BaseURL: server.URL, HTTPClient: server.Client()}).Reconcile(context.Background(), providerclient.ReconciliationRequest{
		Credential: providerclient.AccountCredential{ExternalAccountID: accountID, ProviderSlug: enum.ProviderSlugGitHub, Token: token},
		Cursor: entity.SyncCursor{
			ProviderSlug: enum.ProviderSlugGitHub,
			ScopeType:    enum.SyncCursorScopeWorkItem,
			ScopeRef:     "codex-k8s/kodex#number:7",
			ArtifactKind: enum.SyncArtifactComment,
			CursorValue:  "2026-05-07T10:30:00Z",
		},
		MaxItems:   1,
		ObservedAt: observedAt,
	})
	if err != nil {
		t.Fatalf("Reconcile(): %v", err)
	}
	if len(result.Comments) != 1 || result.Comments[0].Body != "first review after cursor" {
		t.Fatalf("comments = %+v, want earliest review after cursor", result.Comments)
	}
	if result.NextCursorValue != "2026-05-07T11:01:00Z" {
		t.Fatalf("next cursor = %q, want review watermark", result.NextCursorValue)
	}
}

func TestReconcileSupportsGitHubWebURLAndRepositoryIDTargets(t *testing.T) {
	t.Parallel()

	accountID := uuid.New()
	observedAt := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/codex-k8s/kodex/pulls/703":
			_, _ = w.Write([]byte(`{"id":7030,"number":703,"html_url":"https://github.com/codex-k8s/kodex/pull/703","title":"PR title","state":"open","body":"PR body","updated_at":"2026-05-07T11:55:00Z"}`))
		case "/repositories/101":
			_, _ = w.Write([]byte(`{"id":101,"full_name":"codex-k8s/kodex"}`))
		case "/repos/codex-k8s/kodex":
			_, _ = w.Write([]byte(`{"id":101,"full_name":"codex-k8s/kodex"}`))
		default:
			t.Fatalf("unexpected path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	token := secretresolver.NewSecretValue([]byte("token-value"))
	defer token.Clear()
	adapter := New(Config{BaseURL: server.URL, HTTPClient: server.Client()})
	workItemResult, err := adapter.Reconcile(context.Background(), providerclient.ReconciliationRequest{
		Credential: providerclient.AccountCredential{ExternalAccountID: accountID, ProviderSlug: enum.ProviderSlugGitHub, Token: token},
		Cursor: entity.SyncCursor{
			ProviderSlug: enum.ProviderSlugGitHub,
			ScopeType:    enum.SyncCursorScopeWorkItem,
			ScopeRef:     "web_url:https://github.com/codex-k8s/kodex/pull/703",
			ArtifactKind: enum.SyncArtifactPullRequest,
		},
		MaxItems:   1,
		ObservedAt: observedAt,
	})
	if err != nil {
		t.Fatalf("Reconcile() work item: %v", err)
	}
	if len(workItemResult.WorkItems) != 1 || workItemResult.WorkItems[0].ProviderWorkItemID != "github:codex-k8s/kodex:pull_request:703" {
		t.Fatalf("work item result = %+v, want PR from web_url", workItemResult.WorkItems)
	}
	repositoryResult, err := adapter.Reconcile(context.Background(), providerclient.ReconciliationRequest{
		Credential: providerclient.AccountCredential{ExternalAccountID: accountID, ProviderSlug: enum.ProviderSlugGitHub, Token: token},
		Cursor: entity.SyncCursor{
			ProviderSlug: enum.ProviderSlugGitHub,
			ScopeType:    enum.SyncCursorScopeRepository,
			ScopeRef:     "provider_repository_id:101",
			ArtifactKind: enum.SyncArtifactRepository,
		},
		MaxItems:   1,
		ObservedAt: observedAt,
	})
	if err != nil {
		t.Fatalf("Reconcile() repository: %v", err)
	}
	if repositoryResult.NextCursorValue != observedAt.Format(time.RFC3339Nano) {
		t.Fatalf("repository cursor = %q, want observed cursor", repositoryResult.NextCursorValue)
	}
}

func TestReconcileClassifiesRateLimitAndTransientErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		statusCode int
		headers    map[string]string
		wantKind   providerclient.ErrorKind
	}{
		{
			name:       "rate limit",
			statusCode: http.StatusForbidden,
			headers: map[string]string{
				"X-RateLimit-Limit":     "5000",
				"X-RateLimit-Remaining": "0",
				"X-RateLimit-Reset":     strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10),
			},
			wantKind: providerclient.ErrorKindRateLimited,
		},
		{name: "transient", statusCode: http.StatusServiceUnavailable, wantKind: providerclient.ErrorKindTransient},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for key, value := range tc.headers {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(`{"message":"provider error"}`))
			}))
			defer server.Close()
			token := secretresolver.NewSecretValue([]byte("token-value"))
			defer token.Clear()
			_, err := New(Config{BaseURL: server.URL, HTTPClient: server.Client()}).Reconcile(context.Background(), providerclient.ReconciliationRequest{
				Credential: providerclient.AccountCredential{ExternalAccountID: uuid.New(), ProviderSlug: enum.ProviderSlugGitHub, Token: token},
				Cursor: entity.SyncCursor{
					ProviderSlug: enum.ProviderSlugGitHub,
					ScopeType:    enum.SyncCursorScopeRepository,
					ScopeRef:     "codex-k8s/kodex",
					ArtifactKind: enum.SyncArtifactIssue,
				},
				MaxItems:   1,
				ObservedAt: time.Now(),
			})
			var providerErr *providerclient.Error
			if !errors.As(err, &providerErr) || providerErr.Kind != tc.wantKind {
				t.Fatalf("Reconcile() err = %v, want provider kind %s", err, tc.wantKind)
			}
		})
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
