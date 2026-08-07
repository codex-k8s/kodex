// Package team реализует provider-owned Mattermost Team catalog/create/readback.
package team

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	controlplanecontract "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi"
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	domaincontrol "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/controlplane"
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

var mappingOperationNamespace = uuid.MustParse("bd248654-e293-5c21-92d2-d7b0722c8b9c")

const (
	mappingTargetKind = "workspace_mattermost_mapping"
	mappingEffect     = "workspace_mattermost_mapping"
)

type Metrics interface {
	ObserveTeamOperation(string, string)
	ObserveExternalEffect(string, string)
}

type MappingClient interface {
	ListWorkspaceMattermostMappings(context.Context, domaincontrol.ProviderCredential, string) ([]entity.WorkspaceMattermostMapping, error)
	GetWorkspaceMattermostMapping(context.Context, domaincontrol.ProviderCredential, string) (entity.WorkspaceMattermostMapping, error)
	ManageWorkspaceMattermostMapping(context.Context, domaincontrol.ManageWorkspaceMappingInput) (entity.WorkspaceMattermostMapping, error)
}

type ReceiptSigner interface {
	Sign(domaincontrol.ProviderEffectReceipt) (domaincontrol.ProviderCredential, error)
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
	mapping    MappingClient
	receipts   ReceiptSigner
	metrics    Metrics
	config     Config
}

func New(repository domainrepo.Repository, provider domainmattermost.TeamClient, mapping MappingClient,
	receipts ReceiptSigner, metrics Metrics, config Config,
) (*Service, error) {
	if repository == nil || provider == nil || mapping == nil || receipts == nil || metrics == nil || config.InstanceID == "" ||
		config.Lease < time.Second || config.Lease > time.Minute ||
		config.SelectorTTL < time.Minute || config.SelectorTTL > 24*time.Hour ||
		config.RecoveryInterval < time.Second || config.RecoveryInterval > time.Minute ||
		config.RecoveryWindow <= config.RecoveryInterval || config.RecoveryWindow > time.Hour {
		return nil, errors.New("mattermost team service configuration is invalid")
	}
	return &Service{
		repository: repository, provider: provider, mapping: mapping,
		receipts: receipts, metrics: metrics, config: config,
	}, nil
}

func (service *Service) Check(ctx context.Context) error {
	if err := service.repository.Check(ctx); err != nil {
		return err
	}
	if err := service.provider.CheckTeamLifecycle(ctx); err != nil {
		return err
	}
	bindings := service.provider.TeamReadinessBindings()
	if len(bindings) == 0 {
		return errors.New("Mattermost mapping readiness binding is missing")
	}
	for _, binding := range bindings {
		mapping, exists, err := service.currentMapping(ctx, binding.Principal)
		if err != nil {
			return errors.New("Workspace Mattermost mapping working path is not ready")
		}
		// Management path должен оставаться доступным для первого bind. Если
		// mapping уже существует, readiness проверяет fresh provider Team/member
		// именно из авторитетного owner state, а не из environment manifest.
		if !exists {
			continue
		}
		if mapping.State == "UNLINKED" {
			continue
		}
		team, err := service.provider.ReadTeam(ctx, binding.Principal, mapping.ProviderTeamID)
		if err != nil || mapping.State != "BOUND" || team.Status != enum.MattermostTeamActive {
			return errors.New("Workspace Mattermost mapping working path is not ready")
		}
	}
	return nil
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
	if _, _, err := service.currentMapping(ctx, principal); err != nil {
		return nil, "", mapMappingError(err)
	}
	service.metrics.ObserveTeamOperation("catalog", "success")
	return teams, nextCursor, nil
}

// CreateAndBind выполняет provider create только после server-resolved owner
// preflight, а затем связывает принятый Team специализированным owner RPC.
func (service *Service) CreateAndBind(ctx context.Context, principal entity.TeamPrincipal,
	displayName, slugIntent, idempotencyKey string,
) (entity.MattermostTeamOperation, entity.WorkspaceMattermostBinding, error) {
	current, exists, err := service.currentMapping(ctx, principal)
	if err != nil {
		return entity.MattermostTeamOperation{}, entity.WorkspaceMattermostBinding{}, mapMappingError(err)
	}
	if exists || current.ID != "" {
		return entity.MattermostTeamOperation{}, entity.WorkspaceMattermostBinding{}, domainerrs.ErrConflict
	}
	operation, err := service.Create(ctx, principal, displayName, slugIntent, idempotencyKey)
	if err != nil || operation.State != enum.TeamOperationProviderAccepted {
		return operation, entity.WorkspaceMattermostBinding{}, err
	}
	binding, err := service.beginMapping(ctx, principal, "bind", operation.Team, "", 0, 0,
		operation.Team.DisplayName, idempotencyKey)
	return operation, binding, err
}

func (service *Service) Link(ctx context.Context, principal entity.TeamPrincipal,
	selector, idempotencyKey string,
) (entity.WorkspaceMattermostBinding, error) {
	if validatePrincipal(principal) != nil || !validUUID(selector) || !validUUID(idempotencyKey) {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnauthorized
	}
	if _, exists, err := service.currentMapping(ctx, principal); err != nil {
		return entity.WorkspaceMattermostBinding{}, mapMappingError(err)
	} else if exists {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrConflict
	}
	team, err := service.ReadProvider(ctx, principal, selector)
	if err != nil {
		return entity.WorkspaceMattermostBinding{}, err
	}
	if team.Status != enum.MattermostTeamActive {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrConflict
	}
	return service.beginMapping(ctx, principal, "bind", team, "", 0, 0, team.DisplayName, idempotencyKey)
}

func (service *Service) GetBinding(ctx context.Context, principal entity.TeamPrincipal) (entity.WorkspaceMattermostBinding, error) {
	mapping, exists, err := service.currentMapping(ctx, principal)
	if err != nil {
		return entity.WorkspaceMattermostBinding{}, mapMappingError(err)
	}
	if !exists {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrNotFound
	}
	team, err := service.provider.ReadTeam(ctx, principal, mapping.ProviderTeamID)
	if err != nil {
		return entity.WorkspaceMattermostBinding{}, mapProviderError(err)
	}
	team, err = service.repository.RefreshSelector(ctx, principal, team, service.config.SelectorTTL)
	if err != nil {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
	}
	return entity.WorkspaceMattermostBinding{Mapping: mapping, Team: team}, nil
}

func (service *Service) Relink(ctx context.Context, principal entity.TeamPrincipal, selector string,
	expectedVersion, expectedGeneration uint64, idempotencyKey string,
) (entity.WorkspaceMattermostBinding, error) {
	if validatePrincipal(principal) != nil || !validUUID(selector) || !validUUID(idempotencyKey) ||
		expectedVersion == 0 || expectedGeneration == 0 {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnauthorized
	}
	current, exists, err := service.currentMapping(ctx, principal)
	if err != nil {
		return entity.WorkspaceMattermostBinding{}, mapMappingError(err)
	}
	if !exists || current.State != "BOUND" || current.Version != expectedVersion || current.Generation != expectedGeneration {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrConflict
	}
	team, err := service.ReadProvider(ctx, principal, selector)
	if err != nil {
		return entity.WorkspaceMattermostBinding{}, err
	}
	if team.Status != enum.MattermostTeamActive || team.ProviderTeamID == current.ProviderTeamID {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrConflict
	}
	return service.beginMapping(ctx, principal, "relink", team, current.ID,
		expectedVersion, expectedGeneration, "", idempotencyKey)
}

func (service *Service) Unlink(ctx context.Context, principal entity.TeamPrincipal,
	expectedVersion, expectedGeneration uint64, idempotencyKey string,
) (entity.WorkspaceMattermostBinding, error) {
	if validatePrincipal(principal) != nil || !validUUID(idempotencyKey) || expectedVersion == 0 || expectedGeneration == 0 {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnauthorized
	}
	current, exists, err := service.currentMapping(ctx, principal)
	if err != nil {
		return entity.WorkspaceMattermostBinding{}, mapMappingError(err)
	}
	if !exists || current.State != "BOUND" || current.Version != expectedVersion || current.Generation != expectedGeneration {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrConflict
	}
	team, err := service.provider.ReadTeam(ctx, principal, current.ProviderTeamID)
	if err != nil {
		return entity.WorkspaceMattermostBinding{}, mapProviderError(err)
	}
	team, err = service.repository.RefreshSelector(ctx, principal, team, service.config.SelectorTTL)
	if err != nil {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
	}
	return service.beginMapping(ctx, principal, "unlink", team, current.ID,
		expectedVersion, expectedGeneration, "", idempotencyKey)
}

// RequireBoundTeam — общий fail-closed gate для inbound и delivery.
func (service *Service) RequireBoundTeam(ctx context.Context, principal entity.TeamPrincipal,
	providerTeamID string,
) (entity.WorkspaceMattermostMapping, error) {
	if validatePrincipal(principal) != nil || providerTeamID == "" {
		return entity.WorkspaceMattermostMapping{}, domainerrs.ErrUnauthorized
	}
	team, err := service.provider.ReadTeam(ctx, principal, providerTeamID)
	if err != nil || team.Status != enum.MattermostTeamActive {
		return entity.WorkspaceMattermostMapping{}, domainerrs.ErrUnauthorized
	}
	mapping, exists, err := service.currentMapping(ctx, principal)
	if err != nil {
		return entity.WorkspaceMattermostMapping{}, mapMappingError(err)
	}
	if !exists || mapping.State != "BOUND" || mapping.ProviderTeamID != providerTeamID {
		return entity.WorkspaceMattermostMapping{}, domainerrs.ErrUnauthorized
	}
	return mapping, nil
}

func (service *Service) ProcessMappingRecovery(ctx context.Context) (bool, error) {
	operation, found, err := service.repository.ClaimMappingRecovery(ctx, service.config.InstanceID, service.config.Lease)
	if err != nil || !found {
		return false, err
	}
	_, err = service.executeMapping(ctx, operation)
	service.metrics.ObserveTeamOperation("mapping_recovery", outcome(err))
	return true, err
}

func (service *Service) beginMapping(ctx context.Context, principal entity.TeamPrincipal, action string,
	team entity.MattermostTeam, mappingID string, expectedVersion, expectedGeneration uint64,
	displayName, idempotencyKey string,
) (entity.WorkspaceMattermostBinding, error) {
	requestSHA := digestValues("workspace-mattermost-mapping-v1", principal.ActorID, principal.OrganizationID,
		principal.ProjectID, action, mappingID, fmt.Sprint(expectedVersion), fmt.Sprint(expectedGeneration),
		displayName, team.ProviderTeamID, team.ProviderSnapshotSHA256)
	operation := entity.WorkspaceMappingOperation{
		ID: uuid.NewSHA1(mappingOperationNamespace, []byte(strings.Join([]string{
			principal.OrganizationID,
			principal.ProjectID, principal.ActorID, action, idempotencyKey,
		}, "\x00"))).String(),
		Principal: principal, Action: action, IdempotencyKey: idempotencyKey, RequestSHA256: requestSHA,
		MappingID: mappingID, ExpectedVersion: expectedVersion, ExpectedGeneration: expectedGeneration,
		DisplayName: displayName, Team: team, State: enum.WorkspaceMappingOperationPending,
	}
	stored, disposition, err := service.repository.BeginMapping(ctx, operation, service.config.InstanceID, service.config.Lease)
	if err != nil {
		return entity.WorkspaceMattermostBinding{}, mapRepositoryError(err)
	}
	if disposition == domainrepo.MappingBusy {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrBusy
	}
	if disposition == domainrepo.MappingReplay {
		if stored.State == enum.WorkspaceMappingOperationRepairRequired {
			return entity.WorkspaceMattermostBinding{}, domainerrs.ErrConflict
		}
		if stored.State != enum.WorkspaceMappingOperationBound && stored.State != enum.WorkspaceMappingOperationUnlinked {
			return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
		}
		team, readErr := service.provider.ReadTeam(ctx, principal, stored.Team.ProviderTeamID)
		if readErr != nil {
			return entity.WorkspaceMattermostBinding{}, mapProviderError(readErr)
		}
		team, readErr = service.repository.RefreshSelector(ctx, principal, team, service.config.SelectorTTL)
		if readErr != nil {
			return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
		}
		return entity.WorkspaceMattermostBinding{Mapping: stored.Result, Team: team}, nil
	}
	return service.executeMapping(ctx, stored)
}

func (service *Service) executeMapping(ctx context.Context,
	operation entity.WorkspaceMappingOperation,
) (entity.WorkspaceMattermostBinding, error) {
	current, exists, readErr := service.currentMapping(ctx, operation.Principal)
	if readErr != nil {
		return entity.WorkspaceMattermostBinding{}, service.mappingAmbiguous(ctx, operation, readErr)
	}
	if mappingTerminal(operation, current, exists) {
		if err := service.repository.MarkMappingTerminal(ctx, operation, current); err != nil {
			return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
		}
		return entity.WorkspaceMattermostBinding{Mapping: current, Team: operation.Team}, nil
	}
	if !mappingPredecessor(operation, current, exists) {
		if err := service.repository.MarkMappingRepairRequired(ctx, operation, "OWNER_STATE_CONFLICT"); err != nil {
			return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
		}
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrConflict
	}
	if operation.State == enum.WorkspaceMappingOperationAmbiguous &&
		(operation.CreatedAt.IsZero() || !time.Now().Before(operation.CreatedAt.Add(service.config.RecoveryWindow))) {
		if err := service.repository.MarkMappingRepairRequired(ctx, operation, "RECOVERY_TIMEOUT"); err != nil {
			return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
		}
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrConflict
	}
	// Одноразовый receipt предыдущей неоднозначной попытки нельзя повторять.
	// Только доказанный owner predecessor разрешает выпустить новый JTI и
	// monotonic generation перед повтором той же semantic command.
	if operation.State == enum.WorkspaceMappingOperationAmbiguous {
		refreshed, refreshErr := service.repository.RefreshMappingReceipt(ctx, operation)
		if refreshErr != nil {
			return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
		}
		operation = refreshed
	}
	intentSHA, err := controlplanecontract.WorkspaceMattermostMappingIntentSHA256(
		controlplanecontract.WorkspaceMattermostMappingIntent{
			ActorID: operation.Principal.ActorID, OrganizationID: operation.Principal.OrganizationID,
			ProjectID: operation.Principal.ProjectID, WorkspaceID: operation.Principal.ProjectID,
			Action: operation.Action, MappingID: operation.MappingID, DisplayName: operation.DisplayName,
			ExpectedVersion: operation.ExpectedVersion, ExpectedGeneration: operation.ExpectedGeneration,
			ProviderTeamRef: operation.Team.ProviderTeamID, ProviderObjectRef: operation.Team.ProviderTeamID,
			EffectGeneration: operation.EffectGeneration, EffectSHA256: operation.Team.ProviderSnapshotSHA256,
		},
	)
	if err != nil {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrConflict
	}
	credential, err := service.receipts.Sign(domaincontrol.ProviderEffectReceipt{
		FullMethod: controlplanev1.ControlPlaneService_ManageWorkspaceMattermostMapping_FullMethodName,
		ActorID:    operation.Principal.ActorID, OrganizationID: operation.Principal.OrganizationID,
		ProjectID: operation.Principal.ProjectID, WorkspaceID: operation.Principal.ProjectID,
		ProviderTeamRef: operation.Team.ProviderTeamID, ProviderObjectRef: operation.Team.ProviderTeamID,
		Action: operation.Action, Effect: mappingEffect, EffectVersion: operation.EffectGeneration,
		EffectGeneration: operation.EffectGeneration, EffectSHA256: operation.Team.ProviderSnapshotSHA256,
		ReceiptID: operation.ReceiptID, ReceiptRevision: operation.EffectGeneration,
		MaskedStatus: strings.ToLower(string(operation.Team.Status)), Eligible: operation.Team.Status == enum.MattermostTeamActive,
		TargetKind: mappingTargetKind, TargetResourceID: operation.MappingID,
		TargetStableKey:     "workspace-" + strings.ReplaceAll(operation.Principal.ProjectID, "-", ""),
		CommandIntentSHA256: intentSHA,
	})
	if err != nil {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
	}
	managed, err := service.mapping.ManageWorkspaceMattermostMapping(ctx, domaincontrol.ManageWorkspaceMappingInput{
		IdempotencyKey: operation.IdempotencyKey, Action: operation.Action, MappingID: operation.MappingID,
		ExpectedVersion: operation.ExpectedVersion, ExpectedGeneration: operation.ExpectedGeneration,
		Name: operation.DisplayName, Credential: credential,
	})
	if err != nil {
		if errors.Is(err, domaincontrol.ErrConflict) || errors.Is(err, domaincontrol.ErrNotFound) {
			readback, found, retryErr := service.currentMapping(ctx, operation.Principal)
			if retryErr == nil && mappingTerminal(operation, readback, found) {
				if saveErr := service.repository.MarkMappingTerminal(ctx, operation, readback); saveErr != nil {
					return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
				}
				return entity.WorkspaceMattermostBinding{Mapping: readback, Team: operation.Team}, nil
			}
			if saveErr := service.repository.MarkMappingRepairRequired(ctx, operation, "OWNER_STATE_CONFLICT"); saveErr != nil {
				return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
			}
			return entity.WorkspaceMattermostBinding{}, domainerrs.ErrConflict
		}
		return entity.WorkspaceMattermostBinding{}, service.mappingAmbiguous(ctx, operation, err)
	}
	if !mappingTerminal(operation, managed, true) {
		if err := service.repository.MarkMappingRepairRequired(ctx, operation, "OWNER_READBACK_MISMATCH"); err != nil {
			return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
		}
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrConflict
	}
	if err := service.repository.MarkMappingTerminal(ctx, operation, managed); err != nil {
		return entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
	}
	service.metrics.ObserveExternalEffect("workspace_mapping_"+operation.Action, "success")
	return entity.WorkspaceMattermostBinding{Mapping: managed, Team: operation.Team}, nil
}

func (service *Service) mappingAmbiguous(ctx context.Context, operation entity.WorkspaceMappingOperation, _ error) error {
	if err := service.repository.MarkMappingAmbiguous(ctx, operation, "OWNER_OUTCOME_UNKNOWN",
		time.Now().Add(service.config.RecoveryInterval)); err != nil {
		return domainerrs.ErrUnavailable
	}
	service.metrics.ObserveExternalEffect("workspace_mapping_"+operation.Action, "ambiguous")
	return domainerrs.ErrUnavailable
}

func (service *Service) currentMapping(ctx context.Context,
	principal entity.TeamPrincipal,
) (entity.WorkspaceMattermostMapping, bool, error) {
	observation, err := service.provider.ReadOwner(ctx, principal)
	if err != nil {
		return entity.WorkspaceMattermostMapping{}, false, err
	}
	generation, err := service.repository.AdvanceProviderGeneration(ctx, principal)
	if err != nil {
		return entity.WorkspaceMattermostMapping{}, false, err
	}
	listCredential, err := service.readCredential(principal, observation,
		controlplanev1.ControlPlaneService_ListWorkspaceMattermostMappings_FullMethodName,
		"list", generation, "")
	if err != nil {
		return entity.WorkspaceMattermostMapping{}, false, err
	}
	items, err := service.mapping.ListWorkspaceMattermostMappings(ctx, listCredential, principal.ProjectID)
	if err != nil {
		return entity.WorkspaceMattermostMapping{}, false, err
	}
	if len(items) > 1 {
		return entity.WorkspaceMattermostMapping{}, false, domaincontrol.ErrConflict
	}
	if len(items) == 0 {
		return entity.WorkspaceMattermostMapping{}, false, nil
	}
	generation, err = service.repository.AdvanceProviderGeneration(ctx, principal)
	if err != nil {
		return entity.WorkspaceMattermostMapping{}, false, err
	}
	getCredential, err := service.readCredential(principal, observation,
		controlplanev1.ControlPlaneService_GetWorkspaceMattermostMapping_FullMethodName,
		"get", generation, items[0].ID)
	if err != nil {
		return entity.WorkspaceMattermostMapping{}, false, err
	}
	current, err := service.mapping.GetWorkspaceMattermostMapping(ctx, getCredential, items[0].ID)
	if err != nil {
		return entity.WorkspaceMattermostMapping{}, false, err
	}
	if !sameMapping(items[0], current) {
		return entity.WorkspaceMattermostMapping{}, false, domaincontrol.ErrConflict
	}
	return current, true, nil
}

func (service *Service) readCredential(principal entity.TeamPrincipal, observation entity.MattermostOwnerObservation,
	fullMethod, action string, generation uint64, mappingID string,
) (domaincontrol.ProviderCredential, error) {
	intent := digestValues("workspace-mattermost-mapping-read-v1", principal.ActorID, principal.OrganizationID,
		principal.ProjectID, fullMethod, action, mappingID, observation.ProviderObjectRef,
		observation.SnapshotSHA256, fmt.Sprint(generation))
	return service.receipts.Sign(domaincontrol.ProviderEffectReceipt{
		FullMethod: fullMethod, ActorID: principal.ActorID, OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, WorkspaceID: principal.ProjectID,
		ProviderObjectRef: observation.ProviderObjectRef, Action: action,
		Effect: "mattermost_owner_eligibility", EffectVersion: generation, EffectGeneration: generation,
		EffectSHA256: observation.SnapshotSHA256, ReceiptID: uuid.NewString(), ReceiptRevision: generation,
		MaskedStatus: "active", Eligible: true, TargetKind: mappingTargetKind,
		TargetResourceID: mappingID, TargetStableKey: "workspace-" + strings.ReplaceAll(principal.ProjectID, "-", ""),
		CommandIntentSHA256: intent,
	})
}

func mappingPredecessor(operation entity.WorkspaceMappingOperation,
	current entity.WorkspaceMattermostMapping, exists bool,
) bool {
	if operation.Action == "bind" {
		return !exists
	}
	return exists && current.ID == operation.MappingID && current.State == "BOUND" &&
		current.Version == operation.ExpectedVersion && current.Generation == operation.ExpectedGeneration &&
		(operation.Action != "unlink" || current.ProviderTeamID == operation.Team.ProviderTeamID)
}

func mappingTerminal(operation entity.WorkspaceMappingOperation,
	current entity.WorkspaceMattermostMapping, exists bool,
) bool {
	if !exists || current.ProviderEffectGeneration != operation.EffectGeneration ||
		current.ProviderEffectVersion != operation.EffectGeneration {
		return false
	}
	switch operation.Action {
	case "bind":
		return current.State == "BOUND" && current.Version == 1 && current.Generation == 1 &&
			current.ProviderTeamID == operation.Team.ProviderTeamID
	case "relink":
		return current.ID == operation.MappingID && current.State == "BOUND" &&
			current.Version == operation.ExpectedVersion+1 && current.Generation == operation.ExpectedGeneration+1 &&
			current.ProviderTeamID == operation.Team.ProviderTeamID
	case "unlink":
		return current.ID == operation.MappingID && current.State == "UNLINKED" &&
			current.Version == operation.ExpectedVersion+1 && current.Generation == operation.ExpectedGeneration+1 &&
			current.ProviderTeamID == operation.Team.ProviderTeamID
	default:
		return false
	}
}

func sameMapping(left, right entity.WorkspaceMattermostMapping) bool {
	return left.ID == right.ID && left.Version == right.Version && left.Generation == right.Generation &&
		left.State == right.State && left.ProviderTeamID == right.ProviderTeamID &&
		left.ProviderEffectVersion == right.ProviderEffectVersion &&
		left.ProviderEffectGeneration == right.ProviderEffectGeneration
}

func mapMappingError(err error) error {
	switch {
	case errors.Is(err, domaincontrol.ErrNotFound):
		return domainerrs.ErrNotFound
	case errors.Is(err, domaincontrol.ErrConflict):
		return domainerrs.ErrConflict
	default:
		return domainerrs.ErrUnavailable
	}
}

func outcome(err error) string {
	if err == nil {
		return "success"
	}
	return "failure"
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
	return entity.MattermostTeamCreateIntent{
		DisplayName: displayName, Slug: normalizedSlug,
		IdempotencyKey: idempotencyKey, RequestSHA256: requestSHA,
	}, nil
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
