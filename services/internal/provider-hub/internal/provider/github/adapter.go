// Package github contains the GitHub provider adapter.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

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
	if request.Credential.ExternalAccountID == uuid.Nil || strings.TrimSpace(request.Credential.Token) == "" {
		return providerclient.AccountProbeResult{}, errs.ErrInvalidArgument
	}
	observedAt := request.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	rateLimit, err := a.fetchRateLimit(ctx, strings.TrimSpace(request.Credential.Token))
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

func (a *Adapter) fetchRateLimit(ctx context.Context, token string) (rateLimitResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/rate_limit", http.NoBody)
	if err != nil {
		return rateLimitResponse{}, fmt.Errorf("build github rate limit request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", a.userAgent)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return rateLimitResponse{}, fmt.Errorf("request github rate limit: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return rateLimitResponse{}, errs.ErrPreconditionFailed
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return rateLimitResponse{}, fmt.Errorf("%w: github rate limit status %d", errs.ErrDependencyUnavailable, resp.StatusCode)
	}
	var decoded rateLimitResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return rateLimitResponse{}, fmt.Errorf("decode github rate limit response: %w", err)
	}
	return decoded, nil
}

func (a *Adapter) limitSnapshots(externalAccountID uuid.UUID, capturedAt time.Time, response rateLimitResponse) []entity.ProviderLimitSnapshot {
	resources := []struct {
		class string
		limit rateLimitResource
	}{
		{class: limitClassCore, limit: response.Resources.Core},
		{class: limitClassSearch, limit: response.Resources.Search},
		{class: limitClassGraphQL, limit: response.Resources.GraphQL},
	}
	snapshots := make([]entity.ProviderLimitSnapshot, 0, len(resources))
	for _, resource := range resources {
		limitValue := int64(resource.limit.Limit)
		remaining := int64(resource.limit.Remaining)
		resetAt := time.Unix(resource.limit.Reset, 0).UTC()
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

type rateLimitResponse struct {
	Resources struct {
		Core    rateLimitResource `json:"core"`
		Search  rateLimitResource `json:"search"`
		GraphQL rateLimitResource `json:"graphql"`
	} `json:"resources"`
}

type rateLimitResource struct {
	Limit     int64 `json:"limit"`
	Remaining int64 `json:"remaining"`
	Reset     int64 `json:"reset"`
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}
