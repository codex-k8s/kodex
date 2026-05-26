package governance

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	migrationtest "github.com/codex-k8s/kodex/libs/go/migrationtest"
	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

var sqlHeaderPattern = regexp.MustCompile(`^-- name: ([a-z0-9_]+__[a-z0-9_]+) :(one|many|exec)$`)

func TestSQLFilesHaveNamedHeaders(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(SQLFiles, "sql/*.sql")
	if err != nil {
		t.Fatalf("glob sql files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected embedded SQL files")
	}
	for _, file := range files {
		contentBytes, err := SQLFiles.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		firstLine, _, _ := strings.Cut(string(contentBytes), "\n")
		match := sqlHeaderPattern.FindStringSubmatch(firstLine)
		if match == nil {
			t.Fatalf("%s has invalid named query header: %q", file, firstLine)
		}
		queryName := strings.TrimSuffix(filepath.Base(file), ".sql")
		if match[1] != queryName {
			t.Fatalf("%s header query name = %s, want %s", file, match[1], queryName)
		}
	}
}

func TestRepositoryLoadsEverySQLFile(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(SQLFiles, "sql/*.sql")
	if err != nil {
		t.Fatalf("glob sql files: %v", err)
	}
	for _, file := range files {
		queryName := strings.TrimSuffix(filepath.Base(file), ".sql")
		queryText, err := loadQuery(queryName)
		if err != nil {
			t.Fatalf("load query %s: %v", queryName, err)
		}
		if strings.TrimSpace(queryText) == "" {
			t.Fatalf("query %s is empty", queryName)
		}
	}
}

func TestWrapErrorMapsPostgresErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want error
	}{
		{name: "not found", err: pgx.ErrNoRows, want: errs.ErrNotFound},
		{name: "unique", err: &pgconn.PgError{Code: "23505"}, want: errs.ErrAlreadyExists},
		{name: "foreign key", err: &pgconn.PgError{Code: "23503"}, want: errs.ErrPreconditionFailed},
		{name: "check", err: &pgconn.PgError{Code: "23514"}, want: errs.ErrInvalidArgument},
		{name: "serialization", err: &pgconn.PgError{Code: "40001"}, want: errs.ErrConflict},
		{name: "deadlock", err: &pgconn.PgError{Code: "40P01"}, want: errs.ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := wrapError("test operation", tc.err); !errors.Is(got, tc.want) {
				t.Fatalf("wrapError() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRepositoryIntegrationGovernanceStateAndOutbox(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	profile := testRiskProfile(now)
	if err := repository.CreateRiskProfile(ctx, profile, testCommandResult(uuid.New(), operationCreateRiskProfile, "risk_profile", profile.ID, now)); err != nil {
		t.Fatalf("create risk profile: %v", err)
	}

	gatePolicyID := uuid.New()
	version := entity.RiskProfileVersion{
		RiskProfileID:  profile.ID,
		ProfileVersion: 1,
		Status:         enum.RiskProfileVersionStatusDraft,
		ContentDigest:  "sha256:test",
		CreatedAt:      now,
		GatePolicies: []entity.GatePolicy{{
			ID:                     gatePolicyID,
			RiskProfileID:          &profile.ID,
			ProfileVersion:         1,
			GateKind:               enum.GateKindRelease,
			MinRiskClass:           enum.RiskClassR2,
			RequiredActorPolicyRef: "access-policy:release-owner",
			RequiredSignalKinds:    []string{"owner"},
			Status:                 enum.RuleStatusActive,
		}},
		Rules: []entity.RiskRule{{
			ID:                   uuid.New(),
			RiskProfileID:        profile.ID,
			ProfileVersion:       1,
			RuleKind:             enum.RiskRuleKindRelease,
			MatcherJSON:          []byte(`{"release_line":"stable"}`),
			MinRiskClass:         enum.RiskClassR2,
			RequiredGatePolicyID: &gatePolicyID,
			ReasonTemplate:       []value.LocalizedText{{Locale: "ru", Text: "Требуется release gate"}},
			Status:               enum.RuleStatusActive,
			CreatedAt:            now,
			UpdatedAt:            now,
		}},
	}
	if err := repository.CreateRiskProfileVersion(ctx, version, testCommandResult(uuid.New(), operationCreateRiskProfileVersion, "risk_profile_version", profile.ID, now)); err != nil {
		t.Fatalf("create risk profile version: %v", err)
	}
	activeVersion := int64(1)
	profile.ActiveVersion = &activeVersion
	profile.Status = enum.RiskProfileStatusActive
	profile.Version = 2
	profile.UpdatedAt = now.Add(time.Minute)
	version.Status = enum.RiskProfileVersionStatusActive
	version.ActivatedAt = &profile.UpdatedAt
	if err := repository.ActivateRiskProfileVersion(ctx, profile, 1, version, testCommandResult(uuid.New(), operationActivateRiskProfileVersion, "risk_profile", profile.ID, now), testEvent("governance.policy.version_activated", "risk_profile", profile.ID, now)); err != nil {
		t.Fatalf("activate version: %v", err)
	}
	storedVersion, err := repository.GetRiskProfileVersion(ctx, profile.ID, 1)
	if err != nil {
		t.Fatalf("get profile version: %v", err)
	}
	if storedVersion.Status != enum.RiskProfileVersionStatusActive || len(storedVersion.Rules) != 1 || len(storedVersion.GatePolicies) != 1 {
		t.Fatalf("stored version = %+v, want active with one rule and policy", storedVersion)
	}

	assessment := testRiskAssessment(now)
	factor := entity.RiskFactor{ID: uuid.New(), RiskAssessmentID: assessment.ID, SourceType: enum.RiskFactorSourceTypePolicy, SourceRef: "rule:release", RiskClass: enum.RiskClassR2, Summary: "release gate", CreatedAt: now}
	if err := repository.CreateRiskAssessment(ctx, assessment, []entity.RiskFactor{factor}, testCommandResult(uuid.New(), operationCreateRiskAssessment, "risk_assessment", assessment.ID, now), []entity.OutboxEvent{testEvent("governance.risk_assessment.requested", "risk_assessment", assessment.ID, now)}); err != nil {
		t.Fatalf("create assessment: %v", err)
	}
	factors, _, err := repository.ListRiskFactors(ctx, query.RiskFactorFilter{RiskAssessmentID: assessment.ID})
	if err != nil {
		t.Fatalf("list factors: %v", err)
	}
	if len(factors) != 1 || factors[0].RiskClass != enum.RiskClassR2 {
		t.Fatalf("factors = %+v, want one R2 factor", factors)
	}
	assessment.Version = 2
	assessment.UpdatedAt = now.Add(time.Minute)
	assessment.RiskProfileID = &profile.ID
	assessment.RiskProfileVersion = &activeVersion
	assessment.EvaluationSummary = value.RiskEvaluationSummary{
		ChangedFilesSummaryRef: "provider-summary:pr-1-files",
		Summary:                "bounded release summary",
		Factors: []value.RiskEvaluationFactor{{
			SourceType: string(enum.RiskFactorSourceTypeSecret),
			Ref:        "secret-scope:release-token",
			Summary:    "release secret scope changed",
			Tags:       []string{"auth"},
		}},
	}
	assessment.EvidenceRefs = []value.EvidenceRef{{Kind: "summary", Ref: "evidence:release-summary", Summary: "bounded evidence"}}
	assessment.EffectiveRiskClass = enum.RiskClassR3
	assessment.Explanation = "risk_class=R3 factors=1 required_gates=1"
	assessment.RequiredGates = []entity.RequiredGate{{GatePolicyID: gatePolicyID, GateKind: enum.GateKindRelease, MinRiskClass: enum.RiskClassR2, Reason: "DB migration needs release gate"}}
	updatedFactor := entity.RiskFactor{ID: uuid.New(), RiskAssessmentID: assessment.ID, SourceType: enum.RiskFactorSourceTypeSecret, SourceRef: "secret-scope:release-token", RiskClass: enum.RiskClassR3, Summary: "release secret scope changed", CreatedAt: now.Add(time.Minute)}
	if err := repository.UpdateRiskAssessment(ctx, assessment, []entity.RiskFactor{updatedFactor}, 1, testCommandResult(uuid.New(), operationUpdateRiskAssessment, "risk_assessment", assessment.ID, now), []entity.OutboxEvent{testEvent("governance.risk_assessment.changed", "risk_assessment", assessment.ID, now)}); err != nil {
		t.Fatalf("update assessment: %v", err)
	}
	storedAssessment, err := repository.GetRiskAssessment(ctx, assessment.ID)
	if err != nil {
		t.Fatalf("get updated assessment: %v", err)
	}
	if storedAssessment.Version != 2 || storedAssessment.RiskProfileID == nil || *storedAssessment.RiskProfileID != profile.ID || storedAssessment.RiskProfileVersion == nil || *storedAssessment.RiskProfileVersion != activeVersion {
		t.Fatalf("stored assessment profile/version = %+v/%+v version=%d", storedAssessment.RiskProfileID, storedAssessment.RiskProfileVersion, storedAssessment.Version)
	}
	if storedAssessment.EvaluationSummary.ChangedFilesSummaryRef != "provider-summary:pr-1-files" || len(storedAssessment.EvidenceRefs) != 1 {
		t.Fatalf("stored evaluation/evidence = %+v/%+v", storedAssessment.EvaluationSummary, storedAssessment.EvidenceRefs)
	}
	factors, _, err = repository.ListRiskFactors(ctx, query.RiskFactorFilter{RiskAssessmentID: assessment.ID})
	if err != nil {
		t.Fatalf("list updated factors: %v", err)
	}
	if len(factors) != 1 || factors[0].SourceType != enum.RiskFactorSourceTypeSecret || factors[0].RiskClass != enum.RiskClassR3 {
		t.Fatalf("updated factors = %+v, want one R3 secret factor", factors)
	}

	signal := entity.ReviewSignal{ID: uuid.New(), RiskAssessmentID: &assessment.ID, Target: assessment.Target, RoleKind: enum.ReviewRoleKindOwner, AuthorRef: "user:owner", Outcome: enum.ReviewSignalOutcomePass, Severity: enum.SignalSeverityInfo, Confidence: enum.ConfidenceHigh, Summary: "approved", CreatedAt: now}
	if err := repository.RecordReviewSignal(ctx, signal, testCommandResult(uuid.New(), operationRecordReviewSignal, "review_signal", signal.ID, now), testEvent("governance.review_signal.recorded", "review_signal", signal.ID, now)); err != nil {
		t.Fatalf("record review signal: %v", err)
	}
	signals, _, err := repository.ListReviewSignals(ctx, query.ReviewSignalFilter{RiskAssessmentID: &assessment.ID})
	if err != nil {
		t.Fatalf("list review signals: %v", err)
	}
	if len(signals) != 1 || signals[0].AuthorRef != signal.AuthorRef {
		t.Fatalf("signals = %+v, want stored signal", signals)
	}

	gateRequest := entity.GateRequest{
		VersionedBase:          entity.VersionedBase{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		RiskAssessmentID:       &assessment.ID,
		GatePolicyID:           &gatePolicyID,
		Target:                 assessment.Target,
		InteractionDeliveryRef: value.InteractionDeliveryRef{RequestRef: "interaction:request:1"},
		EvidenceSummary:        "release evidence",
		Status:                 enum.GateRequestStatusRequested,
	}
	if err := repository.CreateGateRequest(ctx, gateRequest, testCommandResult(uuid.New(), operationCreateGateRequest, "gate_request", gateRequest.ID, now), testEvent("governance.gate.requested", "gate", gateRequest.ID, now)); err != nil {
		t.Fatalf("create gate request: %v", err)
	}
	decision := entity.GateDecision{ID: uuid.New(), GateRequestID: gateRequest.ID, DecisionActorRef: "user:owner", Outcome: enum.GateOutcomeApprove, Reason: "safe", DecidedAt: now.Add(time.Minute)}
	gateRequest.Version = 2
	gateRequest.Status = enum.GateRequestStatusResolved
	gateRequest.UpdatedAt = now.Add(time.Minute)
	if err := repository.UpdateGateRequestWithDecision(ctx, gateRequest, 1, decision, testCommandResult(uuid.New(), operationSubmitGateDecision, "gate_decision", decision.ID, now), testEvent("governance.gate.resolved", "gate", gateRequest.ID, now)); err != nil {
		t.Fatalf("submit gate decision: %v", err)
	}
	storedDecision, err := repository.GetGateDecision(ctx, decision.ID)
	if err != nil {
		t.Fatalf("get gate decision: %v", err)
	}
	if storedDecision.Outcome != enum.GateOutcomeApprove {
		t.Fatalf("gate decision outcome = %q, want approve", storedDecision.Outcome)
	}
	expiredGateRequest := entity.GateRequest{
		VersionedBase:          entity.VersionedBase{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		RiskAssessmentID:       &assessment.ID,
		GatePolicyID:           &gatePolicyID,
		Target:                 assessment.Target,
		InteractionDeliveryRef: value.InteractionDeliveryRef{RequestRef: "interaction:request:2"},
		EvidenceSummary:        "release evidence",
		Status:                 enum.GateRequestStatusRequested,
	}
	if err := repository.CreateGateRequest(ctx, expiredGateRequest, testCommandResult(uuid.New(), operationCreateGateRequest, "gate_request", expiredGateRequest.ID, now), testEvent("governance.gate.requested", "gate", expiredGateRequest.ID, now)); err != nil {
		t.Fatalf("create expiring gate request: %v", err)
	}
	terminalAt := now.Add(2 * time.Minute)
	expiredGateRequest.Version = 2
	expiredGateRequest.Status = enum.GateRequestStatusExpired
	expiredGateRequest.TerminalActorRef = "service:interaction-hub"
	expiredGateRequest.TerminalReason = "timeout"
	expiredGateRequest.TerminalAt = &terminalAt
	expiredGateRequest.UpdatedAt = terminalAt
	if err := repository.UpdateGateRequestStatus(ctx, expiredGateRequest, 1, testCommandResult(uuid.New(), operationUpdateGateRequestStatus, "gate_request", expiredGateRequest.ID, now), testEvent("governance.gate.expired", "gate", expiredGateRequest.ID, now)); err != nil {
		t.Fatalf("expire gate request: %v", err)
	}
	storedExpiredGate, err := repository.GetGateRequest(ctx, expiredGateRequest.ID)
	if err != nil {
		t.Fatalf("get expired gate request: %v", err)
	}
	if storedExpiredGate.Status != enum.GateRequestStatusExpired || storedExpiredGate.TerminalActorRef != "service:interaction-hub" || storedExpiredGate.TerminalAt == nil {
		t.Fatalf("expired gate request = %+v, want terminal metadata", storedExpiredGate)
	}

	releasePackage := entity.ReleaseDecisionPackage{
		VersionedBase:           entity.VersionedBase{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ReleaseCandidateRef:     "release:v1.0.0",
		ProjectContext:          value.ProjectContextRef{ProjectRef: "project:alpha", ReleasePolicyRef: "release-policy:stable"},
		RepositoryRefs:          []string{"repo:main"},
		RiskAssessmentID:        &assessment.ID,
		ProviderRefs:            []byte(`[{"pull_request_ref":"provider:pr:1"}]`),
		RuntimeRefs:             []byte(`[{"job_ref":"runtime:job:1"}]`),
		AgentContext:            []byte(`{"run_ref":"agent:run:1"}`),
		ReviewSignalIDs:         []uuid.UUID{signal.ID},
		KnownLimitationsSummary: "none",
		Status:                  enum.ReleaseDecisionPackageStatusReady,
	}
	if err := repository.CreateReleaseDecisionPackage(ctx, releasePackage, testCommandResult(uuid.New(), operationBuildReleaseDecisionPackage, "release_decision_package", releasePackage.ID, now), testEvent("governance.release_decision_package.built", "release_decision_package", releasePackage.ID, now)); err != nil {
		t.Fatalf("create release package: %v", err)
	}
	packages, _, err := repository.ListReleaseDecisionPackages(ctx, query.ReleaseDecisionPackageFilter{ProjectContext: value.ProjectContextRef{ProjectRef: "project:alpha"}})
	if err != nil {
		t.Fatalf("list release packages: %v", err)
	}
	if len(packages) != 1 || packages[0].ReleaseCandidateRef != releasePackage.ReleaseCandidateRef {
		t.Fatalf("packages = %+v, want release package", packages)
	}

	claimed, err := repository.ClaimOutboxEvents(ctx, 10, now.Add(2*time.Minute), now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("claim outbox events: %v", err)
	}
	if len(claimed) == 0 {
		t.Fatal("claimed outbox events = 0, want at least one")
	}
	if err := repository.MarkOutboxEventPublished(ctx, claimed[0].ID, claimed[0].AttemptCount, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("mark outbox event published: %v", err)
	}
}

func openIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("KODEX_GOVERNANCE_MANAGER_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("set KODEX_GOVERNANCE_MANAGER_TEST_DATABASE_DSN to run PostgreSQL repository integration tests")
	}
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := "governance_repo_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.WithoutCancel(ctx), "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	for _, statement := range migrationtest.GooseUpStatements(t, "../../../../cmd/cli/migrations") {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("apply governance-manager migration statement %q: %v", statement, err)
		}
	}
	return pool
}

func testRiskProfile(now time.Time) entity.RiskProfile {
	return entity.RiskProfile{
		VersionedBase: entity.VersionedBase{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Scope:         value.ExternalRef{Type: "project", Ref: "project:alpha"},
		Slug:          "default",
		DisplayName:   []value.LocalizedText{{Locale: "ru", Text: "Default"}},
		Status:        enum.RiskProfileStatusDraft,
	}
}

func testRiskAssessment(now time.Time) entity.RiskAssessment {
	return entity.RiskAssessment{
		VersionedBase:      entity.VersionedBase{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Target:             value.ExternalRef{Type: "release_candidate", Ref: "release:v1.0.0"},
		ProjectContext:     value.ProjectContextRef{ProjectRef: "project:alpha", RepositoryRef: "repo:main"},
		ProviderContext:    []byte(`{"pull_request_ref":"provider:pr:1"}`),
		AgentContext:       []byte(`{"run_ref":"agent:run:1"}`),
		RuntimeContext:     []byte(`{"job_ref":"runtime:job:1"}`),
		InitialRiskClass:   enum.RiskClassR2,
		EffectiveRiskClass: enum.RiskClassR2,
		Status:             enum.RiskAssessmentStatusActive,
		Explanation:        "release candidate requires gate",
	}
}

func testCommandResult(commandID uuid.UUID, operation string, aggregateType string, aggregateID uuid.UUID, now time.Time) entity.CommandResult {
	return entity.CommandResult{
		Key:           "command:" + commandID.String(),
		CommandID:     &commandID,
		Actor:         value.Actor{Type: "service", ID: "governance-manager-test"},
		Operation:     operation,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		ResultPayload: []byte(`{"ok":true}`),
		CreatedAt:     now,
	}
}

func testEvent(eventType string, aggregateType string, aggregateID uuid.UUID, occurredAt time.Time) entity.OutboxEvent {
	return entity.OutboxEvent{
		Event:         outboxlib.NewEvent(uuid.New(), eventType, 1, aggregateType, aggregateID, []byte(`{"ok":true}`), occurredAt, 0),
		NextAttemptAt: occurredAt,
	}
}
