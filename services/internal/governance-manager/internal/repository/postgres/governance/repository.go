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

const (
	operationActivateRiskProfileVersion  = "domain.Repository.ActivateRiskProfileVersion"
	operationArchiveRiskProfile          = "domain.Repository.ArchiveRiskProfile"
	operationBuildReleaseDecisionPackage = "domain.Repository.CreateReleaseDecisionPackage"
	operationCreateGateRequest           = "domain.Repository.CreateGateRequest"
	operationCreateRiskAssessment        = "domain.Repository.CreateRiskAssessment"
	operationCreateRiskProfile           = "domain.Repository.CreateRiskProfile"
	operationCreateRiskProfileVersion    = "domain.Repository.CreateRiskProfileVersion"
	operationGetCommandResult            = "domain.Repository.GetCommandResult"
	operationGetGateDecision             = "domain.Repository.GetGateDecision"
	operationGetGateRequest              = "domain.Repository.GetGateRequest"
	operationGetReleaseDecisionPackage   = "domain.Repository.GetReleaseDecisionPackage"
	operationGetReviewSignal             = "domain.Repository.GetReviewSignal"
	operationGetRiskAssessment           = "domain.Repository.GetRiskAssessment"
	operationGetRiskProfile              = "domain.Repository.GetRiskProfile"
	operationGetRiskProfileVersion       = "domain.Repository.GetRiskProfileVersion"
	operationListGateDecisions           = "domain.Repository.ListGateDecisions"
	operationListGatePolicies            = "domain.Repository.ListGatePolicies"
	operationListGateRequests            = "domain.Repository.ListGateRequests"
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
	operationRecordReviewSignal          = "domain.Repository.RecordReviewSignal"
	operationSubmitGateDecision          = "domain.Repository.UpdateGateRequestWithDecision"
	operationUpdateGateRequestStatus     = "domain.Repository.UpdateGateRequestStatus"
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
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		if err := runMutation(ctx, tx, queryRiskProfileVersionSupersede, pgx.NamedArgs{
			"risk_profile_id": activatedVersion.RiskProfileID,
			"profile_version": activatedVersion.ProfileVersion,
		}, false); err != nil {
			return err
		}
		if err := runMutation(ctx, tx, queryRiskProfileVersionActivate, riskProfileVersionArgs(activatedVersion), true); err != nil {
			return err
		}
		if err := runMutation(ctx, tx, queryRiskProfileUpdate, riskProfileUpdateArgs(profile, previousProfileVersion), true); err != nil {
			return err
		}
		if err := runCommandResult(ctx, tx, result); err != nil {
			return err
		}
		return runOutboxEvent(ctx, tx, event)
	})
	return wrapError(operationActivateRiskProfileVersion, err)
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
	return r.mutateWithResult(ctx, operationUpdateGateRequestStatus, queryGateRequestUpdate, gateRequestUpdateArgs(request, previousVersion), result, &event)
}

// UpdateGateRequestWithDecision stores a final gate decision and resolves the request.
func (r *Repository) UpdateGateRequestWithDecision(ctx context.Context, request entity.GateRequest, previousVersion int64, decision entity.GateDecision, result entity.CommandResult, event entity.OutboxEvent) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		if err := runMutation(ctx, tx, queryGateRequestUpdate, gateRequestUpdateArgs(request, previousVersion), true); err != nil {
			return err
		}
		if err := runMutation(ctx, tx, queryGateDecisionCreate, gateDecisionArgs(decision), true); err != nil {
			return err
		}
		if err := runCommandResult(ctx, tx, result); err != nil {
			return err
		}
		return runOutboxEvent(ctx, tx, event)
	})
	return wrapError(operationSubmitGateDecision, err)
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

// GetReleaseDecisionPackage returns a release decision package by id.
func (r *Repository) GetReleaseDecisionPackage(ctx context.Context, id uuid.UUID) (entity.ReleaseDecisionPackage, error) {
	return queryOne(ctx, r.db, operationGetReleaseDecisionPackage, queryReleaseDecisionPackageGet, pgx.NamedArgs{"id": id}, scanReleaseDecisionPackage)
}

// ListReleaseDecisionPackages returns release decision packages by filter.
func (r *Repository) ListReleaseDecisionPackages(ctx context.Context, filter query.ReleaseDecisionPackageFilter) ([]entity.ReleaseDecisionPackage, query.PageResult, error) {
	return queryPage(ctx, r.db, operationListReleaseDecisionPackages, queryReleaseDecisionPackageList, releaseDecisionPackageFilterArgs(filter), scanReleaseDecisionPackage)
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
	return wrapError(operationOutboxMarkFailed, postgreslib.ApplyOutboxDeliveryFailure(ctx, r.db, queryOutboxEventMarkFailed, errs.ErrInvalidArgument, id, attemptCount, "next_attempt_at", nextAttemptAt, lastError))
}

// MarkOutboxEventPermanentlyFailed marks an outbox event terminally failed.
func (r *Repository) MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	return wrapError(operationOutboxMarkPermanent, postgreslib.ApplyOutboxDeliveryFailure(ctx, r.db, queryOutboxEventMarkPermanent, errs.ErrInvalidArgument, id, attemptCount, "failed_permanently_at", failedAt, lastError))
}

func (r *Repository) mutateWithResult(ctx context.Context, operation string, mutationQuery string, mutationArgs pgx.NamedArgs, result entity.CommandResult, event *entity.OutboxEvent) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		if err := runMutation(ctx, tx, mutationQuery, mutationArgs, true); err != nil {
			return err
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

func queueGatePolicies(ctx context.Context, tx pgx.Tx, policies []entity.GatePolicy) error {
	if len(policies) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, policy := range policies {
		batch.Queue(queryGatePolicyCreate, gatePolicyArgs(policy))
	}
	return execBatch(ctx, tx, batch)
}

func queueRiskRules(ctx context.Context, tx pgx.Tx, rules []entity.RiskRule) error {
	if len(rules) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, rule := range rules {
		batch.Queue(queryRiskRuleCreate, riskRuleArgs(rule))
	}
	return execBatch(ctx, tx, batch)
}

func queueRiskFactors(ctx context.Context, tx pgx.Tx, factors []entity.RiskFactor) error {
	if len(factors) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, factor := range factors {
		batch.Queue(queryRiskFactorCreate, riskFactorArgs(factor))
	}
	return execBatch(ctx, tx, batch)
}

func queueOutboxEvents(ctx context.Context, tx pgx.Tx, events []entity.OutboxEvent) error {
	if len(events) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, event := range events {
		batch.Queue(queryOutboxEventCreate, outboxEventArgs(event))
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
