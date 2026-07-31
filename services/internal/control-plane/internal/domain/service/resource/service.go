// Package resource реализует canonical commands/queries control-plane.
package resource

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/event"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

const (
	permissionCreate        = "controlplane.resource.create"
	permissionUpdate        = "controlplane.resource.update"
	permissionTransition    = "controlplane.resource.transition"
	permissionDelete        = "controlplane.resource.delete"
	permissionRead          = "controlplane.resource.read"
	permissionList          = "controlplane.resource.list"
	permissionEnqueueTurn   = "controlplane.turn.enqueue"
	permissionClaimTurn     = "controlplane.turn.claim"
	permissionCompleteTurn  = "controlplane.turn.complete"
	permissionClaimSchedule = "controlplane.schedule.claim"
	permissionResolveGate   = "controlplane.owner_gate.resolve"
)

// Config задаёт security-critical bounded runtime policy.
type Config struct {
	LeaseSigningKey       []byte
	TurnLeaseDuration     time.Duration
	MaximumScheduleClaims int
	Observer              Observer
}

// Service владеет business transitions; adapter только сохраняет намерение.
type Service struct {
	repository            domainrepo.Repository
	leaseSigningKey       []byte
	turnLeaseDuration     time.Duration
	maximumScheduleClaims int
	observer              Observer
	now                   func() time.Time
}

// New создаёт service только с полноценными durable boundaries.
func New(repository domainrepo.Repository, config Config) (*Service, error) {
	if repository == nil || len(config.LeaseSigningKey) < 32 ||
		config.TurnLeaseDuration < 30*time.Second ||
		config.TurnLeaseDuration > 30*time.Minute ||
		config.MaximumScheduleClaims < 1 ||
		config.MaximumScheduleClaims > 100 ||
		config.Observer == nil {
		return nil, errors.New("control-plane service configuration is invalid")
	}
	return &Service{
		repository:            repository,
		leaseSigningKey:       slices.Clone(config.LeaseSigningKey),
		turnLeaseDuration:     config.TurnLeaseDuration,
		maximumScheduleClaims: config.MaximumScheduleClaims,
		observer:              config.Observer,
		now:                   time.Now,
	}, nil
}

// Create создаёт server-owned ID, owner, project scope, state и OCC version.
func (service *Service) Create(ctx context.Context, input CreateInput) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionCreate); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		!input.Kind.Valid() || input.Kind == enum.KindTurn ||
		(input.Kind == enum.KindProject) != input.TenantProject ||
		value.ValidateName(input.Name) != nil ||
		input.Spec == nil || input.Spec.Kind() != input.Kind ||
		input.Spec.Validate() != nil ||
		(input.ParentID != "" && value.ValidateID(input.ParentID) != nil) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	if err := validateTemporalCreation(input.Spec, now); err != nil {
		return entity.Resource{}, err
	}
	resourceID := uuid.NewString()
	projectID, err := authoritativeProject(input.Principal, input.Kind, resourceID)
	if err != nil {
		return entity.Resource{}, err
	}
	resource, err := entity.New(
		resourceID,
		input.Principal.OrganizationID,
		projectID,
		input.ParentID,
		input.Principal.ActorID,
		input.Kind,
		input.Name,
		input.Spec,
		now,
	)
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Kind     enum.Kind
		Name     string
		ParentID string
		Spec     entity.Spec
	}{identity(input.Principal), input.Kind, input.Name, input.ParentID, input.Spec})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"create",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			if err := service.validateReferences(ctx, tx, resource); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.Insert(ctx, resource); err != nil {
				return entity.Resource{}, err
			}
			return resource, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"create",
				resource,
			)
		},
	)
}

// Update обновляет resource только после tenant/project resolution и OCC.
func (service *Service) Update(ctx context.Context, input UpdateInput) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionUpdate); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ResourceID) != nil ||
		input.ExpectedVersion == 0 || value.ValidateName(input.Name) != nil ||
		input.Spec == nil || input.Spec.Validate() != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	if err := validateTemporalCreation(input.Spec, now); err != nil {
		return entity.Resource{}, err
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		ResourceID      string
		ExpectedVersion uint64
		Name            string
		Spec            entity.Spec
	}{
		identity(input.Principal),
		input.ResourceID,
		input.ExpectedVersion,
		input.Name,
		input.Spec,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"update",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			current, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.ResourceID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			if err := validateGenericUpdate(current, input.Spec); err != nil {
				return entity.Resource{}, err
			}
			updated, err := current.Update(input.Name, input.Spec, now)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := service.validateReferences(ctx, tx, updated); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"update",
				updated,
			)
		},
	)
}

// Transition выполняет закрытую state machine; retry turn увеличивает attempt.
func (service *Service) Transition(
	ctx context.Context,
	input TransitionInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionTransition); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ResourceID) != nil ||
		input.ExpectedVersion == 0 ||
		len(input.ReasonCode) < 1 || len(input.ReasonCode) > 96 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		ResourceID      string
		ExpectedVersion uint64
		Target          enum.State
		ReasonCode      string
	}{
		identity(input.Principal),
		input.ResourceID,
		input.ExpectedVersion,
		input.Target,
		input.ReasonCode,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"transition",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			current, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.ResourceID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			updated, err := service.transitionResource(current, input.Target)
			if err != nil {
				return entity.Resource{}, err
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"transition",
				updated,
			)
		},
	)
}

// Delete переводит resource через explicit deletion lifecycle.
func (service *Service) Delete(ctx context.Context, input DeleteInput) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionDelete); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ResourceID) != nil || input.ExpectedVersion == 0 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		ResourceID      string
		ExpectedVersion uint64
	}{identity(input.Principal), input.ResourceID, input.ExpectedVersion})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"delete",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			current, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.ResourceID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			target := enum.StateDeletionPending
			if current.State == enum.StateDeletionPending {
				target = enum.StateDeleted
			}
			updated, err := current.Transition(target, service.now())
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"delete",
				updated,
			)
		},
	)
}

// Get скрывает отсутствующий, чужой, удалённый и wrong-kind resource одинаково.
func (service *Service) Get(ctx context.Context, input GetInput) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionRead); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateID(input.ResourceID) != nil || !input.Kind.Valid() {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	resource, err := service.repository.Get(
		ctx,
		input.Principal.OrganizationID,
		input.Principal.ProjectID,
		input.ResourceID,
	)
	if err != nil {
		return entity.Resource{}, err
	}
	if resource.Kind != input.Kind || resource.State == enum.StateDeleted {
		return entity.Resource{}, errs.ErrNotFound
	}
	return resource, nil
}

// List принудительно заменяет caller filters на verified ownership boundary.
func (service *Service) List(ctx context.Context, input ListInput) ([]entity.Resource, error) {
	if err := authorize(input.Principal, permissionList); err != nil {
		return nil, err
	}
	input.Filter.OrganizationID = input.Principal.OrganizationID
	input.Filter.ProjectID = input.Principal.ProjectID
	if err := input.Filter.Validate(); err != nil {
		return nil, errs.ErrInvalidInput
	}
	if input.TenantProjects {
		if input.Principal.ProjectID != "" || input.Filter.Kind != enum.KindProject {
			return nil, errs.ErrPermissionDenied
		}
		return service.repository.ListEligibleProjects(
			ctx,
			input.Principal.OrganizationID,
			input.Principal.ActorID,
			input.Filter.AfterID,
			input.Filter.Limit,
		)
	}
	if input.Filter.Kind == enum.KindProject || input.Principal.ProjectID == "" {
		return nil, errs.ErrPermissionDenied
	}
	return service.repository.List(ctx, input.Filter)
}

type resourceMutation func(domainrepo.Transaction) (entity.Resource, error)

func (service *Service) withResourceReceipt(
	ctx context.Context,
	principal value.Principal,
	idempotencyKey string,
	scope string,
	requestHash string,
	apply resourceMutation,
) (entity.Resource, error) {
	keyHash := hashString(idempotencyKey)
	var result entity.Resource
	mutated := false
	err := service.repository.Transact(
		ctx,
		principal.OrganizationID,
		principal.ProjectID,
		func(tx domainrepo.Transaction) error {
			receipt, err := tx.GetReceipt(ctx, principal.OrganizationID, scope, keyHash)
			if err == nil {
				if receipt.RequestHash != requestHash {
					return errs.ErrIdempotencyConflict
				}
				result = receipt.Result
				return result.Validate()
			}
			if !errors.Is(err, errs.ErrNotFound) {
				return err
			}
			applied, err := apply(tx)
			if err != nil {
				return err
			}
			receipt = domainrepo.Receipt{
				OrganizationID: principal.OrganizationID,
				ProjectID:      principal.ProjectID,
				Scope:          scope,
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Result:         applied,
				CreatedAt:      service.now().UTC().Truncate(time.Microsecond),
			}
			if err := tx.SaveReceipt(ctx, receipt); err != nil {
				return err
			}
			result = applied
			mutated = true
			return nil
		},
	)
	if err == nil && mutated {
		service.observer.ObserveMutation(result.Kind, scope)
	}
	return result, err
}

func (service *Service) appendMutationRecords(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	action string,
	resource entity.Resource,
) error {
	audit := domainrepo.Audit{
		ID:              uuid.NewString(),
		OrganizationID:  resource.OrganizationID,
		ProjectID:       resource.ProjectID,
		ActorID:         principal.ActorID,
		Action:          action,
		ResourceID:      resource.ID,
		ResourceKind:    string(resource.Kind),
		ResourceVersion: resource.Version,
		Outcome:         "succeeded",
		CorrelationID:   principal.CorrelationID,
		PolicyRevision:  principal.PolicyRevision,
		OccurredAt:      resource.UpdatedAt,
	}
	if err := tx.AppendAudit(ctx, audit); err != nil {
		return err
	}
	eventName, published := event.EventNameForKind(resource.Kind)
	if !published {
		return nil
	}
	return tx.AppendEvent(ctx, event.Change{
		EventID:         uuid.NewString(),
		EventName:       eventName,
		OrganizationID:  resource.OrganizationID,
		ProjectID:       resource.ProjectID,
		ResourceID:      resource.ID,
		ResourceKind:    resource.Kind,
		ResourceState:   resource.State,
		ResourceVersion: resource.Version,
		EventSequence:   resource.Version,
		OccurredAt:      resource.UpdatedAt,
		CorrelationID:   principal.CorrelationID,
	})
}

func (service *Service) transitionResource(
	current entity.Resource,
	target enum.State,
) (entity.Resource, error) {
	if current.Kind == enum.KindTurn && target == enum.StateQueued {
		spec, ok := current.Spec.(entity.TurnSpec)
		if !ok || spec.Attempt >= 100 {
			return entity.Resource{}, errs.ErrStateConflict
		}
		spec.Attempt++
		spec.Outcome = ""
		spec.ResultArtifactID = ""
		updated, err := current.ReplaceAndTransition(spec, target, service.now())
		if err != nil {
			return entity.Resource{}, errs.ErrStateConflict
		}
		return updated, nil
	}
	updated, err := current.Transition(target, service.now())
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	return updated, nil
}

func validateGenericUpdate(current entity.Resource, next entity.Spec) error {
	if next.Kind() != current.Kind || current.Kind == enum.KindTurn {
		return errs.ErrStateConflict
	}
	switch currentSpec := current.Spec.(type) {
	case entity.ProjectSpec:
		nextSpec, ok := next.(entity.ProjectSpec)
		if !ok || currentSpec.Slug != nextSpec.Slug {
			return errs.ErrStateConflict
		}
	case entity.TeamSpec:
		nextSpec, ok := next.(entity.TeamSpec)
		if !ok || currentSpec.StableKey != nextSpec.StableKey {
			return errs.ErrStateConflict
		}
	case entity.ChatSpec:
		nextSpec, ok := next.(entity.ChatSpec)
		if !ok || currentSpec.StableKey != nextSpec.StableKey ||
			currentSpec.RoomType != nextSpec.RoomType ||
			currentSpec.ExternalChannelRef != nextSpec.ExternalChannelRef {
			return errs.ErrStateConflict
		}
	case entity.RoleSpec:
		nextSpec, ok := next.(entity.RoleSpec)
		if !ok || currentSpec.StableKey != nextSpec.StableKey {
			return errs.ErrStateConflict
		}
	case entity.PromptProfileSpec:
		nextSpec, ok := next.(entity.PromptProfileSpec)
		if !ok || currentSpec.Revision == ^uint64(0) ||
			nextSpec.Revision != currentSpec.Revision+1 {
			return errs.ErrStateConflict
		}
	case entity.CredentialBindingSpec:
		nextSpec, ok := next.(entity.CredentialBindingSpec)
		if !ok || currentSpec.Purpose != nextSpec.Purpose ||
			currentSpec.PrincipalRef != nextSpec.PrincipalRef ||
			currentSpec.Revision == ^uint64(0) ||
			nextSpec.Revision != currentSpec.Revision+1 {
			return errs.ErrStateConflict
		}
	case entity.RepositoryWorkspaceSpec:
		nextSpec, ok := next.(entity.RepositoryWorkspaceSpec)
		if !ok || currentSpec.RepositoryRef != nextSpec.RepositoryRef ||
			currentSpec.WorkspaceMode != nextSpec.WorkspaceMode {
			return errs.ErrStateConflict
		}
	case entity.IntegrationSpec:
		nextSpec, ok := next.(entity.IntegrationSpec)
		if !ok || currentSpec.DefinitionRef != nextSpec.DefinitionRef ||
			nextSpec.DefinitionVersion <= currentSpec.DefinitionVersion {
			return errs.ErrStateConflict
		}
	case entity.RuntimeRevisionSpec:
		return errs.ErrStateConflict
	case entity.SessionSpec:
		nextSpec, ok := next.(entity.SessionSpec)
		if !ok || currentSpec.AgentID != nextSpec.AgentID ||
			currentSpec.ProviderAccountBindingID != nextSpec.ProviderAccountBindingID ||
			currentSpec.LastTurnSequence != nextSpec.LastTurnSequence ||
			(currentSpec.ConversationID != "" &&
				currentSpec.ConversationID != nextSpec.ConversationID) {
			return errs.ErrStateConflict
		}
	case entity.OwnerGateSpec:
		nextSpec, ok := next.(entity.OwnerGateSpec)
		if !ok || currentSpec.ProcessRunID != nextSpec.ProcessRunID ||
			currentSpec.ResultRef != nextSpec.ResultRef ||
			currentSpec.ResultSHA256 != nextSpec.ResultSHA256 ||
			currentSpec.ExpiresAt != nextSpec.ExpiresAt ||
			currentSpec.Decision != nextSpec.Decision ||
			currentSpec.DecisionReason != nextSpec.DecisionReason {
			return errs.ErrStateConflict
		}
	case entity.ProcessRunSpec:
		nextSpec, ok := next.(entity.ProcessRunSpec)
		if !ok || currentSpec.ParentProcessRunID != nextSpec.ParentProcessRunID ||
			currentSpec.RootTriggerRef != nextSpec.RootTriggerRef ||
			currentSpec.PlaybookRef != nextSpec.PlaybookRef ||
			currentSpec.PolicyRevision != nextSpec.PolicyRevision ||
			(currentSpec.ResultArtifactID != "" &&
				currentSpec.ResultArtifactID != nextSpec.ResultArtifactID) {
			return errs.ErrStateConflict
		}
	case entity.ScheduleSpec:
		nextSpec, ok := next.(entity.ScheduleSpec)
		if !ok || currentSpec.TargetResourceID != nextSpec.TargetResourceID {
			return errs.ErrStateConflict
		}
	case entity.MemoryRecordSpec:
		nextSpec, ok := next.(entity.MemoryRecordSpec)
		if !ok || currentSpec.Scope != nextSpec.Scope ||
			currentSpec.RoleID != nextSpec.RoleID ||
			currentSpec.Provenance != nextSpec.Provenance {
			return errs.ErrStateConflict
		}
	case entity.WorkClaimSpec:
		nextSpec, ok := next.(entity.WorkClaimSpec)
		if !ok || currentSpec.ProcessRunID != nextSpec.ProcessRunID ||
			currentSpec.TurnID != nextSpec.TurnID {
			return errs.ErrStateConflict
		}
	case entity.ArtifactSpec:
		return errs.ErrStateConflict
	}
	return nil
}

func validateTemporalCreation(spec entity.Spec, now time.Time) error {
	switch typed := spec.(type) {
	case entity.SessionSpec:
		if typed.LastTurnSequence != 0 {
			return errs.ErrInvalidInput
		}
	case entity.ProcessRunSpec:
		if typed.ResultArtifactID != "" {
			return errs.ErrInvalidInput
		}
	case entity.ArtifactSpec:
		if typed.ScanStatus != "PENDING" {
			return errs.ErrInvalidInput
		}
	case entity.CredentialBindingSpec:
		if !typed.ExpiresAt.IsZero() && !typed.ExpiresAt.After(now) {
			return errs.ErrInvalidInput
		}
	case entity.ScheduleSpec:
		if !typed.NextRunAt.After(now) {
			return errs.ErrInvalidInput
		}
		if typed.Cron != "" {
			if _, err := scheduleParser.Parse(typed.Cron); err != nil {
				return errs.ErrInvalidInput
			}
		}
	case entity.OwnerGateSpec:
		if !typed.ExpiresAt.After(now) ||
			typed.Decision != "" || typed.DecisionReason != "" {
			return errs.ErrInvalidInput
		}
	}
	return nil
}

type reference struct {
	id    string
	kinds []enum.Kind
}

func (service *Service) validateReferences(
	ctx context.Context,
	tx domainrepo.Transaction,
	resource entity.Resource,
) error {
	references := make([]reference, 0, 16)
	add := func(identifier string, kinds ...enum.Kind) {
		if identifier != "" {
			references = append(references, reference{id: identifier, kinds: kinds})
		}
	}
	add(resource.ParentID)
	switch spec := resource.Spec.(type) {
	case entity.TeamSpec:
		for _, identifier := range spec.RoleIDs {
			add(identifier, enum.KindRole)
		}
	case entity.ChatSpec:
		add(spec.DefaultAgentID, enum.KindRole)
	case entity.RoleSpec:
		add(spec.PromptProfileID, enum.KindPromptProfile)
		for _, identifier := range spec.AllowedTargetRoleIDs {
			add(identifier, enum.KindRole)
		}
	case entity.RepositoryWorkspaceSpec:
		add(spec.CredentialBindingID, enum.KindCredentialBinding)
	case entity.IntegrationSpec:
		for _, identifier := range spec.CredentialBindingIDs {
			add(identifier, enum.KindCredentialBinding)
		}
	case entity.RuntimeRevisionSpec:
		add(spec.PromptProfileID, enum.KindPromptProfile)
		for _, identifier := range spec.CredentialBindingIDs {
			add(identifier, enum.KindCredentialBinding)
		}
		for _, identifier := range spec.IntegrationIDs {
			add(identifier, enum.KindIntegration)
		}
	case entity.SessionSpec:
		add(spec.AgentID, enum.KindRole)
		add(spec.ProviderAccountBindingID, enum.KindCredentialBinding)
		add(spec.ConversationID, enum.KindChat)
	case entity.TurnSpec:
		add(spec.SessionID, enum.KindSession)
		add(spec.PromptArtifactID, enum.KindArtifact)
		add(spec.RuntimeRevisionID, enum.KindRuntimeRevision)
		add(spec.ProcessRunID, enum.KindProcessRun)
		add(spec.ResultArtifactID, enum.KindArtifact)
	case entity.ProcessRunSpec:
		add(spec.ParentProcessRunID, enum.KindProcessRun)
		add(spec.ResultArtifactID, enum.KindArtifact)
	case entity.ScheduleSpec:
		add(spec.TargetResourceID)
	case entity.OwnerGateSpec:
		add(spec.ProcessRunID, enum.KindProcessRun)
	case entity.MemoryRecordSpec:
		add(spec.RoleID, enum.KindRole)
	case entity.WorkClaimSpec:
		add(spec.ProcessRunID, enum.KindProcessRun)
		add(spec.TurnID, enum.KindTurn)
	}
	slices.SortFunc(references, func(left, right reference) int {
		if left.id < right.id {
			return -1
		}
		if left.id > right.id {
			return 1
		}
		return 0
	})
	for _, expected := range references {
		if expected.id == resource.ID {
			return errs.ErrStateConflict
		}
		referenced, err := tx.GetForUpdate(
			ctx,
			resource.OrganizationID,
			resource.ProjectID,
			expected.id,
		)
		if err != nil {
			return err
		}
		if referenced.State == enum.StateDeleted ||
			(len(expected.kinds) > 0 && !slices.Contains(expected.kinds, referenced.Kind)) {
			return errs.ErrNotFound
		}
	}
	return nil
}

type commandIdentity struct {
	ActorID        string
	OrganizationID string
	ProjectID      string
	Permission     string
	CallerWorkload string
}

func identity(principal value.Principal) commandIdentity {
	return commandIdentity{
		ActorID:        principal.ActorID,
		OrganizationID: principal.OrganizationID,
		ProjectID:      principal.ProjectID,
		Permission:     principal.Permission,
		CallerWorkload: principal.CallerWorkload,
	}
}

func canonicalHash(input any) (string, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal command: %w", err)
	}
	return hashBytes(encoded), nil
}

func hashString(input string) string {
	return hashBytes([]byte(input))
}

func hashBytes(input []byte) string {
	digest := sha256.Sum256(input)
	return hex.EncodeToString(digest[:])
}

func (service *Service) leaseToken(
	turnID string,
	fence uint64,
	idempotencyKey string,
) string {
	mac := hmac.New(sha256.New, service.leaseSigningKey)
	_, _ = fmt.Fprintf(mac, "%s\x00%d\x00%s", turnID, fence, idempotencyKey)
	return hex.EncodeToString(mac.Sum(nil))
}

func authorize(principal value.Principal, permission string) error {
	if err := principal.Validate(); err != nil {
		return errs.ErrUnauthenticated
	}
	if principal.Permission != permission {
		return errs.ErrPermissionDenied
	}
	return nil
}

func authoritativeProject(
	principal value.Principal,
	kind enum.Kind,
	resourceID string,
) (string, error) {
	if kind == enum.KindProject {
		if principal.ProjectID != "" {
			return "", errs.ErrPermissionDenied
		}
		return resourceID, nil
	}
	if principal.ProjectID == "" {
		return "", errs.ErrPermissionDenied
	}
	return principal.ProjectID, nil
}
