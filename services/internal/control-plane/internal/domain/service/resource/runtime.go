package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

var scheduleParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)
var publicProviderAccountNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,47}[a-z0-9])?$`)

func validUniqueRuntimeIDs(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, identifier := range values {
		if value.ValidateID(identifier) != nil {
			return false
		}
		if _, duplicate := seen[identifier]; duplicate {
			return false
		}
		seen[identifier] = struct{}{}
	}
	return true
}

// EnqueueTurn атомарно увеличивает последовательность сессии и создаёт точную попытку.
func (service *Service) EnqueueTurn(
	ctx context.Context,
	input EnqueueTurnInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionEnqueueTurn); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.SessionID) != nil ||
		!validRuntimeReference(input.SourceRef) ||
		value.ValidateID(input.PromptArtifactID) != nil ||
		(input.ProcessRunID != "" && value.ValidateID(input.ProcessRunID) != nil) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	if len(input.InputArtifactIDs) > 32 || !validUniqueRuntimeIDs(input.InputArtifactIDs) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity         commandIdentity
		SessionID        string
		SourceRef        string
		PromptArtifactID string
		ProcessRunID     string
		InputArtifactIDs []string
	}{
		identity(input.Principal),
		input.SessionID,
		input.SourceRef,
		input.PromptArtifactID,
		input.ProcessRunID,
		slices.Clone(input.InputArtifactIDs),
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	var enqueueSession entity.Resource
	var enqueueProcessGraph lockedOwnerGraph
	var enqueueDelegated bool
	return service.withValidatedResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"enqueue_turn",
		requestHash,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			extraSessions := []string{input.SessionID}
			if input.ProcessRunID == "" {
				locked, err := service.lockOwnerGraphSet(
					ctx, tx, input.Principal, nil, extraSessions,
				)
				if err != nil {
					return 0, err
				}
				session, ok := locked.Sessions[input.SessionID]
				if !ok || session.Kind != enum.KindSession ||
					session.State != enum.StateActive ||
					session.OwnerActorID != input.Principal.ActorID {
					return 0, errs.ErrStateConflict
				}
				enqueueSession = session
				return lifecycleReceiptApplyOrReplay, nil
			}
			candidate, err := tx.Get(
				ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, input.ProcessRunID,
			)
			if err != nil {
				return 0, err
			}
			candidateSpec, ok := candidate.Spec.(entity.ProcessRunSpec)
			candidateCurrent, currentErr := currentExecution(candidateSpec)
			if !ok || currentErr != nil || candidate.Kind != enum.KindProcessRun ||
				candidate.State != enum.StateRunning ||
				candidateSpec.RootInitiatorActorID != input.Principal.ActorID {
				return 0, errs.ErrStateConflict
			}
			locked, err := service.lockOwnerGraphSet(
				ctx, tx, input.Principal, []string{candidateCurrent.TurnID}, extraSessions,
			)
			if err != nil {
				return 0, err
			}
			graph, ok := locked.ByTurn[candidateCurrent.TurnID]
			if !ok || graph.Process.ID != input.ProcessRunID ||
				graph.Process.Version != candidate.Version {
				return 0, errs.ErrStateConflict
			}
			processSpec, ok := graph.Process.Spec.(entity.ProcessRunSpec)
			lockedCurrent, lockedCurrentErr := currentExecution(processSpec)
			if !ok || lockedCurrentErr != nil || lockedCurrent != candidateCurrent {
				return 0, errs.ErrStateConflict
			}
			session, ok := locked.Sessions[input.SessionID]
			if !ok || session.Kind != enum.KindSession ||
				session.State != enum.StateActive ||
				session.OwnerActorID != input.Principal.ActorID {
				return 0, errs.ErrStateConflict
			}
			delegated := input.Principal.AuthoritySource == "AGENT_SESSION"
			if delegated {
				turnSpec, turnOK := graph.Turn.Spec.(entity.TurnSpec)
				if !turnOK || graph.Turn.ID != input.Principal.AuthorityReference ||
					turnSpec.Attempt != uint32(input.Principal.AuthorityRevision) ||
					turnSpec.EffectiveInputSHA256 != input.Principal.AuthorityDigest ||
					input.Principal.AuthorityGrantGeneration == 0 {
					return 0, errs.ErrPermissionDenied
				}
				if err := requireOwnerGraphRuntimeDisposition(
					graph, runtimeDispositionAbsent, runtimeDispositionNonterminal,
				); err != nil {
					return 0, err
				}
			} else if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionAbsent,
			); err != nil {
				return 0, err
			}
			enqueueSession = session
			enqueueProcessGraph = graph
			enqueueDelegated = delegated
			return lifecycleReceiptApplyOrReplay, nil
		},
		func(_ domainrepo.Transaction, stored entity.Resource) error {
			storedSpec, ok := stored.Spec.(entity.TurnSpec)
			if !ok || stored.Kind != enum.KindTurn ||
				stored.OwnerActorID != input.Principal.ActorID ||
				storedSpec.SessionID != enqueueSession.ID ||
				storedSpec.ProcessRunID != input.ProcessRunID {
				return errs.ErrStateConflict
			}
			return nil
		},
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			session := enqueueSession
			sessionSpec, ok := session.Spec.(entity.SessionSpec)
			if !ok || session.Kind != enum.KindSession ||
				session.State != enum.StateActive ||
				session.OwnerActorID != input.Principal.ActorID {
				return entity.Resource{}, errs.ErrStateConflict
			}
			var delegation domainrepo.DelegationEdge
			delegated := input.ProcessRunID != "" && enqueueDelegated
			if !delegated {
				roleIDs, err := tx.ActorRoleIDs(
					ctx,
					input.Principal.OrganizationID,
					input.Principal.ProjectID,
					input.Principal.ActorID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
				if !slices.Contains(roleIDs, sessionSpec.AgentID) {
					return entity.Resource{}, errs.ErrNotFound
				}
			}
			if sessionSpec.LastTurnSequence == ^uint64(0) {
				return entity.Resource{}, errs.ErrStateConflict
			}
			promptArtifact, err := service.requireCleanArtifact(
				ctx,
				tx,
				input.Principal,
				input.PromptArtifactID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			inputArtifacts := make([]entity.EffectiveArtifactRef, 0, len(input.InputArtifactIDs))
			for index, artifactID := range input.InputArtifactIDs {
				artifact, spec, artifactErr := service.requireCleanArtifactResource(ctx, tx, input.Principal, artifactID)
				if artifactErr != nil || artifact.ParentID != input.SessionID || spec.Direction != "INPUT" {
					if artifactErr != nil {
						return entity.Resource{}, artifactErr
					}
					return entity.Resource{}, errs.ErrStateConflict
				}
				inputArtifacts = append(inputArtifacts, entity.EffectiveArtifactRef{ArtifactID: artifact.ID,
					Version: artifact.Version, SHA256: spec.SHA256,
					RelativePath: fmt.Sprintf("inputs/%04d-%s", index+1, artifact.ID),
					MediaType:    spec.MediaType, SizeBytes: spec.SizeBytes})
			}
			if input.ProcessRunID != "" {
				process := enqueueProcessGraph.Process
				processSpec, ok := process.Spec.(entity.ProcessRunSpec)
				if !ok || process.Kind != enum.KindProcessRun ||
					process.State != enum.StateRunning ||
					processSpec.RootInitiatorActorID != input.Principal.ActorID ||
					value.ValidateID(processSpec.RuntimeRevisionID) != nil {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if delegated {
					sourceTurn := enqueueProcessGraph.Turn
					sourceSpec, ok := sourceTurn.Spec.(entity.TurnSpec)
					if !ok || sourceTurn.Kind != enum.KindTurn || sourceTurn.State.Terminal() ||
						sourceTurn.OwnerActorID != processSpec.RootInitiatorActorID ||
						sourceSpec.ProcessRunID != process.ID ||
						sourceSpec.Attempt != uint32(input.Principal.AuthorityRevision) ||
						sourceSpec.EffectiveInputSHA256 != input.Principal.AuthorityDigest ||
						input.Principal.AuthorityGrantGeneration == 0 {
						return entity.Resource{}, errs.ErrPermissionDenied
					}
					sourceSession := enqueueProcessGraph.Session
					sourceSessionSpec, ok := sourceSession.Spec.(entity.SessionSpec)
					if !ok || sourceSession.Kind != enum.KindSession ||
						sourceSession.State != enum.StateActive ||
						sourceSession.OwnerActorID != processSpec.RootInitiatorActorID {
						return entity.Resource{}, errs.ErrStateConflict
					}
					sourceRole, err := tx.GetForUpdate(
						ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
						sourceSessionSpec.AgentID,
					)
					if err != nil {
						return entity.Resource{}, err
					}
					sourceRoleSpec, ok := sourceRole.Spec.(entity.RoleSpec)
					if !ok || sourceRole.Kind != enum.KindRole ||
						sourceRole.State != enum.StateActive ||
						!slices.Contains(sourceRoleSpec.AllowedTargetRoleIDs, sessionSpec.AgentID) {
						return entity.Resource{}, errs.ErrPermissionDenied
					}
					delegation = domainrepo.DelegationEdge{
						ID: uuid.NewString(), OrganizationID: process.OrganizationID,
						ProjectID: process.ProjectID, ParentProcessRunID: process.ID,
						SourceSessionID: sourceSession.ID, SourceTurnID: sourceTurn.ID,
						SourceAttempt:     sourceSpec.Attempt,
						SourceInputSHA256: sourceSpec.EffectiveInputSHA256,
						TargetSessionID:   session.ID, TargetRoleID: sessionSpec.AgentID,
						RootInitiatorActorID: processSpec.RootInitiatorActorID,
						GrantGeneration:      input.Principal.AuthorityGrantGeneration,
					}
				} else if processSpec.RootSessionID != session.ID {
					return entity.Resource{}, errs.ErrStateConflict
				}
			}
			// Каждый ход получает свежий server-resolved effective снимок.
			// Revision процесса остаётся связью родословной, но не переиспользуется
			// как authority для нового исполнения.
			runtimeRevision, err := service.createRuntimeRevision(
				ctx,
				tx,
				input.Principal,
				session,
				sessionSpec,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			sessionSpec.LastTurnSequence++
			now := service.now().UTC().Truncate(time.Microsecond)
			updatedSession, err := session.Update(session.Name, sessionSpec, now)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updatedSession, session.Version); err != nil {
				return entity.Resource{}, err
			}
			if err := service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"enqueue_turn_session",
				updatedSession,
			); err != nil {
				return entity.Resource{}, err
			}
			turn, err := entity.New(
				turnIDFromIdempotency(input.IdempotencyKey),
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.SessionID,
				input.Principal.ActorID,
				enum.KindTurn,
				"Turn "+input.SessionID,
				entity.TurnSpec{
					SessionID:         input.SessionID,
					Sequence:          sessionSpec.LastTurnSequence,
					SourceRef:         input.SourceRef,
					PromptArtifactID:  input.PromptArtifactID,
					ProcessRunID:      input.ProcessRunID,
					RuntimeRevisionID: runtimeRevision.ID,
					Attempt:           1,
					EffectiveInputSHA256: hashRuntimeInput(
						input.SourceRef,
						promptArtifact.SHA256,
						runtimeRevision.Spec.(entity.RuntimeRevisionSpec).ManifestSHA256,
						input.ProcessRunID,
					),
					InputArtifacts: inputArtifacts,
				},
				now,
			)
			if err != nil {
				return entity.Resource{}, errs.ErrInvalidInput
			}
			if err := service.validateReferences(ctx, tx, turn); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.Insert(ctx, turn); err != nil {
				return entity.Resource{}, err
			}
			turnSpec := turn.Spec.(entity.TurnSpec)
			if delegation.ID != "" {
				delegation.TargetTurnID = turn.ID
				delegation.TargetAttempt = turnSpec.Attempt
				delegation.TargetInputSHA256 = turnSpec.EffectiveInputSHA256
				delegation.CreatedAt = now
				if err := tx.SaveDelegationEdge(ctx, delegation); err != nil {
					return entity.Resource{}, err
				}
			}
			if err := tx.SaveTurnAttempt(ctx, domainrepo.TurnAttempt{
				TurnID:              turn.ID,
				Attempt:             turnSpec.Attempt,
				WorkloadID:          "unassigned",
				AuthorityGeneration: input.Principal.AuthorityGeneration,
				State:               "QUEUED",
				InputSHA256:         turnSpec.EffectiveInputSHA256,
				LeaseFence:          turn.Version,
				StartedAt:           now,
			}); err != nil {
				return entity.Resource{}, err
			}
			return turn, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"enqueue_turn",
				turn,
			)
		},
	)
}

func turnIDFromIdempotency(idempotencyKey string) string {
	namespace, err := uuid.Parse(idempotencyKey)
	if err != nil {
		namespace = uuid.NameSpaceURL
	}
	return uuid.NewSHA1(namespace, []byte("control-plane-turn")).String()
}

// ClaimTurn выдаёт точной рабочей нагрузке одну ограниченную аренду.
func (service *Service) ClaimTurn(
	ctx context.Context,
	input ClaimTurnInput,
) (ClaimTurnResult, error) {
	if err := authorize(input.Principal, permissionClaimTurn); err != nil {
		return ClaimTurnResult{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		input.Principal.CallerWorkload != agentRunnerWorkload ||
		input.Principal.CallerSPIFFEID != agentRunnerSPIFFEID ||
		input.Principal.AuthoritySource != "AGENT_SESSION" ||
		value.ValidateID(input.Principal.AuthorityReference) != nil ||
		input.Principal.AuthorityRevision == 0 ||
		input.Principal.AuthorityGrantGeneration == 0 ||
		!validSHA256Text(input.Principal.AuthorityDigest) {
		return ClaimTurnResult{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
	}{identity(input.Principal)})
	if err != nil {
		return ClaimTurnResult{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result ClaimTurnResult
	mutated := false
	recovered := false
	err = service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID,
			ActorID:        input.Principal.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			authorityGraph, err := service.lockOwnerGraphByTurn(
				ctx, tx, input.Principal, input.Principal.AuthorityReference,
			)
			if err != nil {
				return err
			}
			if err := requireOwnerGraphRuntimeDisposition(
				authorityGraph, runtimeDispositionAbsent, runtimeDispositionNonterminal,
			); err != nil {
				return err
			}
			authorityTurn := authorityGraph.Turn
			authoritySpec, ok := authorityTurn.Spec.(entity.TurnSpec)
			if authorityGraph.Runtime != nil &&
				(authorityGraph.Runtime.State != "PENDING" || authorityGraph.Runtime.Version != 1 ||
					authorityGraph.Runtime.Fence != 1 || authorityGraph.Runtime.TurnID != authorityTurn.ID ||
					authorityGraph.Runtime.Attempt != authoritySpec.Attempt ||
					authorityGraph.Runtime.GrantGeneration != input.Principal.AuthorityGrantGeneration ||
					authorityGraph.Runtime.ImmutableInputSHA256 != input.Principal.AuthorityDigest) {
				return errs.ErrStateConflict
			}
			if !ok || authorityTurn.Kind != enum.KindTurn ||
				authorityTurn.OwnerActorID != input.Principal.ActorID ||
				uint64(authoritySpec.Attempt) != input.Principal.AuthorityRevision ||
				authoritySpec.EffectiveInputSHA256 != input.Principal.AuthorityDigest ||
				(authorityTurn.State != enum.StateQueued &&
					authorityTurn.State != enum.StateClaimed) {
				return errs.ErrPermissionDenied
			}
			if err := requireCurrentTurnBinding(authorityGraph); err != nil {
				return err
			}
			var authorityLease domainrepo.TurnLease
			if authorityTurn.State == enum.StateClaimed {
				authorityLease, err = tx.GetTurnLeaseForUpdate(ctx, authorityTurn.ID)
				if err != nil || authorityLease.Attempt != authoritySpec.Attempt ||
					authorityLease.AuthorityGeneration !=
						input.Principal.AuthorityGrantGeneration ||
					authorityLease.WorkloadID != input.Principal.CallerWorkload ||
					authorityLease.Fence != authorityTurn.Version {
					if err != nil {
						return err
					}
					return errs.ErrStateConflict
				}
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			receipt, receiptErr := tx.GetReceipt(
				ctx,
				input.Principal.OrganizationID,
				"claim_turn",
				keyHash,
			)
			if receiptErr == nil {
				if receipt.RequestHash != requestHash ||
					authorityTurn.State != enum.StateClaimed ||
					!authorityLease.ExpiresAt.After(now) ||
					resourceReceiptMatchesCurrent(authorityTurn, receipt.Result) != nil {
					return errs.ErrIdempotencyConflict
				}
				var payload claimTurnReceipt
				if json.Unmarshal(receipt.Payload, &payload) != nil ||
					payload.LeaseExpiresAt.IsZero() ||
					!payload.LeaseExpiresAt.Equal(authorityLease.ExpiresAt) ||
					payload.Attempt != authorityLease.Attempt ||
					payload.AuthorityGeneration != authorityLease.AuthorityGeneration {
					return errs.ErrInternal
				}
				leaseToken := service.leaseToken(
					authorityTurn.ID, authorityTurn.Version, payload.Attempt,
					payload.AuthorityGeneration, input.Principal.CallerWorkload,
					input.IdempotencyKey,
				)
				if authorityLease.TokenHash != hashString(leaseToken) {
					return errs.ErrStateConflict
				}
				result = ClaimTurnResult{
					Turn:                authorityTurn,
					LeaseToken:          leaseToken,
					LeaseExpiresAt:      payload.LeaseExpiresAt,
					Attempt:             payload.Attempt,
					AuthorityGeneration: payload.AuthorityGeneration,
				}
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			expired, err := tx.ExpiredClaimedTurnCandidates(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.Principal.AuthorityReference,
				32,
				now,
			)
			if err != nil {
				return err
			}
			for _, stale := range expired {
				// ExpiredClaimedTurnCandidates выполняет только unlocked discovery.
				// Перед эффектом весь owner graph берётся в каноническом порядке и
				// lease/deadline повторно проверяются под locks.
				graph, graphErr := service.lockOwnerGraphByTurn(
					ctx, tx, input.Principal, stale.Turn.ID,
				)
				if graphErr != nil {
					return graphErr
				}
				if err := requireOwnerGraphRuntimeDisposition(
					graph, runtimeDispositionAbsent,
				); err != nil {
					return err
				}
				lockedLease, leaseErr := tx.GetTurnLeaseForUpdate(ctx, graph.Turn.ID)
				if leaseErr != nil {
					return leaseErr
				}
				recoveryNow, err := tx.CurrentTime(ctx)
				if err != nil {
					return err
				}
				staleSpec, ok := graph.Turn.Spec.(entity.TurnSpec)
				if !ok || graph.Turn.State != enum.StateClaimed ||
					graph.Turn.Version != stale.Turn.Version ||
					staleSpec.Attempt != lockedLease.Attempt ||
					lockedLease.AuthorityGeneration == 0 ||
					lockedLease.ExpiresAt.After(recoveryNow) ||
					staleSpec.Attempt >= 100 {
					return errs.ErrStateConflict
				}
				stale.Turn = graph.Turn
				stale.Lease = lockedLease
				if err := tx.FinishTurnAttempt(ctx, domainrepo.TurnAttempt{
					TurnID:              stale.Turn.ID,
					Attempt:             staleSpec.Attempt,
					WorkloadID:          stale.Lease.WorkloadID,
					AuthorityGeneration: stale.Lease.AuthorityGeneration,
					State:               "EXPIRED",
					InputSHA256:         staleSpec.EffectiveInputSHA256,
					LeaseFence:          stale.Lease.Fence,
					FinishedAt:          recoveryNow,
					Outcome:             "lease_expired",
				}); err != nil {
					return err
				}
				if err := service.revokeExecutionClaims(
					ctx, tx, input.Principal, staleSpec.ProcessRunID, stale.Turn.ID,
					"lease_expired", recoveryNow,
				); err != nil {
					return err
				}
				requeued, staleSpec, err := service.prepareRetriedExecution(
					ctx, tx, input.Principal, graph, staleSpec, recoveryNow,
				)
				if err != nil {
					return errs.ErrStateConflict
				}
				if err := tx.Update(ctx, requeued, stale.Turn.Version); err != nil {
					return err
				}
				if err := tx.DeleteTurnLease(
					ctx,
					stale.Turn.ID,
					stale.Lease.Fence,
				); err != nil {
					return err
				}
				if err := tx.SaveTurnAttempt(ctx, domainrepo.TurnAttempt{
					TurnID:              requeued.ID,
					Attempt:             staleSpec.Attempt,
					WorkloadID:          "unassigned",
					AuthorityGeneration: input.Principal.AuthorityGeneration,
					State:               "QUEUED",
					InputSHA256:         staleSpec.EffectiveInputSHA256,
					LeaseFence:          requeued.Version,
					StartedAt:           recoveryNow,
				}); err != nil {
					return err
				}
				if err := service.appendMutationRecords(
					ctx,
					tx,
					input.Principal,
					"requeue_expired_turn",
					requeued,
				); err != nil {
					return err
				}
				recovered = true
			}
			if recovered {
				// Новый immutable tuple должен попасть в outbox и authority issuer
				// до следующего ClaimTurn. Старое proof не может сразу заявить retry.
				mutated = true
				return nil
			}
			turn, err := tx.NextQueuedTurn(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.Principal.AuthorityReference,
			)
			if err != nil {
				return err
			}
			if turn.ID != authorityGraph.Turn.ID ||
				turn.Version != authorityGraph.Turn.Version ||
				turn.State != authorityGraph.Turn.State {
				return errs.ErrStateConflict
			}
			claimNow, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			claimed, err := turn.Transition(enum.StateClaimed, claimNow)
			if err != nil {
				return errs.ErrStateConflict
			}
			turnSpec, ok := claimed.Spec.(entity.TurnSpec)
			if !ok ||
				claimed.OwnerActorID != input.Principal.ActorID ||
				uint64(turnSpec.Attempt) != input.Principal.AuthorityRevision ||
				turnSpec.EffectiveInputSHA256 != input.Principal.AuthorityDigest {
				return errs.ErrPermissionDenied
			}
			token := service.leaseToken(
				claimed.ID,
				claimed.Version,
				turnSpec.Attempt,
				input.Principal.AuthorityGrantGeneration,
				input.Principal.CallerWorkload,
				input.IdempotencyKey,
			)
			expiresAt := claimNow.Add(service.turnLeaseDuration)
			if err := tx.Update(ctx, claimed, turn.Version); err != nil {
				return err
			}
			if _, err := service.propagateCurrentTurnTransition(
				ctx, tx, input.Principal, authorityGraph, claimed, claimNow,
			); err != nil {
				return err
			}
			if err := tx.SaveTurnLease(ctx, domainrepo.TurnLease{
				TurnID:              claimed.ID,
				TokenHash:           hashString(token),
				WorkloadID:          input.Principal.CallerWorkload,
				AuthorityGeneration: input.Principal.AuthorityGrantGeneration,
				Attempt:             turnSpec.Attempt,
				ExpiresAt:           expiresAt,
				Fence:               claimed.Version,
			}); err != nil {
				return err
			}
			if err := tx.SaveTurnAttempt(ctx, domainrepo.TurnAttempt{
				TurnID:              claimed.ID,
				Attempt:             turnSpec.Attempt,
				WorkloadID:          input.Principal.CallerWorkload,
				AuthorityGeneration: input.Principal.AuthorityGrantGeneration,
				State:               "CLAIMED",
				InputSHA256:         turnSpec.EffectiveInputSHA256,
				LeaseFence:          claimed.Version,
				StartedAt:           claimNow,
			}); err != nil {
				return err
			}
			if err := service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"claim_turn",
				claimed,
			); err != nil {
				return err
			}
			if err := service.enqueueInteractionStateDeliveries(ctx, tx, authorityGraph.Session, claimed,
				turnSpec, "claim:"+strconv.FormatUint(claimed.Version, 10), "PROGRESS", "STATUS", "RUN_CARD"); err != nil {
				return err
			}
			payload, err := json.Marshal(claimTurnReceipt{
				LeaseExpiresAt:      expiresAt,
				Attempt:             turnSpec.Attempt,
				AuthorityGeneration: input.Principal.AuthorityGrantGeneration,
			})
			if err != nil {
				return errs.ErrInternal
			}
			if err := tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: input.Principal.OrganizationID,
				ProjectID:      input.Principal.ProjectID,
				Scope:          "claim_turn",
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Result:         claimed,
				Payload:        payload,
				CreatedAt:      service.now().UTC().Truncate(time.Microsecond),
			}); err != nil {
				return err
			}
			result = ClaimTurnResult{
				Turn:                claimed,
				LeaseToken:          token,
				LeaseExpiresAt:      expiresAt,
				Attempt:             turnSpec.Attempt,
				AuthorityGeneration: input.Principal.AuthorityGrantGeneration,
			}
			mutated = true
			return nil
		},
	)
	if err == nil && mutated {
		service.observer.ObserveMutation(enum.KindTurn, "claim_turn")
	}
	if err == nil && recovered {
		return ClaimTurnResult{}, errs.ErrUnavailable
	}
	return result, err
}

// RenewTurn продлевает только точную текущую аренду той же рабочей нагрузки и поколения.
func (service *Service) RenewTurn(
	ctx context.Context,
	input RenewTurnInput,
) (RenewTurnResult, error) {
	if err := authorize(input.Principal, permissionRenewTurn); err != nil {
		return RenewTurnResult{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.TurnID) != nil ||
		len(input.LeaseToken) != 64 ||
		input.ExpectedVersion == 0 ||
		input.Attempt == 0 ||
		input.Principal.AuthorityGrantGeneration == 0 ||
		input.Principal.AuthoritySource != "AGENT_SESSION" ||
		input.Principal.AuthorityReference != input.TurnID ||
		input.Principal.AuthorityRevision != uint64(input.Attempt) ||
		input.Principal.AuthorityGrantGeneration !=
			input.AuthorityGeneration ||
		!validSHA256Text(input.Principal.AuthorityDigest) {
		return RenewTurnResult{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		TurnID          string
		TokenHash       string
		ExpectedVersion uint64
		Attempt         uint32
	}{
		identity(input.Principal),
		input.TurnID,
		hashString(input.LeaseToken),
		input.ExpectedVersion,
		input.Attempt,
	})
	if err != nil {
		return RenewTurnResult{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result RenewTurnResult
	err = service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID,
			ActorID:        input.Principal.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			graph, err := service.lockOwnerGraphByTurn(
				ctx, tx, input.Principal, input.TurnID,
			)
			if err != nil {
				return err
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionAbsent,
			); err != nil {
				return err
			}
			current := graph.Turn
			spec, ok := current.Spec.(entity.TurnSpec)
			if err := requireLifecycleOwner(input.Principal, current); err != nil {
				return err
			}
			if !ok || current.Kind != enum.KindTurn ||
				current.State != enum.StateClaimed ||
				current.Version != input.ExpectedVersion ||
				spec.Attempt != input.Attempt ||
				current.OwnerActorID != input.Principal.ActorID ||
				spec.EffectiveInputSHA256 != input.Principal.AuthorityDigest {
				return errs.ErrStateConflict
			}
			if err := requireActiveTurnSession(
				ctx, tx, input.Principal, current, spec,
			); err != nil {
				return err
			}
			lease, err := tx.GetTurnLeaseForUpdate(ctx, input.TurnID)
			if err != nil {
				return err
			}
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			if lease.TokenHash != hashString(input.LeaseToken) ||
				lease.WorkloadID != input.Principal.CallerWorkload ||
				lease.AuthorityGeneration != input.Principal.AuthorityGrantGeneration ||
				lease.AuthorityGeneration != input.AuthorityGeneration ||
				lease.Attempt != input.Attempt || lease.Fence != current.Version ||
				!lease.ExpiresAt.After(now) {
				return errs.ErrStateConflict
			}
			receipt, receiptErr := tx.GetReceipt(
				ctx,
				input.Principal.OrganizationID,
				"renew_turn",
				keyHash,
			)
			if receiptErr == nil {
				if receipt.RequestHash != requestHash ||
					resourceReceiptMatchesCurrent(current, receipt.Result) != nil {
					return errs.ErrIdempotencyConflict
				}
				var payload claimTurnReceipt
				if json.Unmarshal(receipt.Payload, &payload) != nil ||
					payload.LeaseExpiresAt.IsZero() ||
					payload.Attempt != input.Attempt ||
					payload.AuthorityGeneration != input.Principal.AuthorityGrantGeneration ||
					!payload.LeaseExpiresAt.Equal(lease.ExpiresAt) {
					return errs.ErrInternal
				}
				result = RenewTurnResult{
					Turn:                current,
					LeaseToken:          input.LeaseToken,
					LeaseExpiresAt:      payload.LeaseExpiresAt,
					Attempt:             payload.Attempt,
					AuthorityGeneration: payload.AuthorityGeneration,
				}
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			renewed, err := tx.RenewTurnLease(
				ctx,
				domainrepo.TurnLease{
					TurnID:              input.TurnID,
					TokenHash:           hashString(input.LeaseToken),
					WorkloadID:          input.Principal.CallerWorkload,
					AuthorityGeneration: input.Principal.AuthorityGrantGeneration,
					Attempt:             input.Attempt,
					ExpiresAt:           now.Add(service.turnLeaseDuration),
					Fence:               input.ExpectedVersion,
				},
				now,
			)
			if err != nil {
				return err
			}
			if err := appendOwnerStateAudit(
				ctx, tx, input.Principal, "renew_turn", current.OrganizationID,
				current.ProjectID, current.ID, auditKindTurnLease,
				renewed.Fence, now,
			); err != nil {
				return err
			}
			payload, err := json.Marshal(claimTurnReceipt{
				LeaseExpiresAt:      renewed.ExpiresAt,
				Attempt:             renewed.Attempt,
				AuthorityGeneration: renewed.AuthorityGeneration,
			})
			if err != nil {
				return errs.ErrInternal
			}
			if err := tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: input.Principal.OrganizationID,
				ProjectID:      input.Principal.ProjectID,
				Scope:          "renew_turn",
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Result:         current,
				Payload:        payload,
				CreatedAt:      now,
			}); err != nil {
				return err
			}
			result = RenewTurnResult{
				Turn:                current,
				LeaseToken:          input.LeaseToken,
				LeaseExpiresAt:      renewed.ExpiresAt,
				Attempt:             renewed.Attempt,
				AuthorityGeneration: renewed.AuthorityGeneration,
			}
			return nil
		},
	)
	return result, err
}

// CompleteTurn принимает конечный результат только для текущей неистёкшей аренды.
func (service *Service) CompleteTurn(
	ctx context.Context,
	input CompleteTurnInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionCompleteTurn); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.TurnID) != nil ||
		len(input.LeaseToken) != 64 ||
		input.ExpectedVersion == 0 ||
		input.Attempt == 0 ||
		input.AuthorityGeneration == 0 ||
		(input.TerminalState != enum.StateSucceeded &&
			input.TerminalState != enum.StateFailed &&
			input.TerminalState != enum.StateCancelled) ||
		len(input.Outcome) < 1 || len(input.Outcome) > 256 ||
		(input.ResultArtifactID != "" &&
			value.ValidateID(input.ResultArtifactID) != nil) ||
		input.Principal.AuthoritySource != "AGENT_SESSION" ||
		input.Principal.AuthorityReference != input.TurnID ||
		input.Principal.AuthorityRevision != uint64(input.Attempt) ||
		!validSHA256Text(input.Principal.AuthorityDigest) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity         commandIdentity
		TurnID           string
		LeaseTokenHash   string
		ExpectedVersion  uint64
		TerminalState    enum.State
		Outcome          string
		ResultArtifactID string
		Attempt          uint32
		Generation       uint64
	}{
		identity(input.Principal),
		input.TurnID,
		hashString(input.LeaseToken),
		input.ExpectedVersion,
		input.TerminalState,
		input.Outcome,
		input.ResultArtifactID,
		input.Attempt,
		input.AuthorityGeneration,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	var receiptTurn entity.Resource
	return service.withValidatedResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"complete_turn",
		requestHash,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			graph, err := service.lockOwnerGraphByTurn(
				ctx, tx, input.Principal, input.TurnID,
			)
			if err != nil {
				return 0, err
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionAbsent,
			); err != nil {
				return 0, err
			}
			current := graph.Turn
			spec, ok := current.Spec.(entity.TurnSpec)
			if !ok || requireLifecycleOwner(input.Principal, current) != nil ||
				spec.Attempt != input.Attempt ||
				spec.EffectiveInputSHA256 != input.Principal.AuthorityDigest {
				return 0, errs.ErrStateConflict
			}
			if current.Version == input.ExpectedVersion && !current.State.Terminal() {
				lease, err := tx.ValidateTurnLease(
					ctx, input.TurnID, hashString(input.LeaseToken),
					input.Principal.CallerWorkload, input.AuthorityGeneration,
					input.Attempt, service.now(),
				)
				if err != nil || lease.Fence != current.Version {
					if err != nil {
						return 0, err
					}
					return 0, errs.ErrStateConflict
				}
				return lifecycleReceiptApply, nil
			}
			if current.Version == input.ExpectedVersion+1 &&
				current.State == input.TerminalState && spec.Outcome == input.Outcome &&
				spec.ResultArtifactID == input.ResultArtifactID {
				receiptTurn = current
				return lifecycleReceiptReplay, nil
			}
			return 0, errs.ErrVersionMismatch
		},
		func(_ domainrepo.Transaction, stored entity.Resource) error {
			return resourceReceiptMatchesCurrent(receiptTurn, stored)
		},
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			graph, err := service.prelockScheduledGraphByTurn(
				ctx, tx, input.Principal, input.TurnID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionAbsent,
			); err != nil {
				return entity.Resource{}, err
			}
			current := graph.Turn
			if err := requireLifecycleOwner(input.Principal, current); err != nil {
				return entity.Resource{}, err
			}
			if current.Kind != enum.KindTurn ||
				current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			lease, err := tx.ValidateTurnLease(
				ctx,
				input.TurnID,
				hashString(input.LeaseToken),
				input.Principal.CallerWorkload,
				input.AuthorityGeneration,
				input.Attempt,
				service.now(),
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if lease.Fence != current.Version ||
				lease.Attempt != input.Attempt ||
				lease.AuthorityGeneration != input.AuthorityGeneration ||
				input.AuthorityGeneration !=
					input.Principal.AuthorityGrantGeneration {
				return entity.Resource{}, errs.ErrStateConflict
			}
			spec, ok := current.Spec.(entity.TurnSpec)
			if !ok {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := requireActiveTurnSession(ctx, tx, input.Principal, current, spec); err != nil {
				return entity.Resource{}, err
			}
			if spec.Attempt != input.Attempt ||
				current.OwnerActorID != input.Principal.ActorID ||
				spec.EffectiveInputSHA256 != input.Principal.AuthorityDigest {
				return entity.Resource{}, errs.ErrStateConflict
			}
			var resultArtifact entity.Resource
			var resultArtifactSpec entity.ArtifactSpec
			if input.ResultArtifactID != "" {
				resultArtifact, resultArtifactSpec, err = service.requireCleanArtifactResource(
					ctx,
					tx,
					input.Principal,
					input.ResultArtifactID,
				)
				if err != nil {
					return entity.Resource{}, err
				}
			}
			spec.Outcome = input.Outcome
			spec.ResultArtifactID = input.ResultArtifactID
			if input.ResultArtifactID != "" {
				spec.ResultArtifactVersion = resultArtifact.Version
				spec.ResultArtifactSHA256 = resultArtifactSpec.SHA256
			} else {
				spec.ResultArtifactVersion = 0
				spec.ResultArtifactSHA256 = ""
			}
			updated, err := current.ReplaceAndTransition(
				spec,
				input.TerminalState,
				service.now(),
			)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := service.validateReferences(ctx, tx, updated); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.DeleteTurnLease(ctx, input.TurnID, lease.Fence); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.FinishTurnAttempt(ctx, domainrepo.TurnAttempt{
				TurnID:              updated.ID,
				Attempt:             spec.Attempt,
				WorkloadID:          lease.WorkloadID,
				AuthorityGeneration: lease.AuthorityGeneration,
				State:               string(input.TerminalState),
				InputSHA256:         spec.EffectiveInputSHA256,
				LeaseFence:          lease.Fence,
				FinishedAt:          service.now().UTC().Truncate(time.Microsecond),
				Outcome:             input.Outcome,
			}); err != nil {
				return entity.Resource{}, err
			}
			if err := service.revokeExecutionClaims(
				ctx, tx, input.Principal, spec.ProcessRunID, updated.ID,
				"complete_turn", service.now().UTC().Truncate(time.Microsecond),
			); err != nil {
				return entity.Resource{}, err
			}
			if err := service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"complete_turn",
				updated,
			); err != nil {
				return entity.Resource{}, err
			}
			if err := service.enqueueInteractionTerminalDelivery(ctx, tx, graph.Session, updated, spec, nil); err != nil {
				return entity.Resource{}, err
			}
			if err := service.completeProcessFromTurn(
				ctx, tx, input.Principal, updated, spec,
			); err != nil {
				return entity.Resource{}, err
			}
			return updated, nil
		},
	)
}

func (service *Service) completeProcessFromTurn(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	turn entity.Resource,
	turnSpec entity.TurnSpec,
) error {
	if turnSpec.ProcessRunID == "" || (!turn.State.Terminal() && turn.State != enum.StateBlocked) {
		return nil
	}
	process, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, turnSpec.ProcessRunID,
	)
	if err != nil {
		return err
	}
	processSpec, ok := process.Spec.(entity.ProcessRunSpec)
	if !ok || process.Kind != enum.KindProcessRun ||
		processSpec.RootInitiatorActorID != principal.ActorID {
		return errs.ErrStateConflict
	}
	execution, err := currentExecution(processSpec)
	if err != nil {
		return err
	}
	if !executionMatchesTurn(execution, turn, turnSpec) {
		return nil
	}
	if process.State.Terminal() {
		return nil
	}
	if err := service.revokeExecutionClaims(
		ctx, tx, principal, process.ID, turn.ID, "complete_process",
		service.now().UTC().Truncate(time.Microsecond),
	); err != nil {
		return err
	}
	open, err := tx.ProcessHasOpenWork(
		ctx, process.OrganizationID, process.ProjectID, process.ID, turn.ID, "",
	)
	if err != nil {
		return err
	}
	if open {
		return nil
	}
	processSpec.Outcome = turnSpec.Outcome
	processSpec.ResultArtifactID = turnSpec.ResultArtifactID
	updated, err := process.ReplaceAndTransition(
		processSpec, turn.State, service.now().UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		return errs.ErrStateConflict
	}
	if err := tx.Update(ctx, updated, process.Version); err != nil {
		return err
	}
	if err := service.appendMutationRecords(
		ctx, tx, principal, "complete_process_from_turn", updated,
	); err != nil {
		return err
	}
	if turn.State == enum.StateBlocked {
		return nil
	}
	return service.finishContinuationOccurrence(
		ctx, tx, principal, updated, processSpec, turn, turnSpec,
	)
}

func requireActiveTurnSession(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	turn entity.Resource,
	spec entity.TurnSpec,
) error {
	session, err := tx.GetForUpdate(
		ctx, principal.OrganizationID, principal.ProjectID, spec.SessionID,
	)
	if err != nil {
		return err
	}
	if session.Kind != enum.KindSession || session.State != enum.StateActive ||
		session.OwnerActorID != turn.OwnerActorID {
		return errs.ErrStateConflict
	}
	return nil
}

// RetryTurn создаёт новую неизменяемую попытку после завершённого или ожидающего хода.
func (service *Service) RetryTurn(
	ctx context.Context,
	input RetryTurnInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionRetryTurn); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.TurnID) != nil ||
		input.ExpectedVersion == 0 ||
		value.ValidateStableKey(input.ReasonCode) != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		TurnID          string
		ExpectedVersion uint64
		ReasonCode      string
	}{
		identity(input.Principal),
		input.TurnID,
		input.ExpectedVersion,
		input.ReasonCode,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	var receiptTurn entity.Resource
	return service.withValidatedResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"retry_turn",
		requestHash,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			graph, err := service.lockOwnerGraphByTurn(
				ctx, tx, input.Principal, input.TurnID,
			)
			if err != nil {
				return 0, err
			}
			current := graph.Turn
			spec, ok := current.Spec.(entity.TurnSpec)
			if !ok || requireLifecycleOwner(input.Principal, current) != nil {
				return 0, errs.ErrStateConflict
			}
			if graph.Runtime != nil &&
				(graph.Runtime.State != "SUSPENDED" ||
					current.State != enum.StateWaitingOwner) {
				return 0, errs.ErrStateConflict
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionAbsent, runtimeDispositionTerminal,
			); err != nil {
				return 0, err
			}
			if current.Version == input.ExpectedVersion && slices.Contains(
				[]enum.State{enum.StateFailed, enum.StateBlocked,
					enum.StateWaitingOwner}, current.State,
			) {
				return lifecycleReceiptApply, nil
			}
			if current.Version == input.ExpectedVersion+1 &&
				current.State == enum.StateQueued && spec.Attempt > 1 &&
				graph.Runtime == nil {
				receiptTurn = current
				return lifecycleReceiptReplay, nil
			}
			return 0, errs.ErrVersionMismatch
		},
		func(_ domainrepo.Transaction, stored entity.Resource) error {
			return resourceReceiptMatchesCurrent(receiptTurn, stored)
		},
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			graph, err := service.prelockScheduledGraphByTurn(
				ctx, tx, input.Principal, input.TurnID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if graph.Runtime != nil && graph.Runtime.State != "SUSPENDED" {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionAbsent, runtimeDispositionTerminal,
			); err != nil {
				return entity.Resource{}, err
			}
			current := graph.Turn
			spec, ok := current.Spec.(entity.TurnSpec)
			if err := requireLifecycleOwner(input.Principal, current); err != nil {
				return entity.Resource{}, err
			}
			if !ok || current.Kind != enum.KindTurn ||
				current.Version != input.ExpectedVersion ||
				(current.State != enum.StateFailed &&
					current.State != enum.StateBlocked &&
					current.State != enum.StateWaitingOwner) ||
				spec.Attempt == ^uint32(0) {
				return entity.Resource{}, errs.ErrStateConflict
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			previousAttempt, err := tx.GetTurnAttemptForUpdate(
				ctx, current.ID, spec.Attempt,
			)
			if err != nil || previousAttempt.InputSHA256 != spec.EffectiveInputSHA256 {
				return entity.Resource{}, errs.ErrStateConflict
			}
			switch current.State {
			case enum.StateFailed:
				if previousAttempt.FinishedAt.IsZero() {
					return entity.Resource{}, errs.ErrStateConflict
				}
			case enum.StateWaitingOwner:
				if previousAttempt.State != "WAITING_OWNER" ||
					previousAttempt.FinishedAt.IsZero() ||
					previousAttempt.Outcome != "owner_gate_pending" {
					return entity.Resource{}, errs.ErrStateConflict
				}
			default:
				if !previousAttempt.FinishedAt.IsZero() {
					return entity.Resource{}, errs.ErrStateConflict
				}
				previousAttempt.State = "CANCELLED"
				previousAttempt.FinishedAt = now
				previousAttempt.Outcome = "retry_" + input.ReasonCode
				if err := tx.FinishTurnAttempt(ctx, previousAttempt); err != nil {
					return entity.Resource{}, err
				}
			}
			lease, leaseErr := tx.GetTurnLeaseForUpdate(ctx, current.ID)
			if leaseErr != nil && !errors.Is(leaseErr, errs.ErrNotFound) {
				return entity.Resource{}, leaseErr
			}
			if leaseErr == nil {
				if current.State == enum.StateWaitingOwner {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if lease.Attempt != spec.Attempt ||
					lease.AuthorityGeneration != previousAttempt.AuthorityGeneration {
					return entity.Resource{}, errs.ErrStateConflict
				}
				if err := tx.DeleteTurnLease(ctx, current.ID, lease.Fence); err != nil {
					return entity.Resource{}, err
				}
			}
			if err := service.revokeExecutionClaims(
				ctx, tx, input.Principal, spec.ProcessRunID, current.ID,
				"retry_turn", now,
			); err != nil {
				return entity.Resource{}, err
			}
			retried, spec, err := service.prepareRetriedExecution(
				ctx, tx, input.Principal, graph, spec, now,
			)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, retried, current.Version); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.SaveTurnAttempt(ctx, domainrepo.TurnAttempt{
				TurnID:              retried.ID,
				Attempt:             spec.Attempt,
				WorkloadID:          "unassigned",
				AuthorityGeneration: input.Principal.AuthorityGeneration,
				State:               "QUEUED",
				InputSHA256:         spec.EffectiveInputSHA256,
				LeaseFence:          retried.Version,
				StartedAt:           now,
				Outcome:             input.ReasonCode,
			}); err != nil {
				return entity.Resource{}, err
			}
			return retried, service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"retry_turn",
				retried,
			)
		},
	)
}

// CancelTurn завершает ход и атомарно отзывает существующую аренду.
func (service *Service) CancelTurn(
	ctx context.Context,
	input CancelTurnInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionCancelTurn); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.TurnID) != nil ||
		input.ExpectedVersion == 0 ||
		value.ValidateStableKey(input.ReasonCode) != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		TurnID          string
		ExpectedVersion uint64
		ReasonCode      string
	}{
		identity(input.Principal),
		input.TurnID,
		input.ExpectedVersion,
		input.ReasonCode,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	var receiptTurn entity.Resource
	return service.withValidatedResourceReceipt(
		ctx,
		input.Principal,
		input.IdempotencyKey,
		"cancel_turn",
		requestHash,
		func(tx domainrepo.Transaction) (lifecycleReceiptDisposition, error) {
			graph, err := service.lockOwnerGraphByTurn(
				ctx, tx, input.Principal, input.TurnID,
			)
			if err != nil {
				return 0, err
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionAbsent,
			); err != nil {
				return 0, err
			}
			current := graph.Turn
			spec, ok := current.Spec.(entity.TurnSpec)
			if !ok || requireLifecycleOwner(input.Principal, current) != nil {
				return 0, errs.ErrStateConflict
			}
			if current.Version == input.ExpectedVersion && !current.State.Terminal() {
				return lifecycleReceiptApply, nil
			}
			if current.Version == input.ExpectedVersion+1 &&
				current.State == enum.StateCancelled && spec.Outcome == input.ReasonCode {
				receiptTurn = current
				return lifecycleReceiptReplay, nil
			}
			return 0, errs.ErrVersionMismatch
		},
		func(_ domainrepo.Transaction, stored entity.Resource) error {
			return resourceReceiptMatchesCurrent(receiptTurn, stored)
		},
		func(tx domainrepo.Transaction) (entity.Resource, error) {
			graph, err := service.prelockScheduledGraphByTurn(
				ctx, tx, input.Principal, input.TurnID,
			)
			if err != nil {
				return entity.Resource{}, err
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionAbsent,
			); err != nil {
				return entity.Resource{}, err
			}
			current := graph.Turn
			if err := requireLifecycleOwner(input.Principal, current); err != nil {
				return entity.Resource{}, err
			}
			if current.Kind != enum.KindTurn ||
				current.Version != input.ExpectedVersion ||
				current.State.Terminal() {
				return entity.Resource{}, errs.ErrStateConflict
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			return service.cancelTurnExecution(
				ctx, tx, input.Principal, current, input.ReasonCode, now,
			)
		},
	)
}

// ClaimDueSchedules фиксирует неизменяемые запуски и сдвигает верхнюю границу.
func (service *Service) ClaimDueSchedules(
	ctx context.Context,
	input ClaimDueSchedulesInput,
) (ClaimDueSchedulesResult, error) {
	if err := authorize(input.Principal, permissionClaimSchedule); err != nil {
		return ClaimDueSchedulesResult{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		input.Limit < 1 || input.Limit > service.maximumScheduleClaims {
		return ClaimDueSchedulesResult{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		Limit    int
	}{identity(input.Principal), input.Limit})
	if err != nil {
		return ClaimDueSchedulesResult{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result ClaimDueSchedulesResult
	mutated := false
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
				"claim_due_schedules",
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
			now, err := tx.CurrentTime(ctx)
			if err != nil {
				return err
			}
			now = now.UTC().Truncate(time.Microsecond)
			schedules, err := tx.DueSchedules(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.Limit,
				now,
			)
			if err != nil {
				return err
			}
			result.Occurrences = make([]ScheduleOccurrence, 0, len(schedules))
			changedSchedules := 0
			for _, schedule := range schedules {
				spec, ok := schedule.Spec.(entity.ScheduleSpec)
				if !ok || schedule.Kind != enum.KindSchedule ||
					schedule.State != enum.StateActive {
					return errs.ErrStateConflict
				}
				scheduledFor := spec.NextRunAt.UTC().Truncate(time.Microsecond)
				open, err := tx.HasOpenScheduleOccurrence(
					ctx,
					schedule.OrganizationID,
					schedule.ProjectID,
					schedule.ID,
				)
				if err != nil {
					return err
				}
				if spec.OverlapPolicy == "FORBID" && open {
					continue
				}
				state, outcome := scheduleOccurrenceDisposition(
					spec,
					scheduledFor,
					now,
					open,
				)
				nextRun, err := nextScheduleRun(spec, scheduledFor, now)
				if err != nil {
					return err
				}
				spec.NextRunAt = nextRun
				updated, err := schedule.Update(schedule.Name, spec, now)
				if err != nil {
					return errs.ErrStateConflict
				}
				if err := tx.Update(ctx, updated, schedule.Version); err != nil {
					return err
				}
				if err := service.appendMutationRecords(
					ctx,
					tx,
					input.Principal,
					"claim_due_schedule",
					updated,
				); err != nil {
					return err
				}
				changedSchedules++
				occurrence := ScheduleOccurrence{
					ScheduleID:   schedule.ID,
					ScheduledFor: scheduledFor,
					OccurrenceID: uuid.NewSHA1(
						uuid.NameSpaceOID,
						[]byte(schedule.ID+"\x00"+scheduledFor.Format(time.RFC3339Nano)),
					).String(),
					TargetResourceID:     spec.TargetResourceID,
					TargetKind:           spec.TargetKind,
					TargetVersion:        spec.TargetVersion,
					EffectiveInputSHA256: spec.EffectiveInputSHA,
					PromptProfileID:      spec.PromptProfileID,
					PromptRevision:       spec.PromptRevision,
					RuntimeRevisionID:    spec.RuntimeRevisionID,
					SessionPolicy:        spec.SessionPolicy,
					RoomID:               spec.RoomID,
					NotificationPolicy:   spec.NotificationPolicy,
					MaximumExecution:     spec.MaximumExecutionDuration,
					Coalesce:             spec.Coalesce,
					State:                state,
					Attempt:              1,
					AvailableAt:          scheduledFor,
					Outcome:              outcome,
				}
				if err := tx.SaveScheduleOccurrence(ctx, domainrepo.ScheduleOccurrence{
					ID:                   occurrence.OccurrenceID,
					ScheduleID:           occurrence.ScheduleID,
					OrganizationID:       schedule.OrganizationID,
					ProjectID:            schedule.ProjectID,
					ScheduledFor:         occurrence.ScheduledFor,
					TargetResourceID:     occurrence.TargetResourceID,
					TargetKind:           occurrence.TargetKind,
					TargetVersion:        occurrence.TargetVersion,
					EffectiveInputSHA256: occurrence.EffectiveInputSHA256,
					PromptProfileID:      occurrence.PromptProfileID,
					PromptRevision:       occurrence.PromptRevision,
					RuntimeRevisionID:    occurrence.RuntimeRevisionID,
					SessionPolicy:        occurrence.SessionPolicy,
					RoomID:               occurrence.RoomID,
					NotificationPolicy:   occurrence.NotificationPolicy,
					MaximumExecution:     occurrence.MaximumExecution,
					Coalesce:             occurrence.Coalesce,
					OverlapPolicy:        spec.OverlapPolicy,
					MaximumAttempts:      spec.MaximumAttempts,
					InitialBackoff:       spec.InitialBackoff,
					MaximumBackoff:       spec.MaximumBackoff,
					DeadLetterAt:         scheduledFor.Add(spec.DeadLetterAfter),
					State:                occurrence.State,
					Attempt:              occurrence.Attempt,
					AvailableAt:          occurrence.AvailableAt,
					Outcome:              occurrence.Outcome,
					CreatedAt:            now,
					UpdatedAt:            now,
				}); err != nil {
					return err
				}
				result.Occurrences = append(result.Occurrences, occurrence)
			}
			payload, err := json.Marshal(result)
			if err != nil {
				return errs.ErrInternal
			}
			mutated = changedSchedules > 0
			return tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: input.Principal.OrganizationID,
				ProjectID:      input.Principal.ProjectID,
				Scope:          "claim_due_schedules",
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Payload:        payload,
				CreatedAt:      now,
			})
		},
	)
	if err == nil && mutated {
		service.observer.ObserveMutation(enum.KindSchedule, "claim_schedules")
	}
	return result, err
}

// ResolveOwnerGate связывает решение с точными шлюзом и процессом одной фиксацией.
func (service *Service) ResolveOwnerGate(
	ctx context.Context,
	input ResolveOwnerGateInput,
) (OwnerGateResult, error) {
	if err := authorize(input.Principal, permissionResolveGate); err != nil {
		return OwnerGateResult{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.OwnerGateID) != nil ||
		input.ExpectedVersion == 0 ||
		value.ValidateID(input.ProcessRunID) != nil ||
		input.ProcessExpectedVersion == 0 ||
		value.ValidateID(input.SessionID) != nil ||
		value.ValidateID(input.TurnID) != nil ||
		input.Attempt == 0 ||
		!validSHA256Text(input.ImmutableInputSHA256) ||
		(input.Decision != "APPROVED" && input.Decision != "REJECTED" &&
			input.Decision != "CHANGES_REQUESTED" && input.Decision != "CANCELLED") ||
		len(input.Reason) < 1 || len(input.Reason) > 2048 {
		return OwnerGateResult{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity               commandIdentity
		OwnerGateID            string
		ExpectedVersion        uint64
		Decision               string
		Reason                 string
		ProcessRunID           string
		ProcessExpectedVersion uint64
		SessionID              string
		TurnID                 string
		Attempt                uint32
		ImmutableInputSHA256   string
	}{
		identity(input.Principal),
		input.OwnerGateID,
		input.ExpectedVersion,
		input.Decision,
		input.Reason,
		input.ProcessRunID,
		input.ProcessExpectedVersion,
		input.SessionID,
		input.TurnID,
		input.Attempt,
		input.ImmutableInputSHA256,
	})
	if err != nil {
		return OwnerGateResult{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result OwnerGateResult
	err = service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID,
			ActorID:        input.Principal.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			gateCandidate, err := tx.Get(
				ctx,
				input.Principal.OrganizationID,
				input.Principal.ProjectID,
				input.OwnerGateID,
			)
			if err != nil {
				return err
			}
			candidateSpec, ok := gateCandidate.Spec.(entity.OwnerGateSpec)
			if !ok || gateCandidate.Kind != enum.KindOwnerGate ||
				(gateCandidate.Version != input.ExpectedVersion &&
					gateCandidate.Version != input.ExpectedVersion+1) ||
				candidateSpec.ProcessRunID != input.ProcessRunID ||
				candidateSpec.SessionID != input.SessionID ||
				candidateSpec.TurnID != input.TurnID {
				return errs.ErrNotFound
			}
			graph, err := service.lockOwnerGraphByTurn(
				ctx, tx, input.Principal, candidateSpec.TurnID,
			)
			if err != nil {
				return err
			}
			// OwnerGate является pinned owner resource и блокируется только
			// после полного execution graph.
			gate, err := tx.GetForUpdate(
				ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
				gateCandidate.ID,
			)
			if err != nil {
				return err
			}
			if gate.Kind != enum.KindOwnerGate ||
				(gate.Version != input.ExpectedVersion &&
					gate.Version != input.ExpectedVersion+1) ||
				gate.Version != gateCandidate.Version {
				return errs.ErrVersionMismatch
			}
			gateSpec, ok := gate.Spec.(entity.OwnerGateSpec)
			if !ok ||
				gateSpec.ProcessRunID != input.ProcessRunID ||
				gateSpec.SessionID != input.SessionID ||
				gateSpec.TurnID != input.TurnID ||
				gateSpec.Attempt != input.Attempt ||
				gateSpec.ImmutableInputSHA256 != input.ImmutableInputSHA256 ||
				gateSpec.RecipientActorID != input.Principal.ActorID ||
				gateSpec.MattermostPostID == "" ||
				gateSpec.MattermostChannelID == "" ||
				gateSpec.MattermostRootPostID == "" ||
				gateSpec.DeliveredAt.IsZero() ||
				gateSpec.DeliveryFence == 0 ||
				!validSHA256Text(gateSpec.DeliveryClaimTokenSHA256) {
				return errs.ErrNotFound
			}
			process := graph.Process
			processSpec, ok := process.Spec.(entity.ProcessRunSpec)
			if graph.Runtime != nil &&
				(graph.Runtime.State != "SUSPENDED" ||
					graph.Runtime.TerminalReference != gate.ID) {
				return errs.ErrStateConflict
			}
			if err := requireOwnerGraphRuntimeDisposition(
				graph, runtimeDispositionAbsent, runtimeDispositionTerminal,
			); err != nil {
				return err
			}
			receipt, receiptErr := tx.GetReceipt(
				ctx, input.Principal.OrganizationID, "resolve_owner_gate", keyHash,
			)
			if receiptErr == nil {
				if receipt.RequestHash != requestHash ||
					gate.Version != input.ExpectedVersion+1 {
					return errs.ErrIdempotencyConflict
				}
				var payload ownerGateReceipt
				if json.Unmarshal(receipt.Payload, &payload) != nil {
					return errs.ErrInternal
				}
				if resourceReceiptMatchesCurrent(gate, receipt.Result) != nil ||
					resourceReceiptMatchesCurrent(process, payload.Process) != nil {
					return errs.ErrStateConflict
				}
				result = OwnerGateResult{OwnerGate: gate, Process: process}
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			if gate.Version != input.ExpectedVersion {
				return errs.ErrVersionMismatch
			}
			execution, executionErr := currentExecution(processSpec)
			executionMatches := executionErr == nil &&
				execution.SessionID == gateSpec.SessionID &&
				execution.TurnID == gateSpec.TurnID &&
				execution.Attempt == gateSpec.Attempt
			if !ok || process.Kind != enum.KindProcessRun ||
				process.Version != input.ProcessExpectedVersion ||
				process.State != enum.StateWaitingOwner ||
				processSpec.RootInitiatorActorID != gateSpec.RootInitiatorActorID ||
				!executionMatches ||
				processSpec.ImmutableInputSHA256 != gateSpec.ImmutableInputSHA256 ||
				processSpec.ScheduleID != gateSpec.ScheduleID ||
				processSpec.OccurrenceID != gateSpec.OccurrenceID {
				return errs.ErrStateConflict
			}
			gateTurn := graph.Turn
			gateTurnSpec, ok := gateTurn.Spec.(entity.TurnSpec)
			if !ok || gateTurn.Kind != enum.KindTurn ||
				gateTurn.State != enum.StateWaitingOwner ||
				gateTurnSpec.SessionID != gateSpec.SessionID ||
				gateTurnSpec.Attempt != gateSpec.Attempt ||
				gateTurnSpec.ProcessRunID != process.ID ||
				gateTurnSpec.ResultArtifactID == "" ||
				gateTurnSpec.Outcome != "owner_gate_pending" {
				return errs.ErrStateConflict
			}
			var relatedOccurrence domainrepo.ScheduleOccurrence
			var relatedSchedule entity.Resource
			if gateSpec.OccurrenceID != "" {
				occurrence := graph.Occurrence
				if occurrence.ScheduleID != gateSpec.ScheduleID ||
					occurrence.ID != gateSpec.OccurrenceID ||
					occurrence.State != "WAITING_OWNER" ||
					occurrence.EffectiveInputSHA256 == "" {
					return errs.ErrStateConflict
				}
				schedule := graph.Schedule
				if schedule.Kind != enum.KindSchedule {
					return errs.ErrStateConflict
				}
				relatedOccurrence = occurrence
				relatedSchedule = schedule
			}
			now := service.now().UTC().Truncate(time.Microsecond)
			if !gateSpec.ExpiresAt.After(now) {
				result, err = service.expireOwnerGateGraph(
					ctx, tx, input.Principal, gate, graph, now,
				)
				if err != nil {
					return err
				}
				return saveOwnerGateResultReceipt(
					ctx, tx, input.Principal, keyHash, requestHash, result, now,
				)
			}
			if input.Decision == "CHANGES_REQUESTED" {
				result, err = service.continueOwnerGateGraph(
					ctx, tx, input.Principal, gate, gateSpec, process, processSpec,
					gateTurn, gateTurnSpec, input.Reason, relatedOccurrence,
					relatedSchedule, now,
				)
				if err != nil {
					return err
				}
				return saveOwnerGateResultReceipt(
					ctx, tx, input.Principal, keyHash, requestHash, result, now,
				)
			}
			gateTarget := ownerGateTarget(input.Decision)
			processTarget := enum.StateFailed
			switch input.Decision {
			case "APPROVED":
				processTarget = enum.StateSucceeded
			case "REJECTED":
				processTarget = enum.StateFailed
			case "CHANGES_REQUESTED":
				processTarget = enum.StateFailed
			case "CANCELLED":
				processTarget = enum.StateCancelled
			}
			gateSpec.Decision = input.Decision
			gateSpec.DecisionReason = input.Reason
			if processTarget.Terminal() {
				open, err := tx.ProcessHasOpenWork(
					ctx, process.OrganizationID, process.ProjectID, process.ID,
					gateTurn.ID, gate.ID,
				)
				if err != nil {
					return err
				}
				if open {
					return errs.ErrStateConflict
				}
			}
			turnTarget := processTarget
			gateTurnSpec.Outcome = "owner_gate_" + strings.ToLower(input.Decision)
			if gateTarget == enum.StateExpired {
				turnTarget = enum.StateFailed
				gateTurnSpec.Outcome = "owner_gate_expired"
			}
			if turnTarget != enum.StateSucceeded {
				gateTurnSpec.ResultArtifactID = ""
				gateTurnSpec.ResultArtifactVersion = 0
				gateTurnSpec.ResultArtifactSHA256 = ""
			}
			updatedTurn, err := gateTurn.ReplaceAndTransition(
				gateTurnSpec, turnTarget, now,
			)
			if err != nil {
				return errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updatedTurn, gateTurn.Version); err != nil {
				return err
			}
			attempt, err := tx.GetTurnAttemptForUpdate(
				ctx, gateTurn.ID, gateTurnSpec.Attempt,
			)
			if err != nil || attempt.State != "WAITING_OWNER" ||
				attempt.InputSHA256 != gateTurnSpec.EffectiveInputSHA256 ||
				attempt.FinishedAt.IsZero() || attempt.Outcome != "owner_gate_pending" {
				return errs.ErrStateConflict
			}
			if err := service.appendMutationRecords(
				ctx, tx, input.Principal, "owner_gate_terminal_turn", updatedTurn,
			); err != nil {
				return err
			}
			gateTurn = updatedTurn
			if relatedOccurrence.ID != "" {
				if relatedOccurrence.ExecutionTurnID != gateTurn.ID ||
					relatedOccurrence.ExecutionProcessRunID != process.ID {
					return errs.ErrStateConflict
				}
				expectedToken := relatedOccurrence.TokenHash
				relatedOccurrence.Outcome = gateTurnSpec.Outcome
				relatedOccurrence.ResultArtifactID = gateTurnSpec.ResultArtifactID
				if gateTarget == enum.StateExpired {
					relatedOccurrence.State = "FAILED"
					relatedOccurrence.Outcome = "owner_gate_expired"
				} else if processTarget.Terminal() {
					relatedOccurrence.State = string(processTarget)
				}
				if relatedOccurrence.State != "WAITING_OWNER" {
					relatedOccurrence.ClaimantWorkloadID = ""
					relatedOccurrence.AuthorityGeneration = 0
					relatedOccurrence.TokenHash = ""
					relatedOccurrence.LeaseExpiresAt = time.Time{}
				}
				relatedOccurrence.UpdatedAt = now
				if err := tx.UpdateScheduleOccurrence(
					ctx,
					relatedOccurrence,
					relatedOccurrence.Attempt,
					expectedToken,
				); err != nil {
					return err
				}
				if relatedOccurrence.State != "WAITING_OWNER" {
					if err := tx.FinishScheduledRun(ctx, domainrepo.ScheduledRun{
						OccurrenceID:     relatedOccurrence.ID,
						Attempt:          relatedOccurrence.Attempt,
						State:            relatedOccurrence.State,
						Outcome:          relatedOccurrence.Outcome,
						ResultArtifactID: relatedOccurrence.ResultArtifactID,
						FinishedAt:       now,
					}); err != nil {
						return err
					}
				}
				if err := appendScheduleOccurrenceAudit(
					ctx, tx, input.Principal,
					"owner_gate_occurrence_transition", relatedOccurrence,
				); err != nil {
					return err
				}
			}
			updatedGate, err := gate.ReplaceAndTransition(
				gateSpec,
				gateTarget,
				now,
			)
			if err != nil {
				return errs.ErrStateConflict
			}
			processSpec.Outcome = gateTurnSpec.Outcome
			processSpec.ResultArtifactID = gateTurnSpec.ResultArtifactID
			if processTarget != enum.StateSucceeded {
				processSpec.Outcome = "owner_gate_" + strings.ToLower(input.Decision)
				processSpec.ResultArtifactID = ""
			}
			if gateTarget == enum.StateExpired {
				processSpec.Outcome = "owner_gate_expired"
			}
			updatedProcess, err := process.ReplaceAndTransition(
				processSpec, processTarget, now,
			)
			if err != nil {
				return errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updatedGate, gate.Version); err != nil {
				return err
			}
			if err := tx.Update(
				ctx,
				updatedProcess,
				process.Version,
			); err != nil {
				return err
			}
			if err := service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"resolve_owner_gate",
				updatedGate,
			); err != nil {
				return err
			}
			if err := service.appendMutationRecords(
				ctx,
				tx,
				input.Principal,
				"owner_gate_process_transition",
				updatedProcess,
			); err != nil {
				return err
			}
			payload, err := json.Marshal(ownerGateReceipt{Process: updatedProcess})
			if err != nil {
				return errs.ErrInternal
			}
			if err := tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: input.Principal.OrganizationID,
				ProjectID:      input.Principal.ProjectID,
				Scope:          "resolve_owner_gate",
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Result:         updatedGate,
				Payload:        payload,
				CreatedAt:      now,
			}); err != nil {
				return err
			}
			result = OwnerGateResult{
				OwnerGate: updatedGate,
				Process:   updatedProcess,
			}
			return nil
		},
	)
	return result, err
}

type claimTurnReceipt struct {
	LeaseExpiresAt      time.Time `json:"leaseExpiresAt"`
	Attempt             uint32    `json:"attempt"`
	AuthorityGeneration uint64    `json:"authorityGeneration"`
}

func ownerGateTarget(decision string) enum.State {
	switch decision {
	case "APPROVED":
		return enum.StateSucceeded
	case "REJECTED":
		return enum.StateFailed
	case "CHANGES_REQUESTED":
		return enum.StateSucceeded
	default:
		return enum.StateCancelled
	}
}

func scheduleOccurrenceDisposition(
	spec entity.ScheduleSpec,
	scheduledFor, now time.Time,
	open bool,
) (string, string) {
	if spec.OverlapPolicy == "SKIP" && open {
		return "SKIPPED", "overlap"
	}
	if !scheduledFor.Before(now) {
		return "QUEUED", ""
	}
	switch spec.MisfirePolicy {
	case "SKIP":
		return "SKIPPED", "misfire"
	case "WITHIN_GRACE":
		if now.Sub(scheduledFor) > spec.MisfireGrace {
			return "SKIPPED", "misfire_grace_expired"
		}
	default:
	}
	return "QUEUED", ""
}

func nextScheduleRun(
	spec entity.ScheduleSpec,
	scheduledFor, now time.Time,
) (time.Time, error) {
	var next func(time.Time) time.Time
	if spec.Interval > 0 {
		next = func(current time.Time) time.Time {
			return current.Add(spec.Interval)
		}
	} else {
		schedule, err := scheduleParser.Parse(spec.Cron)
		if err != nil {
			return time.Time{}, errs.ErrInvalidInput
		}
		location, err := time.LoadLocation(spec.Timezone)
		if err != nil {
			return time.Time{}, errs.ErrInvalidInput
		}
		next = func(current time.Time) time.Time {
			return schedule.Next(current.In(location)).UTC()
		}
	}
	nextRun := next(scheduledFor)
	if spec.MisfirePolicy == "CATCH_UP" && !spec.Coalesce {
		return nextRun.UTC().Truncate(time.Microsecond), nil
	}
	for !nextRun.After(now) {
		nextRun = next(nextRun)
	}
	return nextRun.UTC().Truncate(time.Microsecond), nil
}

func validRuntimeReference(reference string) bool {
	if len(reference) < 1 || len(reference) > 512 {
		return false
	}
	for _, symbol := range reference {
		if symbol < 0x20 || symbol == 0x7f {
			return false
		}
	}
	return true
}

func (service *Service) requireCleanArtifact(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	artifactID string,
) (entity.ArtifactSpec, error) {
	_, spec, err := service.requireCleanArtifactResource(ctx, tx, principal, artifactID)
	return spec, err
}

func (service *Service) requireCleanArtifactResource(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	artifactID string,
) (entity.Resource, entity.ArtifactSpec, error) {
	artifact, err := tx.GetForUpdate(
		ctx,
		principal.OrganizationID,
		principal.ProjectID,
		artifactID,
	)
	if err != nil {
		return entity.Resource{}, entity.ArtifactSpec{}, err
	}
	spec, ok := artifact.Spec.(entity.ArtifactSpec)
	if !ok || artifact.Kind != enum.KindArtifact ||
		artifact.State != enum.StateActive ||
		spec.ScanStatus != "CLEAN" ||
		spec.ScanPolicyRevision == 0 ||
		!validSHA256Text(spec.ScanEvidenceSHA256) {
		return entity.Resource{}, entity.ArtifactSpec{}, errs.ErrStateConflict
	}
	return artifact, spec, nil
}

func (service *Service) createRuntimeRevision(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	session entity.Resource,
	sessionSpec entity.SessionSpec,
) (entity.Resource, error) {
	resources, err := tx.ListSnapshotResources(
		ctx,
		principal.OrganizationID,
		principal.ProjectID,
	)
	if err != nil {
		return entity.Resource{}, err
	}
	byID := make(map[string]entity.Resource, len(resources))
	for _, item := range resources {
		if item.State != enum.StateActive {
			return entity.Resource{}, errs.ErrStateConflict
		}
		byID[item.ID] = item
	}
	selected := make(map[string]entity.Resource)
	add := func(identifier string, kind enum.Kind) (entity.Resource, error) {
		item, ok := byID[identifier]
		if !ok || item.Kind != kind || item.State != enum.StateActive {
			return entity.Resource{}, errs.ErrNotFound
		}
		selected[item.ID] = item
		return item, nil
	}
	if _, err := add(principal.ProjectID, enum.KindProject); err != nil {
		return entity.Resource{}, err
	}
	selected[session.ID] = session
	role, err := add(sessionSpec.AgentID, enum.KindRole)
	if err != nil {
		return entity.Resource{}, err
	}
	roleSpec, ok := role.Spec.(entity.RoleSpec)
	if !ok {
		return entity.Resource{}, errs.ErrInternal
	}
	if !slices.Contains(
		roleSpec.ProviderCredentialBindingIDs,
		sessionSpec.ProviderAccountBindingID,
	) {
		return entity.Resource{}, errs.ErrPermissionDenied
	}
	prompt, err := add(roleSpec.PromptProfileID, enum.KindPromptProfile)
	if err != nil {
		return entity.Resource{}, err
	}
	promptSpec, ok := prompt.Spec.(entity.PromptProfileSpec)
	if !ok {
		return entity.Resource{}, errs.ErrInternal
	}
	providerBinding, err := add(
		sessionSpec.ProviderAccountBindingID,
		enum.KindCredentialBinding,
	)
	if err != nil {
		return entity.Resource{}, err
	}
	providerSpec, ok := providerBinding.Spec.(entity.CredentialBindingSpec)
	if !ok || providerSpec.Purpose != "provider-account" {
		return entity.Resource{}, errs.ErrStateConflict
	}
	providerAccountName := strings.ToLower(strings.TrimSpace(providerSpec.PrincipalRef))
	if separator := strings.LastIndexByte(providerAccountName, ':'); separator >= 0 {
		providerAccountName = providerAccountName[separator+1:]
	}
	if !publicProviderAccountNamePattern.MatchString(providerAccountName) {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if sessionSpec.ConversationID != "" {
		if _, err := add(sessionSpec.ConversationID, enum.KindChat); err != nil {
			return entity.Resource{}, err
		}
	}
	credentialIDs := map[string]struct{}{
		sessionSpec.ProviderAccountBindingID: {},
	}
	// Runtime authority выбирает инфраструктурные credentials из закрытого
	// server-owned набора purpose. Request/Role не может назначить их сам.
	requiredRuntimeCredentials := map[string]string{
		"control-plane-application-grant":           "",
		"runtime-materialization-application-grant": "",
		"handoff-private-key":                       "",
		"control-plane-client-tls":                  "",
		"interaction-gateway-client-tls":            "",
		"mcp-client-tls":                            "",
	}
	// Только interaction-session имеет существующий owner transport для MCP.
	// Неинтерактивные producers не получают generic token вместо AgentSession:
	// их revision остаётся без MCP binding до реализации собственного owner path.
	if sessionSpec.ConversationID != "" {
		mcpBindingID := sessionMCPBindingID(session.ID)
		mcpBinding, exists := byID[mcpBindingID]
		if !exists || mcpBinding.Kind != enum.KindCredentialBinding || mcpBinding.State != enum.StateActive ||
			mcpBinding.ParentID != session.ID || sessionSpec.AgentSessionKey == "" || sessionSpec.AgentSessionID <= 0 ||
			sessionSpec.AgentSessionBindingVersion == 0 || !validSHA256Text(sessionSpec.AgentSessionBindingSHA256) {
			return entity.Resource{}, errs.ErrStateConflict
		}
		mcpSpec, ok := mcpBinding.Spec.(entity.CredentialBindingSpec)
		if !ok || mcpSpec.Purpose != "mcp-token" || mcpSpec.Revision != sessionSpec.AgentSessionBindingVersion ||
			mcpSpec.PrincipalRef != "bot-agent-session:"+sessionSpec.AgentSessionKey ||
			mcpSpec.Ownership.SourceRevision != sessionSpec.AgentSessionBindingVersion {
			return entity.Resource{}, errs.ErrStateConflict
		}
		credentialIDs[mcpBindingID] = struct{}{}
	}
	for _, item := range byID {
		if item.Kind != enum.KindCredentialBinding || item.State != enum.StateActive {
			continue
		}
		spec, ok := item.Spec.(entity.CredentialBindingSpec)
		if !ok {
			return entity.Resource{}, errs.ErrInternal
		}
		if _, required := requiredRuntimeCredentials[spec.Purpose]; !required {
			continue
		}
		if spec.Ownership.ManagedBy != "GIT" || requiredRuntimeCredentials[spec.Purpose] != "" {
			return entity.Resource{}, errs.ErrStateConflict
		}
		requiredRuntimeCredentials[spec.Purpose] = item.ID
	}
	for _, identifier := range requiredRuntimeCredentials {
		if identifier == "" {
			return entity.Resource{}, errs.ErrStateConflict
		}
		credentialIDs[identifier] = struct{}{}
	}
	integrationIDs := make([]string, 0, len(roleSpec.IntegrationIDs))
	for _, identifier := range roleSpec.RepositoryWorkspaceIDs {
		item, err := add(identifier, enum.KindRepositoryWorkspace)
		if err != nil {
			return entity.Resource{}, err
		}
		spec, ok := item.Spec.(entity.RepositoryWorkspaceSpec)
		if !ok {
			return entity.Resource{}, errs.ErrInternal
		}
		if spec.CredentialBindingID != "" {
			credentialIDs[spec.CredentialBindingID] = struct{}{}
		}
	}
	for _, identifier := range roleSpec.IntegrationIDs {
		item, err := add(identifier, enum.KindIntegration)
		if err != nil {
			return entity.Resource{}, err
		}
		spec, ok := item.Spec.(entity.IntegrationSpec)
		if !ok {
			return entity.Resource{}, errs.ErrInternal
		}
		integrationIDs = append(integrationIDs, item.ID)
		for _, credentialID := range spec.CredentialBindingIDs {
			credentialIDs[credentialID] = struct{}{}
		}
	}
	credentialList := make([]string, 0, len(credentialIDs))
	for identifier := range credentialIDs {
		if _, err := add(identifier, enum.KindCredentialBinding); err != nil {
			return entity.Resource{}, err
		}
		credentialList = append(credentialList, identifier)
	}
	slices.Sort(credentialList)
	slices.Sort(integrationIDs)
	components := make([]entity.EffectiveResourceRef, 0, len(selected))
	for _, item := range selected {
		digest, err := entity.ProjectionSHA256(item)
		if err != nil {
			return entity.Resource{}, errs.ErrInternal
		}
		components = append(components, entity.EffectiveResourceRef{
			Kind:             item.Kind,
			ResourceID:       item.ID,
			Version:          item.Version,
			ProjectionSHA256: digest,
		})
	}
	slices.SortFunc(components, func(left, right entity.EffectiveResourceRef) int {
		if left.Kind != right.Kind {
			if left.Kind < right.Kind {
				return -1
			}
			return 1
		}
		if left.ResourceID < right.ResourceID {
			return -1
		}
		if left.ResourceID > right.ResourceID {
			return 1
		}
		return 0
	})
	compatibleComponents := make([]entity.EffectiveResourceRef, 0, len(components))
	for _, component := range components {
		// Session lineage и её monotonic turn sequence не меняют исполняемую
		// среду; все code/config/authority/credential projections остаются.
		if component.Kind != enum.KindSession {
			compatibleComponents = append(compatibleComponents, component)
		}
	}
	effectiveRuntimeSHA256, err := canonicalHash(struct {
		OrganizationID                                string
		ProjectID                                     string
		ImageDigest                                   string
		AuthorityPolicyRevision                       uint64
		AuthorityPolicySHA256                         string
		Components                                    []entity.EffectiveResourceRef
		CodexModel, CodexSandbox, CodexApprovalPolicy string
	}{
		principal.OrganizationID,
		principal.ProjectID,
		service.runtimeImageDigest,
		service.authorityPolicyRevision,
		service.authorityPolicySHA256,
		compatibleComponents,
		"gpt-5.4", "workspace-write", "never",
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	predecessorID := ""
	predecessor, err := tx.LatestRuntimeRevision(
		ctx,
		principal.OrganizationID,
		principal.ProjectID,
	)
	if err == nil {
		predecessorID = predecessor.ID
	} else if !errors.Is(err, errs.ErrNotFound) {
		return entity.Resource{}, err
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	manifestSHA256, err := canonicalHash(struct {
		OrganizationID                                   string
		ProjectID                                        string
		PredecessorRevisionID                            string
		ImageDigest                                      string
		AuthorityPolicyRevision                          uint64
		AuthorityPolicySHA256                            string
		Components                                       []entity.EffectiveResourceRef
		CreatedAt                                        time.Time
		SessionID                                        string
		RoleID                                           string
		ChatID                                           string
		ProviderCredentialBindingID, ProviderAccountName string
		CodexModel, CodexSandbox, CodexApprovalPolicy    string
	}{
		principal.OrganizationID,
		principal.ProjectID,
		predecessorID,
		service.runtimeImageDigest,
		service.authorityPolicyRevision,
		service.authorityPolicySHA256,
		components,
		now,
		session.ID,
		role.ID,
		sessionSpec.ConversationID,
		sessionSpec.ProviderAccountBindingID,
		providerAccountName,
		"gpt-5.4", "workspace-write", "never",
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	revision, err := entity.New(
		uuid.NewString(),
		principal.OrganizationID,
		principal.ProjectID,
		session.ID,
		session.OwnerActorID,
		enum.KindRuntimeRevision,
		"Effective runtime revision",
		entity.RuntimeRevisionSpec{
			ManifestSHA256:              manifestSHA256,
			ImageDigest:                 service.runtimeImageDigest,
			PromptProfileID:             prompt.ID,
			PromptRevision:              promptSpec.Revision,
			CredentialBindingIDs:        credentialList,
			IntegrationIDs:              integrationIDs,
			PredecessorRevisionID:       predecessorID,
			AuthorityPolicyVersion:      service.authorityPolicyRevision,
			AuthorityPolicySHA256:       service.authorityPolicySHA256,
			Components:                  components,
			CreatedAt:                   now,
			SessionID:                   session.ID,
			RoleID:                      role.ID,
			ChatID:                      sessionSpec.ConversationID,
			ProviderCredentialBindingID: sessionSpec.ProviderAccountBindingID,
			ProviderAccountName:         providerAccountName,
			EffectiveRuntimeSHA256:      effectiveRuntimeSHA256,
			CodexModel:                  "gpt-5.4", CodexSandbox: "workspace-write", CodexApprovalPolicy: "never",
		},
		now,
	)
	if err != nil {
		return entity.Resource{}, errs.ErrInternal
	}
	if err := tx.Insert(ctx, revision); err != nil {
		return entity.Resource{}, err
	}
	if err := service.appendMutationRecords(
		ctx,
		tx,
		principal,
		"create_runtime_revision",
		revision,
	); err != nil {
		return entity.Resource{}, err
	}
	return revision, nil
}

func hashRuntimeInput(
	sourceRef, promptDigest, runtimeDigest, processRunID string,
) string {
	return hashString(
		sourceRef + "\x00" +
			promptDigest + "\x00" +
			runtimeDigest + "\x00" +
			processRunID,
	)
}
