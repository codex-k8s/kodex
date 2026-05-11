// Package github contains the GitHub provider adapter.
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	githubapi "github.com/google/go-github/v82/github"
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	providerclient "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/client"
)

const (
	defaultBaseURL    = "https://api.github.com"
	defaultUserAgent  = "kodex-provider-hub"
	limitClassCore    = "core"
	limitClassSearch  = "search"
	limitClassGraphQL = "graphql"
	defaultPageSize   = 50
)

var _ providerclient.Adapter = (*Adapter)(nil)

// Config contains GitHub adapter runtime settings.
type Config struct {
	BaseURL     string
	UserAgent   string
	HTTPClient  *http.Client
	IDGenerator interface {
		New() uuid.UUID
	}
}

// Adapter talks to GitHub API and returns provider-neutral runtime data.
type Adapter struct {
	baseURL    string
	userAgent  string
	httpClient *http.Client
	ids        interface {
		New() uuid.UUID
	}
}

// New creates a GitHub adapter.
func New(cfg Config) *Adapter {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	userAgent := strings.TrimSpace(cfg.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	ids := cfg.IDGenerator
	if ids == nil {
		ids = uuidGenerator{}
	}
	return &Adapter{baseURL: baseURL, userAgent: userAgent, httpClient: httpClient, ids: ids}
}

// ProviderSlug returns the provider handled by this adapter.
func (a *Adapter) ProviderSlug() enum.ProviderSlug {
	return enum.ProviderSlugGitHub
}

// ProbeAccount requests GitHub rate-limit state and maps it to provider-hub models.
func (a *Adapter) ProbeAccount(ctx context.Context, request providerclient.AccountProbeRequest) (providerclient.AccountProbeResult, error) {
	if request.Credential.ExternalAccountID == uuid.Nil || request.Credential.Token.Len() == 0 {
		return providerclient.AccountProbeResult{}, errs.ErrInvalidArgument
	}
	observedAt := request.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	rateLimit, err := a.fetchRateLimit(ctx, request.Credential.Token)
	if err != nil {
		return providerclient.AccountProbeResult{}, err
	}
	checkedAt := observedAt
	state := entity.ProviderAccountRuntimeState{
		Base: entity.Base{
			ID:        a.ids.New(),
			Version:   1,
			CreatedAt: observedAt,
			UpdatedAt: observedAt,
		},
		ExternalAccountID: request.Credential.ExternalAccountID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		Status:            enum.ProviderAccountRuntimeStatusActive,
		LastCheckedAt:     &checkedAt,
		LastSuccessAt:     &checkedAt,
	}
	snapshots := a.limitSnapshots(request.Credential.ExternalAccountID, observedAt, rateLimit)
	if hasExhaustedLimit(snapshots) {
		state.Status = enum.ProviderAccountRuntimeStatusLimited
	}
	return providerclient.AccountProbeResult{RuntimeState: state, LimitSnapshots: snapshots}, nil
}

// Reconcile reads GitHub state for one provider-hub sync cursor without writing provider data.
func (a *Adapter) Reconcile(ctx context.Context, request providerclient.ReconciliationRequest) (providerclient.ReconciliationResult, error) {
	if request.Credential.ExternalAccountID == uuid.Nil || request.Credential.Token.Len() == 0 || request.MaxItems <= 0 {
		return providerclient.ReconciliationResult{}, errs.ErrInvalidArgument
	}
	if request.Cursor.ProviderSlug != enum.ProviderSlugGitHub {
		return providerclient.ReconciliationResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
	client, err := a.githubClient(request.Credential.Token)
	if err != nil {
		return providerclient.ReconciliationResult{}, err
	}
	observedAt := request.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	switch request.Cursor.ScopeType {
	case enum.SyncCursorScopeWorkItem:
		return a.reconcileWorkItem(ctx, client, request, observedAt)
	case enum.SyncCursorScopeRepository:
		return a.reconcileRepository(ctx, client, request, observedAt)
	default:
		return providerclient.ReconciliationResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
}

func (a *Adapter) fetchRateLimit(ctx context.Context, token secretresolver.SecretValue) (*githubapi.RateLimits, error) {
	client, err := a.githubClient(token)
	if err != nil {
		return nil, err
	}
	rateLimits, _, err := client.RateLimit.Get(ctx)
	if err != nil {
		return nil, mapGitHubError(err)
	}
	return rateLimits, nil
}

func (a *Adapter) githubClient(token secretresolver.SecretValue) (*githubapi.Client, error) {
	if token.Len() == 0 {
		return nil, errs.ErrInvalidArgument
	}
	httpClient := *a.httpClient
	httpClient.Transport = secretTransport{base: a.httpClient.Transport, token: token}
	client := githubapi.NewClient(&httpClient)
	client.UserAgent = a.userAgent
	if a.baseURL == defaultBaseURL {
		return client, nil
	}
	baseURL, err := url.Parse(a.baseURL + "/")
	if err != nil {
		return nil, fmt.Errorf("parse github base url: %w", err)
	}
	client.BaseURL = baseURL
	return client, nil
}

type secretTransport struct {
	base  http.RoundTripper
	token secretresolver.SecretValue
}

func (t secretTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	token := t.token.Bytes()
	defer clear(token)
	cloned := request.Clone(request.Context())
	cloned.Header.Set("Authorization", "Bearer "+string(token))
	return base.RoundTrip(cloned)
}

func mapGitHubError(err error) error {
	var rateLimitErr *githubapi.RateLimitError
	if errors.As(err, &rateLimitErr) {
		return errs.ErrPreconditionFailed
	}
	var githubErr *githubapi.ErrorResponse
	if errors.As(err, &githubErr) && githubErr.Response != nil {
		switch githubErr.Response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return errs.ErrPreconditionFailed
		default:
			return fmt.Errorf("%w: github status %d", errs.ErrDependencyUnavailable, githubErr.Response.StatusCode)
		}
	}
	return fmt.Errorf("request github rate limit: %w", err)
}

func (a *Adapter) limitSnapshots(externalAccountID uuid.UUID, capturedAt time.Time, rateLimits *githubapi.RateLimits) []entity.ProviderLimitSnapshot {
	if rateLimits == nil {
		return nil
	}
	resources := []struct {
		class string
		limit *githubapi.Rate
	}{
		{class: limitClassCore, limit: rateLimits.Core},
		{class: limitClassSearch, limit: rateLimits.Search},
		{class: limitClassGraphQL, limit: rateLimits.GraphQL},
	}
	snapshots := make([]entity.ProviderLimitSnapshot, 0, len(resources))
	for _, resource := range resources {
		if resource.limit == nil {
			continue
		}
		limitValue := int64(resource.limit.Limit)
		remaining := int64(resource.limit.Remaining)
		resetAt := resource.limit.Reset.UTC()
		snapshots = append(snapshots, entity.ProviderLimitSnapshot{
			ID:                a.ids.New(),
			ExternalAccountID: externalAccountID,
			ProviderSlug:      enum.ProviderSlugGitHub,
			LimitClass:        resource.class,
			Remaining:         &remaining,
			LimitValue:        &limitValue,
			ResetAt:           &resetAt,
			CapturedAt:        capturedAt,
			Source:            enum.ProviderLimitSourceProviderHub,
		})
	}
	return snapshots
}

func hasExhaustedLimit(snapshots []entity.ProviderLimitSnapshot) bool {
	for _, snapshot := range snapshots {
		if snapshot.Remaining != nil && *snapshot.Remaining == 0 {
			return true
		}
	}
	return false
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}
