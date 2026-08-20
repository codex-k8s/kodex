package resource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
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
	permissionLegacyGraphPrepare     = "controlplane.legacy_graph_migration.prepare"
	permissionLegacyGraphMaterialize = "controlplane.legacy_graph_migration.materialize"
	permissionLegacyGraphRead        = "controlplane.legacy_graph_migration.read"
	permissionLegacyGraphAbort       = "controlplane.legacy_graph_migration.abort"
	legacyMigrationWorkload          = "legacy-data-migration"
	legacyMigrationSPIFFEID          = "spiffe://mattercodex.local/ns/mattercodex-system/sa/legacy-data-migration"
)

type PrepareLegacyGraphMigrationInput struct {
	Principal      value.Principal
	IdempotencyKey string
	Plan           entity.LegacyGraphPlan
}

type LegacyGraphMigrationCommandInput struct {
	Principal      value.Principal
	IdempotencyKey string
	PlanID         string
	SemanticSHA256 string
}

type GetLegacyGraphMigrationInput struct {
	Principal value.Principal
	PlanID    string
	Verify    bool
}

func legacyMigrationPrincipal(principal value.Principal, permission string) error {
	if err := authorize(principal, permission); err != nil {
		return err
	}
	if principal.ProjectID != "" || principal.CallerWorkload != legacyMigrationWorkload ||
		principal.CallerSPIFFEID != legacyMigrationSPIFFEID ||
		principal.AuthoritySource != "LEGACY_MIGRATION" {
		return errs.ErrPermissionDenied
	}
	return nil
}

func (service *Service) PrepareLegacyGraphMigration(
	ctx context.Context,
	input PrepareLegacyGraphMigrationInput,
) (entity.LegacyGraphMigration, error) {
	if err := legacyMigrationPrincipal(input.Principal, permissionLegacyGraphPrepare); err != nil {
		return entity.LegacyGraphMigration{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.Plan.PlanID) != nil || input.Plan.SourceRootReference != input.Principal.AuthorityReference ||
		input.Plan.SourceRootSHA256 != input.Principal.AuthorityDigest {
		return entity.LegacyGraphMigration{}, errs.ErrInvalidInput
	}
	semanticInput, err := legacySemanticPlan(input.Plan)
	if err != nil {
		return entity.LegacyGraphMigration{}, err
	}
	semanticSHA256, err := canonicalHash(semanticInput)
	if err != nil {
		return entity.LegacyGraphMigration{}, errs.ErrInvalidInput
	}
	input.Plan.Operations = slices.Clone(input.Plan.Operations)
	for index := range input.Plan.Operations {
		input.Plan.Operations[index].TargetID = uuid.NewString()
	}
	if err := validateLegacyGraphPlan(input.Plan); err != nil {
		return entity.LegacyGraphMigration{}, err
	}
	payload, err := json.Marshal(input.Plan)
	if err != nil || len(payload) > maximumLegacyPlanBytes {
		return entity.LegacyGraphMigration{}, errs.ErrInvalidInput
	}
	requestSHA256, err := canonicalHash(struct {
		Identity       commandIdentity
		SemanticSHA256 string
	}{legacyMigrationCommandIdentity(input.Principal), semanticSHA256})
	if err != nil {
		return entity.LegacyGraphMigration{}, errs.ErrInvalidInput
	}
	keySHA256 := hashString(input.IdempotencyKey)
	projectID := input.Plan.Operations[0].TargetID
	archivedCount := uint32(0)
	for _, disposition := range input.Plan.Dispositions {
		if disposition.Disposition == entity.LegacyDispositionArchiveTerminal {
			archivedCount++
		}
	}
	var result entity.LegacyGraphMigration
	err = service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID,
		ActorID:        input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		migrationTx, ok := tx.(domainrepo.LegacyGraphMigrationTransaction)
		if !ok {
			return errs.ErrInternal
		}
		existing, getErr := migrationTx.GetLegacyGraphPlanForUpdate(ctx, input.Plan.PlanID)
		if getErr == nil {
			if existing.OrganizationID != input.Principal.OrganizationID ||
				existing.OwnerActorID != input.Principal.ActorID ||
				existing.SourceRootReference != input.Plan.SourceRootReference ||
				existing.SourceRootSHA256 != input.Plan.SourceRootSHA256 ||
				existing.IdempotencyKeySHA256 != keySHA256 || existing.RequestSHA256 != requestSHA256 ||
				existing.SemanticSHA256 != semanticSHA256 || existing.SourceSnapshotSHA256 != input.Plan.SourceSnapshotSHA256 {
				return errs.ErrAborted
			}
			var readErr error
			result, readErr = legacyMigrationResult(ctx, tx, migrationTx, existing,
				existing.State == entity.LegacyMigrationCommitted)
			return readErr
		}
		if !errors.Is(getErr, errs.ErrNotFound) {
			return getErr
		}
		if _, compileErr := service.compileLegacyGraph(input.Principal, input.Plan); compileErr != nil {
			return compileErr
		}
		now, timeErr := tx.CurrentTime(ctx)
		if timeErr != nil {
			return timeErr
		}
		record := domainrepo.LegacyGraphPlanRecord{
			PlanID: input.Plan.PlanID, OrganizationID: input.Principal.OrganizationID,
			OwnerActorID: input.Principal.ActorID, SourceRootReference: input.Plan.SourceRootReference,
			SourceRootSHA256: input.Plan.SourceRootSHA256, SourceSnapshotSHA256: input.Plan.SourceSnapshotSHA256,
			IdempotencyKeySHA256: keySHA256, RequestSHA256: requestSHA256, SemanticSHA256: semanticSHA256,
			ProjectID: projectID, State: entity.LegacyMigrationPrepared,
			VerificationState: entity.LegacyVerificationOK, Payload: payload,
			OperationCount: uint32(len(input.Plan.Operations)), ArchivedSourceCount: archivedCount,
			PreparedAt: now,
		}
		if err := migrationTx.InsertLegacyGraphPlan(ctx, record); err != nil {
			return err
		}
		for _, disposition := range input.Plan.Dispositions {
			if err := migrationTx.InsertLegacySourceDisposition(ctx, domainrepo.LegacySourceDispositionRecord{
				PlanID: input.Plan.PlanID, SourceTable: disposition.SourceTable,
				Disposition: disposition.Disposition, SourceSHA256: disposition.SourceSHA256,
				TerminalStateSHA256: disposition.TerminalStateSHA256, RowCount: disposition.RowCount,
			}); err != nil {
				return err
			}
		}
		for ordinal, operation := range input.Plan.Operations {
			source, kind, _ := legacyOperationSource(operation)
			operation.TargetID = ""
			inputSHA256, hashErr := canonicalHash(operation)
			if hashErr != nil {
				return errs.ErrInvalidInput
			}
			provenanceSHA256, hashErr := canonicalHash(struct {
				RootReference, RootSHA256 string
				Source                    entity.LegacyOperationSource
				Kind                      string
				Operation                 entity.LegacyGraphOperation
			}{input.Plan.SourceRootReference, input.Plan.SourceRootSHA256, source, kind, operation})
			if hashErr != nil {
				return errs.ErrInvalidInput
			}
			if err := migrationTx.InsertLegacyOperationIntent(ctx, domainrepo.LegacyOperationRecord{
				PlanID: input.Plan.PlanID, Ordinal: uint32(ordinal + 1), OperationKind: kind,
				InputSHA256: inputSHA256, TargetID: input.Plan.Operations[ordinal].TargetID,
				TargetKind: kind, ProvenanceSHA256: provenanceSHA256,
			}); err != nil {
				return err
			}
		}
		var readErr error
		result, readErr = legacyMigrationResult(ctx, tx, migrationTx, record, false)
		return readErr
	})
	return result, err
}

func legacySemanticPlan(plan entity.LegacyGraphPlan) (entity.LegacyGraphPlan, error) {
	result := plan
	result.Operations = slices.Clone(plan.Operations)
	for index := range result.Operations {
		if result.Operations[index].TargetID != "" {
			return entity.LegacyGraphPlan{}, errs.ErrInvalidInput
		}
		result.Operations[index].TargetID = ""
	}
	return result, nil
}

func legacyMigrationCommandIdentity(principal value.Principal) commandIdentity {
	result := identity(principal)
	// Grant JTI/revision/generation и обслуживаемая policy revision являются
	// transport replay metadata. Exact retry после refresh обязан находить тот
	// же durable intent по actor/organization/workload/source root.
	result.PolicyRevision = 0
	result.AuthorityGeneration = 0
	result.AuthorityRevision = 0
	result.GrantGeneration = 0
	return result
}

func (service *Service) MaterializeLegacyGraphMigration(
	ctx context.Context,
	input LegacyGraphMigrationCommandInput,
) (entity.LegacyGraphMigration, error) {
	if err := legacyMigrationPrincipal(input.Principal, permissionLegacyGraphMaterialize); err != nil {
		return entity.LegacyGraphMigration{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.PlanID) != nil ||
		!validSHA256Text(input.SemanticSHA256) {
		return entity.LegacyGraphMigration{}, errs.ErrInvalidInput
	}
	var result entity.LegacyGraphMigration
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID, ActorID: input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		migrationTx, ok := tx.(domainrepo.LegacyGraphMigrationTransaction)
		if !ok {
			return errs.ErrInternal
		}
		planRecord, err := migrationTx.GetLegacyGraphPlanForUpdate(ctx, input.PlanID)
		if err != nil {
			return err
		}
		if err := requireLegacyPlanOwner(input.Principal, planRecord); err != nil {
			return err
		}
		if planRecord.IdempotencyKeySHA256 != hashString(input.IdempotencyKey) ||
			planRecord.SemanticSHA256 != input.SemanticSHA256 {
			return errs.ErrAborted
		}
		if planRecord.State == entity.LegacyMigrationAborted {
			return errs.ErrFailedPrecondition
		}
		if planRecord.State == entity.LegacyMigrationCommitted {
			result, err = legacyMigrationResult(ctx, tx, migrationTx, planRecord, true)
			return err
		}
		var plan entity.LegacyGraphPlan
		if len(planRecord.Payload) > maximumLegacyPlanBytes || decodeLegacyGraphPlan(planRecord.Payload, &plan) != nil ||
			validateLegacyGraphPlan(plan) != nil || plan.PlanID != planRecord.PlanID ||
			plan.SourceRootReference != planRecord.SourceRootReference ||
			plan.SourceRootSHA256 != planRecord.SourceRootSHA256 ||
			plan.SourceSnapshotSHA256 != planRecord.SourceSnapshotSHA256 {
			return errs.ErrDataLoss
		}
		receipts, err := migrationTx.ListLegacyOperationReceipts(ctx, plan.PlanID)
		if err != nil || len(receipts) != len(plan.Operations) {
			return errs.ErrDataLoss
		}
		dispositions, err := migrationTx.ListLegacySourceDispositions(ctx, plan.PlanID)
		if err != nil || validatePersistedLegacyPlan(planRecord, plan, dispositions, receipts) != nil {
			return errs.ErrDataLoss
		}
		compiled, err := service.compileLegacyGraph(input.Principal, plan)
		if err != nil {
			return err
		}
		for index, operation := range plan.Operations {
			receipt := receipts[index]
			if receipt.Ordinal != uint32(index+1) || receipt.TargetID != operation.TargetID ||
				receipt.TargetVersion != 0 || receipt.MaterializedAt != (time.Time{}) {
				return errs.ErrDataLoss
			}
			materialized, materializeErr := service.materializeLegacyOperation(
				ctx, tx, migrationTx, input.Principal, plan, operation, compiled, receipt,
			)
			if materializeErr != nil {
				if errors.Is(materializeErr, errs.ErrDataLoss) && errs.SafeCode(materializeErr) == "" {
					materializeErr = errs.WithSafeCode(materializeErr,
						fmt.Sprintf("LEGACY_MATERIALIZE_%s_%d", receipt.OperationKind, receipt.Ordinal))
				}
				return materializeErr
			}
			if materialized.TargetKind == string(enum.KindProject) && materialized.TargetID != planRecord.ProjectID {
				return errs.ErrStateConflict
			}
		}
		verified, err := migrationTx.ListLegacyOperationReceipts(ctx, plan.PlanID)
		if err != nil || len(verified) != len(plan.Operations) {
			return errs.ErrDataLoss
		}
		for _, receipt := range verified {
			evidence, verifyErr := migrationTx.VerifyLegacyOperationEvidence(ctx, receipt)
			if verifyErr != nil {
				return verifyErr
			}
			if !evidence.Valid() {
				return errs.WithSafeCode(errs.ErrDataLoss, legacyEvidenceFailureCode(receipt, evidence))
			}
		}
		now, err := tx.CurrentTime(ctx)
		if err != nil {
			return err
		}
		verifiedRecord := planRecord
		verifiedRecord.State = entity.LegacyMigrationCommitted
		verifiedRecord.VerificationState = entity.LegacyVerificationOK
		verifiedRecord.TerminalAt = now
		verifiedResult, err := legacyMigrationResult(ctx, tx, migrationTx, verifiedRecord, true)
		if err != nil {
			return err
		}
		if verifiedResult.VerificationState != entity.LegacyVerificationOK || len(verifiedResult.Drift) != 0 {
			return errs.WithSafeCode(errs.ErrDataLoss, legacyDriftFailureCode(verifiedResult.Drift))
		}
		if err := migrationTx.SetLegacyGraphPlanTerminal(ctx, plan.PlanID, entity.LegacyMigrationCommitted,
			entity.LegacyVerificationOK, now); err != nil {
			return err
		}
		result = verifiedResult
		return nil
	})
	return result, err
}

func (service *Service) GetLegacyGraphMigration(
	ctx context.Context,
	input GetLegacyGraphMigrationInput,
) (entity.LegacyGraphMigration, error) {
	if err := legacyMigrationPrincipal(input.Principal, permissionLegacyGraphRead); err != nil {
		return entity.LegacyGraphMigration{}, err
	}
	if value.ValidateID(input.PlanID) != nil {
		return entity.LegacyGraphMigration{}, errs.ErrInvalidInput
	}
	var result entity.LegacyGraphMigration
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID, ActorID: input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		migrationTx, ok := tx.(domainrepo.LegacyGraphMigrationTransaction)
		if !ok {
			return errs.ErrInternal
		}
		record, err := migrationTx.GetLegacyGraphPlanForUpdate(ctx, input.PlanID)
		if err != nil {
			return err
		}
		if err := requireLegacyPlanOwner(input.Principal, record); err != nil {
			return err
		}
		result, err = legacyMigrationResult(ctx, tx, migrationTx, record, input.Verify)
		return err
	})
	return result, err
}

func (service *Service) AbortLegacyGraphMigration(
	ctx context.Context,
	input LegacyGraphMigrationCommandInput,
) (entity.LegacyGraphMigration, error) {
	if err := legacyMigrationPrincipal(input.Principal, permissionLegacyGraphAbort); err != nil {
		return entity.LegacyGraphMigration{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.PlanID) != nil ||
		!validSHA256Text(input.SemanticSHA256) {
		return entity.LegacyGraphMigration{}, errs.ErrInvalidInput
	}
	var result entity.LegacyGraphMigration
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID, ActorID: input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		migrationTx, ok := tx.(domainrepo.LegacyGraphMigrationTransaction)
		if !ok {
			return errs.ErrInternal
		}
		record, err := migrationTx.GetLegacyGraphPlanForUpdate(ctx, input.PlanID)
		if err != nil {
			return err
		}
		if err := requireLegacyPlanOwner(input.Principal, record); err != nil {
			return err
		}
		if record.IdempotencyKeySHA256 != hashString(input.IdempotencyKey) || record.SemanticSHA256 != input.SemanticSHA256 {
			return errs.ErrAborted
		}
		if record.State == entity.LegacyMigrationCommitted {
			return errs.ErrFailedPrecondition
		}
		if record.State == entity.LegacyMigrationPrepared {
			now, timeErr := tx.CurrentTime(ctx)
			if timeErr != nil {
				return timeErr
			}
			if err := migrationTx.SetLegacyGraphPlanTerminal(ctx, record.PlanID, entity.LegacyMigrationAborted,
				entity.LegacyVerificationOK, now); err != nil {
				return err
			}
			record.State, record.TerminalAt = entity.LegacyMigrationAborted, now
		}
		result, err = legacyMigrationResult(ctx, tx, migrationTx, record, false)
		return err
	})
	return result, err
}

func requireLegacyPlanOwner(principal value.Principal, record domainrepo.LegacyGraphPlanRecord) error {
	if record.OrganizationID != principal.OrganizationID || record.OwnerActorID != principal.ActorID ||
		record.SourceRootReference != principal.AuthorityReference || record.SourceRootSHA256 != principal.AuthorityDigest {
		return errs.ErrNotFound
	}
	return nil
}

func legacyMigrationRecordResult(record domainrepo.LegacyGraphPlanRecord) entity.LegacyGraphMigration {
	return entity.LegacyGraphMigration{
		PlanID: record.PlanID, State: record.State, VerificationState: record.VerificationState,
		SemanticSHA256: record.SemanticSHA256, SourceSnapshotSHA256: record.SourceSnapshotSHA256,
		ProjectID: record.ProjectID, OperationCount: record.OperationCount,
		ArchivedSourceCount: record.ArchivedSourceCount, PreparedAt: record.PreparedAt, TerminalAt: record.TerminalAt,
	}
}

func legacyMigrationResult(ctx context.Context, tx domainrepo.Transaction,
	migrationTx domainrepo.LegacyGraphMigrationTransaction,
	record domainrepo.LegacyGraphPlanRecord, verify bool,
) (entity.LegacyGraphMigration, error) {
	result := legacyMigrationRecordResult(record)
	receipts, err := migrationTx.ListLegacyOperationReceipts(ctx, record.PlanID)
	if err != nil {
		return entity.LegacyGraphMigration{}, err
	}
	dispositions, err := migrationTx.ListLegacySourceDispositions(ctx, record.PlanID)
	if err != nil {
		return entity.LegacyGraphMigration{}, err
	}
	var persistedPlan entity.LegacyGraphPlan
	integrityErr := decodeLegacyGraphPlan(record.Payload, &persistedPlan)
	if integrityErr == nil {
		integrityErr = validatePersistedLegacyPlan(record, persistedPlan, dispositions, receipts)
	}
	if integrityErr != nil {
		if !verify || record.State != entity.LegacyMigrationCommitted {
			return entity.LegacyGraphMigration{}, errs.ErrDataLoss
		}
		result.VerificationState = entity.LegacyVerificationDrift
		result.Drift = append(result.Drift, entity.LegacyGraphDrift{Predicate: "persisted plan does not match immutable receipts"})
	}
	result.OperationReceipts = make([]entity.LegacyOperationReceipt, 0, len(receipts))
	if verify && record.State == entity.LegacyMigrationCommitted {
		workspaceTx, ok := tx.(domainrepo.WorkspaceRecoveryTransaction)
		if !ok {
			return entity.LegacyGraphMigration{}, errs.ErrInternal
		}
		if err := workspaceTx.SwitchWorkspaceProject(ctx, record.ProjectID); err != nil {
			return entity.LegacyGraphMigration{}, err
		}
	}
	for _, receipt := range receipts {
		result.OperationReceipts = append(result.OperationReceipts, entity.LegacyOperationReceipt{
			Ordinal: receipt.Ordinal, OperationKind: receipt.OperationKind, InputSHA256: receipt.InputSHA256,
			TargetID: receipt.TargetID, TargetKind: receipt.TargetKind, TargetVersion: receipt.TargetVersion,
			TargetState: receipt.TargetState, ProjectionSHA256: receipt.ProjectionSHA256,
			ProvenanceSHA256:         receipt.ProvenanceSHA256,
			ProvenanceEvidenceSHA256: receipt.ProvenanceEvidenceSHA256,
			AuditIDs:                 slices.Clone(receipt.AuditIDs),
			EventIDs:                 slices.Clone(receipt.EventIDs), EventSequences: slices.Clone(receipt.EventSequences),
		})
		if verify && record.State == entity.LegacyMigrationCommitted {
			provenanceProjection, provenanceErr := migrationTx.GetLegacyProvenanceProjection(
				ctx, receipt.PlanID, receipt.Ordinal,
			)
			if provenanceErr != nil && !errors.Is(provenanceErr, errs.ErrDataLoss) &&
				!errors.Is(provenanceErr, errs.ErrNotFound) {
				return entity.LegacyGraphMigration{}, provenanceErr
			}
			provenanceSHA256, provenanceHashErr := canonicalJSONTextHash(provenanceProjection)
			if provenanceErr != nil || provenanceHashErr != nil ||
				provenanceSHA256 != receipt.ProvenanceEvidenceSHA256 {
				result.VerificationState = entity.LegacyVerificationDrift
				if len(result.Drift) < 32 {
					result.Drift = append(result.Drift, entity.LegacyGraphDrift{
						Ordinal: receipt.Ordinal, Predicate: "provenance projection does not match committed receipt",
					})
				}
			}
			evidence, verifyErr := migrationTx.VerifyLegacyOperationEvidence(ctx, receipt)
			if verifyErr != nil {
				return entity.LegacyGraphMigration{}, verifyErr
			}
			if !evidence.Valid() {
				result.VerificationState = entity.LegacyVerificationDrift
				if len(result.Drift) < 32 {
					result.Drift = append(result.Drift, entity.LegacyGraphDrift{
						Ordinal: receipt.Ordinal, Predicate: "operation evidence does not match committed receipt",
					})
				}
			}
			if legacyCustomOperationKind(receipt.TargetKind) {
				projection, projectionErr := migrationTx.GetLegacyCustomOperationProjection(
					ctx, receipt.PlanID, receipt.Ordinal,
				)
				if projectionErr != nil && !errors.Is(projectionErr, errs.ErrDataLoss) &&
					!errors.Is(projectionErr, errs.ErrNotFound) {
					return entity.LegacyGraphMigration{}, projectionErr
				}
				projectionSHA256, projectionHashErr := canonicalJSONTextHash(projection)
				if projectionErr != nil || projectionHashErr != nil || projectionSHA256 != receipt.ProjectionSHA256 {
					result.VerificationState = entity.LegacyVerificationDrift
					if len(result.Drift) < 32 {
						result.Drift = append(result.Drift, entity.LegacyGraphDrift{
							Ordinal: receipt.Ordinal, Predicate: "custom target projection does not match committed receipt",
						})
					}
				}
			}
			if kind := enum.Kind(receipt.TargetKind); kind.Valid() {
				resource, resourceErr := tx.Get(ctx, record.OrganizationID, record.ProjectID, receipt.TargetID)
				if resourceErr != nil && !errors.Is(resourceErr, errs.ErrNotFound) &&
					!errors.Is(resourceErr, errs.ErrDataLoss) {
					return entity.LegacyGraphMigration{}, resourceErr
				}
				projectionSHA256, projectionErr := "", resourceErr
				if resourceErr == nil {
					projectionSHA256, projectionErr = entity.ProjectionSHA256(resource)
				}
				if resourceErr != nil || projectionErr != nil || resource.Kind != kind ||
					resource.Version != receipt.TargetVersion || resource.State != receipt.TargetState ||
					projectionSHA256 != receipt.ProjectionSHA256 {
					result.VerificationState = entity.LegacyVerificationDrift
					if len(result.Drift) < 32 {
						result.Drift = append(result.Drift, entity.LegacyGraphDrift{
							Ordinal: receipt.Ordinal, Predicate: "target projection does not match committed receipt",
						})
					}
					continue
				}
				if legacyProtectedHistoryKind(kind) {
					protectedTx, ok := tx.(domainrepo.ProtectedTransaction)
					if !ok {
						return entity.LegacyGraphMigration{}, errs.ErrInternal
					}
					history, historyErr := protectedTx.GetProtectedResourceHistoryVersion(ctx, resource.ID, resource.Version)
					if historyErr != nil && !errors.Is(historyErr, errs.ErrNotFound) &&
						!errors.Is(historyErr, errs.ErrDataLoss) {
						return entity.LegacyGraphMigration{}, historyErr
					}
					if historyErr != nil || history.SnapshotSHA256 != receipt.ProjectionSHA256 {
						result.VerificationState = entity.LegacyVerificationDrift
						if len(result.Drift) < 32 {
							result.Drift = append(result.Drift, entity.LegacyGraphDrift{
								Ordinal: receipt.Ordinal, Predicate: "protected history does not match committed receipt",
							})
						}
					}
				}
				if kind == enum.KindRuntimeRevision {
					spec, ok := resource.Spec.(entity.RuntimeRevisionSpec)
					componentsOK, componentsErr := verifyLegacyRuntimeComponents(ctx, tx, record, spec)
					if componentsErr != nil {
						return entity.LegacyGraphMigration{}, componentsErr
					}
					if !ok || !componentsOK {
						result.VerificationState = entity.LegacyVerificationDrift
						if len(result.Drift) < 32 {
							result.Drift = append(result.Drift, entity.LegacyGraphDrift{
								Ordinal: receipt.Ordinal, Predicate: "runtime component projection is drifted",
							})
						}
					}
				}
			}
		}
	}
	if len(receipts) != int(record.OperationCount) {
		if record.State == entity.LegacyMigrationCommitted {
			result.VerificationState = entity.LegacyVerificationDrift
			result.Drift = append(result.Drift, entity.LegacyGraphDrift{Predicate: "operation receipt cardinality mismatch"})
		} else {
			return entity.LegacyGraphMigration{}, errs.ErrDataLoss
		}
	}
	return result, nil
}

func legacyEvidenceFailureCode(receipt domainrepo.LegacyOperationRecord,
	evidence domainrepo.LegacyOperationEvidence,
) string {
	predicate := "UNKNOWN"
	switch {
	case !evidence.Audit:
		predicate = "AUDIT"
	case !evidence.Events:
		predicate = "EVENTS"
	case !evidence.Provenance:
		predicate = "PROVENANCE"
	case !evidence.Target:
		predicate = "TARGET"
	}
	return fmt.Sprintf("LEGACY_EVIDENCE_%s_%s_%d", predicate, receipt.TargetKind, receipt.Ordinal)
}

func legacyDriftFailureCode(drift []entity.LegacyGraphDrift) string {
	if len(drift) == 0 {
		return "LEGACY_DRIFT_UNKNOWN"
	}
	predicate := "UNKNOWN"
	switch drift[0].Predicate {
	case "persisted plan does not match immutable receipts":
		predicate = "PLAN"
	case "provenance projection does not match committed receipt":
		predicate = "PROVENANCE_PROJECTION"
	case "operation evidence does not match committed receipt":
		predicate = "OPERATION_EVIDENCE"
	case "custom target projection does not match committed receipt":
		predicate = "CUSTOM_PROJECTION"
	case "target projection does not match committed receipt":
		predicate = "TARGET_PROJECTION"
	case "protected history does not match committed receipt":
		predicate = "PROTECTED_HISTORY"
	case "runtime component projection is drifted":
		predicate = "RUNTIME_COMPONENTS"
	case "operation receipt cardinality mismatch":
		predicate = "RECEIPT_CARDINALITY"
	}
	if drift[0].Ordinal == 0 {
		return "LEGACY_DRIFT_" + predicate
	}
	return fmt.Sprintf("LEGACY_DRIFT_%s_%d", predicate, drift[0].Ordinal)
}

func validatePersistedLegacyPlan(record domainrepo.LegacyGraphPlanRecord, plan entity.LegacyGraphPlan,
	dispositions []domainrepo.LegacySourceDispositionRecord,
	receipts []domainrepo.LegacyOperationRecord,
) error {
	if validateLegacyGraphPlan(plan) != nil || plan.PlanID != record.PlanID ||
		plan.SourceRootReference != record.SourceRootReference || plan.SourceRootSHA256 != record.SourceRootSHA256 ||
		plan.SourceSnapshotSHA256 != record.SourceSnapshotSHA256 || len(plan.Operations) != int(record.OperationCount) ||
		len(plan.Dispositions) != len(dispositions) || len(plan.Operations) != len(receipts) ||
		plan.Operations[0].TargetID != record.ProjectID {
		return errs.ErrDataLoss
	}
	semanticPlan := plan
	semanticPlan.Operations = slices.Clone(plan.Operations)
	for index := range semanticPlan.Operations {
		semanticPlan.Operations[index].TargetID = ""
	}
	semanticSHA256, err := canonicalHash(semanticPlan)
	if err != nil || semanticSHA256 != record.SemanticSHA256 {
		return errs.ErrDataLoss
	}
	storedDispositions := make(map[string]domainrepo.LegacySourceDispositionRecord, len(dispositions))
	for _, disposition := range dispositions {
		storedDispositions[disposition.SourceTable] = disposition
	}
	for _, disposition := range plan.Dispositions {
		stored, ok := storedDispositions[disposition.SourceTable]
		if !ok || stored.PlanID != plan.PlanID || stored.Disposition != disposition.Disposition ||
			stored.RowCount != disposition.RowCount || stored.SourceSHA256 != disposition.SourceSHA256 ||
			stored.TerminalStateSHA256 != disposition.TerminalStateSHA256 {
			return errs.ErrDataLoss
		}
	}
	if validatePersistedLegacyOperationIntents(plan, receipts) != nil {
		return errs.ErrDataLoss
	}
	return nil
}

func validatePersistedLegacyOperationIntents(plan entity.LegacyGraphPlan,
	receipts []domainrepo.LegacyOperationRecord,
) error {
	if len(plan.Operations) != len(receipts) {
		return errs.ErrDataLoss
	}
	for index, operation := range plan.Operations {
		receipt := receipts[index]
		source, kind, sourceErr := legacyOperationSource(operation)
		if sourceErr != nil || receipt.PlanID != plan.PlanID || receipt.Ordinal != uint32(index+1) ||
			receipt.OperationKind != kind || receipt.TargetKind != kind || receipt.TargetID != operation.TargetID {
			return errs.ErrDataLoss
		}
		operation.TargetID = ""
		inputSHA256, hashErr := canonicalHash(operation)
		if hashErr != nil || inputSHA256 != receipt.InputSHA256 {
			return errs.ErrDataLoss
		}
		lineageSHA256, hashErr := canonicalHash(struct {
			RootReference, RootSHA256 string
			Source                    entity.LegacyOperationSource
			Kind                      string
			Operation                 entity.LegacyGraphOperation
		}{plan.SourceRootReference, plan.SourceRootSHA256, source, kind, operation})
		if hashErr != nil || lineageSHA256 != receipt.ProvenanceSHA256 {
			return errs.ErrDataLoss
		}
		if receipt.MaterializedAt.IsZero() != (receipt.ProvenanceEvidenceSHA256 == "") ||
			(receipt.ProvenanceEvidenceSHA256 != "" && !validSHA256Text(receipt.ProvenanceEvidenceSHA256)) {
			return errs.ErrDataLoss
		}
	}
	return nil
}

func legacyCustomOperationKind(kind string) bool {
	switch kind {
	case "TURN_ATTEMPT", "DELEGATION_EDGE", "CALLBACK_MANIFEST", "CALLBACK_DELIVERY":
		return true
	default:
		return false
	}
}

func canonicalJSONTextHash(encoded string) (string, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", errors.New("persisted projection has trailing data")
	}
	return canonicalHash(value)
}

func decodeLegacyGraphPlan(payload []byte, target *entity.LegacyGraphPlan) error {
	if target == nil || len(payload) == 0 || len(payload) > maximumLegacyPlanBytes {
		return errors.New("persisted legacy graph plan is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("persisted legacy graph plan has trailing data")
	}
	return nil
}

func verifyLegacyRuntimeComponents(ctx context.Context, tx domainrepo.Transaction,
	record domainrepo.LegacyGraphPlanRecord, spec entity.RuntimeRevisionSpec,
) (bool, error) {
	for _, component := range spec.Components {
		var resource entity.Resource
		var err error
		if component.Kind == enum.KindRole || component.Kind == enum.KindPromptProfile {
			projectionTx, ok := tx.(domainrepo.RuntimeProjectionTransaction)
			if !ok {
				return false, errs.ErrInternal
			}
			resource, err = projectionTx.GetDerivedRuntimeResource(
				ctx, record.OrganizationID, record.ProjectID, component.ResourceID, component.Kind, component.Version,
			)
		} else {
			resource, err = tx.Get(ctx, record.OrganizationID, record.ProjectID, component.ResourceID)
		}
		if err != nil {
			if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrDataLoss) {
				return false, nil
			}
			return false, err
		}
		if resource.Kind != component.Kind || resource.Version != component.Version {
			return false, nil
		}
		projectionSHA256, err := entity.ProjectionSHA256(resource)
		if err != nil || projectionSHA256 != component.ProjectionSHA256 {
			return false, nil
		}
	}
	return true, nil
}

type compiledLegacyGraph struct {
	Resources map[string]entity.Resource
	Sources   map[string]entity.LegacyOperationSource
	Kinds     map[string]string
	Derived   map[string][]entity.Resource
}

func (service *Service) materializeLegacyOperation(ctx context.Context, tx domainrepo.Transaction,
	migrationTx domainrepo.LegacyGraphMigrationTransaction, principal value.Principal,
	plan entity.LegacyGraphPlan, operation entity.LegacyGraphOperation, compiled compiledLegacyGraph,
	receipt domainrepo.LegacyOperationRecord,
) (domainrepo.LegacyOperationRecord, error) {
	source, kind, err := legacyOperationSource(operation)
	if err != nil || kind != receipt.OperationKind || source != compiled.Sources[source.LocalRef] {
		return domainrepo.LegacyOperationRecord{}, errs.ErrDataLoss
	}
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return domainrepo.LegacyOperationRecord{}, err
	}
	var targetVersion uint64 = 1
	var targetState = enum.StateActive
	var projectionSHA256 string
	if resource, ok := compiled.Resources[source.LocalRef]; ok {
		if err := tx.Insert(ctx, resource); err != nil {
			return domainrepo.LegacyOperationRecord{}, err
		}
		if resource.Kind == enum.KindProject {
			workspaceTx, ok := tx.(domainrepo.WorkspaceRecoveryTransaction)
			if !ok {
				return domainrepo.LegacyOperationRecord{}, errs.ErrInternal
			}
			if err := workspaceTx.SwitchWorkspaceProject(ctx, resource.ID); err != nil {
				return domainrepo.LegacyOperationRecord{}, err
			}
		}
		if legacyProtectedHistoryKind(resource.Kind) {
			protectedTx, ok := tx.(domainrepo.ProtectedTransaction)
			if !ok {
				return domainrepo.LegacyOperationRecord{}, errs.ErrInternal
			}
			projectionSHA256, hashErr := entity.ProjectionSHA256(resource)
			if hashErr != nil {
				return domainrepo.LegacyOperationRecord{}, errs.ErrInternal
			}
			if err := protectedTx.AppendProtectedResourceHistory(ctx, domainrepo.ProtectedResourceHistory{
				Resource: resource, Action: "materialize_legacy_" + strings.ToLower(kind),
				SnapshotSHA256: projectionSHA256, OccurredAt: resource.UpdatedAt,
			}); err != nil {
				return domainrepo.LegacyOperationRecord{}, err
			}
		}
		if resource.Kind == enum.KindAgent {
			projectionTx, ok := tx.(domainrepo.RuntimeProjectionTransaction)
			if !ok {
				return domainrepo.LegacyOperationRecord{}, errs.ErrInternal
			}
			agentInput := operation.Agent
			if agentInput == nil {
				return domainrepo.LegacyOperationRecord{}, errs.ErrDataLoss
			}
			for _, derived := range compiled.Derived[source.LocalRef] {
				sourceResource, sourceKind := resource, enum.KindAgent
				if derived.Kind == enum.KindPromptProfile {
					sourceResource = compiled.Resources[agentInput.InstructionSetRef]
					sourceKind = enum.KindInstructionSet
				}
				sourceSHA256, hashErr := entity.ProjectionSHA256(sourceResource)
				if hashErr != nil {
					return domainrepo.LegacyOperationRecord{}, errs.ErrInternal
				}
				if err := projectionTx.InsertDerivedRuntimeResource(ctx, derived, sourceKind,
					sourceResource.ID, sourceResource.Version, sourceSHA256); err != nil {
					return domainrepo.LegacyOperationRecord{}, err
				}
			}
		}
		projectionSHA256, err = entity.ProjectionSHA256(resource)
		if err != nil {
			return domainrepo.LegacyOperationRecord{}, errs.ErrInternal
		}
		targetVersion, targetState = resource.Version, resource.State
	} else {
		switch {
		case operation.TurnAttempt != nil:
			turn := compiled.Resources[operation.TurnAttempt.TurnRef]
			runtimeRevision := compiled.Resources[operation.TurnAttempt.RuntimeRevisionRef]
			finishedAt := operation.TurnAttempt.FinishedAt
			if err := migrationTx.SaveLegacyTurnAttempt(ctx, domainrepo.TurnAttempt{
				TurnID: turn.ID, Attempt: operation.TurnAttempt.Attempt,
				WorkloadID: legacyMigrationWorkload, AuthorityGeneration: principal.AuthorityGeneration,
				State: operation.TurnAttempt.State, Outcome: operation.TurnAttempt.Outcome,
				InputSHA256: operation.TurnAttempt.ImmutableInputSHA256, LeaseFence: 1,
				StartedAt: operation.TurnAttempt.StartedAt, FinishedAt: finishedAt,
			}, runtimeRevision.ID, runtimeRevision.Version); err != nil {
				return domainrepo.LegacyOperationRecord{}, err
			}
			targetState = enum.State(operation.TurnAttempt.State)
		case operation.DelegationEdge != nil:
			input := operation.DelegationEdge
			parentProcess := compiled.Resources[input.ParentProcessRef]
			parentSession := compiled.Resources[input.ParentSessionRef]
			parentTurn := compiled.Resources[input.ParentTurnRef]
			childSession := compiled.Resources[input.ChildSessionRef]
			childTurn := compiled.Resources[input.ChildTurnRef]
			parentAttempt := plan.Operations[legacyRefIndex(plan, input.ParentAttemptRef)].TurnAttempt
			childAttempt := plan.Operations[legacyRefIndex(plan, input.ChildAttemptRef)].TurnAttempt
			if parentAttempt == nil || childAttempt == nil {
				return domainrepo.LegacyOperationRecord{}, errs.ErrFailedPrecondition
			}
			childProcess := compiled.Resources[input.ChildProcessRef]
			if err := tx.SaveDelegationEdge(ctx, domainrepo.DelegationEdge{
				ID: operation.TargetID, OrganizationID: principal.OrganizationID, ProjectID: plan.Operations[0].TargetID,
				ParentProcessRunID: parentProcess.ID, SourceSessionID: parentSession.ID,
				SourceTurnID: parentTurn.ID, SourceAttempt: parentAttempt.Attempt,
				SourceInputSHA256: parentAttempt.ImmutableInputSHA256,
				TargetSessionID:   childSession.ID, TargetRoleID: compiled.Resources[input.ChildRoleRef].ID,
				TargetTurnID: childTurn.ID, TargetAttempt: childAttempt.Attempt,
				TargetInputSHA256:    childAttempt.ImmutableInputSHA256,
				RootInitiatorActorID: principal.ActorID,
				GrantGeneration:      input.GrantGeneration, CreatedAt: now,
			}); err != nil {
				return domainrepo.LegacyOperationRecord{}, err
			}
			if childProcess.ID == "" {
				return domainrepo.LegacyOperationRecord{}, errs.ErrFailedPrecondition
			}
		case operation.CallbackManifest != nil:
			input := operation.CallbackManifest
			if err := migrationTx.SaveLegacyCallbackManifest(ctx, domainrepo.LegacyCallbackManifest{
				ID: operation.TargetID, PlanID: plan.PlanID,
				DelegationID:      legacyTargetID(plan, input.DelegationRef),
				CallbackProcessID: compiled.Resources[input.CallbackProcessRef].ID,
				ManifestSHA256:    input.ManifestSHA256, Destinations: slices.Clone(input.Destinations), CreatedAt: now,
			}); err != nil {
				return domainrepo.LegacyOperationRecord{}, err
			}
		case operation.CallbackDelivery != nil:
			input := operation.CallbackDelivery
			if err := migrationTx.SaveLegacyCallbackDelivery(ctx, domainrepo.LegacyCallbackDelivery{
				ID: operation.TargetID, PlanID: plan.PlanID, ManifestID: legacyTargetID(plan, input.CallbackManifestRef),
				Destination: input.Destination, ReceiptSHA256: input.ReceiptSHA256,
				State: input.TerminalState, DeliveredAt: input.DeliveredAt,
			}); err != nil {
				return domainrepo.LegacyOperationRecord{}, err
			}
			targetState, err = legacyCallbackReceiptState(input.TerminalState)
			if err != nil {
				return domainrepo.LegacyOperationRecord{}, err
			}
		default:
			return domainrepo.LegacyOperationRecord{}, errs.ErrInternal
		}
	}
	auditID := uuid.NewString()
	if err := tx.AppendAudit(ctx, domainrepo.Audit{
		ID: auditID, OrganizationID: principal.OrganizationID, ProjectID: plan.Operations[0].TargetID,
		ActorID: principal.ActorID, Action: "materialize_legacy_" + strings.ToLower(kind),
		ResourceID: operation.TargetID, ResourceKind: kind, ResourceVersion: targetVersion,
		Outcome: "succeeded", CorrelationID: principal.CorrelationID,
		PolicyRevision: principal.PolicyRevision, OccurredAt: now,
	}); err != nil {
		return domainrepo.LegacyOperationRecord{}, err
	}
	eventIDs := []string{}
	eventSequences := []uint64{}
	if resource, ok := compiled.Resources[source.LocalRef]; ok {
		if eventName, published := event.EventNameForKind(resource.Kind); published {
			eventID := uuid.NewString()
			if err := tx.AppendEvent(ctx, event.Change{
				EventID: eventID, EventName: eventName, OrganizationID: resource.OrganizationID,
				ProjectID: resource.ProjectID, ResourceID: resource.ID, ResourceKind: resource.Kind,
				ResourceState: resource.State, ResourceVersion: resource.Version, EventSequence: resource.Version,
				OccurredAt: now, CorrelationID: principal.CorrelationID,
			}); err != nil {
				return domainrepo.LegacyOperationRecord{}, err
			}
			eventIDs, eventSequences = append(eventIDs, eventID), append(eventSequences, resource.Version)
		}
	}
	provenance := legacyProvenance(plan, operation, compiled, receipt)
	if err := migrationTx.AppendLegacyProvenance(ctx, provenance); err != nil {
		return domainrepo.LegacyOperationRecord{}, err
	}
	provenanceProjection, err := migrationTx.GetLegacyProvenanceProjection(ctx, receipt.PlanID, receipt.Ordinal)
	if err != nil {
		return domainrepo.LegacyOperationRecord{}, err
	}
	receipt.ProvenanceEvidenceSHA256, err = canonicalJSONTextHash(provenanceProjection)
	if err != nil {
		return domainrepo.LegacyOperationRecord{}, errs.ErrDataLoss
	}
	if legacyCustomOperationKind(kind) {
		projection, projectionErr := migrationTx.GetLegacyCustomOperationProjection(ctx, receipt.PlanID, receipt.Ordinal)
		if projectionErr != nil {
			return domainrepo.LegacyOperationRecord{}, projectionErr
		}
		projectionSHA256, err = canonicalJSONTextHash(projection)
		if err != nil {
			return domainrepo.LegacyOperationRecord{}, errs.ErrDataLoss
		}
	}
	receipt.TargetKind, receipt.TargetVersion, receipt.TargetState = kind, targetVersion, targetState
	receipt.ProjectionSHA256, receipt.AuditIDs = projectionSHA256, []string{auditID}
	receipt.EventIDs, receipt.EventSequences, receipt.MaterializedAt = eventIDs, eventSequences, now
	if err := migrationTx.MaterializeLegacyOperationReceipt(ctx, receipt); err != nil {
		return domainrepo.LegacyOperationRecord{}, err
	}
	return receipt, nil
}

func legacyCallbackReceiptState(state string) (enum.State, error) {
	switch state {
	case "DELIVERED":
		return enum.StateSucceeded, nil
	case "FAILED":
		return enum.StateFailed, nil
	case "CANCELLED":
		return enum.StateCancelled, nil
	default:
		return "", errs.ErrInvalidInput
	}
}

func legacyRefIndex(plan entity.LegacyGraphPlan, reference string) int {
	for index, operation := range plan.Operations {
		source, _, _ := legacyOperationSource(operation)
		if source.LocalRef == reference {
			return index
		}
	}
	return -1
}

func legacyTargetID(plan entity.LegacyGraphPlan, reference string) string {
	index := legacyRefIndex(plan, reference)
	if index < 0 {
		return ""
	}
	return plan.Operations[index].TargetID
}

func legacyProvenance(plan entity.LegacyGraphPlan, operation entity.LegacyGraphOperation,
	compiled compiledLegacyGraph, receipt domainrepo.LegacyOperationRecord,
) domainrepo.LegacyProvenanceRecord {
	source, kind, _ := legacyOperationSource(operation)
	record := domainrepo.LegacyProvenanceRecord{
		PlanID: plan.PlanID, Ordinal: receipt.Ordinal, TargetID: operation.TargetID, TargetKind: kind,
		SourceTable: source.SourceTable, SourceRef: source.SourceRef, SourceRevision: source.SourceRevision,
		SourceSHA256: source.SourceSHA256, RootActorID: compiled.Resources[plan.Operations[0].Project.Source.LocalRef].OwnerActorID,
		ImmutableInputSHA256: receipt.InputSHA256, LineageSHA256: receipt.ProvenanceSHA256,
	}
	if input := operation.Turn; input != nil {
		record.RootSessionID = compiled.Resources[input.SessionRef].ID
		record.ParentTargetID = legacyTargetID(plan, input.ParentTurnRef)
	}
	if input := operation.TurnAttempt; input != nil {
		record.RootTurnID = compiled.Resources[input.TurnRef].ID
		record.RootAttempt = input.Attempt
		runtime := compiled.Resources[input.RuntimeRevisionRef]
		record.RuntimeRevisionID, record.RuntimeRevisionVersion = runtime.ID, runtime.Version
		record.ImmutableInputSHA256 = input.ImmutableInputSHA256
	}
	if input := operation.ProcessRun; input != nil {
		record.RootSessionID = compiled.Resources[input.RootSessionRef].ID
		record.RootTurnID = compiled.Resources[input.RootTurnRef].ID
		rootAttemptIndex := legacyRefIndex(plan, input.RootAttemptRef)
		if rootAttemptIndex >= 0 {
			record.RootAttempt = plan.Operations[rootAttemptIndex].TurnAttempt.Attempt
		}
		runtime := compiled.Resources[input.RuntimeRevisionRef]
		record.RuntimeRevisionID, record.RuntimeRevisionVersion = runtime.ID, runtime.Version
		record.ParentTargetID = legacyTargetID(plan, input.ParentProcessRef)
		record.LaunchingTurnID = legacyTargetID(plan, input.LaunchingTurnRef)
		launchingIndex := legacyRefIndex(plan, input.LaunchingAttemptRef)
		if launchingIndex >= 0 {
			record.LaunchingAttemptTargetID = plan.Operations[launchingIndex].TargetID
			record.LaunchingAttempt = plan.Operations[launchingIndex].TurnAttempt.Attempt
		}
		record.ImmutableInputSHA256 = input.ImmutableInputSHA256
		record.MachinePolicyRevision, record.MachinePolicySHA256 =
			serviceAuthorityPolicy(compiled, input.RuntimeRevisionRef)
		record.LegacyPolicyRevision, record.LegacyPolicySHA256 = input.LegacyPolicyRevision, input.LegacyPolicySHA256
	}
	return record
}

func serviceAuthorityPolicy(compiled compiledLegacyGraph, reference string) (uint64, string) {
	resource := compiled.Resources[reference]
	if spec, ok := resource.Spec.(entity.RuntimeRevisionSpec); ok {
		return spec.AuthorityPolicyVersion, spec.AuthorityPolicySHA256
	}
	return 0, ""
}

func legacyProtectedHistoryKind(kind enum.Kind) bool {
	switch kind {
	case enum.KindRoleDefinition, enum.KindAgent, enum.KindAgentAssignment,
		enum.KindInstructionSet, enum.KindProviderReference, enum.KindProviderPool:
		return true
	default:
		return false
	}
}
