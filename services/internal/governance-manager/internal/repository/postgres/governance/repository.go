// Package governance implements the PostgreSQL repository for governance-manager state.
package governance

import (
	"context"
	"embed"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governancerepo "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/repository/governance"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
)

// SQLFiles contains named SQL queries for governance-manager repository.
//
//go:embed sql/*.sql
var SQLFiles embed.FS

var _ governancerepo.Repository = (*Repository)(nil)

type database interface {
	dataRunner
	postgreslib.TxBeginner
}

type dataRunner interface {
	postgreslib.ExecQuerier
	postgreslib.RowQuerier
	queryRowGetter
}

type queryRowGetter interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Repository stores governance-manager state in PostgreSQL.
type Repository struct {
	db database
}

type mutationStep struct {
	query           string
	args            pgx.NamedArgs
	requireAffected bool
}

const (
	operationActivateRiskProfileVersion  = "domain.Repository.ActivateRiskProfileVersion"
	operationArchiveRiskProfile          = "domain.Repository.ArchiveRiskProfile"
	operationCreateReleaseDecision       = "domain.Repository.CreateReleaseDecision"
	operationBuildReleaseDecisionPackage = "domain.Repository.CreateReleaseDecisionPackage"
	operationCreateGateRequest           = "domain.Repository.CreateGateRequest"
	operationCreateRiskAssessment        = "domain.Repository.CreateRiskAssessment"
	operationCreateRiskProfile           = "domain.Repository.CreateRiskProfile"
	operationCreateRiskProfileVersion    = "domain.Repository.CreateRiskProfileVersion"
	operationGetBlockingSignal           = "domain.Repository.GetBlockingSignal"
	operationGetCommandResult            = "domain.Repository.GetCommandResult"
	operationGetGateDecision             = "domain.Repository.GetGateDecision"
	operationGetGateRequest              = "domain.Repository.GetGateRequest"
	operationGetReleaseDecision          = "domain.Repository.GetReleaseDecision"
	operationGetReleaseDecisionByPackage = "domain.Repository.GetReleaseDecisionByPackage"
	operationGetReleaseDecisionPackage   = "domain.Repository.GetReleaseDecisionPackage"
	operationGetReleaseSafetyState       = "domain.Repository.GetReleaseSafetyStateByPackage"
	operationGetReviewSignal             = "domain.Repository.GetReviewSignal"
	operationGetRiskAssessment           = "domain.Repository.GetRiskAssessment"
	operationGetRiskProfile              = "domain.Repository.GetRiskProfile"
	operationGetRiskProfileVersion       = "domain.Repository.GetRiskProfileVersion"
	operationListBlockingSignals         = "domain.Repository.ListBlockingSignals"
	operationListGateDecisions           = "domain.Repository.ListGateDecisions"
	operationListGatePolicies            = "domain.Repository.ListGatePolicies"
	operationListGateRequests            = "domain.Repository.ListGateRequests"
	operationListReleaseDecisions        = "domain.Repository.ListReleaseDecisions"
	operationListReleaseDecisionPackages = "domain.Repository.ListReleaseDecisionPackages"
	operationListReviewSignals           = "domain.Repository.ListReviewSignals"
	operationListRiskAssessments         = "domain.Repository.ListRiskAssessments"
	operationListRiskFactors             = "domain.Repository.ListRiskFactors"
	operationListRiskProfiles            = "domain.Repository.ListRiskProfiles"
	operationListRiskRules               = "domain.Repository.ListRiskRules"
	operationOutboxClaim                 = "domain.Repository.ClaimOutboxEvents"
	operationOutboxMarkFailed            = "domain.Repository.MarkOutboxEventFailed"
	operationOutboxMarkPermanent         = "domain.Repository.MarkOutboxEventPermanentlyFailed"
	operationOutboxMarkPublished         = "domain.Repository.MarkOutboxEventPublished"
	operationRecordCommandResult         = "domain.Repository.RecordCommandResult"
	operationRecordBlockingSignal        = "domain.Repository.RecordBlockingSignal"
	operationRecordReleaseSafetyState    = "domain.Repository.RecordReleaseSafetyState"
	operationRecordReviewSignal          = "domain.Repository.RecordReviewSignal"
	operationUpdateBlockingSignal        = "domain.Repository.UpdateBlockingSignal"
	operationSubmitGateDecision          = "domain.Repository.UpdateGateRequestWithDecision"
	operationUpdateGateRequestStatus     = "domain.Repository.UpdateGateRequestStatus"
	operationUpdateReleaseDecision       = "domain.Repository.UpdateReleaseDecision"
	operationUpdateReleasePackageStatus  = "domain.Repository.UpdateReleaseDecisionPackageStatus"
	operationUpdateReleaseSafetyState    = "domain.Repository.UpdateReleaseSafetyState"
	operationUpdateRiskAssessment        = "domain.Repository.UpdateRiskAssessment"
)

// NewRepository creates a PostgreSQL repository.
func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// Ready reports whether the repository has a database handle.
func (r *Repository) Ready() bool {
	return r != nil && r.db != nil
}

// CreateRiskProfile stores risk profile metadata and a command result.
func (r *Repository) CreateRiskProfile(ctx context.Context, profile entity.RiskProfile, result entity.CommandResult) error {
	return r.mutateWithResult(ctx, operationCreateRiskProfile, queryRiskProfileCreate, riskProfileArgs(profile), result, nil)
}

// CreateRiskProfileVersion stores an immutable profile version with rules and gate policies.
func (r *Repository) CreateRiskProfileVersion(ctx context.Context, version entity.RiskProfileVersion, result entity.CommandResult) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		if err := runMutation(ctx, tx, queryRiskProfileVersionCreate, riskProfileVersionArgs(version), true); err != nil {
			return err
		}
		if err := queueGatePolicies(ctx, tx, version.GatePolicies); err != nil {
			return err
		}
		if err := queueRiskRules(ctx, tx, version.Rules); err != nil {
			return err
		}
		return runCommandResult(ctx, tx, result)
	})
	return wrapError(operationCreateRiskProfileVersion, err)
}

// ActivateRiskProfileVersion activates a profile version and records an event.
func (r *Repository) ActivateRiskProfileVersion(ctx context.Context, profile entity.RiskProfile, previousProfileVersion int64, activatedVersion entity.RiskProfileVersion, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateStepsWithResult(ctx, operationActivateRiskProfileVersion, result, &event,
		optionalMutation(queryRiskProfileVersionSupersede, pgx.NamedArgs{
			"risk_profile_id": activatedVersion.RiskProfileID,
			"profile_version": activatedVersion.ProfileVersion,
		}),
		requiredMutation(queryRiskProfileVersionActivate, riskProfileVersionArgs(activatedVersion)),
		requiredMutation(queryRiskProfileUpdate, riskProfileUpdateArgs(profile, previousProfileVersion)),
	)
}

// ArchiveRiskProfile archives a profile without deleting historical decisions.
func (r *Repository) ArchiveRiskProfile(ctx context.Context, profile entity.RiskProfile, previousVersion int64, result entity.CommandResult) error {
	return r.mutateWithResult(ctx, operationArchiveRiskProfile, queryRiskProfileUpdate, riskProfileUpdateArgs(profile, previousVersion), result, nil)
}

// GetRiskProfile returns profile metadata by id.
func (r *Repository) GetRiskProfile(ctx context.Context, id uuid.UUID) (entity.RiskProfile, error) {
	return queryOne(ctx, r.db, operationGetRiskProfile, queryRiskProfileGet, pgx.NamedArgs{"id": id}, scanRiskProfile)
}

// GetRiskProfileVersion returns a profile version with child rules and policies.
func (r *Repository) GetRiskProfileVersion(ctx context.Context, id uuid.UUID, profileVersion int64) (entity.RiskProfileVersion, error) {
	version, err := queryOne(ctx, r.db, operationGetRiskProfileVersion, queryRiskProfileVersionGet, pgx.NamedArgs{"risk_profile_id": id, "profile_version": profileVersion}, scanRiskProfileVersion)
	if err != nil {
		return entity.RiskProfileVersion{}, err
	}
	rules, _, err := r.ListRiskRules(ctx, query.RuleFilter{RiskProfileID: id, ProfileVersion: profileVersion})
	if err != nil {
		return entity.RiskProfileVersion{}, err
	}
	policies, _, err := r.ListGatePolicies(ctx, query.GatePolicyFilter{RiskProfileID: id, ProfileVersion: profileVersion})
	if err != nil {
		return entity.RiskProfileVersion{}, err
	}
	version.Rules = rules
	version.GatePolicies = policies
	return version, nil
}

// ListRiskProfiles returns profiles by scope and status.
func (r *Repository) ListRiskProfiles(ctx context.Context, filter query.RiskProfileFilter) ([]entity.RiskProfile, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListRiskProfiles, queryRiskProfileList, riskProfileFilterArgs(filter), scanRiskProfile)
}

// ListRiskRules returns rules by profile version.
func (r *Repository) ListRiskRules(ctx context.Context, filter query.RuleFilter) ([]entity.RiskRule, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListRiskRules, queryRiskRuleList, ruleFilterArgs(filter), scanRiskRule)
}

// ListGatePolicies returns gate policies by profile version.
func (r *Repository) ListGatePolicies(ctx context.Context, filter query.GatePolicyFilter) ([]entity.GatePolicy, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListGatePolicies, queryGatePolicyList, gatePolicyFilterArgs(filter), scanGatePolicy)
}

// CreateRiskAssessment stores an assessment with factors and domain events.
func (r *Repository) CreateRiskAssessment(ctx context.Context, assessment entity.RiskAssessment, factors []entity.RiskFactor, result entity.CommandResult, events []entity.OutboxEvent) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		if err := runMutation(ctx, tx, queryRiskAssessmentCreate, riskAssessmentArgs(assessment), true); err != nil {
			return err
		}
		if err := queueRiskFactors(ctx, tx, factors); err != nil {
			return err
		}
		if err := runCommandResult(ctx, tx, result); err != nil {
			return err
		}
		return queueOutboxEvents(ctx, tx, events)
	})
	return wrapError(operationCreateRiskAssessment, err)
}

// UpdateRiskAssessment replaces the current assessment outcome and factors.
func (r *Repository) UpdateRiskAssessment(ctx context.Context, assessment entity.RiskAssessment, factors []entity.RiskFactor, previousVersion int64, result entity.CommandResult, events []entity.OutboxEvent) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		if err := runMutation(ctx, tx, queryRiskAssessmentUpdate, riskAssessmentUpdateArgs(assessment, previousVersion), true); err != nil {
			return err
		}
		if err := runMutation(ctx, tx, queryRiskFactorDeleteByAssessment, pgx.NamedArgs{"risk_assessment_id": assessment.ID}, false); err != nil {
			return err
		}
		if err := queueRiskFactors(ctx, tx, factors); err != nil {
			return err
		}
		if err := runCommandResult(ctx, tx, result); err != nil {
			return err
		}
		return queueOutboxEvents(ctx, tx, events)
	})
	return wrapError(operationUpdateRiskAssessment, err)
}

// GetRiskAssessment returns one assessment.
func (r *Repository) GetRiskAssessment(ctx context.Context, id uuid.UUID) (entity.RiskAssessment, error) {
	return queryOne(ctx, r.db, operationGetRiskAssessment, queryRiskAssessmentGet, pgx.NamedArgs{"id": id}, scanRiskAssessment)
}

// ListRiskAssessments returns assessments by filter.
func (r *Repository) ListRiskAssessments(ctx context.Context, filter query.RiskAssessmentFilter) ([]entity.RiskAssessment, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListRiskAssessments, queryRiskAssessmentList, riskAssessmentFilterArgs(filter), scanRiskAssessment)
}

// ListRiskFactors returns risk factors by assessment.
func (r *Repository) ListRiskFactors(ctx context.Context, filter query.RiskFactorFilter) ([]entity.RiskFactor, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListRiskFactors, queryRiskFactorList, riskFactorFilterArgs(filter), scanRiskFactor)
}

// RecordReviewSignal stores a review signal and outbox event.
func (r *Repository) RecordReviewSignal(ctx context.Context, signal entity.ReviewSignal, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationRecordReviewSignal, queryReviewSignalCreate, reviewSignalArgs(signal), result, &event)
}

// GetReviewSignal returns a review signal by id.
func (r *Repository) GetReviewSignal(ctx context.Context, id uuid.UUID) (entity.ReviewSignal, error) {
	return queryOne(ctx, r.db, operationGetReviewSignal, queryReviewSignalGet, pgx.NamedArgs{"id": id}, scanReviewSignal)
}

// ListReviewSignals returns review signals by target or assessment.
func (r *Repository) ListReviewSignals(ctx context.Context, filter query.ReviewSignalFilter) ([]entity.ReviewSignal, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListReviewSignals, queryReviewSignalList, reviewSignalFilterArgs(filter), scanReviewSignal)
}

// CreateGateRequest stores a gate request and outbox event.
func (r *Repository) CreateGateRequest(ctx context.Context, request entity.GateRequest, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationCreateGateRequest, queryGateRequestCreate, gateRequestArgs(request), result, &event)
}

// UpdateGateRequestStatus stores a terminal gate request lifecycle transition.
func (r *Repository) UpdateGateRequestStatus(ctx context.Context, request entity.GateRequest, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.updateGateRequestLifecycle(ctx, request, previousVersion, result, event)
}

func (r *Repository) updateGateRequestLifecycle(ctx context.Context, request entity.GateRequest, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	args := gateRequestUpdateArgs(request, previousVersion)
	eventRef := &event
	return r.mutateWithResult(ctx, operationUpdateGateRequestStatus, queryGateRequestUpdate, args, result, eventRef)
}

// UpdateGateRequestWithDecision stores a final gate decision and resolves the request.
func (r *Repository) UpdateGateRequestWithDecision(ctx context.Context, request entity.GateRequest, previousVersion int64, decision entity.GateDecision, result entity.CommandResult, event entity.OutboxEvent) error {
	steps := []mutationStep{
		requiredMutation(queryGateRequestUpdate, gateRequestUpdateArgs(request, previousVersion)),
		requiredMutation(queryGateDecisionCreate, gateDecisionArgs(decision)),
	}
	return r.mutateStepsWithResult(ctx, operationSubmitGateDecision, result, &event, steps...)
}

// GetGateRequest returns a gate request by id.
func (r *Repository) GetGateRequest(ctx context.Context, id uuid.UUID) (entity.GateRequest, error) {
	return queryOne(ctx, r.db, operationGetGateRequest, queryGateRequestGet, pgx.NamedArgs{"id": id}, scanGateRequest)
}

// GetGateDecision returns a gate decision by id.
func (r *Repository) GetGateDecision(ctx context.Context, id uuid.UUID) (entity.GateDecision, error) {
	return queryOne(ctx, r.db, operationGetGateDecision, queryGateDecisionGet, pgx.NamedArgs{"id": id}, scanGateDecision)
}

// ListGateRequests returns gate requests by filter.
func (r *Repository) ListGateRequests(ctx context.Context, filter query.GateRequestFilter) ([]entity.GateRequest, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListGateRequests, queryGateRequestList, gateRequestFilterArgs(filter), scanGateRequest)
}

// ListGateDecisions returns gate decisions by filter.
func (r *Repository) ListGateDecisions(ctx context.Context, filter query.GateDecisionFilter) ([]entity.GateDecision, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListGateDecisions, queryGateDecisionList, gateDecisionFilterArgs(filter), scanGateDecision)
}

// CreateReleaseDecisionPackage stores a bounded release evidence package.
func (r *Repository) CreateReleaseDecisionPackage(ctx context.Context, item entity.ReleaseDecisionPackage, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationBuildReleaseDecisionPackage, queryReleaseDecisionPackageCreate, releaseDecisionPackageArgs(item), result, &event)
}

// UpdateReleaseDecisionPackageStatus updates release package lifecycle status.
func (r *Repository) UpdateReleaseDecisionPackageStatus(ctx context.Context, item entity.ReleaseDecisionPackage, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.updateReleasePackageStatus(ctx, item, previousVersion, result, event)
}

func (r *Repository) updateReleasePackageStatus(ctx context.Context, item entity.ReleaseDecisionPackage, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	args := releaseDecisionPackageUpdateArgs(item, previousVersion)
	return r.mutateWithResult(ctx, operationUpdateReleasePackageStatus, queryReleaseDecisionPackageUpdate, args, result, &event)
}

// GetReleaseDecisionPackage returns a release decision package by id.
func (r *Repository) GetReleaseDecisionPackage(ctx context.Context, id uuid.UUID) (entity.ReleaseDecisionPackage, error) {
	return queryOne(ctx, r.db, operationGetReleaseDecisionPackage, queryReleaseDecisionPackageGet, pgx.NamedArgs{"id": id}, scanReleaseDecisionPackage)
}

// ListReleaseDecisionPackages returns release decision packages by filter.
func (r *Repository) ListReleaseDecisionPackages(ctx context.Context, filter query.ReleaseDecisionPackageFilter) ([]entity.ReleaseDecisionPackage, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListReleaseDecisionPackages, queryReleaseDecisionPackageList, releaseDecisionPackageFilterArgs(filter), scanReleaseDecisionPackage)
}

// CreateReleaseDecision stores a requested release decision and advances the package.
func (r *Repository) CreateReleaseDecision(ctx context.Context, pkg entity.ReleaseDecisionPackage, previousPackageVersion int64, decision entity.ReleaseDecision, result entity.CommandResult, event entity.OutboxEvent) error {
	packageUpdate := requiredMutation(queryReleaseDecisionPackageUpdate, releaseDecisionPackageUpdateArgs(pkg, previousPackageVersion))
	decisionCreate := requiredMutation(queryReleaseDecisionCreate, releaseDecisionArgs(decision))
	return r.mutateStepsWithResult(ctx, operationCreateReleaseDecision, result, &event,
		packageUpdate,
		decisionCreate,
	)
}

// UpdateReleaseDecision stores a terminal release decision and closes the package.
func (r *Repository) UpdateReleaseDecision(ctx context.Context, pkg entity.ReleaseDecisionPackage, previousPackageVersion int64, decision entity.ReleaseDecision, previousDecisionVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateReleaseDecisionUpdate(ctx, pkg, previousPackageVersion, decision, previousDecisionVersion, result, event)
}

func (r *Repository) mutateReleaseDecisionUpdate(ctx context.Context, pkg entity.ReleaseDecisionPackage, previousPackageVersion int64, decision entity.ReleaseDecision, previousDecisionVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateStepsWithResult(ctx, operationUpdateReleaseDecision, result, &event,
		requiredMutation(queryReleaseDecisionPackageUpdate, releaseDecisionPackageUpdateArgs(pkg, previousPackageVersion)),
		requiredMutation(queryReleaseDecisionUpdate, releaseDecisionUpdateArgs(decision, previousDecisionVersion)),
	)
}

// GetReleaseDecision returns one release decision by id.
func (r *Repository) GetReleaseDecision(ctx context.Context, id uuid.UUID) (entity.ReleaseDecision, error) {
	return queryOne(ctx, r.db, operationGetReleaseDecision, queryReleaseDecisionGet, pgx.NamedArgs{"id": id}, scanReleaseDecision)
}

// GetReleaseDecisionByPackage returns the current release decision for a package.
func (r *Repository) GetReleaseDecisionByPackage(ctx context.Context, releaseDecisionPackageID uuid.UUID) (entity.ReleaseDecision, error) {
	return queryOne(ctx, r.db, operationGetReleaseDecisionByPackage, queryReleaseDecisionGetByPackage, pgx.NamedArgs{"release_decision_package_id": releaseDecisionPackageID}, scanReleaseDecision)
}

// ListReleaseDecisions returns release decisions by filter.
func (r *Repository) ListReleaseDecisions(ctx context.Context, filter query.ReleaseDecisionFilter) ([]entity.ReleaseDecision, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListReleaseDecisions, queryReleaseDecisionList, releaseDecisionFilterArgs(filter), scanReleaseDecision)
}

// RecordReleaseSafetyState creates the current safety-loop state.
func (r *Repository) RecordReleaseSafetyState(ctx context.Context, state entity.ReleaseSafetyState, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationRecordReleaseSafetyState, queryReleaseSafetyStateCreate, releaseSafetyStateArgs(state), result, &event)
}

// UpdateReleaseSafetyState updates the current safety-loop state.
func (r *Repository) UpdateReleaseSafetyState(ctx context.Context, state entity.ReleaseSafetyState, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	step := requiredMutation(queryReleaseSafetyStateUpdate, releaseSafetyStateUpdateArgs(state, previousVersion))
	return r.mutateStepsWithResult(ctx, operationUpdateReleaseSafetyState, result, &event, step)
}

// GetReleaseSafetyStateByPackage returns current safety-loop state for a package.
func (r *Repository) GetReleaseSafetyStateByPackage(ctx context.Context, releaseDecisionPackageID uuid.UUID) (entity.ReleaseSafetyState, error) {
	return queryOne(ctx, r.db, operationGetReleaseSafetyState, queryReleaseSafetyStateGet, pgx.NamedArgs{"release_decision_package_id": releaseDecisionPackageID}, scanReleaseSafetyState)
}

// RecordBlockingSignal stores a blocking signal.
func (r *Repository) RecordBlockingSignal(ctx context.Context, signal entity.BlockingSignal, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationRecordBlockingSignal, queryBlockingSignalCreate, blockingSignalArgs(signal), result, &event)
}

// UpdateBlockingSignal stores a terminal blocking signal transition.
func (r *Repository) UpdateBlockingSignal(ctx context.Context, signal entity.BlockingSignal, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	eventRef := &event
	updateArgs := blockingSignalUpdateArgs(signal, previousVersion)
	return r.mutateWithResult(ctx, operationUpdateBlockingSignal, queryBlockingSignalUpdate, updateArgs, result, eventRef)
}

// GetBlockingSignal returns one blocking signal by id.
func (r *Repository) GetBlockingSignal(ctx context.Context, id uuid.UUID) (entity.BlockingSignal, error) {
	return queryOne(ctx, r.db, operationGetBlockingSignal, queryBlockingSignalGet, pgx.NamedArgs{"id": id}, scanBlockingSignal)
}

// ListBlockingSignals returns blocking signals by target and state.
func (r *Repository) ListBlockingSignals(ctx context.Context, filter query.BlockingSignalFilter) ([]entity.BlockingSignal, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListBlockingSignals, queryBlockingSignalList, blockingSignalFilterArgs(filter), scanBlockingSignal)
}

// GetCommandResult returns a stored idempotency result.
func (r *Repository) GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	return queryOne(ctx, r.db, operationGetCommandResult, queryCommandResultGet, commandIdentityArgs(identity), scanCommandResult)
}

// RecordCommandResult stores an idempotency result without a domain mutation.
func (r *Repository) RecordCommandResult(ctx context.Context, result entity.CommandResult) error {
	return wrapError(operationRecordCommandResult, runCommandResult(ctx, r.db, result))
}

// ClaimOutboxEvents leases service-local outbox events for dispatch.
func (r *Repository) ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error) {
	events, queryRan, err := postgreslib.ClaimOutboxRows(ctx, r.db, queryOutboxEventClaim, limit, now, lockedUntil, scanOutboxEvent)
	if !queryRan {
		return nil, wrapError(operationOutboxClaim, errs.ErrInvalidArgument)
	}
	if err != nil {
		return nil, wrapError(operationOutboxClaim, err)
	}
	return events, nil
}

// MarkOutboxEventPublished marks a claimed outbox event as delivered.
func (r *Repository) MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error {
	return wrapError(operationOutboxMarkPublished, postgreslib.ApplyOutboxPublished(ctx, r.db, queryOutboxEventMarkPublished, errs.ErrInvalidArgument, id, attemptCount, publishedAt))
}

// MarkOutboxEventFailed schedules an outbox retry.
func (r *Repository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	return r.markOutboxFailure(ctx, operationOutboxMarkFailed, queryOutboxEventMarkFailed, id, attemptCount, "next_attempt_at", nextAttemptAt, lastError)
}

// MarkOutboxEventPermanentlyFailed marks an outbox event terminally failed.
func (r *Repository) MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	return r.markOutboxFailure(ctx, operationOutboxMarkPermanent, queryOutboxEventMarkPermanent, id, attemptCount, "failed_permanently_at", failedAt, lastError)
}

func (r *Repository) markOutboxFailure(ctx context.Context, operation string, sqlText string, id uuid.UUID, attemptCount int, timestampColumn string, timestamp time.Time, lastError string) error {
	err := postgreslib.ApplyOutboxDeliveryFailure(ctx, r.db, sqlText, errs.ErrInvalidArgument, id, attemptCount, timestampColumn, timestamp, lastError)
	return wrapError(operation, err)
}

func (r *Repository) mutateWithResult(ctx context.Context, operation string, mutationQuery string, mutationArgs pgx.NamedArgs, result entity.CommandResult, event *entity.OutboxEvent) error {
	return r.mutateStepsWithResult(ctx, operation, result, event, requiredMutation(mutationQuery, mutationArgs))
}

func (r *Repository) mutateStepsWithResult(ctx context.Context, operation string, result entity.CommandResult, event *entity.OutboxEvent, steps ...mutationStep) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		for _, step := range steps {
			if err := runMutation(ctx, tx, step.query, step.args, step.requireAffected); err != nil {
				return err
			}
		}
		if err := runCommandResult(ctx, tx, result); err != nil {
			return err
		}
		if event == nil {
			return nil
		}
		return runOutboxEvent(ctx, tx, *event)
	})
	return wrapError(operation, err)
}

func requiredMutation(query string, args pgx.NamedArgs) mutationStep {
	return mutationStep{query: query, args: args, requireAffected: true}
}

func optionalMutation(query string, args pgx.NamedArgs) mutationStep {
	return mutationStep{query: query, args: args}
}

func queueGatePolicies(ctx context.Context, tx pgx.Tx, policies []entity.GatePolicy) error {
	return queueBatch(ctx, tx, policies, queueGatePolicy)
}

func queueRiskRules(ctx context.Context, tx pgx.Tx, rules []entity.RiskRule) error {
	return queueBatch(ctx, tx, rules, queueRiskRule)
}

func queueRiskFactors(ctx context.Context, tx pgx.Tx, factors []entity.RiskFactor) error {
	return queueBatch(ctx, tx, factors, queueRiskFactor)
}

func queueOutboxEvents(ctx context.Context, tx pgx.Tx, events []entity.OutboxEvent) error {
	return queueBatch(ctx, tx, events, queueOutboxEvent)
}

func queueGatePolicy(batch *pgx.Batch, policy entity.GatePolicy) {
	batch.Queue(queryGatePolicyCreate, gatePolicyArgs(policy))
}

func queueRiskRule(batch *pgx.Batch, rule entity.RiskRule) {
	batch.Queue(queryRiskRuleCreate, riskRuleArgs(rule))
}

func queueRiskFactor(batch *pgx.Batch, factor entity.RiskFactor) {
	batch.Queue(queryRiskFactorCreate, riskFactorArgs(factor))
}

func queueOutboxEvent(batch *pgx.Batch, event entity.OutboxEvent) {
	batch.Queue(queryOutboxEventCreate, outboxEventArgs(event))
}

func queueBatch[T any](ctx context.Context, tx pgx.Tx, items []T, queue func(*pgx.Batch, T)) error {
	if len(items) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, item := range items {
		queue(batch, item)
	}
	return execBatch(ctx, tx, batch)
}

func execBatch(ctx context.Context, tx pgx.Tx, batch *pgx.Batch) error {
	results := tx.SendBatch(ctx, batch)
	defer func() {
		_ = results.Close()
	}()
	for i := 0; i < batch.Len(); i++ {
		if _, err := results.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func runMutation(ctx context.Context, db dataRunner, query string, args pgx.NamedArgs, requireAffected bool) error {
	return postgreslib.RunMutation(ctx, db, errs.ErrConflict, postgreslib.Mutation{Query: query, Args: args, RequireAffected: requireAffected})
}

func runCommandResult(ctx context.Context, db dataRunner, result entity.CommandResult) error {
	return runMutation(ctx, db, queryCommandResultCreate, commandResultArgs(result), true)
}

func runOutboxEvent(ctx context.Context, db dataRunner, event entity.OutboxEvent) error {
	return runMutation(ctx, db, queryOutboxEventCreate, outboxEventArgs(event), true)
}

func queryOne[T any](ctx context.Context, db dataRunner, operation string, sqlText string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) (T, error) {
	row := db.QueryRow(ctx, sqlText, args)
	item, err := scan(row)
	if err != nil {
		var zero T
		return zero, wrapError(operation, err)
	}
	return item, nil
}

func queryMany[T any](ctx context.Context, db dataRunner, operation string, sqlText string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) ([]T, error) {
	rows, err := db.Query(ctx, sqlText, args)
	if err != nil {
		return nil, wrapError(operation, err)
	}
	items, scanErr := postgreslib.ScanRows(rows, scan)
	if scanErr != nil {
		return nil, wrapError(operation, scanErr)
	}
	return items, nil
}

func queryPage[T any](ctx context.Context, db dataRunner, operation string, sqlText string, page pageQueryArgs, scan func(postgreslib.RowScanner) (T, error)) ([]T, query.PageResult, error) {
	items, err := queryMany(ctx, db, operation, sqlText, page.NamedArgs, scan)
	if err != nil {
		return nil, query.PageResult{}, err
	}
	trimmed, result := pageResult(items, page)
	return trimmed, result, nil
}

func wrapError(operation string, err error) error {
	return postgreslib.WrapError(operation, err, postgreslib.ErrorSentinels{
		AlreadyExists:      errs.ErrAlreadyExists,
		Conflict:           errs.ErrConflict,
		InvalidArgument:    errs.ErrInvalidArgument,
		NotFound:           errs.ErrNotFound,
		PreconditionFailed: errs.ErrPreconditionFailed,
	})
}
