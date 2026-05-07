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
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
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

type idQueue []uuid.UUID

func (q *idQueue) New() uuid.UUID {
	if len(*q) == 0 {
		panic("test id sequence is empty")
	}
	id := (*q)[0]
	*q = (*q)[1:]
	return id
}
