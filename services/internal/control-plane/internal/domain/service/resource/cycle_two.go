package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"math/bits"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

const (
	maximumWorkClaimTTL             = 24 * time.Hour
	localMemoryProjectionDimensions = 256
	localMemoryProjectionModelID    = "mattercodex-local-token-hash"
	localMemoryProjectionRevision   = 1
)

// ManageSession создаёт и завершает SESSION без выбранной вызывающей стороной
// привязки учётных данных.
func (service *Service) ManageSession(
	ctx context.Context,
	input ManageSessionInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionManageSession); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Action == "CREATE" {
		if value.ValidateName(input.Name) != nil ||
			value.ValidateID(input.RoleID) != nil ||
			(input.ConversationID != "" && value.ValidateID(input.ConversationID) != nil) ||
			(input.PreferredProviderCredentialBindingID != "" &&
				value.ValidateID(input.PreferredProviderCredentialBindingID) != nil) ||
			input.SessionID != "" || input.ExpectedVersion != 0 ||
			input.ArchiveRef != "" || input.ReasonCode != "" {
			return entity.Resource{}, errs.ErrInvalidInput
		}
	} else if value.ValidateID(input.SessionID) != nil ||
		input.ExpectedVersion == 0 || value.ValidateStableKey(input.ReasonCode) != nil ||
		input.Name != "" || input.RoleID != "" || input.ConversationID != "" ||
		input.PreferredProviderCredentialBindingID != "" {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Action != "CREATE" && input.Action != "CLOSE" &&
		input.Action != "ARCHIVE" && input.Action != "CANCEL" &&
		input.Action != "CLEANUP" {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Action == "ARCHIVE" && !validRuntimeReference(input.ArchiveRef) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Action != "ARCHIVE" && input.ArchiveRef != "" {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Input    ManageSessionInput
	}{identity(input.Principal), input})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"manage_session_"+input.Action,
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			now := service.now().UTC().Truncate(time.Microsecond)
			if input.Action == "CREATE" {
				role, err := tx.GetForUpdate(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					input.RoleID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				roleSpec, ok := role.Spec.(entity.RoleSpec)
				if !ok || role.Kind != enum.KindRole || role.State != enum.StateActive ||
					len(roleSpec.ProviderCredentialBindingIDs) == 0 {
					return entity.Resource{}, errs.ErrStateConflict
				}
				roleIDs, err := tx.ActorRoleIDs(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					input.Principal.ActorID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				if !slices.Contains(roleIDs, role.ID) {
					return entity.Resource{}, errs.ErrNotFound
				}
				sessionID := uuid.NewString()
				binding, err := service.selectProviderBinding(
					ctx, tx, input.Principal, role.ID, roleSpec,
					input.PreferredProviderCredentialBindingID,
					now,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				if input.ConversationID != "" {
					conversation, err := tx.GetForUpdate(
						ctx,
						input.Principal.OrganizationID,
						input.Principal.ProjectID,
						input.ConversationID,
					)
					if err != nil {
						return entity.Resource{}, err
					}
					if conversation.Kind != enum.KindChat ||
						conversation.State != enum.StateActive {
						return entity.Resource{}, errs.ErrNotFound
					}
				}
				session, err := entity.New(
					sessionID,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					input.ConversationID,
					input.Principal.ActorID,
					enum.KindSession,
					input.Name,
					entity.SessionSpec{
						AgentID:                  role.ID,
						ProviderAccountBindingID: binding.ID,
						ConversationID:           input.ConversationID,
					},
					now,
				)
				if err != nil {
					return entity.Resource{}, errs.ErrInvalidInput
				}
				if err := tx.Insert(ctx, session); err != nil {
					return entity.Resource{}, err
				}
				return session, service.appendMutationRecords(
					ctx, tx, input.Principal, "create_session", session,
				)
			}
			current, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.SessionID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if current.Kind != enum.KindSession ||
				current.OwnerActorID != input.Principal.ActorID {
				return entity.Resource{}, errs.ErrNotFound
			}
			if current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			spec, ok := current.Spec.(entity.SessionSpec)
			if !ok {
				return entity.Resource{}, errs.ErrInternal
			}
			target := enum.StateArchived
			switch input.Action {
			case "ARCHIVE":
				spec.ArchiveRef = input.ArchiveRef
			case "CANCEL":
				target = enum.StateCancelled
			case "CLEANUP":
				if current.State == enum.StateDeletionPending {
					target = enum.StateDeleted
				} else if current.State == enum.StateArchived ||
					current.State == enum.StateCancelled {
					target = enum.StateDeletionPending
				} else {
					return entity.Resource{}, errs.ErrStateConflict
				}
			}
			if input.Action == "CLOSE" || input.Action == "CANCEL" {
				if err := service.cancelSessionTurns(
					ctx, tx, input.Principal, current.ID, input.ReasonCode, now,
				); err != nil {
					return entity.Resource{}, err
				}
			}
			updated, err := current.ReplaceAndTransition(spec, target, now)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(
				ctx, tx, input.Principal, "session_"+input.Action, updated,
			)
		},
	)
}

func (service *Service) selectProviderBinding(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	roleID string,
	roleSpec entity.RoleSpec,
	preferredBindingID string,
	now time.Time,
) (entity.Resource, error) {
	type candidate struct {
		resource entity.Resource
		spec     entity.CredentialBindingSpec
		active   uint64
		weight   uint32
	}
	weights := make(map[string]uint32, len(roleSpec.ProviderAccountPool.Bindings))
	for _, binding := range roleSpec.ProviderAccountPool.Bindings {
		weights[binding.CredentialBindingID] = binding.Weight
	}
	var candidates []candidate
	for _, bindingID := range roleSpec.ProviderCredentialBindingIDs {
		binding, err := tx.GetForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID, bindingID,
		)
		if err != nil {
			return entity.Resource{}, err
		}
		spec, ok := binding.Spec.(entity.CredentialBindingSpec)
		if !ok || binding.Kind != enum.KindCredentialBinding ||
			binding.State != enum.StateActive || spec.Purpose != "provider-account" ||
			!spec.ProviderEligible ||
			(!spec.ExpiresAt.IsZero() && !spec.ExpiresAt.After(now)) ||
			now.Sub(spec.ProviderObservedAt) > roleSpec.ProviderAccountPool.ObservationMaxAge {
			continue
		}
		active, err := tx.ActiveProviderSessions(
			ctx, principal.OrganizationID, principal.ProjectID, binding.ID,
		)
		if err != nil {
			return entity.Resource{}, err
		}
		if spec.ProviderObservedUsage >= spec.ProviderObservedLimit ||
			active >= spec.ProviderObservedLimit-spec.ProviderObservedUsage {
			continue
		}
		candidates = append(candidates, candidate{
			resource: binding, spec: spec, active: active,
			weight: weights[binding.ID],
		})
	}
	if preferredBindingID != "" {
		for _, item := range candidates {
			if item.resource.ID == preferredBindingID {
				return item.resource, nil
			}
		}
		return entity.Resource{}, errs.ErrNotFound
	}
	if len(candidates) == 0 {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if roleSpec.ProviderAccountPool.Policy == "least_used" {
		slices.SortFunc(candidates, func(left, right candidate) int {
			leftUsage := left.spec.ProviderObservedUsage + left.active
			rightUsage := right.spec.ProviderObservedUsage + right.active
			leftHigh, leftLow := bits.Mul64(
				leftUsage, right.spec.ProviderObservedLimit,
			)
			rightHigh, rightLow := bits.Mul64(
				rightUsage, left.spec.ProviderObservedLimit,
			)
			if leftHigh < rightHigh ||
				(leftHigh == rightHigh && leftLow < rightLow) {
				return -1
			}
			if leftHigh > rightHigh ||
				(leftHigh == rightHigh && leftLow > rightLow) {
				return 1
			}
			return strings.Compare(left.resource.ID, right.resource.ID)
		})
		return candidates[0].resource, nil
	}
	slices.SortFunc(candidates, func(left, right candidate) int {
		return strings.Compare(left.resource.ID, right.resource.ID)
	})
	type poolSnapshotBinding struct {
		ID                  string
		Weight              uint32
		ObservationRevision uint64
		ObservedLimit       uint64
	}
	snapshot := make([]poolSnapshotBinding, 0, len(candidates))
	var totalWeight uint64
	for _, item := range candidates {
		if totalWeight > 80000-uint64(item.weight) {
			return entity.Resource{}, errs.ErrStateConflict
		}
		totalWeight += uint64(item.weight)
		snapshot = append(snapshot, poolSnapshotBinding{
			ID: item.resource.ID, Weight: item.weight,
			ObservationRevision: item.spec.ProviderObservationRevision,
			ObservedLimit:       item.spec.ProviderObservedLimit,
		})
	}
	snapshotSHA256, err := canonicalHash(snapshot)
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	slot, err := tx.NextProviderPoolSlot(ctx, domainrepo.ProviderPoolCursor{
		RoleID: roleID, PolicyRevision: roleSpec.ProviderAccountPool.PolicyRevision,
		SnapshotSHA256: snapshotSHA256, TotalWeight: totalWeight,
	})
	if err != nil {
		return entity.Resource{}, err
	}
	selectionWeights := make([]uint32, len(candidates))
	for index := range candidates {
		selectionWeights[index] = candidates[index].weight
	}
	selected, ok := weightedCandidateIndex(selectionWeights, slot)
	if ok {
		return candidates[selected].resource, nil
	}
	return entity.Resource{}, errs.ErrInternal
}

// weightedCandidateIndex отображает durable slot полного цикла на закрытый
// список весов. Функция не использует случайность и не переполняет сумму:
// вызывающий код заранее ограничивает totalWeight значением 80000.
func weightedCandidateIndex(weights []uint32, slot uint64) (int, bool) {
	for _, weight := range weights {
		if weight == 0 {
			return 0, false
		}
	}
	for index, weight := range weights {
		if slot < uint64(weight) {
			return index, true
		}
		slot -= uint64(weight)
	}
	return 0, false
}

func (service *Service) cancelSessionTurns(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	sessionID, reason string,
	now time.Time,
) error {
	turns, err := tx.OpenSessionTurns(
		ctx, principal.OrganizationID, principal.ProjectID, sessionID,
	)
	if err != nil {
		return err
	}
	for _, item := range turns {
		if item.Attempt.TurnID != item.Turn.ID {
			return errs.ErrStateConflict
		}
		if _, err := service.cancelTurnExecution(
			ctx, tx, principal, item.Turn, reason, now,
		); err != nil {
			return err
		}
	}
	return nil
}

// ManageMemoryRecord проверяет полномочия проекта и роли и назначает владельца
// на сервере.
func (service *Service) ManageMemoryRecord(
	ctx context.Context,
	input ManageMemoryRecordInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionWriteMemory); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		(input.Action != "CREATE" && input.Action != "SUPERSEDE" &&
			input.Action != "ARCHIVE") {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Action == "CREATE" {
		if input.MemoryRecordID != "" || input.ExpectedVersion != 0 {
			return entity.Resource{}, errs.ErrInvalidInput
		}
	} else if value.ValidateID(input.MemoryRecordID) != nil ||
		input.ExpectedVersion == 0 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	spec := entity.MemoryRecordSpec{
		Scope:         input.Scope,
		RoleID:        input.RoleID,
		Title:         input.Title,
		Content:       input.Content,
		ContentSHA256: input.ContentSHA256,
		Provenance:    input.Provenance,
		Importance:    input.Importance,
	}
	if input.Action != "ARCHIVE" && spec.Validate() != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Input    ManageMemoryRecordInput
	}{identity(input.Principal), input})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"manage_memory_"+input.Action,
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			if err := authorizeMemoryScope(ctx, tx, input.Principal, spec.Scope, spec.RoleID); err != nil {
				return entity.Resource{}, err
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			if input.Action == "CREATE" {
				record, err := entity.New(
					uuid.NewString(),
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					spec.RoleID,
					input.Principal.ActorID,
					enum.KindMemoryRecord,
					spec.Title,
					spec,
					now,
				)
				if err != nil {
					return entity.Resource{}, errs.ErrInvalidInput
				}
				if err := tx.Insert(ctx, record); err != nil {
					return entity.Resource{}, err
				}
				return record, service.appendMutationRecords(
					ctx, tx, input.Principal, "create_memory_record", record,
				)
			}
			current, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.MemoryRecordID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			currentSpec, ok := current.Spec.(entity.MemoryRecordSpec)
			if !ok || current.Kind != enum.KindMemoryRecord ||
				current.OwnerActorID != input.Principal.ActorID {
				return entity.Resource{}, errs.ErrNotFound
			}
			if current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			if input.Action == "ARCHIVE" {
				archived, err := current.Transition(enum.StateArchived, now)
				if err != nil {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if err := tx.Update(ctx, archived, current.Version); err != nil {
					return entity.Resource{}, err
				}
				return archived, service.appendMutationRecords(
					ctx, tx, input.Principal, "archive_memory_record", archived,
				)
			}
			if spec.Scope != currentSpec.Scope || spec.RoleID != currentSpec.RoleID {
				return entity.Resource{}, errs.ErrStateConflict
			}
			archived, err := current.Transition(enum.StateArchived, now)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, archived, current.Version); err != nil {
				return entity.Resource{}, err
			}
			replacement, err := entity.New(
				uuid.NewString(),
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				current.ID,
				input.Principal.ActorID,
				enum.KindMemoryRecord,
				spec.Title,
				spec,
				now,
			)
			if err != nil {
				return entity.Resource{}, errs.ErrInvalidInput
			}
			if err := tx.Insert(ctx, replacement); err != nil {
				return entity.Resource{}, err
			}
			if err := service.appendMutationRecords(
				ctx, tx, input.Principal, "supersede_memory_record", replacement,
			); err != nil {
				return entity.Resource{}, err
			}
			return replacement, service.appendMutationRecords(
				ctx, tx, input.Principal, "archive_superseded_memory_record", archived,
			)
		},
	)
}

func authorizeMemoryScope(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	scope, roleID string,
) error {
	permissions, err := tx.ActorPermissions(
		ctx, principal.OrganizationID, principal.ProjectID, principal.ActorID,
	)
	if err != nil {
		return err
	}
	if scope == "PROJECT" {
		if roleID != "" ||
			(!slices.Contains(permissions, "*") &&
				!slices.Contains(permissions, permissionWriteProjectMemory)) {
			return errs.ErrPermissionDenied
		}
		return nil
	}
	if scope != "ROLE" || value.ValidateID(roleID) != nil {
		return errs.ErrInvalidInput
	}
	roleIDs, err := tx.ActorRoleIDs(
		ctx, principal.OrganizationID, principal.ProjectID, principal.ActorID,
	)
	if err != nil {
		return err
	}
	if !slices.Contains(roleIDs, roleID) {
		return errs.ErrNotFound
	}
	return nil
}

// ManageWorkClaim связывает получение работы с неизменяемой родословной выполнения.
func (service *Service) ManageWorkClaim(
	ctx context.Context,
	input ManageWorkClaimInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionManageWorkClaim); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		(input.Action != "CREATE" && input.Action != "RENEW" &&
			input.Action != "RELEASE") {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Action == "CREATE" {
		if input.WorkClaimID != "" || input.ExpectedVersion != 0 ||
			value.ValidateID(input.ProcessRunID) != nil ||
			value.ValidateID(input.TurnID) != nil ||
			input.TTL < time.Minute || input.TTL > maximumWorkClaimTTL {
			return entity.Resource{}, errs.ErrInvalidInput
		}
	} else if value.ValidateID(input.WorkClaimID) != nil ||
		input.ExpectedVersion == 0 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Input    ManageWorkClaimInput
	}{identity(input.Principal), input})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"manage_work_claim_"+input.Action,
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			now := service.now().UTC().Truncate(time.Microsecond)
			if input.Action == "CREATE" {
				process, err := tx.GetForUpdate(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					input.ProcessRunID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				turn, err := tx.GetForUpdate(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					input.TurnID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				processSpec, processOK := process.Spec.(entity.ProcessRunSpec)
				turnSpec, turnOK := turn.Spec.(entity.TurnSpec)
				if !processOK || !turnOK ||
					process.Kind != enum.KindProcessRun ||
					process.State.Terminal() ||
					turn.Kind != enum.KindTurn || turn.State.Terminal() ||
					processSpec.RootInitiatorActorID != input.Principal.ActorID ||
					turnSpec.ProcessRunID != process.ID {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if processSpec.ParentProcessRunID == "" {
					if processSpec.RootSessionID != turnSpec.SessionID ||
						processSpec.RootTurnID != turn.ID ||
						processSpec.RootAttempt != turnSpec.Attempt {
						return entity.Resource{}, errs.ErrStateConflict
					}
				} else {
					if processSpec.TargetSessionID != turnSpec.SessionID ||
						processSpec.TargetTurnID != turn.ID ||
						processSpec.TargetAttempt != turnSpec.Attempt {
						return entity.Resource{}, errs.ErrStateConflict
					}
					edge, err := tx.GetDelegationEdgeByTargetTurn(
						ctx, input.Principal.OrganizationID,
						input.Principal.ProjectID, turn.ID,
					)
					if err != nil || edge.ID != processSpec.DelegationID ||
						edge.ParentProcessRunID != processSpec.ParentProcessRunID ||
						edge.TargetInputSHA256 != turnSpec.EffectiveInputSHA256 {
						return entity.Resource{}, errs.ErrStateConflict
					}
				}
				if input.Principal.CallerWorkload == "agent-runner" &&
					(input.Principal.AuthoritySource != "AGENT_SESSION" ||
						input.Principal.AuthorityReference != turn.ID ||
						input.Principal.AuthorityRevision != uint64(turnSpec.Attempt) ||
						input.Principal.AuthorityDigest !=
							turnSpec.EffectiveInputSHA256) {
					return entity.Resource{}, errs.ErrPermissionDenied
				}
				authorityGeneration := input.Principal.AuthorityGeneration
				if input.Principal.CallerWorkload == "agent-runner" {
					authorityGeneration = input.Principal.AuthorityGrantGeneration
					if authorityGeneration == 0 {
						return entity.Resource{}, errs.ErrPermissionDenied
					}
				}
				spec := entity.WorkClaimSpec{
					ProcessRunID:        input.ProcessRunID,
					TurnID:              input.TurnID,
					Summary:             input.Summary,
					Domains:             input.Domains,
					ResourceKeys:        input.ResourceKeys,
					OwnerActorID:        input.Principal.ActorID,
					WorkloadID:          input.Principal.CallerWorkload,
					SessionID:           turnSpec.SessionID,
					Attempt:             turnSpec.Attempt,
					AuthorityGeneration: authorityGeneration,
					ExpiresAt:           now.Add(input.TTL),
				}
				if spec.Validate() != nil {
					return entity.Resource{}, errs.ErrInvalidInput
				}
				claim, err := entity.New(
					uuid.NewString(),
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					process.ID,
					input.Principal.ActorID,
					enum.KindWorkClaim,
					"Work claim "+turn.ID,
					spec,
					now,
				)
				if err != nil {
					return entity.Resource{}, errs.ErrInvalidInput
				}
				if err := tx.Insert(ctx, claim); err != nil {
					return entity.Resource{}, err
				}
				return claim, service.appendMutationRecords(
					ctx, tx, input.Principal, "create_work_claim", claim,
				)
			}
			current, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.WorkClaimID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			spec, ok := current.Spec.(entity.WorkClaimSpec)
			if !ok || current.Kind != enum.KindWorkClaim ||
				spec.OwnerActorID != input.Principal.ActorID ||
				spec.WorkloadID != input.Principal.CallerWorkload {
				return entity.Resource{}, errs.ErrNotFound
			}
			if input.Principal.CallerWorkload == "agent-runner" &&
				(input.Principal.AuthoritySource != "AGENT_SESSION" ||
					input.Principal.AuthorityReference != spec.TurnID ||
					input.Principal.AuthorityRevision != uint64(spec.Attempt) ||
					input.Principal.AuthorityGrantGeneration !=
						spec.AuthorityGeneration) {
				return entity.Resource{}, errs.ErrPermissionDenied
			}
			if current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			if input.Action == "RENEW" {
				if input.TTL < time.Minute || input.TTL > maximumWorkClaimTTL ||
					current.State != enum.StateActive {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if err := service.validateActiveWorkClaimGraph(
					ctx, tx, input.Principal, current, spec,
				); err != nil {
					return entity.Resource{}, err
				}
				spec.ExpiresAt = now.Add(input.TTL)
				updated, err := current.Update(current.Name, spec, now)
				if err != nil {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if err := tx.Update(ctx, updated, current.Version); err != nil {
					return entity.Resource{}, err
				}
				return updated, service.appendMutationRecords(
					ctx, tx, input.Principal, "renew_work_claim", updated,
				)
			}
			released, err := current.Transition(enum.StateCancelled, now)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, released, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return released, service.appendMutationRecords(
				ctx, tx, input.Principal, "release_work_claim", released,
			)
		},
	)
}

// ClaimOwnerGateDelivery выдаёт ограниченное право доставки следующей карточки.
func (service *Service) ClaimOwnerGateDelivery(
	ctx context.Context,
	input ClaimOwnerGateDeliveryInput,
) (ClaimOwnerGateDeliveryResult, error) {
	if err := authorize(input.Principal, permissionDeliverGate); err != nil {
		return ClaimOwnerGateDeliveryResult{}, err
	}
	if input.Principal.CallerWorkload != service.ownerGateDeliveryWorkload ||
		input.Principal.CallerSPIFFEID != service.ownerGateDeliverySPIFFEID {
		return ClaimOwnerGateDeliveryResult{}, errs.ErrPermissionDenied
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil {
		return ClaimOwnerGateDeliveryResult{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
	}{identity(input.Principal)})
	if err != nil {
		return ClaimOwnerGateDeliveryResult{}, errs.ErrInvalidInput
	}
	gate, err := service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"claim_owner_gate_delivery",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			now := service.now().UTC().Truncate(time.Microsecond)
			current, err := tx.NextOwnerGateDelivery(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				now,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			spec, ok := current.Spec.(entity.OwnerGateSpec)
			if !ok || current.Kind != enum.KindOwnerGate ||
				current.State != enum.StateWaitingOwner ||
				spec.MattermostPostID != "" ||
				spec.DeliveryWorkloadID != input.Principal.CallerWorkload ||
				spec.DeliverySPIFFEID != input.Principal.CallerSPIFFEID {
				return entity.Resource{}, errs.ErrStateConflict
			}
			nextVersion := current.Version + 1
			expiresAt := now.Add(service.turnLeaseDuration)
			token := service.leaseToken(
				current.ID,
				nextVersion,
				1,
				input.Principal.AuthorityGeneration,
				input.Principal.CallerWorkload,
				input.IdempotencyKey,
			)
			spec.DeliveryClaimTokenSHA256 = hashString(token)
			spec.DeliveryFence = nextVersion
			spec.DeliveryClaimExpiresAt = expiresAt
			updated, err := current.Update(current.Name, spec, now)
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
				"claim_owner_gate_delivery",
				updated,
			)
		},
	)
	if err != nil {
		return ClaimOwnerGateDeliveryResult{}, err
	}
	spec, ok := gate.Spec.(entity.OwnerGateSpec)
	if !ok || spec.DeliveryFence != gate.Version ||
		spec.DeliveryClaimExpiresAt.IsZero() {
		return ClaimOwnerGateDeliveryResult{}, errs.ErrInternal
	}
	return ClaimOwnerGateDeliveryResult{
		OwnerGate: gate,
		ClaimToken: service.leaseToken(
			gate.ID,
			gate.Version,
			1,
			input.Principal.AuthorityGeneration,
			input.Principal.CallerWorkload,
			input.IdempotencyKey,
		),
		ExpiresAt: spec.DeliveryClaimExpiresAt,
	}, nil
}

// RecordOwnerGateDelivery фиксирует точную привязку сообщения до ResolveOwnerGate.
func (service *Service) RecordOwnerGateDelivery(
	ctx context.Context,
	input RecordOwnerGateDeliveryInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionDeliverGate); err != nil {
		return entity.Resource{}, err
	}
	if input.Principal.CallerWorkload != service.ownerGateDeliveryWorkload ||
		input.Principal.CallerSPIFFEID != service.ownerGateDeliverySPIFFEID {
		return entity.Resource{}, errs.ErrPermissionDenied
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.OwnerGateID) != nil ||
		input.ExpectedVersion == 0 ||
		value.ValidateID(input.DeliveryID) != nil ||
		!validSHA256Text(input.DeliveryPayloadSHA256) ||
		len(input.DeliveryClaimToken) < 32 || len(input.DeliveryClaimToken) > 512 ||
		input.DeliveryFence == 0 ||
		!validRuntimeReference(input.MattermostPostID) ||
		!validRuntimeReference(input.MattermostChannelID) ||
		!validRuntimeReference(input.MattermostRootPostID) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Input    RecordOwnerGateDeliveryInput
	}{identity(input.Principal), input})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"record_owner_gate_delivery",
		requestHash,
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			gate, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.OwnerGateID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if gate.Kind != enum.KindOwnerGate ||
				gate.Version != input.ExpectedVersion ||
				gate.State != enum.StateWaitingOwner {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			spec, ok := gate.Spec.(entity.OwnerGateSpec)
			if !ok || spec.DeliveryID != input.DeliveryID ||
				spec.DeliveryPayloadSHA256 != input.DeliveryPayloadSHA256 ||
				spec.DeliveryWorkloadID != input.Principal.CallerWorkload ||
				spec.DeliverySPIFFEID != input.Principal.CallerSPIFFEID ||
				spec.DeliveryFence != input.DeliveryFence ||
				spec.DeliveryClaimTokenSHA256 != hashString(input.DeliveryClaimToken) ||
				!spec.DeliveryClaimExpiresAt.After(service.now()) ||
				spec.MattermostPostID != "" {
				return entity.Resource{}, errs.ErrStateConflict
			}
			spec.MattermostPostID = input.MattermostPostID
			spec.MattermostChannelID = input.MattermostChannelID
			spec.MattermostRootPostID = input.MattermostRootPostID
			spec.DeliveredAt = service.now().UTC().Truncate(time.Microsecond)
			updated, err := gate.Update(gate.Name, spec, spec.DeliveredAt)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, gate.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(
				ctx, tx, input.Principal, "record_owner_gate_delivery", updated,
			)
		},
	)
}

// GetRuntimeRevision предоставляет runtime-controller точное чтение закреплённой версии.
func (service *Service) GetRuntimeRevision(
	ctx context.Context,
	input GetRuntimeRevisionInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionReadRuntimeRevision); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateID(input.RuntimeRevisionID) != nil ||
		input.ExpectedVersion == 0 {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	revision, err := service.repository.Get(
		ctx,
		input.Principal.OrganizationID,
		input.Principal.ProjectID,
		input.RuntimeRevisionID,
		enum.KindRuntimeRevision,
	)
	if err != nil {
		return entity.Resource{}, err
	}
	if revision.Version != input.ExpectedVersion ||
		revision.State != enum.StateActive {
		return entity.Resource{}, errs.ErrNotFound
	}
	return revision, nil
}

// SearchMemory всегда выполняет полнотекстовый поиск, а векторное ранжирование
// использует только при точном происхождении проекции.
func (service *Service) SearchMemory(
	ctx context.Context,
	input SearchMemoryInput,
) ([]MemorySearchHit, error) {
	if err := authorize(input.Principal, permissionRead); err != nil {
		return nil, err
	}
	if input.Query != "" && (len(input.Query) < 2 || len(input.Query) > 256) ||
		input.Limit < 1 || input.Limit > 100 ||
		(input.AfterID != "" && value.ValidateID(input.AfterID) != nil) ||
		(input.Scope != "" && input.Scope != "PROJECT" && input.Scope != "ROLE") ||
		(input.RoleID != "" && value.ValidateID(input.RoleID) != nil) {
		return nil, errs.ErrInvalidInput
	}
	queryEmbedding := localMemoryProjection(input.Query)
	modelSHA256 := localMemoryProjectionModelSHA256()
	var hits []domainrepo.MemorySearchHit
	err := service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID,
			ActorID:        input.Principal.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			permissions, err := tx.ActorPermissions(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.Principal.ActorID,
			)
			if err != nil {
				return err
			}
			roleIDs, err := tx.ActorRoleIDs(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.Principal.ActorID,
			)
			if err != nil {
				return err
			}
			canReadProject := slices.Contains(permissions, "*") ||
				slices.Contains(permissions, permissionWriteProjectMemory)
			if input.Scope != "" {
				if err := authorizeMemoryScope(
					ctx, tx, input.Principal, input.Scope, input.RoleID,
				); err != nil {
					return err
				}
			}
			hits, err = tx.SearchMemory(ctx, domainrepo.MemorySearch{
				OrganizationID:      input.Principal.OrganizationID,
				ProjectID:           input.Principal.ProjectID,
				Scope:               input.Scope,
				RoleID:              input.RoleID,
				Query:               input.Query,
				QueryEmbedding:      queryEmbedding,
				ModelID:             localMemoryProjectionModelID,
				ModelRevision:       localMemoryProjectionRevision,
				ModelSHA256:         modelSHA256,
				AfterID:             input.AfterID,
				AfterTextRank:       input.AfterTextRank,
				AfterVectorDistance: input.AfterVectorDistance,
				AfterVectorUsed:     input.AfterVectorUsed,
				Limit:               input.Limit,
				CanReadProject:      canReadProject,
				ActorRoleIDs:        roleIDs,
			})
			return err
		},
	)
	if err != nil {
		return nil, err
	}
	result := make([]MemorySearchHit, 0, len(hits))
	for _, hit := range hits {
		result = append(result, MemorySearchHit{
			Resource:             hit.Resource,
			TextRank:             hit.TextRank,
			VectorDistance:       hit.VectorDistance,
			VectorProjectionUsed: hit.VectorProjectionUsed,
		})
	}
	return result, nil
}

// RecordMemoryEmbedding сохраняет только локальную перестраиваемую проекцию.
func (service *Service) RecordMemoryEmbedding(
	ctx context.Context,
	input RecordMemoryEmbeddingInput,
) (RecordMemoryEmbeddingResult, error) {
	if err := authorize(input.Principal, permissionIndexMemory); err != nil {
		return RecordMemoryEmbeddingResult{}, err
	}
	if input.Principal.CallerWorkload != service.memoryIndexerWorkload ||
		input.Principal.CallerSPIFFEID != service.memoryIndexerSPIFFEID {
		return RecordMemoryEmbeddingResult{}, errs.ErrPermissionDenied
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.MemoryRecordID) != nil ||
		input.ExpectedResourceVersion == 0 ||
		!validSHA256Text(input.ContentSHA256) {
		return RecordMemoryEmbeddingResult{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Input    RecordMemoryEmbeddingInput
	}{identity(input.Principal), input})
	if err != nil {
		return RecordMemoryEmbeddingResult{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result RecordMemoryEmbeddingResult
	err = service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID,
			ActorID:        input.Principal.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			receipt, receiptErr := tx.GetReceipt(
				ctx,
				input.Principal.OrganizationID,
				"record_memory_embedding",
				keyHash,
			)
			if receiptErr == nil {
				if receipt.RequestHash != requestHash ||
					json.Unmarshal(receipt.Payload, &result) != nil {
					return errs.ErrIdempotencyConflict
				}
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			record, err := tx.GetForUpdate(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.MemoryRecordID,
			)
			if err != nil {
				return err
			}
			spec, ok := record.Spec.(entity.MemoryRecordSpec)
			if !ok || record.Kind != enum.KindMemoryRecord ||
				record.State != enum.StateActive ||
				record.Version != input.ExpectedResourceVersion ||
				spec.ContentSHA256 != input.ContentSHA256 {
				return errs.ErrStateConflict
			}
			embedding := localMemoryProjection(spec.Title + " " + spec.Content)
			modelSHA256 := localMemoryProjectionModelSHA256()
			projectionDigest, err := canonicalHash(struct {
				ResourceID      string
				ResourceVersion uint64
				ContentSHA256   string
				ModelID         string
				ModelRevision   uint64
				ModelSHA256     string
				Embedding       []float32
			}{
				record.ID,
				record.Version,
				spec.ContentSHA256,
				localMemoryProjectionModelID,
				uint64(localMemoryProjectionRevision),
				modelSHA256,
				embedding,
			})
			if err != nil {
				return errs.ErrInternal
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			if err := tx.SaveMemoryProjection(ctx, domainrepo.MemoryProjection{
				ResourceID:       record.ID,
				OrganizationID:   record.OrganizationID,
				ProjectID:        record.ProjectID,
				ResourceVersion:  record.Version,
				ContentSHA256:    spec.ContentSHA256,
				ModelID:          localMemoryProjectionModelID,
				ModelRevision:    localMemoryProjectionRevision,
				ModelSHA256:      modelSHA256,
				Embedding:        embedding,
				ProjectionSHA256: projectionDigest,
				UpdatedAt:        now,
			}); err != nil {
				return err
			}
			result = RecordMemoryEmbeddingResult{
				MemoryRecordID:   record.ID,
				ResourceVersion:  record.Version,
				ProjectionSHA256: projectionDigest,
			}
			payload, err := json.Marshal(result)
			if err != nil {
				return errs.ErrInternal
			}
			if err := tx.AppendAudit(ctx, domainrepo.Audit{
				ID:              uuid.NewString(),
				OrganizationID:  record.OrganizationID,
				ProjectID:       record.ProjectID,
				ActorID:         input.Principal.ActorID,
				Action:          "record_memory_embedding",
				ResourceID:      record.ID,
				ResourceKind:    string(record.Kind),
				ResourceVersion: record.Version,
				Outcome:         "succeeded",
				CorrelationID:   input.Principal.CorrelationID,
				PolicyRevision:  input.Principal.PolicyRevision,
				OccurredAt:      now,
			}); err != nil {
				return err
			}
			return tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: record.OrganizationID,
				ProjectID:      record.ProjectID,
				Scope:          "record_memory_embedding",
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Result:         record,
				Payload:        payload,
				CreatedAt:      now,
			})
		},
	)
	return result, err
}

func localMemoryProjection(text string) []float32 {
	tokens := strings.Fields(strings.ToLower(text))
	if len(tokens) == 0 {
		return nil
	}
	projection := make([]float32, localMemoryProjectionDimensions)
	for _, token := range tokens {
		digest := sha256.Sum256([]byte(token))
		index := (uint16(digest[0])<<8 | uint16(digest[1])) %
			localMemoryProjectionDimensions
		delta := float32(1)
		if digest[2]&1 != 0 {
			delta = -1
		}
		projection[index] += delta
	}
	var squared float64
	for _, component := range projection {
		squared += float64(component * component)
	}
	if squared == 0 {
		return nil
	}
	norm := float32(math.Sqrt(squared))
	for index := range projection {
		projection[index] /= norm
	}
	return projection
}

func localMemoryProjectionModelSHA256() string {
	digest := sha256.Sum256([]byte(
		"mattercodex-local-token-hash:v1:sha256-index-sign:l2:dim-256",
	))
	return hex.EncodeToString(digest[:])
}
