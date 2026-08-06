// Package team реализует provider-owned Mattermost Team catalog/create/readback.
package team

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	domainmattermost "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/mattermost"
	domainerrs "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/repository/team"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
	"github.com/google/uuid"
)

const (
	defaultPageSize                    = uint32(50)
	maximumPageSize                    = uint32(100)
	failureProviderOutcomeUnknown      = "PROVIDER_OUTCOME_UNKNOWN"
	failureProviderReadbackMismatch    = "PROVIDER_READBACK_MISMATCH"
	failureProviderReadbackUnavailable = "PROVIDER_READBACK_UNAVAILABLE"
)

var operationNamespace = uuid.MustParse("e6d4449d-1a8a-5d4f-9e47-7f5aac35cf05")

type Metrics interface {
	ObserveTeamOperation(string, string)
	ObserveExternalEffect(string, string)
}

type Config struct {
	InstanceID       string
	Lease            time.Duration
	SelectorTTL      time.Duration
	RecoveryInterval time.Duration
	RecoveryWindow   time.Duration
}

type Service struct {
	repository domainrepo.Repository
	provider   domainmattermost.TeamClient
	metrics    Metrics
	config     Config
}

func New(repository domainrepo.Repository, provider domainmattermost.TeamClient, metrics Metrics, config Config) (*Service, error) {
	if repository == nil || provider == nil || metrics == nil || config.InstanceID == "" ||
		config.Lease < time.Second || config.Lease > time.Minute ||
		config.SelectorTTL < time.Minute || config.SelectorTTL > 24*time.Hour ||
		config.RecoveryInterval < time.Second || config.RecoveryInterval > time.Minute ||
		config.RecoveryWindow <= config.RecoveryInterval || config.RecoveryWindow > time.Hour {
		return nil, errors.New("mattermost team service configuration is invalid")
	}
	return &Service{repository: repository, provider: provider, metrics: metrics, config: config}, nil
}

func (service *Service) Check(ctx context.Context) error {
	if err := service.repository.Check(ctx); err != nil {
		return err
	}
	return service.provider.CheckTeamLifecycle(ctx)
}

func (service *Service) List(ctx context.Context, principal entity.TeamPrincipal, pageSize uint32, cursor string) ([]entity.MattermostTeam, string, error) {
	if err := validatePrincipal(principal); err != nil {
		return nil, "", domainerrs.ErrUnauthorized
	}
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maximumPageSize {
		return nil, "", domainerrs.ErrConflict
	}
	offset, err := service.repository.ResolveCatalogOffset(ctx, principal, cursor, pageSize)
	if err != nil {
		return nil, "", mapRepositoryError(err)
	}
	teams, hasMore, err := service.provider.ListTeams(ctx, principal, offset, pageSize)
	if err != nil {
		return nil, "", mapProviderError(err)
	}
	teams, nextCursor, err := service.repository.SaveCatalogPage(ctx, principal, teams, offset, pageSize, hasMore, service.config.SelectorTTL)
	if err != nil {
		return nil, "", domainerrs.ErrUnavailable
	}
	service.metrics.ObserveTeamOperation("catalog", "success")
	return teams, nextCursor, nil
}

func (service *Service) ReadProvider(ctx context.Context, principal entity.TeamPrincipal, selector string) (entity.MattermostTeam, error) {
	if validatePrincipal(principal) != nil || !validUUID(selector) {
		return entity.MattermostTeam{}, domainerrs.ErrUnauthorized
	}
	providerTeamID, err := service.repository.ResolveSelector(ctx, principal, selector)
	if err != nil {
		return entity.MattermostTeam{}, mapRepositoryError(err)
	}
	team, err := service.provider.ReadTeam(ctx, principal, providerTeamID)
	if err != nil {
		return entity.MattermostTeam{}, mapProviderError(err)
	}
	team.Selector = selector
	team, err = service.repository.RefreshSelector(ctx, principal, team, service.config.SelectorTTL)
	if err != nil {
		return entity.MattermostTeam{}, domainerrs.ErrUnavailable
	}
	service.metrics.ObserveTeamOperation("readback", "success")
	return team, nil
}

func (service *Service) Create(ctx context.Context, principal entity.TeamPrincipal, displayName, slugIntent, idempotencyKey string) (entity.MattermostTeamOperation, error) {
	if err := validatePrincipal(principal); err != nil || !validUUID(idempotencyKey) {
		return entity.MattermostTeamOperation{}, domainerrs.ErrUnauthorized
	}
	intent, err := normalizeCreateIntent(displayName, slugIntent, idempotencyKey)
	if err != nil {
		return entity.MattermostTeamOperation{}, domainerrs.ErrConflict
	}
	operation := entity.MattermostTeamOperation{
		ID: uuid.NewSHA1(operationNamespace, []byte(strings.Join([]string{
			principal.OrganizationID, principal.ProjectID, principal.ActorID, idempotencyKey,
		}, "\x00"))).String(),
		Principal: principal,
		Intent:    intent,
		State:     enum.TeamOperationPending,
	}
	operation, disposition, err := service.repository.BeginCreate(ctx, operation, service.config.InstanceID, service.config.Lease)
	if err != nil {
		return entity.MattermostTeamOperation{}, mapRepositoryError(err)
	}
	if disposition != domainrepo.CreateClaimed {
		if disposition == domainrepo.CreateBusy {
			service.metrics.ObserveTeamOperation("create", "replay")
		}
		return operation, nil
	}
	return service.executeCreate(ctx, operation)
}

func (service *Service) ProcessRecovery(ctx context.Context) (bool, error) {
	operation, found, err := service.repository.ClaimRecovery(ctx, service.config.InstanceID, service.config.Lease)
	if err != nil {
		service.metrics.ObserveTeamOperation("recovery", "failure")
		return false, err
	}
	if !found {
		return false, err
	}
	if operation.State == enum.TeamOperationPending {
		result, executeErr := service.executeCreate(ctx, operation)
		outcome := "success"
		if executeErr != nil || result.State == enum.TeamOperationRepairRequired {
			outcome = "failure"
		} else if result.State == enum.TeamOperationAmbiguous {
			outcome = "retry"
		}
		service.metrics.ObserveTeamOperation("recovery", outcome)
		return true, executeErr
	}
	team, recoverErr := service.provider.RecoverCreatedTeam(ctx, operation.Principal, operation.Intent)
	if recoverErr == nil {
		result, acceptErr := service.accept(ctx, operation, team)
		outcome := "success"
		if acceptErr != nil || result.State == enum.TeamOperationRepairRequired {
			outcome = "failure"
		}
		service.metrics.ObserveTeamOperation("recovery", outcome)
		return true, acceptErr
	}
	if errors.Is(recoverErr, domainmattermost.ErrTeamNotFound) && time.Now().Before(operation.EffectStartedAt.Add(service.config.RecoveryWindow)) {
		err = service.repository.MarkAmbiguous(ctx, operation, failureProviderOutcomeUnknown, time.Now().Add(service.config.RecoveryInterval))
		outcome := "retry"
		if err != nil {
			outcome = "failure"
		}
		service.metrics.ObserveTeamOperation("recovery", outcome)
		return true, err
	}
	if errors.Is(recoverErr, domainmattermost.ErrTeamForbidden) && time.Now().Before(operation.EffectStartedAt.Add(service.config.RecoveryWindow)) {
		err = service.repository.MarkAmbiguous(ctx, operation, failureProviderReadbackUnavailable, time.Now().Add(service.config.RecoveryInterval))
		outcome := "retry"
		if err != nil {
			outcome = "failure"
		}
		service.metrics.ObserveTeamOperation("recovery", outcome)
		return true, err
	}
	code := "RECOVERY_TIMEOUT"
	if errors.Is(recoverErr, domainmattermost.ErrTeamConflict) {
		code = "PROVIDER_STATE_CONFLICT"
	}
	err = service.repository.MarkRepairRequired(ctx, operation, code)
	service.metrics.ObserveTeamOperation("recovery", "failure")
	return true, err
}

func (service *Service) executeCreate(ctx context.Context, operation entity.MattermostTeamOperation) (entity.MattermostTeamOperation, error) {
	started, err := service.repository.MarkEffectStarted(ctx, operation)
	if err != nil {
		return entity.MattermostTeamOperation{}, domainerrs.ErrUnavailable
	}
	team, providerErr := service.provider.CreateTeam(ctx, started.Principal, started.Intent)
	if providerErr == nil {
		return service.accept(ctx, started, team)
	}
	if errors.Is(providerErr, domainmattermost.ErrTeamConflict) || errors.Is(providerErr, domainmattermost.ErrTeamForbidden) {
		code := "PROVIDER_CONFLICT"
		if errors.Is(providerErr, domainmattermost.ErrTeamForbidden) {
			code = "PROVIDER_FORBIDDEN"
		}
		if err := service.repository.MarkRepairRequired(ctx, started, code); err != nil {
			return entity.MattermostTeamOperation{}, domainerrs.ErrUnavailable
		}
		started.State, started.FailureCode = enum.TeamOperationRepairRequired, code
		started.LeaseToken = ""
		service.metrics.ObserveTeamOperation("create", "failure")
		return started, nil
	}
	if err := service.repository.MarkAmbiguous(ctx, started, failureProviderOutcomeUnknown, time.Now().Add(service.config.RecoveryInterval)); err != nil {
		return entity.MattermostTeamOperation{}, domainerrs.ErrUnavailable
	}
	started.State, started.FailureCode = enum.TeamOperationAmbiguous, failureProviderOutcomeUnknown
	started.RetryNotBefore, started.LeaseToken = time.Now().Add(service.config.RecoveryInterval), ""
	service.metrics.ObserveExternalEffect("create_team", "ambiguous")
	service.metrics.ObserveTeamOperation("create", "retry")
	return started, nil
}

func (service *Service) accept(ctx context.Context, operation entity.MattermostTeamOperation, team entity.MattermostTeam) (entity.MattermostTeamOperation, error) {
	if team.ProviderTeamID == "" || team.ProviderSnapshotSHA256 == "" || team.Slug != operation.Intent.Slug ||
		team.DisplayName != operation.Intent.DisplayName || team.Status != enum.MattermostTeamActive ||
		(!operation.EffectStartedAt.IsZero() && team.CreatedAt.Before(operation.EffectStartedAt.Add(-2*time.Second))) {
		if err := service.repository.MarkRepairRequired(ctx, operation, failureProviderReadbackMismatch); err != nil {
			return entity.MattermostTeamOperation{}, domainerrs.ErrUnavailable
		}
		operation.State, operation.FailureCode = enum.TeamOperationRepairRequired, failureProviderReadbackMismatch
		return operation, nil
	}
	receipt := digestValues("mattermost-team-provider-receipt-v1", operation.ID, operation.Intent.RequestSHA256,
		team.ProviderTeamID, team.ProviderSnapshotSHA256)
	accepted, err := service.repository.AcceptProvider(ctx, operation, team, receipt, service.config.SelectorTTL)
	if err != nil {
		return entity.MattermostTeamOperation{}, domainerrs.ErrUnavailable
	}
	service.metrics.ObserveExternalEffect("create_team", "success")
	service.metrics.ObserveTeamOperation("create", "success")
	return accepted, nil
}

func normalizeCreateIntent(displayName, slugIntent, idempotencyKey string) (entity.MattermostTeamCreateIntent, error) {
	displayName = strings.Join(strings.Fields(displayName), " ")
	if count := utf8.RuneCountInString(displayName); count < 3 || count > 64 || strings.ContainsAny(displayName, "\x00\r\n") {
		return entity.MattermostTeamCreateIntent{}, errors.New("mattermost team display name is invalid")
	}
	source := slugIntent
	if strings.TrimSpace(source) == "" {
		source = displayName
	}
	var slug strings.Builder
	separator := false
	for _, symbol := range strings.ToLower(strings.TrimSpace(source)) {
		if symbol >= 'a' && symbol <= 'z' || symbol >= '0' && symbol <= '9' {
			if separator && slug.Len() > 0 {
				slug.WriteByte('-')
			}
			separator = false
			slug.WriteRune(symbol)
			continue
		}
		separator = separator || symbol == '-' || symbol == '_' || unicode.IsSpace(symbol) || unicode.IsPunct(symbol)
	}
	normalizedSlug := strings.Trim(slug.String(), "-")
	if len(normalizedSlug) > 48 {
		normalizedSlug = strings.TrimRight(normalizedSlug[:48], "-")
	}
	if len(normalizedSlug) < 3 {
		normalizedSlug = "workspace-" + digestValues(displayName)[:12]
	}
	requestSHA := digestValues("mattermost-team-create-v1", displayName, normalizedSlug)
	return entity.MattermostTeamCreateIntent{DisplayName: displayName, Slug: normalizedSlug,
		IdempotencyKey: idempotencyKey, RequestSHA256: requestSHA}, nil
}

func validatePrincipal(principal entity.TeamPrincipal) error {
	if !validUUID(principal.ActorID) || !validUUID(principal.OrganizationID) || !validUUID(principal.ProjectID) {
		return errors.New("mattermost team principal is invalid")
	}
	return nil
}

func validUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil
}

func mapProviderError(err error) error {
	switch {
	case errors.Is(err, domainmattermost.ErrTeamNotFound):
		return domainerrs.ErrNotFound
	case errors.Is(err, domainmattermost.ErrTeamConflict):
		return domainerrs.ErrConflict
	case errors.Is(err, domainmattermost.ErrTeamForbidden):
		return domainerrs.ErrUnauthorized
	default:
		return domainerrs.ErrUnavailable
	}
}

func mapRepositoryError(err error) error {
	switch {
	case errors.Is(err, domainrepo.ErrNotFound):
		return domainerrs.ErrNotFound
	case errors.Is(err, domainrepo.ErrIdempotencyConflict):
		return domainerrs.ErrConflict
	default:
		return domainerrs.ErrUnavailable
	}
}

func digestValues(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}
