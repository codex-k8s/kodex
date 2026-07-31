package resource

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

const (
	maximumWorkClaimTTL  = 24 * time.Hour
	maximumEmbeddingSize = 2000
)

// ManageSession создаёт и завершает SESSION без caller-selected credential binding.
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
			input.SessionID != "" || input.ExpectedVersion != 0 ||
			input.ArchiveRef != "" || input.ReasonCode != "" {
			return entity.Resource{}, errs.ErrInvalidInput
		}
	} else if value.ValidateID(input.SessionID) != nil ||
		input.ExpectedVersion == 0 || value.ValidateStableKey(input.ReasonCode) != nil ||
		input.Name != "" || input.RoleID != "" || input.ConversationID != "" {
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
					len(roleSpec.ProviderCredentialBindingIDs) != 1 {
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
				binding, err := tx.GetForUpdate(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					roleSpec.ProviderCredentialBindingIDs[0],
				)
				if err != nil {
					return entity.Resource{}, err
				}
				bindingSpec, ok := binding.Spec.(entity.CredentialBindingSpec)
				if !ok || binding.Kind != enum.KindCredentialBinding ||
					binding.State != enum.StateActive ||
					bindingSpec.Purpose != "provider-account" ||
					(!bindingSpec.ExpiresAt.IsZero() && !bindingSpec.ExpiresAt.After(now)) {
					return entity.Resource{}, errs.ErrStateConflict
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
					uuid.NewString(),
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
				if current.State != enum.StateArchived &&
					current.State != enum.StateCancelled {
					return entity.Resource{}, errs.ErrStateConflict
				}
				target = enum.StateDeletionPending
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

// ManageMemoryRecord применяет project/role capability и server-owned owner.
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

// ManageWorkClaim связывает claim с immutable runtime lineage.
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
					turnSpec.ProcessRunID != process.ID ||
					processSpec.RootSessionID != turnSpec.SessionID ||
					processSpec.RootTurnID != turn.ID ||
					processSpec.RootAttempt != turnSpec.Attempt {
					return entity.Resource{}, errs.ErrStateConflict
				}
				spec := entity.WorkClaimSpec{
					ProcessRunID: input.ProcessRunID,
					TurnID:       input.TurnID,
					Summary:      input.Summary,
					Domains:      input.Domains,
					ResourceKeys: input.ResourceKeys,
					OwnerActorID: input.Principal.ActorID,
					WorkloadID:   input.Principal.CallerWorkload,
					SessionID:    turnSpec.SessionID,
					Attempt:      turnSpec.Attempt,
					ExpiresAt:    now.Add(input.TTL),
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
			if current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			if input.Action == "RENEW" {
				if input.TTL < time.Minute || input.TTL > maximumWorkClaimTTL ||
					current.State != enum.StateActive {
					return entity.Resource{}, errs.ErrStateConflict
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

// RecordOwnerGateDelivery фиксирует exact post binding до ResolveOwnerGate.
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
				spec.MattermostPostID != "" {
				return entity.Resource{}, errs.ErrStateConflict
			}
			spec.DeliveryClaimTokenSHA256 = hashString(input.DeliveryClaimToken)
			spec.DeliveryFence = input.DeliveryFence
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

// GetRuntimeRevision предоставляет exact version-pinned read runtime-controller.
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

// SearchMemory выполняет FTS всегда и vector ranking только при exact projection provenance.
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
		(input.RoleID != "" && value.ValidateID(input.RoleID) != nil) ||
		len(input.QueryEmbedding) > maximumEmbeddingSize {
		return nil, errs.ErrInvalidInput
	}
	if len(input.QueryEmbedding) > 0 {
		if value.ValidateStableKey(input.EmbeddingModelID) != nil ||
			input.EmbeddingModelRevision == 0 ||
			!validSHA256Text(input.EmbeddingModelSHA256) {
			return nil, errs.ErrInvalidInput
		}
		for _, component := range input.QueryEmbedding {
			if math.IsNaN(float64(component)) || math.IsInf(float64(component), 0) {
				return nil, errs.ErrInvalidInput
			}
		}
	}
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
				QueryEmbedding:      input.QueryEmbedding,
				ModelID:             input.EmbeddingModelID,
				ModelRevision:       input.EmbeddingModelRevision,
				ModelSHA256:         input.EmbeddingModelSHA256,
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

// RecordMemoryEmbedding сохраняет только локальную перестраиваемую projection.
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
		!validSHA256Text(input.ContentSHA256) ||
		value.ValidateStableKey(input.ModelID) != nil ||
		input.ModelRevision == 0 || !validSHA256Text(input.ModelSHA256) ||
		len(input.Embedding) < 2 || len(input.Embedding) > maximumEmbeddingSize {
		return RecordMemoryEmbeddingResult{}, errs.ErrInvalidInput
	}
	for _, component := range input.Embedding {
		if math.IsNaN(float64(component)) || math.IsInf(float64(component), 0) {
			return RecordMemoryEmbeddingResult{}, errs.ErrInvalidInput
		}
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
				input.ModelID,
				input.ModelRevision,
				input.ModelSHA256,
				input.Embedding,
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
				ModelID:          input.ModelID,
				ModelRevision:    input.ModelRevision,
				ModelSHA256:      input.ModelSHA256,
				Embedding:        input.Embedding,
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
