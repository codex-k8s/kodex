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
			value.ValidateStableKey(input.AgentStableKey) != nil ||
			(input.ConversationID != "" && value.ValidateID(input.ConversationID) != nil) ||
			input.SessionID != "" || input.ExpectedVersion != 0 ||
			input.ArchiveRef != "" || input.ReasonCode != "" {
			return entity.Resource{}, errs.ErrInvalidInput
		}
	} else if value.ValidateID(input.SessionID) != nil ||
		input.ExpectedVersion == 0 || value.ValidateStableKey(input.ReasonCode) != nil ||
		input.Name != "" || input.AgentStableKey != "" || input.ConversationID != "" {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Action != "CREATE" && input.Action != "CLOSE" &&
		input.Action != "ARCHIVE" && input.Action != "CANCEL" &&
		input.Action != "CLEANUP" && input.Action != "RESTORE" {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Action == "ARCHIVE" && !validRuntimeReference(input.ArchiveRef) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Action != "ARCHIVE" && input.ArchiveRef != "" {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		Action          string
		SessionID       string
		ExpectedVersion uint64
		Name            string
		AgentStableKey  string
		ConversationID  string
		ArchiveRef      string
		ReasonCode      string
	}{
		input.Action, input.SessionID, input.ExpectedVersion, input.Name,
		input.AgentStableKey, input.ConversationID, input.ArchiveRef,
		input.ReasonCode,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if input.Action == "CREATE" {
		return service.withResourceReceipt(
			ctx, input.Principal, input.IdempotencyKey, "manage_session_CREATE",
			requestHash,
			func(tx domainrepo.Transaction) (entity.Resource, error) {
				now := service.now().UTC().Truncate(time.Microsecond)
				protected, ok := tx.(domainrepo.ProtectedTransaction)
				if !ok {
					return entity.Resource{}, errs.ErrInternal
				}
				workspace, workspaceSHA, err := lockActiveWorkspace(ctx, tx, input.Principal)
				if err != nil {
					return entity.Resource{}, err
				}
				agent, err := requireProtectedStable(ctx, protected, input.Principal,
					enum.KindAgent, input.AgentStableKey)
				if err != nil {
					return entity.Resource{}, err
				}
				agentSpec, ok := agent.Spec.(entity.AgentSpec)
				if !ok || agent.State != enum.StateActive || !agentSpec.Enabled {
					return entity.Resource{}, errs.ErrStateConflict
				}
				agentSHA, err := entity.ProjectionSHA256(agent)
				if err != nil {
					return entity.Resource{}, errs.ErrInternal
				}
				pool, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
					input.Principal.ProjectID, agentSpec.ProviderPoolID)
				if err != nil {
					return entity.Resource{}, err
				}
				poolSHA, err := entity.ProjectionSHA256(pool)
				if err != nil || pool.Kind != enum.KindProviderPool || pool.State != enum.StateActive ||
					pool.Version != agentSpec.ProviderPoolVersion || poolSHA != agentSpec.ProviderPoolSHA256 {
					return entity.Resource{}, errs.ErrStateConflict
				}
				resources, err := tx.ListSnapshotResources(ctx, input.Principal.OrganizationID, input.Principal.ProjectID)
				if err != nil {
					return entity.Resource{}, err
				}
				assignmentIDs := make([]string, 0, 1)
				for _, candidate := range resources {
					spec, specOK := candidate.Spec.(entity.AgentAssignmentSpec)
					if specOK && candidate.Kind == enum.KindAgentAssignment && candidate.State == enum.StateActive &&
						candidate.OwnerActorID == input.Principal.ActorID && spec.RootActorID == input.Principal.ActorID &&
						spec.AgentID == agent.ID && spec.WorkspaceID == input.Principal.ProjectID &&
						(input.ConversationID == "" && spec.RoomID == "" || spec.RoomID == input.ConversationID) {
						assignmentIDs = append(assignmentIDs, candidate.ID)
					}
				}
				slices.Sort(assignmentIDs)
				if len(assignmentIDs) == 0 {
					return entity.Resource{}, errs.ErrNotFound
				}
				if len(assignmentIDs) != 1 {
					return entity.Resource{}, errs.ErrStateConflict
				}
				assignment, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
					input.Principal.ProjectID, assignmentIDs[0])
				if err != nil {
					return entity.Resource{}, err
				}
				assignmentSpec, assignmentOK := assignment.Spec.(entity.AgentAssignmentSpec)
				assignmentSHA, digestErr := entity.ProjectionSHA256(assignment)
				if !assignmentOK || digestErr != nil || assignment.Kind != enum.KindAgentAssignment ||
					assignment.State != enum.StateActive || assignment.OwnerActorID != input.Principal.ActorID ||
					assignmentSpec.RootActorID != input.Principal.ActorID || assignmentSpec.AgentID != agent.ID ||
					assignmentSpec.AgentVersion != agent.Version || assignmentSpec.AgentSHA256 != agentSHA ||
					assignmentSpec.WorkspaceID != input.Principal.ProjectID ||
					assignmentSpec.WorkspaceVersion != workspace.Version ||
					assignmentSpec.WorkspaceSHA256 != workspaceSHA ||
					(input.ConversationID == "" && assignmentSpec.RoomID != "" ||
						input.ConversationID != "" && assignmentSpec.RoomID != input.ConversationID) {
					return entity.Resource{}, errs.ErrStateConflict
				}
				sessionID := uuid.NewString()
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
						AgentID: agent.ID, AgentVersion: agent.Version, AgentSHA256: agentSHA,
						ProviderPoolID: pool.ID, ProviderPoolVersion: pool.Version, ProviderPoolSHA256: poolSHA,
						AgentAssignmentID: assignment.ID, AgentAssignmentVersion: assignment.Version,
						AgentAssignmentSHA256: assignmentSHA,
						ConversationID:        input.ConversationID,
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
			},
		)
	}

	var locked lockedSessionLifecycle
	var replaySession entity.Resource
	return service.withValidatedResourceReceipt(
		ctx, input.Principal, input.IdempotencyKey,
		"manage_session_"+input.Action, requestHash,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			current, err := service.lockSessionLifecycleGraph(
				ctx, tx, input.Principal, input.SessionID,
			)
			if err != nil {
				return 0, err
			}
			locked = current
			if current.Session.OwnerActorID != input.Principal.ActorID {
				return 0, errs.ErrNotFound
			}
			live, err := tx.SessionHasLiveRuntimeExecution(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
				input.SessionID,
			)
			if err != nil {
				return 0, err
			}
			blocked, err := tx.IntegrationContinuationBlocksCleanup(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
				input.SessionID,
			)
			if err != nil {
				return 0, err
			}
			if live || blocked {
				return 0, errs.ErrStateConflict
			}
			if input.Action == "ARCHIVE" || input.Action == "CLEANUP" {
				if len(current.Graphs) != 0 {
					return 0, errs.ErrStateConflict
				}
			} else {
				for _, graph := range current.Graphs {
					if graph.Occurrence.ID != "" ||
						requireOwnerGraphRuntimeDisposition(
							graph, runtimeDispositionAbsent,
						) != nil {
						return 0, errs.ErrStateConflict
					}
				}
			}
			if current.Session.Version == input.ExpectedVersion {
				if _, err := sessionLifecycleTarget(current.Session, input); err != nil {
					return 0, err
				}
				return lifecycleReceiptApply, nil
			}
			if current.Session.Version == input.ExpectedVersion+1 &&
				sessionLifecycleResultMatches(current.Session, input) {
				replaySession = current.Session
				return lifecycleReceiptReplay, nil
			}
			return 0, errs.ErrVersionMismatch
		},
		func(_ domainrepo.Transaction, stored entity.Resource) error {
			return resourceReceiptMatchesCurrent(replaySession, stored)
		},
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return entity.Resource{}, err
			}
			if input.Action == "CLOSE" || input.Action == "CANCEL" {
				if err := service.cancelSessionGraphs(
					ctx, tx, input.Principal, locked.Graphs, input.ReasonCode, now,
				); err != nil {
					return entity.Resource{}, err
				}
			}
			target, err := sessionLifecycleTarget(locked.Session, input)
			if err != nil {
				return entity.Resource{}, err
			}
			spec, ok := locked.Session.Spec.(entity.SessionSpec)
			if !ok {
				return entity.Resource{}, errs.ErrInternal
			}
			if input.Action == "ARCHIVE" {
				spec.ArchiveRef = input.ArchiveRef
			}
			updated, err := locked.Session.ReplaceAndTransition(spec, target, now)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, locked.Session.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(
				ctx, tx, input.Principal, "session_"+input.Action, updated,
			)
		},
	)
}

// ManageConversationLifecycle материализует единственный transport-specific
// delete/restore/finalize path для server-owned Chat/Session.
func (service *Service) ManageConversationLifecycle(
	ctx context.Context,
	input ManageConversationLifecycleInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionConversationLifecycle); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.ResourceID) != nil ||
		(input.Kind != "CHANNEL" && input.Kind != "THREAD") ||
		(input.Action != "DELETE" && input.Action != "RESTORE" && input.Action != "FINALIZE") {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	resourceKind := enum.KindChat
	if input.Kind == "THREAD" {
		resourceKind = enum.KindSession
	}
	current, err := service.repository.GetIncludingDeleted(ctx, input.Principal.OrganizationID,
		input.Principal.ProjectID, input.ResourceID, resourceKind)
	if err != nil {
		return entity.Resource{}, err
	}
	if ownerBoundLifecycleKind(current.Kind) && current.OwnerActorID != input.Principal.ActorID {
		return entity.Resource{}, errs.ErrNotFound
	}
	if input.Kind == "CHANNEL" {
		target := enum.StateDeletionPending
		switch input.Action {
		case "RESTORE":
			target = enum.StateActive
		case "FINALIZE":
			target = enum.StateDeleted
		}
		transitionPrincipal := input.Principal
		transitionPrincipal.Permission = permissionTransition
		expectedVersion := current.Version
		if current.State == target {
			if expectedVersion <= 1 {
				return entity.Resource{}, errs.ErrStateConflict
			}
			expectedVersion--
		}
		return service.Transition(ctx, TransitionInput{Principal: transitionPrincipal,
			IdempotencyKey: input.IdempotencyKey, ResourceID: input.ResourceID,
			ExpectedVersion: expectedVersion, Target: target, ReasonCode: "mattermost-transport-lifecycle"})
	}
	managePrincipal := input.Principal
	managePrincipal.Permission = permissionManageSession
	manage := func(key, action string, version uint64) (entity.Resource, error) {
		return service.ManageSession(ctx, ManageSessionInput{Principal: managePrincipal,
			IdempotencyKey: key, Action: action, SessionID: input.ResourceID,
			ExpectedVersion: version, ReasonCode: "mattermost-transport-lifecycle"})
	}
	switch input.Action {
	case "DELETE":
		archived := current
		if current.State == enum.StateActive {
			archived, err = manage(uuid.NewSHA1(uuid.NameSpaceURL, []byte(input.IdempotencyKey+"\x00close")).String(),
				"CLOSE", current.Version)
			if err != nil {
				return entity.Resource{}, err
			}
		}
		expectedVersion := archived.Version
		if archived.State == enum.StateDeletionPending {
			if expectedVersion <= 1 {
				return entity.Resource{}, errs.ErrStateConflict
			}
			expectedVersion--
		}
		return manage(uuid.NewSHA1(uuid.NameSpaceURL, []byte(input.IdempotencyKey+"\x00pending")).String(),
			"CLEANUP", expectedVersion)
	case "RESTORE":
		expectedVersion := current.Version
		if current.State == enum.StateActive {
			if expectedVersion <= 1 {
				return entity.Resource{}, errs.ErrStateConflict
			}
			expectedVersion--
		}
		return manage(input.IdempotencyKey, "RESTORE", expectedVersion)
	case "FINALIZE":
		expectedVersion := current.Version
		if current.State == enum.StateDeleted {
			if expectedVersion <= 1 {
				return entity.Resource{}, errs.ErrStateConflict
			}
			expectedVersion--
		}
		return manage(input.IdempotencyKey, "CLEANUP", expectedVersion)
	default:
		return entity.Resource{}, errs.ErrInvalidInput
	}
}

func sessionLifecycleTarget(
	current entity.Resource,
	input ManageSessionInput,
) (enum.State, error) {
	switch input.Action {
	case "CLOSE", "ARCHIVE":
		if current.State != enum.StateActive {
			return "", errs.ErrStateConflict
		}
		return enum.StateArchived, nil
	case "CANCEL":
		if current.State != enum.StateActive {
			return "", errs.ErrStateConflict
		}
		return enum.StateCancelled, nil
	case "CLEANUP":
		if current.State == enum.StateArchived || current.State == enum.StateCancelled {
			return enum.StateDeletionPending, nil
		}
		if current.State == enum.StateDeletionPending {
			return enum.StateDeleted, nil
		}
	case "RESTORE":
		if current.State == enum.StateArchived || current.State == enum.StateDeletionPending {
			return enum.StateActive, nil
		}
	}
	return "", errs.ErrStateConflict
}

func sessionLifecycleResultMatches(current entity.Resource, input ManageSessionInput) bool {
	switch input.Action {
	case "CLOSE":
		return current.State == enum.StateArchived
	case "ARCHIVE":
		spec, ok := current.Spec.(entity.SessionSpec)
		return ok && current.State == enum.StateArchived && spec.ArchiveRef == input.ArchiveRef
	case "CANCEL":
		return current.State == enum.StateCancelled
	case "CLEANUP":
		return current.State == enum.StateDeletionPending || current.State == enum.StateDeleted
	case "RESTORE":
		return current.State == enum.StateActive
	}
	return false
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

func (service *Service) cancelSessionGraphs(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	graphs []lockedOwnerGraph,
	reason string,
	now time.Time,
) error {
	currentProcessTurns := make(map[string]entity.Resource, len(graphs))
	for _, graph := range graphs {
		if graph.Occurrence.ID != "" ||
			requireOwnerGraphRuntimeDisposition(graph, runtimeDispositionAbsent) != nil {
			return errs.ErrStateConflict
		}
		turnSpec, ok := graph.Turn.Spec.(entity.TurnSpec)
		if !ok || graph.Turn.State.Terminal() ||
			graph.Session.ID != turnSpec.SessionID {
			return errs.ErrStateConflict
		}
		cancelled, err := service.cancelTurnExecution(
			ctx, tx, principal, graph.Turn, reason, now,
		)
		if err != nil {
			return err
		}
		cancelledSpec := cancelled.Spec.(entity.TurnSpec)
		if cancelledSpec.ProcessRunID != "" {
			processSpec, processOK := graph.Process.Spec.(entity.ProcessRunSpec)
			current, currentErr := currentExecution(processSpec)
			if !processOK || currentErr != nil {
				return errs.ErrStateConflict
			}
			if current.TurnID == cancelled.ID && current.Attempt == cancelledSpec.Attempt {
				currentProcessTurns[cancelledSpec.ProcessRunID] = cancelled
			}
		}
	}
	orderedProcessIDs := make([]string, 0, len(currentProcessTurns))
	for processID := range currentProcessTurns {
		orderedProcessIDs = append(orderedProcessIDs, processID)
	}
	slices.Sort(orderedProcessIDs)
	for _, processID := range orderedProcessIDs {
		cancelled := currentProcessTurns[processID]
		cancelledSpec := cancelled.Spec.(entity.TurnSpec)
		if err := service.completeProcessFromTurn(
			ctx, tx, principal, cancelled, cancelledSpec,
		); err != nil {
			return err
		}
		process, err := tx.GetForUpdate(
			ctx, principal.OrganizationID, principal.ProjectID, processID,
		)
		if err != nil {
			return err
		}
		if process.Kind != enum.KindProcessRun || !process.State.Terminal() {
			return errs.ErrStateConflict
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

type memoryEligibility struct {
	CanReadProject bool
	RoleIDs        []string
}

func resolveMemoryEligibility(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
) (memoryEligibility, error) {
	roleIDs, err := tx.ActorRoleIDs(
		ctx, principal.OrganizationID, principal.ProjectID, principal.ActorID,
	)
	if err != nil {
		return memoryEligibility{}, err
	}
	return memoryEligibility{
		// Verified project authority уже доказывает членство; capability
		// публикации нужна только для записи, а не для чтения PROJECT scope.
		CanReadProject: principal.ProjectID != "",
		RoleIDs:        roleIDs,
	}, nil
}

func memoryResourceEligible(
	resource entity.Resource,
	eligibility memoryEligibility,
) bool {
	spec, ok := resource.Spec.(entity.MemoryRecordSpec)
	if !ok || resource.Kind != enum.KindMemoryRecord ||
		resource.State == enum.StateDeleted {
		return false
	}
	if spec.Scope == "PROJECT" {
		return eligibility.CanReadProject
	}
	return spec.Scope == "ROLE" && slices.Contains(eligibility.RoleIDs, spec.RoleID)
}

func (service *Service) searchEligibleMemory(
	ctx context.Context,
	principal value.Principal,
	search domainrepo.MemorySearch,
) ([]domainrepo.MemorySearchHit, error) {
	var hits []domainrepo.MemorySearchHit
	err := service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: principal.OrganizationID,
			ProjectID:      principal.ProjectID,
			ActorID:        principal.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			eligibility, err := resolveMemoryEligibility(ctx, tx, principal)
			if err != nil {
				return err
			}
			search.OrganizationID = principal.OrganizationID
			search.ProjectID = principal.ProjectID
			search.CanReadProject = eligibility.CanReadProject
			search.ActorRoleIDs = eligibility.RoleIDs
			hits, err = tx.SearchMemory(ctx, search)
			return err
		},
	)
	return hits, err
}

func (service *Service) getEligibleMemory(
	ctx context.Context,
	principal value.Principal,
	resourceID string,
) (entity.Resource, error) {
	var result entity.Resource
	err := service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: principal.OrganizationID,
			ProjectID:      principal.ProjectID,
			ActorID:        principal.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			eligibility, err := resolveMemoryEligibility(ctx, tx, principal)
			if err != nil {
				return err
			}
			resource, err := tx.GetForUpdate(
				ctx, principal.OrganizationID, principal.ProjectID, resourceID,
			)
			if err != nil {
				return err
			}
			if !memoryResourceEligible(resource, eligibility) ||
				resource.State == enum.StateDeleted {
				return errs.ErrNotFound
			}
			result = resource
			return nil
		},
	)
	return result, err
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
	requestHash, err := semanticCommandHash(input.Principal, struct {
		Action          string
		WorkClaimID     string
		ExpectedVersion uint64
		ProcessRunID    string
		TurnID          string
		Summary         string
		Domains         []string
		ResourceKeys    []string
		TTL             time.Duration
	}{
		input.Action, input.WorkClaimID, input.ExpectedVersion,
		input.ProcessRunID, input.TurnID, input.Summary,
		append([]string{}, input.Domains...),
		append([]string{}, input.ResourceKeys...), input.TTL,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	var receiptClaim entity.Resource
	var lockedGraph lockedOwnerGraph
	var workClaimNow time.Time
	return service.withValidatedResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"manage_work_claim_"+input.Action,
		requestHash,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			if input.Action == "CREATE" {
				graph, err := service.lockOwnerGraphByTurn(
					ctx, tx, input.Principal, input.TurnID,
				)
				if err != nil {
					return 0, err
				}
				if err := requireOwnerGraphRuntimeDisposition(
					graph, runtimeDispositionAbsent, runtimeDispositionNonterminal,
				); err != nil {
					return 0, err
				}
				turnSpec, ok := graph.Turn.Spec.(entity.TurnSpec)
				processSpec, processOK := graph.Process.Spec.(entity.ProcessRunSpec)
				current, currentErr := currentExecution(processSpec)
				if !ok || !processOK || currentErr != nil ||
					graph.Process.ID != input.ProcessRunID || graph.Process.State.Terminal() ||
					graph.Turn.State.Terminal() || turnSpec.ProcessRunID != graph.Process.ID ||
					!executionMatchesTurn(current, graph.Turn, turnSpec) {
					return 0, errs.ErrStateConflict
				}
				lockedGraph = graph
				workClaimNow, err = tx.CurrentTime(ctx)
				if err != nil {
					return 0, err
				}
				return lifecycleReceiptApplyOrReplay, nil
			}
			candidate, err := tx.Get(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
				input.WorkClaimID,
			)
			if err != nil {
				return 0, err
			}
			spec, ok := candidate.Spec.(entity.WorkClaimSpec)
			if !ok || candidate.Kind != enum.KindWorkClaim ||
				spec.OwnerActorID != input.Principal.ActorID ||
				spec.WorkloadID != input.Principal.CallerWorkload {
				return 0, errs.ErrNotFound
			}
			graph, err := service.lockOwnerGraphByTurn(
				ctx, tx, input.Principal, spec.TurnID,
			)
			if err != nil {
				return 0, err
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionAbsent, runtimeDispositionNonterminal,
			); err != nil {
				return 0, err
			}
			if spec.ProcessRunID != graph.Process.ID || spec.TurnID != graph.Turn.ID {
				return 0, errs.ErrStateConflict
			}
			lockedGraph = graph
			current, err := tx.GetForUpdate(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
				input.WorkClaimID,
			)
			if err != nil || current.Version != candidate.Version {
				if err != nil {
					return 0, err
				}
				return 0, errs.ErrStateConflict
			}
			currentSpec, ok := current.Spec.(entity.WorkClaimSpec)
			if !ok || current.Kind != enum.KindWorkClaim ||
				currentSpec.ProcessRunID != spec.ProcessRunID ||
				currentSpec.TurnID != spec.TurnID ||
				currentSpec.OwnerActorID != spec.OwnerActorID ||
				currentSpec.WorkloadID != spec.WorkloadID {
				return 0, errs.ErrStateConflict
			}
			workClaimNow, err = tx.CurrentTime(ctx)
			if err != nil {
				return 0, err
			}
			if current.Version == input.ExpectedVersion && current.State == enum.StateActive {
				if input.Action == "RENEW" {
					if err := requireUnexpiredWorkClaim(currentSpec, workClaimNow); err != nil {
						return 0, err
					}
				}
				receiptClaim = current
				return lifecycleReceiptApply, nil
			}
			if current.Version == input.ExpectedVersion+1 &&
				((input.Action == "RENEW" && current.State == enum.StateActive) ||
					(input.Action == "RELEASE" && current.State == enum.StateCancelled)) {
				if input.Action == "RENEW" {
					if err := requireUnexpiredWorkClaim(currentSpec, workClaimNow); err != nil {
						return 0, err
					}
				}
				receiptClaim = current
				return lifecycleReceiptReplay, nil
			}
			return 0, errs.ErrVersionMismatch
		},
		func(tx domainrepo.Transaction, stored entity.Resource) error {
			current, err := tx.GetForUpdate(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID, stored.ID,
			)
			if err != nil {
				return err
			}
			replayNow, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if receiptClaim.ID == "" {
				receiptClaim = current
			}
			storedSpec, storedOK := stored.Spec.(entity.WorkClaimSpec)
			currentSpec, currentOK := receiptClaim.Spec.(entity.WorkClaimSpec)
			if !storedOK || !currentOK || stored.Kind != enum.KindWorkClaim ||
				storedSpec.ProcessRunID != lockedGraph.Process.ID ||
				storedSpec.TurnID != lockedGraph.Turn.ID ||
				currentSpec.ProcessRunID != storedSpec.ProcessRunID ||
				currentSpec.TurnID != storedSpec.TurnID {
				return errs.ErrStateConflict
			}
			if input.Action == "CREATE" || input.Action == "RENEW" {
				if err := requireUnexpiredWorkClaim(storedSpec, replayNow); err != nil {
					return err
				}
				if err := requireUnexpiredWorkClaim(currentSpec, replayNow); err != nil {
					return err
				}
			}
			return resourceReceiptMatchesCurrent(receiptClaim, stored)
		},
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			now := workClaimNow
			if input.Action == "CREATE" {
				process := lockedGraph.Process
				turn := lockedGraph.Turn
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
				execution, err := currentExecution(processSpec)
				if err != nil || !executionMatchesTurn(execution, turn, turnSpec) {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if processSpec.ParentProcessRunID != "" &&
					processSpec.ContinuationTurnID == "" {
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
				if input.Principal.CallerWorkload == agentRunnerWorkload &&
					(input.Principal.AuthoritySource != "AGENT_SESSION" ||
						input.Principal.AuthorityReference != turn.ID ||
						input.Principal.AuthorityRevision != uint64(turnSpec.Attempt) ||
						input.Principal.AuthorityDigest !=
							turnSpec.EffectiveInputSHA256) {
					return entity.Resource{}, errs.ErrPermissionDenied
				}
				authorityGeneration := input.Principal.AuthorityGeneration
				if input.Principal.CallerWorkload == agentRunnerWorkload {
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
			if input.Principal.CallerWorkload == agentRunnerWorkload &&
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
				if err := requireUnexpiredWorkClaim(spec, now); err != nil {
					return entity.Resource{}, err
				}
				if err := service.validateActiveWorkClaimGraph(
					ctx, tx, input.Principal, lockedGraph, current, spec,
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

func requireUnexpiredWorkClaim(spec entity.WorkClaimSpec, now time.Time) error {
	if spec.ExpiresAt.IsZero() || !spec.ExpiresAt.After(now) {
		return errs.ErrStateConflict
	}
	return nil
}

func (service *Service) lockOwnerGateAfterGraph(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	candidate entity.Resource,
) (lockedOwnerGraph, entity.Resource, entity.OwnerGateSpec, error) {
	spec, ok := candidate.Spec.(entity.OwnerGateSpec)
	if !ok || candidate.Kind != enum.KindOwnerGate ||
		value.ValidateID(spec.TurnID) != nil {
		return lockedOwnerGraph{}, entity.Resource{}, entity.OwnerGateSpec{},
			errs.ErrStateConflict
	}
	graph, err := service.lockOwnerGraphByTurn(ctx, tx, principal, spec.TurnID)
	if err != nil {
		return lockedOwnerGraph{}, entity.Resource{}, entity.OwnerGateSpec{}, err
	}
	gate, err := tx.GetForUpdate(
		ctx, candidate.OrganizationID, candidate.ProjectID, candidate.ID,
	)
	if err != nil {
		return lockedOwnerGraph{}, entity.Resource{}, entity.OwnerGateSpec{}, err
	}
	lockedSpec, ok := gate.Spec.(entity.OwnerGateSpec)
	if !ok || gate.Kind != enum.KindOwnerGate || gate.ID != candidate.ID ||
		gate.Version != candidate.Version || lockedSpec.ProcessRunID != graph.Process.ID ||
		lockedSpec.SessionID != graph.Session.ID || lockedSpec.TurnID != graph.Turn.ID {
		return lockedOwnerGraph{}, entity.Resource{}, entity.OwnerGateSpec{},
			errs.ErrStateConflict
	}
	return graph, gate, lockedSpec, nil
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
	keyHash := hashString(input.IdempotencyKey)
	var gate entity.Resource
	mutated := false
	err = service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID,
		ProjectID:      input.Principal.ProjectID,
		ActorID:        input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		candidate, candidateErr := tx.OwnerGateByDeliveryClaimKey(
			ctx, input.Principal.OrganizationID, input.Principal.ProjectID, keyHash,
		)
		replayCandidate := candidateErr == nil
		if candidateErr != nil {
			if !errors.Is(candidateErr, errs.ErrNotFound) {
				return candidateErr
			}
			now, nowErr := tx.CurrentTime(ctx)
			if nowErr != nil {
				return nowErr
			}
			candidate, candidateErr = tx.NextOwnerGateDelivery(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID, now,
			)
			if candidateErr != nil {
				return candidateErr
			}
		}
		graph, current, currentSpec, lockErr := service.lockOwnerGateAfterGraph(
			ctx, tx, input.Principal, candidate,
		)
		if lockErr != nil {
			return lockErr
		}
		if currentSpec.DeliveryWorkloadID != input.Principal.CallerWorkload ||
			currentSpec.DeliverySPIFFEID != input.Principal.CallerSPIFFEID ||
			currentSpec.MattermostPostID != "" {
			return errs.ErrStateConflict
		}
		if err := requireClosedRuntimeConsistentWithTurn(graph); err != nil {
			return err
		}
		now, nowErr := tx.CurrentTime(ctx)
		if nowErr != nil {
			return nowErr
		}
		receipt, receiptErr := tx.GetReceipt(
			ctx, input.Principal.OrganizationID, "claim_owner_gate_delivery", keyHash,
		)
		if receiptErr == nil {
			if !replayCandidate || receipt.RequestHash != requestHash ||
				currentSpec.DeliveryClaimKeySHA256 != keyHash ||
				currentSpec.DeliveryFence != current.Version ||
				!currentSpec.DeliveryClaimExpiresAt.After(now) ||
				resourceReceiptMatchesCurrent(current, receipt.Result) != nil {
				return errs.ErrStateConflict
			}
			gate = current
			return nil
		}
		if !errors.Is(receiptErr, errs.ErrNotFound) {
			return receiptErr
		}
		if replayCandidate || current.State != enum.StateWaitingOwner ||
			currentSpec.DeliveryClaimKeySHA256 != "" ||
			(currentSpec.DeliveryClaimTokenSHA256 != "" &&
				currentSpec.DeliveryClaimExpiresAt.After(now)) {
			return errs.ErrStateConflict
		}
		nextVersion := current.Version + 1
		expiresAt := now.Add(service.turnLeaseDuration)
		if currentSpec.ExpiresAt.Before(expiresAt) {
			expiresAt = currentSpec.ExpiresAt
		}
		if !expiresAt.After(now) {
			return errs.ErrStateConflict
		}
		token := service.leaseToken(
			current.ID, nextVersion, 1, input.Principal.AuthorityGeneration,
			input.Principal.CallerWorkload, input.IdempotencyKey,
		)
		currentSpec.DeliveryClaimTokenSHA256 = hashString(token)
		currentSpec.DeliveryClaimKeySHA256 = keyHash
		currentSpec.DeliveryFence = nextVersion
		currentSpec.DeliveryClaimExpiresAt = expiresAt
		updated, updateErr := current.Update(current.Name, currentSpec, now)
		if updateErr != nil {
			return errs.ErrStateConflict
		}
		if updateErr = tx.Update(ctx, updated, current.Version); updateErr != nil {
			return updateErr
		}
		if updateErr = service.appendMutationRecords(
			ctx, tx, input.Principal, "claim_owner_gate_delivery", updated,
		); updateErr != nil {
			return updateErr
		}
		if updateErr = tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID,
			Scope:          "claim_owner_gate_delivery",
			KeyHash:        keyHash,
			RequestHash:    requestHash,
			Result:         updated,
			CreatedAt:      now,
		}); updateErr != nil {
			return updateErr
		}
		gate = updated
		mutated = true
		return nil
	})
	if err != nil {
		return ClaimOwnerGateDeliveryResult{}, err
	}
	if mutated {
		service.observer.ObserveMutation(enum.KindOwnerGate, "claim_owner_gate_delivery")
	}
	spec, ok := gate.Spec.(entity.OwnerGateSpec)
	if !ok || spec.DeliveryFence != gate.Version ||
		spec.DeliveryClaimKeySHA256 != keyHash ||
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
		!validRuntimeReference(input.MattermostRootPostID) ||
		!validSHA256Text(input.ProviderReceiptSHA256) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := semanticCommandHash(input.Principal, struct {
		OwnerGateID           string
		ExpectedVersion       uint64
		DeliveryID            string
		DeliveryPayloadSHA256 string
		DeliveryClaimSHA256   string
		DeliveryFence         uint64
		MattermostPostID      string
		MattermostChannelID   string
		MattermostRootPostID  string
		ProviderReceiptSHA256 string
	}{
		input.OwnerGateID, input.ExpectedVersion, input.DeliveryID,
		input.DeliveryPayloadSHA256, hashString(input.DeliveryClaimToken),
		input.DeliveryFence, input.MattermostPostID, input.MattermostChannelID,
		input.MattermostRootPostID, input.ProviderReceiptSHA256,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	var lockedGate entity.Resource
	var lockedGateSpec entity.OwnerGateSpec
	var lockedNow time.Time
	return service.withValidatedResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"record_owner_gate_delivery",
		requestHash,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			candidate, err := tx.Get(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
				input.OwnerGateID,
			)
			if err != nil {
				return 0, err
			}
			graph, gate, spec, err := service.lockOwnerGateAfterGraph(
				ctx, tx, input.Principal, candidate,
			)
			if err != nil {
				return 0, err
			}
			if err := requireClosedRuntimeConsistentWithTurn(graph); err != nil {
				return 0, err
			}
			lockedNow, err = tx.CurrentTime(ctx)
			if err != nil {
				return 0, err
			}
			if gate.State != enum.StateWaitingOwner || spec.DeliveryID != input.DeliveryID ||
				spec.DeliveryPayloadSHA256 != input.DeliveryPayloadSHA256 ||
				spec.DeliveryWorkloadID != input.Principal.CallerWorkload ||
				spec.DeliverySPIFFEID != input.Principal.CallerSPIFFEID ||
				spec.DeliveryFence != input.DeliveryFence ||
				spec.DeliveryClaimTokenSHA256 != hashString(input.DeliveryClaimToken) ||
				!validSHA256Text(spec.DeliveryClaimKeySHA256) {
				return 0, errs.ErrStateConflict
			}
			lockedGate, lockedGateSpec = gate, spec
			if gate.Version == input.ExpectedVersion && spec.MattermostPostID == "" &&
				spec.DeliveryClaimExpiresAt.After(lockedNow) &&
				spec.ExpiresAt.After(lockedNow) {
				return lifecycleReceiptApply, nil
			}
			if gate.Version == input.ExpectedVersion+1 &&
				spec.MattermostPostID == input.MattermostPostID &&
				spec.MattermostChannelID == input.MattermostChannelID &&
				spec.MattermostRootPostID == input.MattermostRootPostID &&
				spec.DeliveryProviderReceiptSHA256 == input.ProviderReceiptSHA256 &&
				!spec.DeliveredAt.IsZero() {
				return lifecycleReceiptReplay, nil
			}
			return 0, errs.ErrVersionMismatch
		},
		func(_ domainrepo.Transaction, stored entity.Resource) error {
			return resourceReceiptMatchesCurrent(lockedGate, stored)
		},
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			gate, spec := lockedGate, lockedGateSpec
			spec.MattermostPostID = input.MattermostPostID
			spec.MattermostChannelID = input.MattermostChannelID
			spec.MattermostRootPostID = input.MattermostRootPostID
			spec.DeliveryProviderReceiptSHA256 = input.ProviderReceiptSHA256
			spec.DeliveredAt = lockedNow
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
	hits, err := service.searchEligibleMemory(
		ctx,
		input.Principal,
		domainrepo.MemorySearch{
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
			States:              []enum.State{enum.StateActive},
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
