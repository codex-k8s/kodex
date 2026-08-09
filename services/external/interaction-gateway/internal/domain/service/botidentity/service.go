// Package botidentity реализует durable Agent Mattermost bot effect/readback.
package botidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	controlplanecontract "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi"
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	domaincontrol "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/controlplane"
	domaincredential "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/credential"
	domainmattermost "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/mattermost"
	domainerrs "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/repository/botidentity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/enum"
	"github.com/google/uuid"
)

const (
	defaultPageSize               = uint32(50)
	maximumPageSize               = uint32(100)
	ownerManageFullMethod         = "/controlplane.v1.ControlPlaneService/ManageAgentMattermostBotIdentity"
	ownerGetFullMethod            = "/controlplane.v1.ControlPlaneService/GetAgent"
	failureProviderOutcomeUnknown = "PROVIDER_OUTCOME_UNKNOWN"
	failureProviderMismatch       = "PROVIDER_READBACK_MISMATCH"
	providerName                  = "mattermost"
)

var (
	operationNamespace      = uuid.MustParse("da0b5fd3-c64a-59d5-ae3c-bcece6651c65")
	identityNamespace       = uuid.MustParse("a6d64b15-691e-50d2-a4c5-93ac1c224347")
	providerObjectNamespace = uuid.MustParse("05687f90-df38-5868-b67b-2b87fa3ee925")
	credentialNamespace     = uuid.MustParse("2710d1fd-8045-51f4-9a16-158c3c53a9d2")
)

type Metrics interface {
	ObserveBotIdentityOperation(string, string)
	ObserveExternalEffect(string, string)
	SetBotIdentityRepairBacklog(string, float64)
}

type OwnerClient interface {
	GetAgentMattermostBotIdentity(context.Context, domaincontrol.ProviderCredential, string) (domaincontrol.AgentMattermostBotOwner, error)
	ManageAgentMattermostBotIdentity(context.Context, domaincontrol.ManageAgentMattermostBotIdentityInput) (domaincontrol.AgentMattermostBotOwner, error)
}

type TeamSource interface {
	GetBinding(context.Context, entity.TeamPrincipal) (entity.WorkspaceMattermostBinding, error)
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

type AgentReadiness struct {
	Ready                   bool
	PostgresReady           bool
	MattermostReady         bool
	ControlPlaneReady       bool
	IdentityGenerationReady bool
	FailureCode             string
}

type Service struct {
	repository  domainrepo.Repository
	provider    domainmattermost.BotIdentityClient
	credentials domaincredential.Store
	owner       OwnerClient
	teams       TeamSource
	receipts    ReceiptSigner
	metrics     Metrics
	config      Config
}

func New(repository domainrepo.Repository, provider domainmattermost.BotIdentityClient,
	credentials domaincredential.Store, owner OwnerClient, teams TeamSource, receipts ReceiptSigner,
	metrics Metrics, config Config,
) (*Service, error) {
	if repository == nil || provider == nil || credentials == nil || owner == nil || teams == nil || receipts == nil ||
		metrics == nil || config.InstanceID == "" || config.Lease < time.Second || config.Lease > time.Minute ||
		config.SelectorTTL < time.Minute || config.SelectorTTL > 24*time.Hour ||
		config.RecoveryInterval < time.Second || config.RecoveryInterval > time.Minute ||
		config.RecoveryWindow <= config.RecoveryInterval || config.RecoveryWindow > time.Hour {
		return nil, errors.New("agent Mattermost bot identity service configuration is invalid")
	}
	return &Service{
		repository: repository, provider: provider, credentials: credentials,
		owner: owner, teams: teams, receipts: receipts, metrics: metrics, config: config,
	}, nil
}

func (service *Service) Check(ctx context.Context) error {
	if err := service.repository.Check(ctx); err != nil {
		return err
	}
	if err := service.provider.CheckBotIdentityLifecycle(ctx); err != nil {
		return err
	}
	if err := service.credentials.Check(ctx); err != nil {
		return err
	}
	return service.refreshRepairMetrics(ctx)
}

func (service *Service) List(ctx context.Context, principal entity.TeamPrincipal, pageSize uint32,
	cursor string,
) ([]entity.AgentMattermostBotIdentity, string, error) {
	if validatePrincipal(principal) != nil {
		return nil, "", domainerrs.ErrUnauthorized
	}
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maximumPageSize {
		return nil, "", domainerrs.ErrConflict
	}
	binding, err := service.teams.GetBinding(ctx, principal)
	if err != nil || binding.Mapping.State != "BOUND" || binding.Team.ProviderTeamID == "" {
		return nil, "", domainerrs.ErrUnavailable
	}
	offset, err := service.repository.ResolveCatalogOffset(ctx, principal, cursor, pageSize)
	if err != nil {
		return nil, "", mapRepositoryError(err)
	}
	identities, hasMore, err := service.provider.ListBotIdentities(ctx, principal,
		binding.Team.ProviderTeamID, offset, pageSize)
	if err != nil {
		return nil, "", mapProviderError(err)
	}
	identities, next, err := service.repository.SaveCatalogPage(ctx, principal, identities,
		offset, pageSize, hasMore, service.config.SelectorTTL)
	if err != nil {
		return nil, "", domainerrs.ErrUnavailable
	}
	service.metrics.ObserveBotIdentityOperation("catalog", "success")
	return identities, next, nil
}

func (service *Service) CreateAndBind(ctx context.Context, principal entity.TeamPrincipal, agentRef string,
	expectedAgentVersion uint64, usernameIntent, displayName, idempotencyKey string,
) (entity.AgentMattermostBotOperation, entity.AgentMattermostBotBinding, error) {
	if validatePrincipal(principal) != nil || uuid.Validate(agentRef) != nil || uuid.Validate(idempotencyKey) != nil {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnauthorized
	}
	if expectedAgentVersion == 0 || displayName == "" || len(displayName) > 64 ||
		len(usernameIntent) > 128 || strings.ContainsAny(displayName+usernameIntent, "\x00\r\n") {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	owner, teamBinding, err := service.resolveOwner(ctx, principal, agentRef)
	if err != nil {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, err
	}
	if owner.AgentVersion != expectedAgentVersion || owner.BotIdentityRef != "" {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, domainerrs.ErrVersionMismatch
	}
	operationID := stableOperationID(principal, agentRef, expectedAgentVersion, 0)
	username, displayName, err := normalizeBotIntent(usernameIntent, displayName, operationID)
	if err != nil {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	correlation := uuid.NewSHA1(operationNamespace, []byte(operationID+"\x00provider")).String()
	requestSHA := requestDigest(principal, enum.AgentBotActionCreateAndBind, agentRef,
		fmt.Sprint(expectedAgentVersion), username, displayName)
	operation := entity.AgentMattermostBotOperation{
		ID: operationID, Principal: principal, Action: enum.AgentBotActionCreateAndBind,
		IdempotencyKey: idempotencyKey, AgentRef: agentRef, ExpectedAgentVersion: expectedAgentVersion,
		RequestSHA256: requestSHA, State: enum.AgentBotOperationEffectPending,
		IdentityRef: uuid.NewSHA1(identityNamespace, []byte(operationID)).String(),
		Intent: entity.AgentMattermostBotCreateIntent{
			AgentRef: agentRef, ExpectedAgentVersion: expectedAgentVersion,
			Username: username, DisplayName: displayName, ProviderCorrelation: correlation, RequestSHA256: requestSHA,
		},
	}
	stored, disposition, err := service.repository.BeginOperation(ctx, operation, service.config.InstanceID,
		service.config.Lease, service.config.RecoveryWindow)
	if err != nil {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, mapRepositoryError(err)
	}
	if disposition == domainrepo.Replay || disposition == domainrepo.Busy {
		return service.replayOutcome(stored, disposition)
	}
	stored, err = service.repository.MarkEffectStarted(ctx, stored)
	if err != nil {
		return stored, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnavailable
	}
	identity, err := service.provider.CreateBotIdentity(ctx, principal, stored.Intent,
		teamBinding.Team.ProviderTeamID)
	if err != nil {
		if errors.Is(err, domainmattermost.ErrBotAmbiguousEffect) {
			return service.deferRecovery(ctx, stored, failureProviderOutcomeUnknown)
		}
		_ = service.repository.MarkRepairRequired(ctx, stored, failureProviderMismatch)
		return stored, entity.AgentMattermostBotBinding{}, mapProviderError(err)
	}
	return service.completeCreatedProvider(ctx, stored, owner, identity)
}

func (service *Service) Bind(ctx context.Context, principal entity.TeamPrincipal, agentRef string,
	expectedAgentVersion uint64, selector, idempotencyKey string,
) (entity.AgentMattermostBotOperation, entity.AgentMattermostBotBinding, error) {
	return service.bindExisting(ctx, principal, enum.AgentBotActionBind, agentRef,
		expectedAgentVersion, 0, selector, idempotencyKey)
}

func (service *Service) Rebind(ctx context.Context, principal entity.TeamPrincipal, agentRef string,
	expectedAgentVersion, predecessorGeneration uint64, selector, idempotencyKey string,
) (entity.AgentMattermostBotOperation, entity.AgentMattermostBotBinding, error) {
	return service.bindExisting(ctx, principal, enum.AgentBotActionRebind, agentRef,
		expectedAgentVersion, predecessorGeneration, selector, idempotencyKey)
}

func (service *Service) bindExisting(ctx context.Context, principal entity.TeamPrincipal, action, agentRef string,
	expectedAgentVersion, predecessorGeneration uint64, selector, idempotencyKey string,
) (entity.AgentMattermostBotOperation, entity.AgentMattermostBotBinding, error) {
	if validatePrincipal(principal) != nil || uuid.Validate(agentRef) != nil ||
		uuid.Validate(selector) != nil || uuid.Validate(idempotencyKey) != nil {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnauthorized
	}
	if expectedAgentVersion == 0 || (action == enum.AgentBotActionRebind && predecessorGeneration == 0) ||
		(action == enum.AgentBotActionBind && predecessorGeneration != 0) {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	owner, teamBinding, err := service.resolveOwner(ctx, principal, agentRef)
	if err != nil {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, err
	}
	if owner.AgentVersion != expectedAgentVersion || (action == enum.AgentBotActionBind && owner.BotIdentityRef != "") ||
		(action == enum.AgentBotActionRebind && (owner.BotIdentityRef == "" || owner.BotProviderGeneration != predecessorGeneration)) {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, domainerrs.ErrVersionMismatch
	}
	var predecessor entity.AgentMattermostBotBinding
	if action == enum.AgentBotActionRebind {
		predecessor, err = service.repository.GetBinding(ctx, principal, agentRef)
		if err != nil || predecessor.Identity.ProviderObjectRef != owner.BotIdentityRef ||
			predecessor.Identity.ProviderGeneration != predecessorGeneration {
			return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
		}
	}
	identity, err := service.repository.ResolveSelector(ctx, principal, selector)
	if err != nil {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, mapRepositoryError(err)
	}
	fresh, err := service.provider.ReadBotIdentity(ctx, principal, identity.ProviderUserID,
		teamBinding.Team.ProviderTeamID)
	if err != nil || fresh.Status != enum.AgentBotIdentityAvailable {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, mapProviderError(err)
	}
	if identity.AgentRef != "" || (action == enum.AgentBotActionRebind && identity.ProviderObjectRef == owner.BotIdentityRef) {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	operationID := stableOperationID(principal, agentRef, expectedAgentVersion, predecessorGeneration)
	fresh.IdentityRef, fresh.ProviderObjectRef, fresh.Selector = uuid.NewSHA1(identityNamespace, []byte(operationID)).String(),
		identity.ProviderObjectRef, selector
	requestSHA := requestDigest(principal, action, agentRef, fmt.Sprint(expectedAgentVersion),
		fmt.Sprint(predecessorGeneration), selector, fresh.ProviderSnapshotSHA256)
	operation := entity.AgentMattermostBotOperation{
		ID: operationID, Principal: principal,
		Action: action, IdempotencyKey: idempotencyKey, AgentRef: agentRef,
		ExpectedAgentVersion: expectedAgentVersion, PredecessorGeneration: predecessorGeneration,
		IdentityRef: fresh.IdentityRef, Selector: selector, RequestSHA256: requestSHA,
		State: enum.AgentBotOperationMembershipPending, Identity: fresh,
	}
	stored, disposition, err := service.repository.BeginOperation(ctx, operation, service.config.InstanceID,
		service.config.Lease, service.config.RecoveryWindow)
	if err != nil {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, mapRepositoryError(err)
	}
	if disposition == domainrepo.Replay || disposition == domainrepo.Busy {
		return service.replayOutcome(stored, disposition)
	}
	if err := service.repository.ReserveProviderObject(ctx, stored, fresh.ProviderObjectRef); err != nil {
		return stored, entity.AgentMattermostBotBinding{}, mapRepositoryError(err)
	}
	if action == enum.AgentBotActionRebind {
		if err := service.repository.CloseGeneration(ctx, stored, predecessorGeneration); err != nil {
			return stored, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
		}
		if err := service.revokeCredential(ctx, principal, predecessor.Identity); err != nil {
			return service.deferRecovery(ctx, stored, failureProviderOutcomeUnknown)
		}
	}
	fresh, err = service.ensureCredential(ctx, stored, fresh)
	if err != nil {
		return service.deferRecovery(ctx, stored, failureProviderOutcomeUnknown)
	}
	fresh.ProviderGeneration, fresh.AgentRef, fresh.AgentStableKey = 0, agentRef, owner.AgentStableKey
	stored, err = service.repository.AcceptProvider(ctx, stored, fresh)
	if err != nil {
		return stored, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnavailable
	}
	return service.applyOwner(ctx, stored, owner)
}

func (service *Service) Revoke(ctx context.Context, principal entity.TeamPrincipal, agentRef string,
	expectedAgentVersion, predecessorGeneration uint64, idempotencyKey string,
) (entity.AgentMattermostBotOperation, entity.AgentMattermostBotBinding, error) {
	if validatePrincipal(principal) != nil || uuid.Validate(agentRef) != nil || uuid.Validate(idempotencyKey) != nil {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnauthorized
	}
	if expectedAgentVersion == 0 || predecessorGeneration == 0 {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	owner, _, err := service.resolveOwner(ctx, principal, agentRef)
	if err != nil {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, err
	}
	binding, err := service.repository.GetBinding(ctx, principal, agentRef)
	if err != nil || owner.AgentVersion != expectedAgentVersion || owner.BotIdentityRef == "" ||
		owner.BotIdentityRef != binding.Identity.ProviderObjectRef || owner.BotProviderGeneration != predecessorGeneration ||
		binding.Identity.ProviderGeneration != predecessorGeneration {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, domainerrs.ErrVersionMismatch
	}
	requestSHA := requestDigest(principal, enum.AgentBotActionRevoke, agentRef,
		fmt.Sprint(expectedAgentVersion), fmt.Sprint(predecessorGeneration), binding.Identity.ProviderSnapshotSHA256)
	operationID := stableOperationID(principal, agentRef, expectedAgentVersion, predecessorGeneration)
	operation := entity.AgentMattermostBotOperation{
		ID:        operationID,
		Principal: principal, Action: enum.AgentBotActionRevoke, IdempotencyKey: idempotencyKey,
		AgentRef: agentRef, ExpectedAgentVersion: expectedAgentVersion,
		PredecessorGeneration: predecessorGeneration,
		IdentityRef:           uuid.NewSHA1(identityNamespace, []byte(operationID)).String(),
		RequestSHA256:         requestSHA, State: enum.AgentBotOperationEffectPending, Identity: binding.Identity,
	}
	stored, disposition, err := service.repository.BeginOperation(ctx, operation, service.config.InstanceID,
		service.config.Lease, service.config.RecoveryWindow)
	if err != nil {
		return entity.AgentMattermostBotOperation{}, entity.AgentMattermostBotBinding{}, mapRepositoryError(err)
	}
	if disposition == domainrepo.Replay || disposition == domainrepo.Busy {
		return service.replayOutcome(stored, disposition)
	}
	// Старая generation становится inadmissible до первого provider effect.
	if err := service.repository.CloseGeneration(ctx, stored, predecessorGeneration); err != nil {
		return stored, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	stored, err = service.repository.MarkEffectStarted(ctx, stored)
	if err != nil {
		return stored, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnavailable
	}
	providerTokenRevoked, err := service.provider.RevokeBotAccessToken(ctx, principal, binding.Identity)
	if err != nil {
		return service.deferRecovery(ctx, stored, failureProviderOutcomeUnknown)
	}
	if providerTokenRevoked {
		service.metrics.ObserveExternalEffect("revoke_provider_bot_token", "success")
	}
	revoked, providerIdentityRevoked, err := service.provider.RevokeBotIdentity(ctx, principal, binding.Identity)
	if err != nil {
		return service.deferRecovery(ctx, stored, failureProviderOutcomeUnknown)
	}
	if providerIdentityRevoked {
		service.metrics.ObserveExternalEffect("revoke_bot_identity", "success")
	}
	vaultCredentialRevoked, err := service.credentials.RevokeBotToken(ctx, binding.Identity.CredentialBindingID,
		binding.Identity.CredentialSecretVersion)
	if err != nil {
		return service.deferRecovery(ctx, stored, failureProviderOutcomeUnknown)
	}
	if vaultCredentialRevoked {
		service.metrics.ObserveExternalEffect("revoke_vault_bot_credential", "success")
	}
	revoked = mergeInternalIdentity(revoked, binding.Identity)
	revoked.IdentityRef, revoked.Status, revoked.ProviderGeneration = stored.IdentityRef, enum.AgentBotIdentityRevoked, 0
	stored, err = service.repository.AcceptProvider(ctx, stored, revoked)
	if err != nil {
		return stored, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnavailable
	}
	return service.applyOwner(ctx, stored, owner)
}

func (service *Service) Get(ctx context.Context, principal entity.TeamPrincipal,
	agentRef string,
) (entity.AgentMattermostBotBinding, error) {
	owner, _, err := service.resolveOwner(ctx, principal, agentRef)
	if err != nil {
		return entity.AgentMattermostBotBinding{}, err
	}
	binding, err := service.repository.GetBinding(ctx, principal, agentRef)
	if err != nil {
		return entity.AgentMattermostBotBinding{}, mapRepositoryError(err)
	}
	if owner.BotIdentityRef != binding.Identity.ProviderObjectRef || owner.BotProviderGeneration != binding.Identity.ProviderGeneration ||
		owner.BotMaskedStatus != string(binding.Identity.Status) || owner.AgentVersion != binding.AgentVersion {
		return entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	fresh, err := service.provider.ReadBotIdentity(ctx, principal, binding.Identity.ProviderUserID,
		binding.Identity.ProviderTeamID)
	if err != nil || fresh.ProviderSnapshotSHA256 != binding.Identity.ProviderSnapshotSHA256 {
		return entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	return binding, nil
}

func (service *Service) GetOperation(ctx context.Context, principal entity.TeamPrincipal,
	agentRef, action, idempotencyKey string,
) (entity.AgentMattermostBotOperation, error) {
	if validatePrincipal(principal) != nil || !validAction(action) || uuid.Validate(agentRef) != nil ||
		uuid.Validate(idempotencyKey) != nil {
		return entity.AgentMattermostBotOperation{}, domainerrs.ErrUnauthorized
	}
	operation, err := service.repository.GetOperation(ctx, principal, agentRef, action, idempotencyKey)
	if err != nil {
		return entity.AgentMattermostBotOperation{}, mapRepositoryError(err)
	}
	return operation, nil
}

func (service *Service) ReadProvider(ctx context.Context, principal entity.TeamPrincipal,
	agentRef, selector string,
) (entity.AgentMattermostBotIdentity, error) {
	owner, teamBinding, err := service.resolveOwner(ctx, principal, agentRef)
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, err
	}
	var identity entity.AgentMattermostBotIdentity
	if selector != "" {
		identity, err = service.repository.ResolveSelector(ctx, principal, selector)
	} else {
		binding, bindingErr := service.repository.GetBinding(ctx, principal, agentRef)
		identity, err = binding.Identity, bindingErr
		if err == nil && (owner.BotIdentityRef != identity.ProviderObjectRef ||
			owner.BotProviderGeneration != identity.ProviderGeneration) {
			return entity.AgentMattermostBotIdentity{}, domainerrs.ErrConflict
		}
	}
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, mapRepositoryError(err)
	}
	if identity.ProviderTeamID != teamBinding.Team.ProviderTeamID {
		return entity.AgentMattermostBotIdentity{}, domainerrs.ErrNotFound
	}
	fresh, err := service.provider.ReadBotIdentity(ctx, principal, identity.ProviderUserID,
		teamBinding.Team.ProviderTeamID)
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, mapProviderError(err)
	}
	fresh.IdentityRef, fresh.ProviderObjectRef, fresh.Selector, fresh.ProviderGeneration = identity.IdentityRef,
		identity.ProviderObjectRef, identity.Selector, identity.ProviderGeneration
	return fresh, nil
}

func (service *Service) CheckAgent(ctx context.Context, principal entity.TeamPrincipal,
	agentRef string,
) AgentReadiness {
	result := AgentReadiness{FailureCode: "POSTGRES_NOT_READY"}
	if validatePrincipal(principal) != nil || uuid.Validate(agentRef) != nil {
		result.FailureCode = "AUTHORITY_NOT_READY"
		return result
	}
	if err := service.repository.Check(ctx); err != nil {
		return result
	}
	result.PostgresReady = true
	if err := service.provider.CheckBotIdentityLifecycle(ctx); err != nil {
		result.FailureCode = "MATTERMOST_LIFECYCLE_NOT_READY"
		return result
	}
	if err := service.credentials.Check(ctx); err != nil {
		result.FailureCode = "CREDENTIAL_STORE_NOT_READY"
		return result
	}
	binding, err := service.repository.GetBinding(ctx, principal, agentRef)
	if err != nil || binding.Identity.Status != enum.AgentBotIdentityAvailable {
		result.FailureCode = "IDENTITY_BINDING_NOT_READY"
		return result
	}
	_, teamBinding, err := service.resolveOwner(ctx, principal, agentRef)
	if err != nil {
		result.FailureCode = "CONTROL_PLANE_READBACK_NOT_READY"
		return result
	}
	result.ControlPlaneReady = true
	if binding.Identity.ProviderTeamID != teamBinding.Team.ProviderTeamID ||
		binding.Identity.ProviderObjectRef == "" || binding.Identity.ProviderGeneration == 0 {
		result.FailureCode = "OWNER_PREDECESSOR_NOT_READY"
		return result
	}
	_, _, failure := service.proveRuntimeBotToken(ctx, principal, binding.Identity.AgentStableKey,
		binding.Identity.ProviderUserID, binding.Identity.ProviderGeneration)
	if failure != "" {
		if failure == "CONTROL_PLANE_READBACK_NOT_READY" {
			result.ControlPlaneReady = false
		}
		result.FailureCode = failure
		return result
	}
	result.IdentityGenerationReady, result.MattermostReady = true, true
	result.Ready, result.FailureCode = true, ""
	return result
}

func (service *Service) ProcessRecovery(ctx context.Context) (bool, error) {
	claim, err := service.repository.ClaimRecovery(ctx, service.config.InstanceID, service.config.Lease)
	if err != nil {
		return false, err
	}
	if metricErr := service.refreshRepairMetrics(ctx); metricErr != nil {
		return false, metricErr
	}
	if !claim.Found {
		return false, nil
	}
	operation := claim.Operation
	var outcomeErr error
	if operation.State == enum.AgentBotOperationProviderAccepted {
		_, _, outcomeErr = service.recoverOwnerOutcome(ctx, operation)
		service.metrics.ObserveBotIdentityOperation("recovery", outcome(outcomeErr))
		return true, outcomeErr
	}
	switch operation.Action {
	case enum.AgentBotActionCreateAndBind:
		owner, teamBinding, resolveErr := service.resolveOwner(ctx, operation.Principal, operation.AgentRef)
		if resolveErr != nil {
			outcomeErr = resolveErr
			break
		}
		identity, readErr := service.provider.RecoverCreatedBotIdentity(ctx, operation.Principal,
			operation.Intent, teamBinding.Team.ProviderTeamID)
		if readErr != nil {
			outcomeErr = mapProviderError(readErr)
			break
		}
		_, _, outcomeErr = service.completeCreatedProvider(ctx, operation, owner, identity)
	case enum.AgentBotActionBind, enum.AgentBotActionRebind:
		owner, teamBinding, resolveErr := service.resolveOwner(ctx, operation.Principal, operation.AgentRef)
		if resolveErr != nil {
			outcomeErr = resolveErr
			break
		}
		selected, selectorErr := service.repository.ResolveSelector(ctx, operation.Principal, operation.Selector)
		if selectorErr != nil {
			outcomeErr = mapRepositoryError(selectorErr)
			break
		}
		if selected.AgentRef != "" {
			outcomeErr = domainerrs.ErrConflict
			break
		}
		fresh, readErr := service.provider.ReadBotIdentity(ctx, operation.Principal,
			selected.ProviderUserID, teamBinding.Team.ProviderTeamID)
		if readErr != nil || fresh.Status != enum.AgentBotIdentityAvailable {
			outcomeErr = mapProviderError(readErr)
			break
		}
		if reserveErr := service.repository.ReserveProviderObject(ctx, operation,
			selected.ProviderObjectRef); reserveErr != nil {
			outcomeErr = mapRepositoryError(reserveErr)
			break
		}
		if operation.Action == enum.AgentBotActionRebind {
			predecessor, bindingErr := service.repository.GetBinding(ctx, operation.Principal, operation.AgentRef)
			if bindingErr != nil || predecessor.Identity.ProviderGeneration != operation.PredecessorGeneration {
				outcomeErr = domainerrs.ErrConflict
				break
			}
			if closeErr := service.repository.CloseGeneration(ctx, operation,
				operation.PredecessorGeneration); closeErr != nil {
				outcomeErr = domainerrs.ErrConflict
				break
			}
			if revokeErr := service.revokeCredential(ctx, operation.Principal, predecessor.Identity); revokeErr != nil {
				outcomeErr = domainerrs.ErrUnavailable
				break
			}
		}
		fresh.IdentityRef, fresh.ProviderObjectRef, fresh.Selector, fresh.AgentRef, fresh.AgentStableKey = operation.IdentityRef,
			selected.ProviderObjectRef, operation.Selector, operation.AgentRef, owner.AgentStableKey
		fresh, outcomeErr = service.ensureCredential(ctx, operation, fresh)
		if outcomeErr != nil {
			break
		}
		fresh.ProviderGeneration = 0
		operation, outcomeErr = service.repository.AcceptProvider(ctx, operation, fresh)
		if outcomeErr == nil {
			_, _, outcomeErr = service.applyOwner(ctx, operation, owner)
		}
	case enum.AgentBotActionRevoke:
		binding, bindingErr := service.repository.GetBinding(ctx, operation.Principal, operation.AgentRef)
		if bindingErr != nil {
			outcomeErr = mapRepositoryError(bindingErr)
			break
		}
		providerTokenRevoked, tokenErr := service.provider.RevokeBotAccessToken(ctx, operation.Principal, binding.Identity)
		if tokenErr != nil {
			outcomeErr = domainerrs.ErrUnavailable
			break
		}
		if providerTokenRevoked {
			service.metrics.ObserveExternalEffect("revoke_provider_bot_token", "success")
		}
		fresh, providerIdentityRevoked, revokeErr := service.provider.RevokeBotIdentity(ctx,
			operation.Principal, binding.Identity)
		if revokeErr != nil || fresh.Status != enum.AgentBotIdentityRevoked {
			outcomeErr = domainerrs.ErrUnavailable
			break
		}
		if providerIdentityRevoked {
			service.metrics.ObserveExternalEffect("revoke_bot_identity", "success")
		}
		credentialRevoked, credentialErr := service.credentials.RevokeBotToken(ctx,
			binding.Identity.CredentialBindingID, binding.Identity.CredentialSecretVersion)
		if credentialErr != nil {
			outcomeErr = domainerrs.ErrUnavailable
			break
		}
		if credentialRevoked {
			service.metrics.ObserveExternalEffect("revoke_vault_bot_credential", "success")
		}
		owner, _, resolveErr := service.resolveOwner(ctx, operation.Principal, operation.AgentRef)
		if resolveErr != nil {
			outcomeErr = resolveErr
			break
		}
		fresh = mergeInternalIdentity(fresh, binding.Identity)
		fresh.ProviderGeneration = 0
		operation, outcomeErr = service.repository.AcceptProvider(ctx, operation, fresh)
		if outcomeErr == nil {
			_, _, outcomeErr = service.applyOwner(ctx, operation, owner)
		}
	default:
		owner, _, resolveErr := service.resolveOwner(ctx, operation.Principal, operation.AgentRef)
		if resolveErr != nil {
			outcomeErr = resolveErr
			break
		}
		_, _, outcomeErr = service.applyOwner(ctx, operation, owner)
	}
	service.metrics.ObserveBotIdentityOperation("recovery", outcome(outcomeErr))
	_ = service.refreshRepairMetrics(ctx)
	return true, outcomeErr
}

func (service *Service) refreshRepairMetrics(ctx context.Context) error {
	backlog, err := service.repository.RepairBacklog(ctx)
	if err != nil {
		return err
	}
	service.metrics.SetBotIdentityRepairBacklog("recovery_timeout", float64(backlog.RecoveryTimeout))
	service.metrics.SetBotIdentityRepairBacklog("other", float64(backlog.Other))
	return nil
}

func (service *Service) RequireCurrentGeneration(ctx context.Context, principal entity.TeamPrincipal,
	agentStableKey, providerUserID string, generation uint64,
) (entity.AgentMattermostBotIdentity, error) {
	identity, err := service.repository.AdmitRuntimeIdentity(ctx, principal, agentStableKey, providerUserID, generation)
	if err != nil || identity.Status != enum.AgentBotIdentityAvailable {
		return entity.AgentMattermostBotIdentity{}, domainerrs.ErrUnauthorized
	}
	return identity, nil
}

func (service *Service) ResolveCurrentRuntimeIdentity(ctx context.Context, principal entity.TeamPrincipal,
	agentStableKey, providerUserID string,
) (entity.AgentMattermostBotIdentity, error) {
	identity, err := service.repository.ResolveRuntimeIdentity(ctx, principal, agentStableKey, providerUserID)
	if err != nil || identity.Status != enum.AgentBotIdentityAvailable {
		return entity.AgentMattermostBotIdentity{}, domainerrs.ErrUnauthorized
	}
	fresh, err := service.provider.ReadBotIdentity(ctx, principal, identity.ProviderUserID, identity.ProviderTeamID)
	if err != nil || fresh.Status != enum.AgentBotIdentityAvailable ||
		fresh.ProviderSnapshotSHA256 != identity.ProviderSnapshotSHA256 {
		return entity.AgentMattermostBotIdentity{}, domainerrs.ErrUnauthorized
	}
	return identity, nil
}

// ReadCurrentRuntimeBotToken выдаёт credential только доверенному Mattermost
// adapter после PostgreSQL admission и fresh provider readback точной generation.
func (service *Service) ReadCurrentRuntimeBotToken(ctx context.Context, principal entity.TeamPrincipal,
	agentStableKey, providerUserID string, generation uint64,
) (entity.AgentMattermostBotIdentity, string, error) {
	identity, token, failure := service.proveRuntimeBotToken(ctx, principal, agentStableKey, providerUserID, generation)
	if failure != "" {
		return entity.AgentMattermostBotIdentity{}, "", domainerrs.ErrUnauthorized
	}
	return identity, token, nil
}

func (service *Service) proveRuntimeBotToken(ctx context.Context, principal entity.TeamPrincipal,
	agentStableKey, providerUserID string, generation uint64,
) (entity.AgentMattermostBotIdentity, string, string) {
	var identity entity.AgentMattermostBotIdentity
	var err error
	if generation == 0 {
		identity, err = service.repository.ResolveRuntimeIdentity(ctx, principal, agentStableKey, providerUserID)
	} else {
		identity, err = service.repository.AdmitRuntimeIdentity(ctx, principal, agentStableKey, providerUserID, generation)
	}
	if err != nil || identity.Status != enum.AgentBotIdentityAvailable ||
		identity.CredentialBindingID == "" || identity.CredentialSecretVersion == 0 ||
		identity.CredentialSHA256 == "" {
		return entity.AgentMattermostBotIdentity{}, "", "IDENTITY_GENERATION_NOT_READY"
	}
	fresh, err := service.provider.ReadBotIdentity(ctx, principal, identity.ProviderUserID, identity.ProviderTeamID)
	if err != nil || fresh.Status != enum.AgentBotIdentityAvailable ||
		fresh.ProviderSnapshotSHA256 != identity.ProviderSnapshotSHA256 {
		return entity.AgentMattermostBotIdentity{}, "", "MATTERMOST_IDENTITY_NOT_READY"
	}
	owner, teamBinding, err := service.resolveOwner(ctx, principal, identity.AgentRef)
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, "", "CONTROL_PLANE_READBACK_NOT_READY"
	}
	if owner.BotIdentityRef != identity.ProviderObjectRef ||
		owner.BotProviderGeneration != identity.ProviderGeneration ||
		owner.BotMaskedStatus != string(identity.Status) ||
		teamBinding.Mapping.ID == "" || identity.ProviderTeamID != teamBinding.Team.ProviderTeamID {
		return entity.AgentMattermostBotIdentity{}, "", "OWNER_PREDECESSOR_NOT_READY"
	}
	token, err := service.credentials.ReadBotToken(ctx, identity.CredentialBindingID,
		identity.CredentialSecretVersion, identity.CredentialSHA256)
	if err != nil || token == "" {
		return entity.AgentMattermostBotIdentity{}, "", "CREDENTIAL_NOT_READY"
	}
	if err := service.provider.VerifyRuntimeBotCredential(ctx, principal, identity, token); err != nil {
		return entity.AgentMattermostBotIdentity{}, "", "MATTERMOST_RUNTIME_NOT_READY"
	}
	return identity, token, ""
}

func (service *Service) completeCreatedProvider(ctx context.Context, operation entity.AgentMattermostBotOperation,
	owner domaincontrol.AgentMattermostBotOwner, identity entity.AgentMattermostBotIdentity,
) (entity.AgentMattermostBotOperation, entity.AgentMattermostBotBinding, error) {
	identity.IdentityRef, identity.ProviderObjectRef, identity.AgentRef, identity.AgentStableKey = operation.IdentityRef,
		stableProviderObjectRef(operation.Principal, identity.ProviderUserID), operation.AgentRef, owner.AgentStableKey
	operation, err := service.repository.MarkMembershipPending(ctx, operation, identity)
	if err != nil {
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnavailable
	}
	identity, err = service.provider.EnsureBotTeamMembership(ctx, operation.Principal, identity)
	if err != nil {
		return service.deferRecovery(ctx, operation, failureProviderOutcomeUnknown)
	}
	identity.IdentityRef, identity.ProviderObjectRef, identity.AgentRef, identity.AgentStableKey = operation.IdentityRef,
		stableProviderObjectRef(operation.Principal, identity.ProviderUserID), operation.AgentRef, owner.AgentStableKey
	identity, err = service.ensureCredential(ctx, operation, identity)
	if err != nil {
		return service.deferRecovery(ctx, operation, failureProviderOutcomeUnknown)
	}
	identity.ProviderGeneration = 0
	operation, err = service.repository.AcceptProvider(ctx, operation, identity)
	if err != nil {
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnavailable
	}
	service.metrics.ObserveExternalEffect("create_bot_identity", "success")
	return service.applyOwner(ctx, operation, owner)
}

func (service *Service) ensureCredential(ctx context.Context, operation entity.AgentMattermostBotOperation,
	identity entity.AgentMattermostBotIdentity,
) (entity.AgentMattermostBotIdentity, error) {
	bindingID := uuid.NewSHA1(credentialNamespace, []byte(operation.ID+"\x00"+identity.ProviderUserID)).String()
	if recovered, err := service.credentials.RecoverBotToken(ctx, bindingID); err == nil {
		correlation := operation.Intent.ProviderCorrelation
		if correlation == "" {
			correlation = operation.ID
		}
		tokenID, active, resolveErr := service.provider.ResolveBotAccessToken(ctx, operation.Principal, identity, correlation)
		if resolveErr != nil || tokenID == "" || !active {
			return entity.AgentMattermostBotIdentity{}, errors.New("recovered bot credential has no active provider token")
		}
		identity.ProviderTokenID = tokenID
		identity.CredentialBindingID, identity.CredentialSecretRef = bindingID, recovered.SecretRef
		identity.CredentialSecretVersion, identity.CredentialSHA256 = recovered.Version, recovered.ContentSHA256
		return identity, nil
	}
	correlation := operation.Intent.ProviderCorrelation
	if correlation == "" {
		correlation = operation.ID
	}
	if _, found, err := service.provider.RecoverBotAccessToken(ctx, operation.Principal, identity, correlation); err != nil {
		return entity.AgentMattermostBotIdentity{}, err
	} else if found {
		service.metrics.ObserveExternalEffect("revoke_orphan_bot_token", "success")
	}
	tokenID, token, err := service.provider.CreateBotAccessToken(ctx, operation.Principal, identity, correlation)
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, err
	}
	materialized, err := service.credentials.MaterializeBotToken(ctx, bindingID, token)
	token = ""
	if err != nil {
		return entity.AgentMattermostBotIdentity{}, err
	}
	identity.ProviderTokenID = tokenID
	identity.CredentialBindingID, identity.CredentialSecretRef = bindingID, materialized.SecretRef
	identity.CredentialSecretVersion, identity.CredentialSHA256 = materialized.Version, materialized.ContentSHA256
	service.metrics.ObserveExternalEffect("create_bot_token", "success")
	return identity, nil
}

func (service *Service) recoverOwnerOutcome(ctx context.Context,
	operation entity.AgentMattermostBotOperation,
) (entity.AgentMattermostBotOperation, entity.AgentMattermostBotBinding, error) {
	owner, _, err := service.resolveOwner(ctx, operation.Principal, operation.AgentRef)
	if err != nil {
		return operation, entity.AgentMattermostBotBinding{}, err
	}
	if ownerMatchesTerminal(operation, owner) {
		return service.finishOwnerOutcome(ctx, operation, owner)
	}
	if !ownerMatchesPredecessor(operation, owner) {
		_ = service.repository.MarkRepairRequired(ctx, operation, "OWNER_PREDECESSOR_MISMATCH")
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	operation, err = service.refreshProviderCheckpoint(ctx, operation)
	if err != nil {
		return service.deferRecovery(ctx, operation, failureProviderOutcomeUnknown)
	}
	return service.applyOwner(ctx, operation, owner)
}

func (service *Service) refreshProviderCheckpoint(ctx context.Context,
	operation entity.AgentMattermostBotOperation,
) (entity.AgentMattermostBotOperation, error) {
	stored := operation.Identity
	fresh, err := service.provider.ReadBotIdentity(ctx, operation.Principal,
		stored.ProviderUserID, stored.ProviderTeamID)
	if err != nil || fresh.ProviderUserID != stored.ProviderUserID || fresh.ProviderTeamID != stored.ProviderTeamID ||
		fresh.Status != stored.Status || fresh.ProviderVersion != stored.ProviderVersion ||
		fresh.ProviderSnapshotSHA256 != stored.ProviderSnapshotSHA256 {
		return operation, errors.New("provider checkpoint readback mismatch")
	}
	if stored.Status == enum.AgentBotIdentityAvailable {
		_, readErr := service.credentials.ReadBotToken(ctx, stored.CredentialBindingID,
			stored.CredentialSecretVersion, stored.CredentialSHA256)
		if readErr != nil {
			return operation, errors.New("bot credential checkpoint readback mismatch")
		}
	} else if stored.Status == enum.AgentBotIdentityRevoked {
		if err := service.credentials.CheckBotTokenRevoked(ctx, stored.CredentialBindingID,
			stored.CredentialSecretVersion); err != nil {
			return operation, errors.New("bot credential revoke readback mismatch")
		}
	}
	fresh = mergeInternalIdentity(fresh, stored)
	fresh.ProviderGeneration = stored.ProviderGeneration
	operation.Identity = fresh
	return operation, nil
}

func (service *Service) revokeCredential(ctx context.Context, principal entity.TeamPrincipal,
	identity entity.AgentMattermostBotIdentity,
) error {
	if identity.ProviderTokenID == "" || identity.CredentialBindingID == "" ||
		identity.CredentialSecretVersion == 0 {
		return errors.New("agent bot predecessor credential is incomplete")
	}
	providerRevoked, err := service.provider.RevokeBotAccessToken(ctx, principal, identity)
	if err != nil {
		return err
	}
	if providerRevoked {
		service.metrics.ObserveExternalEffect("revoke_provider_bot_token", "success")
	}
	credentialRevoked, err := service.credentials.RevokeBotToken(ctx, identity.CredentialBindingID,
		identity.CredentialSecretVersion)
	if err != nil {
		return err
	}
	if credentialRevoked {
		service.metrics.ObserveExternalEffect("revoke_vault_bot_credential", "success")
	}
	return nil
}

func (service *Service) applyOwner(ctx context.Context, operation entity.AgentMattermostBotOperation,
	owner domaincontrol.AgentMattermostBotOwner,
) (entity.AgentMattermostBotOperation, entity.AgentMattermostBotBinding, error) {
	action := operation.Action
	if action == enum.AgentBotActionCreateAndBind {
		action = enum.AgentBotActionBind
	}
	if err := service.requireProviderOwnerCheckpoint(ctx, operation); err != nil {
		_ = service.repository.MarkRepairRequired(ctx, operation, "PROVIDER_OWNER_PREDECESSOR_MISMATCH")
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	freshOwner, teamBinding, resolveErr := service.resolveOwner(ctx, operation.Principal, operation.AgentRef)
	if resolveErr != nil || !sameOwnerPredecessor(owner, freshOwner) ||
		teamBinding.Mapping.ID == "" || teamBinding.Mapping.State != "BOUND" ||
		teamBinding.Team.ProviderTeamID != operation.Identity.ProviderTeamID {
		_ = service.repository.MarkRepairRequired(ctx, operation, "OWNER_PREDECESSOR_MISMATCH")
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	owner = freshOwner
	if !ownerMatchesPredecessor(operation, owner) {
		_ = service.repository.MarkRepairRequired(ctx, operation, "OWNER_PREDECESSOR_MISMATCH")
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	actionValue := controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_BIND
	if action == enum.AgentBotActionRebind {
		actionValue = controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REBIND
	} else if action == enum.AgentBotActionRevoke {
		actionValue = controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REVOKE
	}
	authority := controlplanecontract.VerifiedCommandAuthority{
		ActorID:        operation.Principal.ActorID,
		OrganizationID: operation.Principal.OrganizationID, ProjectID: operation.Principal.ProjectID,
		WorkloadID: "interaction-gateway", FullMethod: ownerManageFullMethod,
	}
	intentSHA, err := controlplanecontract.AgentMattermostBotIdentityIntentSHA256(authority,
		&controlplanev1.ManageAgentMattermostBotIdentityRequest{
			Action:  actionValue,
			AgentId: operation.AgentRef, ExpectedVersion: operation.ExpectedAgentVersion,
		}, owner.AgentStableKey)
	if err != nil {
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnavailable
	}
	receiptID := uuid.NewString()
	mappingProofRef, err := agentBotMappingProofRef(teamBinding.Mapping)
	if err != nil {
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnavailable
	}
	receipt := domaincontrol.ProviderEffectReceipt{
		FullMethod: ownerManageFullMethod, ActorID: operation.Principal.ActorID,
		OrganizationID: operation.Principal.OrganizationID, ProjectID: operation.Principal.ProjectID,
		WorkspaceID: operation.Principal.ProjectID, ProviderTeamRef: mappingProofRef,
		ProviderObjectRef: operation.Identity.ProviderObjectRef, ProviderUsername: operation.Identity.Username,
		Action: action, Effect: "agent_bot_identity", EffectVersion: operation.Identity.ProviderVersion,
		EffectGeneration: operation.Identity.ProviderGeneration, EffectSHA256: operation.Identity.ProviderSnapshotSHA256,
		ReceiptID: receiptID, ReceiptRevision: operation.Identity.ProviderGeneration,
		Provider: providerName, MaskedLabel: operation.Identity.DisplayName,
		Capabilities: []string{"mattermost.post", "mattermost.readback"},
		MaskedStatus: string(operation.Identity.Status), Eligible: operation.Identity.Status == enum.AgentBotIdentityAvailable,
		TargetKind: "agent_bot_identity", TargetResourceID: operation.AgentRef,
		TargetStableKey: owner.AgentStableKey, CommandIntentSHA256: intentSHA,
	}
	credential, err := service.receipts.Sign(receipt)
	if err != nil {
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnavailable
	}
	receiptSHA, err := internalrpcauth.CanonicalJSONSHA256(credential.Receipt)
	if err != nil {
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnavailable
	}
	managed, err := service.owner.ManageAgentMattermostBotIdentity(ctx,
		domaincontrol.ManageAgentMattermostBotIdentityInput{
			IdempotencyKey: operation.IdempotencyKey,
			Action:         action, AgentRef: operation.AgentRef, ExpectedVersion: operation.ExpectedAgentVersion,
			Credential: credential,
		})
	if err != nil {
		return service.deferRecovery(ctx, operation, "OWNER_OUTCOME_UNKNOWN")
	}
	if managed.BotReceiptID != receiptID || managed.BotReceiptVersion != operation.Identity.ProviderGeneration ||
		managed.BotReceiptSHA256 != receiptSHA {
		_ = service.repository.MarkRepairRequired(ctx, operation, "OWNER_READBACK_MISMATCH")
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	if err := service.requireProviderOwnerCheckpoint(ctx, operation); err != nil {
		_ = service.repository.MarkRepairRequired(ctx, operation, "PROVIDER_OWNER_READBACK_MISMATCH")
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	return service.finishOwnerOutcome(ctx, operation, managed)
}

func (service *Service) finishOwnerOutcome(ctx context.Context, operation entity.AgentMattermostBotOperation,
	owner domaincontrol.AgentMattermostBotOwner,
) (entity.AgentMattermostBotOperation, entity.AgentMattermostBotBinding, error) {
	if !ownerMatchesTerminal(operation, owner) || uuid.Validate(owner.BotReceiptID) != nil ||
		owner.BotReceiptVersion != operation.Identity.ProviderGeneration || !validDigest(owner.BotReceiptSHA256) {
		_ = service.repository.MarkRepairRequired(ctx, operation, "OWNER_READBACK_MISMATCH")
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	if err := service.requireProviderOwnerCheckpoint(ctx, operation); err != nil {
		_ = service.repository.MarkRepairRequired(ctx, operation, "PROVIDER_OWNER_READBACK_MISMATCH")
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrConflict
	}
	action := operation.Action
	if action == enum.AgentBotActionCreateAndBind {
		action = enum.AgentBotActionBind
	}
	actionValue := controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_BIND
	if action == enum.AgentBotActionRebind {
		actionValue = controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REBIND
	} else if action == enum.AgentBotActionRevoke {
		actionValue = controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REVOKE
	}
	authority := controlplanecontract.VerifiedCommandAuthority{
		ActorID:        operation.Principal.ActorID,
		OrganizationID: operation.Principal.OrganizationID, ProjectID: operation.Principal.ProjectID,
		WorkloadID: "interaction-gateway", FullMethod: ownerManageFullMethod,
	}
	intentSHA, err := controlplanecontract.AgentMattermostBotIdentityIntentSHA256(authority,
		&controlplanev1.ManageAgentMattermostBotIdentityRequest{
			Action:  actionValue,
			AgentId: operation.AgentRef, ExpectedVersion: operation.ExpectedAgentVersion,
		}, owner.AgentStableKey)
	if err != nil {
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnavailable
	}
	binding := entity.AgentMattermostBotBinding{
		AgentRef: owner.AgentRef, AgentVersion: owner.AgentVersion,
		Identity: operation.Identity, ReceiptSHA256: owner.BotReceiptSHA256, UpdatedAt: time.Now().UTC(),
	}
	operation.ReceiptID, operation.ReceiptRevision, operation.ReceiptSHA256 = owner.BotReceiptID,
		owner.BotReceiptVersion, owner.BotReceiptSHA256
	operation.CommandIntentSHA256, operation.Result = intentSHA, binding
	if err := service.repository.Finish(ctx, operation, binding); err != nil {
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnavailable
	}
	if action == enum.AgentBotActionRevoke {
		operation.State = enum.AgentBotOperationRevoked
	} else {
		operation.State = enum.AgentBotOperationBound
	}
	operation.Result = binding
	service.metrics.ObserveBotIdentityOperation(action, "success")
	return operation, binding, nil
}

func (service *Service) requireProviderOwnerCheckpoint(ctx context.Context,
	operation entity.AgentMattermostBotOperation,
) error {
	stored := operation.Identity
	fresh, err := service.provider.ReadBotIdentity(ctx, operation.Principal,
		stored.ProviderUserID, stored.ProviderTeamID)
	if err != nil || fresh.ProviderUserID != stored.ProviderUserID || fresh.ProviderTeamID != stored.ProviderTeamID ||
		fresh.Status != stored.Status || fresh.ProviderVersion != stored.ProviderVersion ||
		fresh.ProviderSnapshotSHA256 != stored.ProviderSnapshotSHA256 {
		return errors.New("provider owner checkpoint mismatch")
	}
	return nil
}

func ownerMatchesPredecessor(operation entity.AgentMattermostBotOperation,
	owner domaincontrol.AgentMattermostBotOwner,
) bool {
	action := operation.Action
	if action == enum.AgentBotActionCreateAndBind {
		action = enum.AgentBotActionBind
	}
	return owner.AgentRef == operation.AgentRef && owner.AgentVersion == operation.ExpectedAgentVersion &&
		owner.AgentStableKey != "" &&
		((action == enum.AgentBotActionBind && owner.BotIdentityRef == "") ||
			(action == enum.AgentBotActionRebind && owner.BotIdentityRef != "" &&
				owner.BotProviderGeneration == operation.PredecessorGeneration) ||
			(action == enum.AgentBotActionRevoke && owner.BotIdentityRef != "" &&
				owner.BotProviderGeneration == operation.PredecessorGeneration))
}

func ownerMatchesTerminal(operation entity.AgentMattermostBotOperation,
	owner domaincontrol.AgentMattermostBotOwner,
) bool {
	return owner.AgentRef == operation.AgentRef && owner.AgentStableKey == operation.Identity.AgentStableKey &&
		owner.AgentVersion == operation.ExpectedAgentVersion+1 &&
		owner.BotIdentityRef == operation.Identity.ProviderObjectRef &&
		owner.BotProviderGeneration == operation.Identity.ProviderGeneration &&
		owner.BotMaskedStatus == string(operation.Identity.Status)
}

func (service *Service) resolveOwner(ctx context.Context, principal entity.TeamPrincipal,
	agentRef string,
) (domaincontrol.AgentMattermostBotOwner, entity.WorkspaceMattermostBinding, error) {
	if validatePrincipal(principal) != nil || uuid.Validate(agentRef) != nil {
		return domaincontrol.AgentMattermostBotOwner{}, entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnauthorized
	}
	binding, err := service.teams.GetBinding(ctx, principal)
	if err != nil || binding.Mapping.State != "BOUND" || binding.Team.ProviderTeamID == "" {
		return domaincontrol.AgentMattermostBotOwner{}, entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
	}
	intentSHA := requestDigest(principal, "agent_source_read", agentRef,
		fmt.Sprint(binding.Mapping.Version), fmt.Sprint(binding.Mapping.Generation), binding.Team.ProviderSnapshotSHA256)
	mappingProofRef, err := agentBotMappingProofRef(binding.Mapping)
	if err != nil {
		return domaincontrol.AgentMattermostBotOwner{}, entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
	}
	credential, err := service.receipts.Sign(domaincontrol.ProviderEffectReceipt{
		FullMethod: ownerGetFullMethod, ActorID: principal.ActorID, OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, WorkspaceID: principal.ProjectID,
		ProviderTeamRef: mappingProofRef, ProviderObjectRef: mappingProofRef,
		ProviderUsername: "source-readback", Action: "read", Effect: "agent_bot_identity_source_readback",
		EffectVersion: binding.Mapping.ProviderEffectVersion, EffectGeneration: binding.Mapping.ProviderEffectGeneration,
		EffectSHA256: binding.Team.ProviderSnapshotSHA256, ReceiptID: uuid.NewString(),
		ReceiptRevision: binding.Mapping.ProviderEffectGeneration, MaskedStatus: "AVAILABLE", Eligible: true,
		TargetKind: "agent_bot_identity", TargetResourceID: agentRef,
		TargetStableKey: "agent-source-" + strings.ReplaceAll(agentRef, "-", ""), CommandIntentSHA256: intentSHA,
	})
	if err != nil {
		return domaincontrol.AgentMattermostBotOwner{}, entity.WorkspaceMattermostBinding{}, domainerrs.ErrUnavailable
	}
	owner, err := service.owner.GetAgentMattermostBotIdentity(ctx, credential, agentRef)
	if err != nil || owner.AgentRef != agentRef {
		return domaincontrol.AgentMattermostBotOwner{}, entity.WorkspaceMattermostBinding{}, mapOwnerError(err)
	}
	return owner, binding, nil
}

func agentBotMappingProofRef(mapping entity.WorkspaceMattermostMapping) (string, error) {
	return controlplanecontract.AgentBotMappingProofRef(mapping.ID, mapping.Version, mapping.Generation,
		mapping.ProviderEffectVersion, mapping.ProviderEffectGeneration)
}

func sameOwnerPredecessor(expected, current domaincontrol.AgentMattermostBotOwner) bool {
	return expected.AgentRef == current.AgentRef && expected.AgentVersion == current.AgentVersion &&
		expected.AgentStableKey == current.AgentStableKey && expected.BotIdentityRef == current.BotIdentityRef &&
		expected.BotProviderGeneration == current.BotProviderGeneration &&
		expected.BotMaskedStatus == current.BotMaskedStatus && expected.BotReceiptID == current.BotReceiptID &&
		expected.BotReceiptVersion == current.BotReceiptVersion && expected.BotReceiptSHA256 == current.BotReceiptSHA256
}

func (service *Service) replayOutcome(operation entity.AgentMattermostBotOperation,
	disposition domainrepo.Disposition,
) (entity.AgentMattermostBotOperation, entity.AgentMattermostBotBinding, error) {
	if disposition == domainrepo.Busy {
		return operation, operation.Result, domainerrs.ErrBusy
	}
	switch operation.State {
	case enum.AgentBotOperationBound, enum.AgentBotOperationRevoked:
		return operation, operation.Result, nil
	case enum.AgentBotOperationRepairRequired:
		return operation, operation.Result, domainerrs.ErrRepairRequired
	default:
		return operation, operation.Result, domainerrs.ErrUnavailable
	}
}

func (service *Service) deferRecovery(ctx context.Context, operation entity.AgentMattermostBotOperation,
	code string,
) (entity.AgentMattermostBotOperation, entity.AgentMattermostBotBinding, error) {
	deferred, err := service.repository.DeferRecovery(ctx, operation, code, service.config.RecoveryInterval)
	if err != nil {
		return operation, entity.AgentMattermostBotBinding{}, domainerrs.ErrUnavailable
	}
	service.metrics.ObserveBotIdentityOperation(operation.Action, "ambiguous")
	return deferred, entity.AgentMattermostBotBinding{}, domainerrs.ErrAmbiguousEffect
}

func mergeInternalIdentity(fresh, stored entity.AgentMattermostBotIdentity) entity.AgentMattermostBotIdentity {
	fresh.IdentityRef, fresh.ProviderObjectRef, fresh.Selector, fresh.AgentRef, fresh.AgentStableKey = stored.IdentityRef,
		stored.ProviderObjectRef, stored.Selector, stored.AgentRef, stored.AgentStableKey
	fresh.ProviderTokenID = stored.ProviderTokenID
	fresh.CredentialBindingID, fresh.CredentialSecretRef = stored.CredentialBindingID, stored.CredentialSecretRef
	fresh.CredentialSecretVersion, fresh.CredentialSHA256 = stored.CredentialSecretVersion, stored.CredentialSHA256
	fresh.ProviderCausalitySHA256 = stored.ProviderCausalitySHA256
	return fresh
}

func normalizeBotIntent(usernameIntent, displayName, operationID string) (string, string, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len(displayName) > 64 {
		return "", "", errors.New("bot display name is invalid")
	}
	base := strings.ToLower(strings.TrimSpace(usernameIntent))
	var normalized strings.Builder
	for _, symbol := range base {
		if symbol >= 'a' && symbol <= 'z' || symbol >= '0' && symbol <= '9' || symbol == '.' || symbol == '_' || symbol == '-' {
			normalized.WriteRune(symbol)
		} else if normalized.Len() > 0 && !strings.HasSuffix(normalized.String(), "-") {
			normalized.WriteByte('-')
		}
	}
	base = strings.Trim(normalized.String(), "-._")
	if base == "" || base[0] < 'a' || base[0] > 'z' {
		base = "agent-" + base
	}
	digest := sha256.Sum256([]byte(operationID))
	suffix := "-" + hex.EncodeToString(digest[:])[:6]
	if len(base) > 22-len(suffix) {
		base = strings.TrimRight(base[:22-len(suffix)], "-._")
	}
	username := base + suffix
	if len(username) < 3 || len(username) > 22 {
		return "", "", errors.New("bot username is invalid")
	}
	return username, displayName, nil
}

func stableOperationID(principal entity.TeamPrincipal, agentRef string, expectedAgentVersion,
	predecessorGeneration uint64,
) string {
	return uuid.NewSHA1(operationNamespace, []byte(strings.Join([]string{
		principal.OrganizationID,
		principal.ProjectID, agentRef, fmt.Sprint(expectedAgentVersion),
		fmt.Sprint(predecessorGeneration),
	}, "\x00"))).String()
}

func stableProviderObjectRef(principal entity.TeamPrincipal, providerUserID string) string {
	return uuid.NewSHA1(providerObjectNamespace, []byte(strings.Join([]string{
		principal.OrganizationID, principal.ProjectID, providerUserID,
	}, "\x00"))).String()
}

func requestDigest(principal entity.TeamPrincipal, values ...string) string {
	values = append([]string{
		"agent-mattermost-bot-intent-v1", principal.ActorID,
		principal.OrganizationID, principal.ProjectID,
	}, values...)
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	for _, symbol := range value {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}

func validatePrincipal(principal entity.TeamPrincipal) error {
	for _, value := range []string{principal.ActorID, principal.OrganizationID, principal.ProjectID} {
		if uuid.Validate(value) != nil {
			return errors.New("principal is invalid")
		}
	}
	return nil
}

func validAction(action string) bool {
	return action == enum.AgentBotActionCreateAndBind || action == enum.AgentBotActionBind ||
		action == enum.AgentBotActionRebind || action == enum.AgentBotActionRevoke
}

func mapRepositoryError(err error) error {
	switch {
	case errors.Is(err, domainrepo.ErrNotFound):
		return domainerrs.ErrNotFound
	case errors.Is(err, domainrepo.ErrIdempotencyConflict):
		return domainerrs.ErrIdempotencyConflict
	case errors.Is(err, domainrepo.ErrGenerationConflict):
		return domainerrs.ErrVersionMismatch
	default:
		return domainerrs.ErrUnavailable
	}
}

func mapProviderError(err error) error {
	switch {
	case errors.Is(err, domainmattermost.ErrBotNotFound):
		return domainerrs.ErrProviderDeleted
	case errors.Is(err, domainmattermost.ErrBotConflict):
		return domainerrs.ErrProviderConflict
	case errors.Is(err, domainmattermost.ErrBotForbidden):
		return domainerrs.ErrUnauthorized
	case errors.Is(err, domainmattermost.ErrBotAmbiguousEffect):
		return domainerrs.ErrAmbiguousEffect
	default:
		return domainerrs.ErrUnavailable
	}
}

func mapOwnerError(err error) error {
	switch {
	case errors.Is(err, domaincontrol.ErrNotFound):
		return domainerrs.ErrNotFound
	case errors.Is(err, domaincontrol.ErrConflict):
		return domainerrs.ErrVersionMismatch
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
