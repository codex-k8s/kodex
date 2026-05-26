package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	governanceevents "github.com/codex-k8s/kodex/libs/go/platformevents/governance"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governancerepo "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/repository/governance"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

func TestBacklogOperationReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	service := newTestService(&fakeRepository{ready: true})
	err := service.BacklogOperation(context.Background(), BacklogOperationInput{Operation: enum.OperationReevaluateRisk})
	if !errors.Is(err, errs.ErrNotImplemented) {
		t.Fatalf("BacklogOperation() error = %v, want ErrNotImplemented", err)
	}
}

func TestReadyRequiresRepository(t *testing.T) {
	t.Parallel()

	if New(nil).Ready() {
		t.Fatal("Ready() = true for missing repository, want false")
	}
	if New(&fakeRepository{ready: true}).Ready() {
		t.Fatal("Ready() = true for missing authorizer, want false")
	}
	if !newTestService(&fakeRepository{ready: true}).Ready() {
		t.Fatal("Ready() = false for ready repository and explicit authorizer, want true")
	}
}

func TestEvaluateRiskStoresAssessmentAndOutboxEvents(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	eventOneID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	eventTwoID := uuid.MustParse("dddddddd-dddd-4ddd-dddd-dddddddddddd")
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &fixedIDs{ids: []uuid.UUID{assessmentID, eventOneID, eventTwoID}},
		Authorizer:  AllowAllAuthorizer{},
	})

	assessment, err := service.EvaluateRisk(context.Background(), EvaluateRiskInput{
		Target:         value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/1"},
		ProjectContext: value.ProjectContextRef{ProjectRef: "project:core"},
		Meta: CommandMeta{
			CommandID: &commandID,
			Actor:     value.Actor{Type: "service", ID: "provider-hub"},
			Reason:    "provider checks changed",
		},
	})
	if err != nil {
		t.Fatalf("EvaluateRisk(): %v", err)
	}
	if assessment.ID != assessmentID || assessment.EffectiveRiskClass != enum.RiskClassR0 {
		t.Fatalf("assessment = %#v, want id %s and R0", assessment, assessmentID)
	}
	if repository.assessment.ID != assessmentID {
		t.Fatalf("stored assessment id = %s, want %s", repository.assessment.ID, assessmentID)
	}
	if len(repository.events) != 2 {
		t.Fatalf("stored events = %d, want 2", len(repository.events))
	}
	if repository.result.CommandID == nil || *repository.result.CommandID != commandID {
		t.Fatalf("command result command id = %v, want %s", repository.result.CommandID, commandID)
	}
}

func TestEvaluateRiskClassifiesSafeFactorsAndPolicyRules(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	profileID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	gatePolicyID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	activeVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		profile: entity.RiskProfile{
			VersionedBase: entity.VersionedBase{ID: profileID, Version: 2},
			Status:        enum.RiskProfileStatusActive,
			ActiveVersion: &activeVersion,
		},
		profileVersion: entity.RiskProfileVersion{
			RiskProfileID:  profileID,
			ProfileVersion: activeVersion,
			Status:         enum.RiskProfileVersionStatusActive,
			GatePolicies: []entity.GatePolicy{{
				ID:           gatePolicyID,
				GateKind:     enum.GateKindRelease,
				MinRiskClass: enum.RiskClassR2,
				Status:       enum.RuleStatusActive,
			}},
			Rules: []entity.RiskRule{{
				ID:                   uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd"),
				RiskProfileID:        profileID,
				ProfileVersion:       activeVersion,
				RuleKind:             enum.RiskRuleKindDatabase,
				MatcherJSON:          []byte(`{"path_glob":"services/api/*.sql","factor_tag":"migration"}`),
				MinRiskClass:         enum.RiskClassR2,
				RequiredGatePolicyID: &gatePolicyID,
				ReasonTemplate:       []value.LocalizedText{{Locale: "ru", Text: "DB migration needs release gate"}},
				Status:               enum.RuleStatusActive,
			}},
		},
	}
	service := newTestService(repository)

	assessment, err := service.EvaluateRisk(context.Background(), EvaluateRiskInput{
		Target: value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/827"},
		ProjectContext: value.ProjectContextRef{
			ProjectRef:     "project:core",
			RepositoryRef:  "repo:kodex",
			ReleaseLineRef: "stable",
		},
		EvaluationSummary: value.RiskEvaluationSummary{
			ChangedFilesSummaryRef: "provider-summary:pr-827-files",
			Summary:                "bounded summary",
			Factors: []value.RiskEvaluationFactor{{
				SourceType: string(enum.RiskFactorSourceTypeDatabase),
				Ref:        "services/api/schema.sql",
				Summary:    "schema migration",
				Tags:       []string{"migration"},
			}, {
				SourceType: string(enum.RiskFactorSourceTypeSecret),
				Ref:        "secret-scope:runtime-token",
				Summary:    "secret scope changed",
				Tags:       []string{"auth"},
			}},
		},
		RiskProfileRef: profileID.String(),
		Meta:           CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "provider-hub"}},
	})
	if err != nil {
		t.Fatalf("EvaluateRisk(): %v", err)
	}
	if assessment.EffectiveRiskClass != enum.RiskClassR3 {
		t.Fatalf("effective risk = %s, want R3", assessment.EffectiveRiskClass)
	}
	if len(assessment.RequiredGates) != 1 || assessment.RequiredGates[0].GatePolicyID != gatePolicyID {
		t.Fatalf("required gates = %+v, want gate policy %s", assessment.RequiredGates, gatePolicyID)
	}
	if assessment.RiskProfileID == nil || *assessment.RiskProfileID != profileID || assessment.RiskProfileVersion == nil || *assessment.RiskProfileVersion != activeVersion {
		t.Fatalf("assessment profile refs = %v/%v, want %s/%d", assessment.RiskProfileID, assessment.RiskProfileVersion, profileID, activeVersion)
	}
	if len(repository.riskFactors) < 3 {
		t.Fatalf("risk factors = %+v, want input and policy factors", repository.riskFactors)
	}
	for _, event := range repository.events {
		payload := string(event.Payload)
		if strings.Contains(payload, "schema.sql") || strings.Contains(payload, "runtime-token") || strings.Contains(payload, "raw_diff") {
			t.Fatalf("outbox payload leaked unsafe evaluation detail: %s", payload)
		}
	}
}

func TestEvaluateRiskRejectsUnsafeSummary(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	repository := &fakeRepository{ready: true}
	service := newTestService(repository)

	_, err := service.EvaluateRisk(context.Background(), EvaluateRiskInput{
		Target:            value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/827"},
		EvaluationSummary: value.RiskEvaluationSummary{Summary: "raw_diff: full provider diff"},
		Meta:              CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "provider-hub"}},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("EvaluateRisk() error = %v, want ErrInvalidArgument", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0", repository.mutationCalls)
	}
}

func TestEvaluateRiskAccessDenied(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository: repository,
		Authorizer: authorizerFunc(func(context.Context, AuthorizationRequest) error {
			return errs.ErrForbidden
		}),
	})

	_, err := service.EvaluateRisk(context.Background(), EvaluateRiskInput{
		Target: value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/827"},
		Meta:   CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "provider-hub"}},
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("EvaluateRisk() error = %v, want ErrForbidden", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0", repository.mutationCalls)
	}
}

func TestRiskReadAccessDeniedBeforeRepositoryRead(t *testing.T) {
	t.Parallel()

	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	meta := QueryMeta{Actor: value.Actor{Type: "service", ID: "provider-hub"}}
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository: repository,
		Authorizer: authorizerFunc(func(context.Context, AuthorizationRequest) error {
			return errs.ErrForbidden
		}),
	})

	if _, err := service.GetRiskAssessment(context.Background(), GetRiskAssessmentInput{RiskAssessmentID: assessmentID, Meta: meta}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("GetRiskAssessment() error = %v, want ErrForbidden", err)
	}
	if _, _, err := service.ListRiskFactors(context.Background(), ListRiskFactorsInput{
		Filter: query.RiskFactorFilter{RiskAssessmentID: assessmentID},
		Meta:   meta,
	}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("ListRiskFactors() error = %v, want ErrForbidden", err)
	}
	if _, _, err := service.ListReviewSignals(context.Background(), ListReviewSignalsInput{
		Filter: query.ReviewSignalFilter{RiskAssessmentID: &assessmentID},
		Meta:   meta,
	}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("ListReviewSignals() error = %v, want ErrForbidden", err)
	}
	if _, _, err := service.ListRiskAssessments(context.Background(), ListRiskAssessmentsInput{
		Filter: query.RiskAssessmentFilter{Target: value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/827"}},
		Meta:   meta,
	}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("ListRiskAssessments() error = %v, want ErrForbidden", err)
	}
	if repository.assessmentReads != 0 || repository.riskAssessmentListCalls != 0 || repository.riskFactorListCalls != 0 || repository.reviewSignalListCalls != 0 {
		t.Fatalf("risk reads = assessment:%d list:%d factors:%d signals:%d, want 0 before access allow", repository.assessmentReads, repository.riskAssessmentListCalls, repository.riskFactorListCalls, repository.reviewSignalListCalls)
	}
}

func TestListRiskAssessmentsRejectsContextRefsNotAppliedBySQL(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{ready: true}
	service := newTestService(repository)

	_, _, err := service.ListRiskAssessments(context.Background(), ListRiskAssessmentsInput{
		Filter: query.RiskAssessmentFilter{
			ProjectContext:     value.ProjectContextRef{ServiceRef: "service:api"},
			EffectiveRiskClass: enum.RiskClassR2,
		},
		Meta: QueryMeta{Actor: value.Actor{Type: "service", ID: "provider-hub"}},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListRiskAssessments() error = %v, want ErrInvalidArgument", err)
	}
	if repository.riskAssessmentListCalls != 0 {
		t.Fatalf("risk assessment list calls = %d, want 0", repository.riskAssessmentListCalls)
	}
}

func TestListReviewSignalsRejectsOutcomeOnlyFilter(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{ready: true}
	service := newTestService(repository)

	_, _, err := service.ListReviewSignals(context.Background(), ListReviewSignalsInput{
		Filter: query.ReviewSignalFilter{Outcome: enum.ReviewSignalOutcomePass},
		Meta:   QueryMeta{Actor: value.Actor{Type: "service", ID: "provider-hub"}},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListReviewSignals() error = %v, want ErrInvalidArgument", err)
	}
	if repository.reviewSignalListCalls != 0 {
		t.Fatalf("review signal list calls = %d, want 0", repository.reviewSignalListCalls)
	}
}

func TestReevaluateRiskUsesExpectedVersionAndReviewSignals(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	expectedVersion := int64(3)
	repository := &fakeRepository{
		ready: true,
		assessment: entity.RiskAssessment{
			VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: expectedVersion},
			Target:             value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/827"},
			EvaluationSummary:  value.RiskEvaluationSummary{Summary: "stored safe summary"},
			InitialRiskClass:   enum.RiskClassR1,
			EffectiveRiskClass: enum.RiskClassR1,
			Status:             enum.RiskAssessmentStatusActive,
		},
		reviewSignals: []entity.ReviewSignal{{
			ID:               uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
			RiskAssessmentID: &assessmentID,
			Outcome:          enum.ReviewSignalOutcomeBlock,
			Severity:         enum.SignalSeverityCritical,
			Summary:          "critical owner block",
		}},
	}
	service := newTestService(repository)

	assessment, err := service.ReevaluateRisk(context.Background(), ReevaluateRiskInput{
		RiskAssessmentID: assessmentID,
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "provider-hub"},
		},
	})
	if err != nil {
		t.Fatalf("ReevaluateRisk(): %v", err)
	}
	if assessment.Version != expectedVersion+1 || assessment.EffectiveRiskClass != enum.RiskClassR3 {
		t.Fatalf("assessment version/risk = %d/%s, want %d/R3", assessment.Version, assessment.EffectiveRiskClass, expectedVersion+1)
	}
	if repository.assessmentUpdateCalls != 1 {
		t.Fatalf("assessment update calls = %d, want 1", repository.assessmentUpdateCalls)
	}
	if len(repository.events) != 2 || repository.events[1].EventType != governanceevents.EventRiskAssessmentChanged {
		t.Fatalf("events = %+v, want completed and changed", repository.events)
	}
}

func TestReevaluateRiskPublishesChangedWhenFactorSignatureChanges(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		assessment: entity.RiskAssessment{
			VersionedBase: entity.VersionedBase{ID: assessmentID, Version: expectedVersion},
			Target:        value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/827"},
			EvaluationSummary: value.RiskEvaluationSummary{Factors: []value.RiskEvaluationFactor{{
				SourceType: string(enum.RiskFactorSourceTypeDatabase),
				Ref:        "db:migration",
				Summary:    "new migration summary",
				Tags:       []string{"migration"},
			}}},
			InitialRiskClass:   enum.RiskClassR2,
			EffectiveRiskClass: enum.RiskClassR2,
			Status:             enum.RiskAssessmentStatusActive,
		},
		riskFactors: []entity.RiskFactor{{
			ID:               uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
			RiskAssessmentID: assessmentID,
			SourceType:       enum.RiskFactorSourceTypeDatabase,
			SourceRef:        "db:migration",
			RiskClass:        enum.RiskClassR2,
			Summary:          "old migration summary",
		}},
	}
	service := newTestService(repository)

	assessment, err := service.ReevaluateRisk(context.Background(), ReevaluateRiskInput{
		RiskAssessmentID: assessmentID,
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "provider-hub"},
		},
	})
	if err != nil {
		t.Fatalf("ReevaluateRisk(): %v", err)
	}
	if assessment.EffectiveRiskClass != enum.RiskClassR2 || len(assessment.RequiredGates) != 0 {
		t.Fatalf("assessment risk/gates = %s/%d, want same R2/0", assessment.EffectiveRiskClass, len(assessment.RequiredGates))
	}
	if len(repository.events) != 2 || repository.events[1].EventType != governanceevents.EventRiskAssessmentChanged {
		t.Fatalf("events = %+v, want changed event for factor signature change", repository.events)
	}
}

func TestReevaluateRiskRejectsStaleExpectedVersion(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	expectedVersion := int64(2)
	repository := &fakeRepository{
		ready:      true,
		assessment: entity.RiskAssessment{VersionedBase: entity.VersionedBase{ID: assessmentID, Version: 3}},
	}
	service := newTestService(repository)

	_, err := service.ReevaluateRisk(context.Background(), ReevaluateRiskInput{
		RiskAssessmentID: assessmentID,
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "provider-hub"},
		},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("ReevaluateRisk() error = %v, want ErrConflict", err)
	}
	if repository.assessmentUpdateCalls != 0 {
		t.Fatalf("assessment update calls = %d, want 0", repository.assessmentUpdateCalls)
	}
}

func TestMutatingUseCasesReplayStoredCommandResults(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 30, 0, 0, time.UTC)
	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	meta := CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "provider-hub"}}
	scope := value.ExternalRef{Type: "project", Ref: "project:core"}
	target := value.ExternalRef{Type: "provider_native.pr", Ref: "github://codex-k8s/kodex/pull/804"}
	projectContext := value.ProjectContextRef{ProjectRef: "project:core"}
	profileID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	assessmentID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	reviewSignalID := uuid.MustParse("dddddddd-dddd-4ddd-dddd-dddddddddddd")
	gateRequestID := uuid.MustParse("eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee")
	gateDecisionID := uuid.MustParse("ffffffff-ffff-4fff-ffff-ffffffffffff")
	cancelledGateID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	expiredGateID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	releasePackageID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	profileVersion := int64(20260526123000)

	tests := []struct {
		name      string
		result    entity.CommandResult
		configure func(*fakeRepository)
		run       func(*testing.T, *Service)
	}{
		{
			name:   "create risk profile",
			result: commandResult(meta, enum.OperationCreateRiskProfile.String(), governanceevents.AggregateRiskProfile, profileID, now),
			configure: func(repository *fakeRepository) {
				repository.profile = entity.RiskProfile{VersionedBase: entity.VersionedBase{ID: profileID, Version: 1}, Scope: scope, Slug: "default"}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				profile, err := service.CreateRiskProfile(context.Background(), CreateRiskProfileInput{Scope: scope, Slug: "default", Meta: meta})
				if err != nil {
					t.Fatalf("CreateRiskProfile(): %v", err)
				}
				if profile.ID != profileID {
					t.Fatalf("profile id = %s, want %s", profile.ID, profileID)
				}
			},
		},
		{
			name:   "create risk profile version",
			result: commandResultWithPayload(meta, enum.OperationCreateRiskProfileVersion.String(), aggregateRiskProfileVersion, profileID, now, map[string]any{"profile_version": profileVersion}),
			configure: func(repository *fakeRepository) {
				repository.profileVersion = entity.RiskProfileVersion{RiskProfileID: profileID, ProfileVersion: profileVersion}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				version, err := service.CreateRiskProfileVersion(context.Background(), CreateRiskProfileVersionInput{RiskProfileID: profileID, Meta: meta})
				if err != nil {
					t.Fatalf("CreateRiskProfileVersion(): %v", err)
				}
				if version.ProfileVersion != profileVersion {
					t.Fatalf("profile version = %d, want %d", version.ProfileVersion, profileVersion)
				}
			},
		},
		{
			name:   "activate risk profile version",
			result: commandResultWithPayload(meta, enum.OperationActivateRiskProfileVersion.String(), governanceevents.AggregateRiskProfile, profileID, now, map[string]any{"profile_version": profileVersion}),
			configure: func(repository *fakeRepository) {
				repository.profileVersion = entity.RiskProfileVersion{RiskProfileID: profileID, ProfileVersion: profileVersion, Status: enum.RiskProfileVersionStatusActive}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				version, err := service.ActivateRiskProfileVersion(context.Background(), ActivateRiskProfileVersionInput{RiskProfileID: profileID, ProfileVersion: profileVersion, Meta: meta})
				if err != nil {
					t.Fatalf("ActivateRiskProfileVersion(): %v", err)
				}
				if version.ProfileVersion != profileVersion {
					t.Fatalf("profile version = %d, want %d", version.ProfileVersion, profileVersion)
				}
			},
		},
		{
			name:   "archive risk profile",
			result: commandResult(meta, enum.OperationArchiveRiskProfile.String(), governanceevents.AggregateRiskProfile, profileID, now),
			configure: func(repository *fakeRepository) {
				repository.profile = entity.RiskProfile{VersionedBase: entity.VersionedBase{ID: profileID, Version: 2}, Scope: scope, Slug: "default", Status: enum.RiskProfileStatusArchived}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				profile, err := service.ArchiveRiskProfile(context.Background(), ArchiveRiskProfileInput{RiskProfileID: profileID, Meta: meta})
				if err != nil {
					t.Fatalf("ArchiveRiskProfile(): %v", err)
				}
				if profile.ID != profileID {
					t.Fatalf("profile id = %s, want %s", profile.ID, profileID)
				}
			},
		},
		{
			name:   "evaluate risk",
			result: commandResult(meta, enum.OperationEvaluateRisk.String(), governanceevents.AggregateRiskAssessment, assessmentID, now),
			configure: func(repository *fakeRepository) {
				repository.assessment = entity.RiskAssessment{VersionedBase: entity.VersionedBase{ID: assessmentID, Version: 1}, Target: target, ProjectContext: projectContext}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				assessment, err := service.EvaluateRisk(context.Background(), EvaluateRiskInput{Target: target, ProjectContext: projectContext, Meta: meta})
				if err != nil {
					t.Fatalf("EvaluateRisk(): %v", err)
				}
				if assessment.ID != assessmentID {
					t.Fatalf("assessment id = %s, want %s", assessment.ID, assessmentID)
				}
			},
		},
		{
			name:   "record review signal",
			result: commandResult(meta, enum.OperationRecordReviewSignal.String(), governanceevents.AggregateReviewSignal, reviewSignalID, now),
			configure: func(repository *fakeRepository) {
				repository.reviewSignal = entity.ReviewSignal{ID: reviewSignalID, Target: target, AuthorRef: "reviewer:owner"}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				signal, err := service.RecordReviewSignal(context.Background(), RecordReviewSignalInput{Target: target, AuthorRef: "reviewer:owner", Meta: meta})
				if err != nil {
					t.Fatalf("RecordReviewSignal(): %v", err)
				}
				if signal.ID != reviewSignalID {
					t.Fatalf("review signal id = %s, want %s", signal.ID, reviewSignalID)
				}
			},
		},
		{
			name:   "request gate",
			result: commandResult(meta, enum.OperationRequestGate.String(), aggregateGateRequest, gateRequestID, now),
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 1}, Target: target}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				request, err := service.RequestGate(context.Background(), RequestGateInput{Target: target, Meta: meta})
				if err != nil {
					t.Fatalf("RequestGate(): %v", err)
				}
				if request.ID != gateRequestID {
					t.Fatalf("gate request id = %s, want %s", request.ID, gateRequestID)
				}
			},
		},
		{
			name:   "submit gate decision",
			result: commandResultWithPayload(meta, enum.OperationSubmitGateDecision.String(), aggregateGateDecision, gateDecisionID, now, map[string]any{"gate_request_id": gateRequestID.String()}),
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 2}, Target: target, Status: enum.GateRequestStatusResolved}
				repository.gateDecision = entity.GateDecision{ID: gateDecisionID, GateRequestID: gateRequestID, DecisionActorRef: "user:owner"}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				decision, request, err := service.SubmitGateDecision(context.Background(), SubmitGateDecisionInput{GateRequestID: gateRequestID, DecisionActorRef: "user:owner", Meta: meta})
				if err != nil {
					t.Fatalf("SubmitGateDecision(): %v", err)
				}
				if decision.ID != gateDecisionID || request.ID != gateRequestID {
					t.Fatalf("decision/request ids = %s/%s, want %s/%s", decision.ID, request.ID, gateDecisionID, gateRequestID)
				}
			},
		},
		{
			name:   "cancel gate",
			result: commandResultWithPayload(meta, enum.OperationCancelGate.String(), aggregateGateRequest, cancelledGateID, now, map[string]any{"status": string(enum.GateRequestStatusCancelled)}),
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: cancelledGateID, Version: 2}, Target: target, Status: enum.GateRequestStatusCancelled}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				request, err := service.CancelGate(context.Background(), CancelGateInput{GateRequestID: cancelledGateID, Meta: meta})
				if err != nil {
					t.Fatalf("CancelGate(): %v", err)
				}
				if request.ID != cancelledGateID || request.Status != enum.GateRequestStatusCancelled {
					t.Fatalf("gate request = %+v, want cancelled %s", request, cancelledGateID)
				}
			},
		},
		{
			name:   "expire gate",
			result: commandResultWithPayload(meta, enum.OperationExpireGate.String(), aggregateGateRequest, expiredGateID, now, map[string]any{"status": string(enum.GateRequestStatusExpired)}),
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: expiredGateID, Version: 2}, Target: target, Status: enum.GateRequestStatusExpired}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				request, err := service.ExpireGate(context.Background(), ExpireGateInput{GateRequestID: expiredGateID, Meta: meta})
				if err != nil {
					t.Fatalf("ExpireGate(): %v", err)
				}
				if request.ID != expiredGateID || request.Status != enum.GateRequestStatusExpired {
					t.Fatalf("gate request = %+v, want expired %s", request, expiredGateID)
				}
			},
		},
		{
			name:   "build release decision package",
			result: commandResult(meta, enum.OperationBuildReleaseDecisionPackage.String(), governanceevents.AggregateReleaseDecisionPackage, releasePackageID, now),
			configure: func(repository *fakeRepository) {
				repository.releasePackage = entity.ReleaseDecisionPackage{VersionedBase: entity.VersionedBase{ID: releasePackageID, Version: 1}, ReleaseCandidateRef: "release:v1", ProjectContext: projectContext}
			},
			run: func(t *testing.T, service *Service) {
				t.Helper()
				item, err := service.BuildReleaseDecisionPackage(context.Background(), BuildReleaseDecisionPackageInput{ReleaseCandidateRef: "release:v1", ProjectContext: projectContext, Meta: meta})
				if err != nil {
					t.Fatalf("BuildReleaseDecisionPackage(): %v", err)
				}
				if item.ID != releasePackageID {
					t.Fatalf("release package id = %s, want %s", item.ID, releasePackageID)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repository := &fakeRepository{ready: true, hasCommandResult: true, commandResult: tt.result}
			tt.configure(repository)
			service := NewWithConfig(Config{
				Repository:  repository,
				Clock:       fixedClock{now: now},
				IDGenerator: &fixedIDs{},
				Authorizer:  AllowAllAuthorizer{},
			})

			tt.run(t, service)
			if repository.mutationCalls != 0 {
				t.Fatalf("mutation calls = %d, want 0 for replay", repository.mutationCalls)
			}
		})
	}
}

func TestExistingAggregateMutationsRequireExpectedVersion(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	meta := CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "provider-hub"}}
	profileID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	profileVersion := int64(20260526123000)
	gateRequestID := uuid.MustParse("eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee")

	tests := []struct {
		name string
		run  func(*Service) error
	}{
		{
			name: "activate risk profile version",
			run: func(service *Service) error {
				_, err := service.ActivateRiskProfileVersion(context.Background(), ActivateRiskProfileVersionInput{RiskProfileID: profileID, ProfileVersion: profileVersion, Meta: meta})
				return err
			},
		},
		{
			name: "archive risk profile",
			run: func(service *Service) error {
				_, err := service.ArchiveRiskProfile(context.Background(), ArchiveRiskProfileInput{RiskProfileID: profileID, Meta: meta})
				return err
			},
		},
		{
			name: "submit gate decision",
			run: func(service *Service) error {
				_, _, err := service.SubmitGateDecision(context.Background(), SubmitGateDecisionInput{GateRequestID: gateRequestID, DecisionActorRef: "user:owner", Meta: meta})
				return err
			},
		},
		{
			name: "cancel gate",
			run: func(service *Service) error {
				_, err := service.CancelGate(context.Background(), CancelGateInput{GateRequestID: gateRequestID, Meta: meta})
				return err
			},
		},
		{
			name: "expire gate",
			run: func(service *Service) error {
				_, err := service.ExpireGate(context.Background(), ExpireGateInput{GateRequestID: gateRequestID, Meta: meta})
				return err
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.run(newTestService(&fakeRepository{ready: true}))
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("%s error = %v, want ErrInvalidArgument", tt.name, err)
			}
		})
	}
}

func TestExistingAggregateMutationsRejectStaleExpectedVersion(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	expectedVersion := int64(1)
	meta := CommandMeta{CommandID: &commandID, ExpectedVersion: &expectedVersion, Actor: value.Actor{Type: "service", ID: "provider-hub"}}
	profileID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	profileVersion := int64(20260526123000)
	gateRequestID := uuid.MustParse("eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee")

	tests := []struct {
		name      string
		configure func(*fakeRepository)
		run       func(*Service) error
	}{
		{
			name: "activate risk profile version",
			configure: func(repository *fakeRepository) {
				repository.profile = entity.RiskProfile{VersionedBase: entity.VersionedBase{ID: profileID, Version: 2}}
				repository.profileVersion = entity.RiskProfileVersion{RiskProfileID: profileID, ProfileVersion: profileVersion}
			},
			run: func(service *Service) error {
				_, err := service.ActivateRiskProfileVersion(context.Background(), ActivateRiskProfileVersionInput{RiskProfileID: profileID, ProfileVersion: profileVersion, Meta: meta})
				return err
			},
		},
		{
			name: "archive risk profile",
			configure: func(repository *fakeRepository) {
				repository.profile = entity.RiskProfile{VersionedBase: entity.VersionedBase{ID: profileID, Version: 2}}
			},
			run: func(service *Service) error {
				_, err := service.ArchiveRiskProfile(context.Background(), ArchiveRiskProfileInput{RiskProfileID: profileID, Meta: meta})
				return err
			},
		},
		{
			name: "submit gate decision",
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 2}}
			},
			run: func(service *Service) error {
				_, _, err := service.SubmitGateDecision(context.Background(), SubmitGateDecisionInput{GateRequestID: gateRequestID, DecisionActorRef: "user:owner", Meta: meta})
				return err
			},
		},
		{
			name: "cancel gate",
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 2}, Status: enum.GateRequestStatusRequested}
			},
			run: func(service *Service) error {
				_, err := service.CancelGate(context.Background(), CancelGateInput{GateRequestID: gateRequestID, Meta: meta})
				return err
			},
		},
		{
			name: "expire gate",
			configure: func(repository *fakeRepository) {
				repository.gateRequest = entity.GateRequest{VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 2}, Status: enum.GateRequestStatusRequested}
			},
			run: func(service *Service) error {
				_, err := service.ExpireGate(context.Background(), ExpireGateInput{GateRequestID: gateRequestID, Meta: meta})
				return err
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repository := &fakeRepository{ready: true}
			tt.configure(repository)
			err := tt.run(newTestService(repository))
			if !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("%s error = %v, want ErrConflict", tt.name, err)
			}
			if repository.mutationCalls != 0 {
				t.Fatalf("mutation calls = %d, want 0 for stale expected_version", repository.mutationCalls)
			}
		})
	}
}

func TestCancelGateStoresTerminalStateAndSafeOutbox(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 13, 0, 0, 0, time.UTC)
	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	eventID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	gateRequestID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		gateRequest: entity.GateRequest{
			VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 1, CreatedAt: now, UpdatedAt: now},
			Target:        value.ExternalRef{Type: "pull_request", Ref: "provider:pr:1"},
			Status:        enum.GateRequestStatusRequested,
		},
	}
	service := NewWithConfig(Config{Repository: repository, Clock: fixedClock{now: now}, IDGenerator: &fixedIDs{ids: []uuid.UUID{eventID}}, Authorizer: AllowAllAuthorizer{}})

	request, err := service.CancelGate(context.Background(), CancelGateInput{
		GateRequestID: gateRequestID,
		Reason:        "safe operator cancellation summary",
		InteractionDeliveryRef: value.InteractionDeliveryRef{
			RequestRef: "interaction:request:1",
		},
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if err != nil {
		t.Fatalf("CancelGate(): %v", err)
	}
	if request.Status != enum.GateRequestStatusCancelled || request.Version != 2 {
		t.Fatalf("gate request status/version = %s/%d, want cancelled/2", request.Status, request.Version)
	}
	if request.TerminalActorRef != "service:agent-manager" || request.TerminalReason != "safe operator cancellation summary" || request.TerminalAt == nil {
		t.Fatalf("terminal metadata = actor %q reason %q at %v", request.TerminalActorRef, request.TerminalReason, request.TerminalAt)
	}
	if len(repository.events) != 1 || repository.events[0].EventType != governanceevents.EventGateCancelled {
		t.Fatalf("events = %+v, want one gate cancelled event", repository.events)
	}
	payload := string(repository.events[0].Payload)
	if strings.Contains(payload, "safe operator cancellation summary") || strings.Contains(payload, "interaction:request:1") {
		t.Fatalf("outbox payload leaked terminal summary or interaction ref: %s", payload)
	}
}

func TestExpireGateRejectsClosedGate(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	expectedVersion := int64(1)
	service := newTestService(&fakeRepository{
		ready: true,
		gateRequest: entity.GateRequest{
			VersionedBase: entity.VersionedBase{ID: gateRequestID, Version: 1},
			Status:        enum.GateRequestStatusResolved,
		},
	})

	_, err := service.ExpireGate(context.Background(), ExpireGateInput{
		GateRequestID: gateRequestID,
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "interaction-hub"},
		},
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("ExpireGate() error = %v, want ErrPreconditionFailed", err)
	}
}

func TestGateLifecycleAccessDenied(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	meta := CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}}
	queryMeta := QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}}
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository: repository,
		Authorizer: authorizerFunc(func(context.Context, AuthorizationRequest) error {
			return errs.ErrForbidden
		}),
	})

	if _, err := service.RequestGate(context.Background(), RequestGateInput{Target: value.ExternalRef{Type: "pull_request", Ref: "provider:pr:1"}, Meta: meta}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("RequestGate() error = %v, want ErrForbidden", err)
	}
	if _, err := service.GetGateRequest(context.Background(), GetGateRequestInput{GateRequestID: gateRequestID, Meta: queryMeta}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("GetGateRequest() error = %v, want ErrForbidden", err)
	}
	if _, err := service.GetGateDecision(context.Background(), GetGateDecisionInput{GateDecisionID: uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd"), GateRequestID: gateRequestID, Meta: queryMeta}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("GetGateDecision() error = %v, want ErrForbidden", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0 when access denied", repository.mutationCalls)
	}
	if repository.gateRequestReads != 0 || repository.gateDecisionReads != 0 {
		t.Fatalf("gate reads = request:%d decision:%d, want 0 before access allow", repository.gateRequestReads, repository.gateDecisionReads)
	}
}

func TestGateLifecycleRequiresExplicitAuthorizer(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	service := NewWithConfig(Config{Repository: &fakeRepository{ready: true}, Clock: systemClock{}, IDGenerator: uuidGenerator{}})

	_, err := service.RequestGate(context.Background(), RequestGateInput{
		Target: value.ExternalRef{Type: "pull_request", Ref: "provider:pr:1"},
		Meta:   CommandMeta{CommandID: &commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("RequestGate() error = %v, want ErrDependencyUnavailable", err)
	}
}

func TestGateLifecycleRejectsMissingAccessResource(t *testing.T) {
	t.Parallel()

	service := newTestService(&fakeRepository{ready: true})
	meta := QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}}

	_, _, err := service.ListGateRequests(context.Background(), ListGateRequestsInput{Meta: meta})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListGateRequests() error = %v, want ErrInvalidArgument", err)
	}
	_, _, err = service.ListGateDecisions(context.Background(), ListGateDecisionsInput{Meta: meta})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListGateDecisions() error = %v, want ErrInvalidArgument", err)
	}
	_, _, err = service.ListGateDecisions(context.Background(), ListGateDecisionsInput{
		Filter: query.GateDecisionFilter{Outcome: enum.GateOutcomeApprove},
		Meta:   meta,
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListGateDecisions(outcome only) error = %v, want ErrInvalidArgument", err)
	}
}

func TestListGateRequestsByAssessmentUsesRiskReadAccessContext(t *testing.T) {
	t.Parallel()

	riskAssessmentID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	repository := &fakeRepository{ready: true}
	var captured AuthorizationRequest
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       systemClock{},
		IDGenerator: uuidGenerator{},
		Authorizer: authorizerFunc(func(_ context.Context, request AuthorizationRequest) error {
			captured = request
			return nil
		}),
	})

	_, _, err := service.ListGateRequests(context.Background(), ListGateRequestsInput{
		Filter: query.GateRequestFilter{RiskAssessmentID: &riskAssessmentID},
		Meta:   QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if err != nil {
		t.Fatalf("ListGateRequests(): %v", err)
	}
	if captured.ActionKey != actionRiskRead || captured.ResourceType != "governance_risk_assessment" || captured.ResourceID != riskAssessmentID.String() {
		t.Fatalf("access request = %+v, want risk read on assessment %s", captured, riskAssessmentID)
	}
	if repository.gateRequestListCalls != 1 {
		t.Fatalf("gate request list calls = %d, want 1", repository.gateRequestListCalls)
	}
}

func TestGetGateDecisionRequiresGateRequestForAuthorization(t *testing.T) {
	t.Parallel()

	decisionID := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	service := newTestService(&fakeRepository{ready: true})

	_, err := service.GetGateDecision(context.Background(), GetGateDecisionInput{
		GateDecisionID: decisionID,
		Meta:           QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("GetGateDecision() error = %v, want ErrInvalidArgument", err)
	}
}

func TestGetGateDecisionRejectsMismatchedGateRequest(t *testing.T) {
	t.Parallel()

	decisionID := uuid.MustParse("dddddddd-dddd-4ddd-8ddd-dddddddddddd")
	gateRequestID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	otherGateRequestID := uuid.MustParse("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee")
	service := newTestService(&fakeRepository{
		ready:        true,
		gateDecision: entity.GateDecision{ID: decisionID, GateRequestID: otherGateRequestID, DecisionActorRef: "user:owner"},
	})

	_, err := service.GetGateDecision(context.Background(), GetGateDecisionInput{
		GateDecisionID: decisionID,
		GateRequestID:  gateRequestID,
		Meta:           QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("GetGateDecision() error = %v, want ErrNotFound", err)
	}
}

func TestCancelGateReturnsNotFound(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	gateRequestID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	expectedVersion := int64(1)
	service := newTestService(&fakeRepository{ready: true, gateRequestErr: errs.ErrNotFound})

	_, err := service.CancelGate(context.Background(), CancelGateInput{
		GateRequestID: gateRequestID,
		Meta: CommandMeta{
			CommandID:       &commandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("CancelGate() error = %v, want ErrNotFound", err)
	}
}

func TestReleaseDecisionLifecycleHappyPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	decisionID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	requestCommandID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	submitCommandID := uuid.MustParse("dddddddd-dddd-4ddd-dddd-dddddddddddd")
	requestEventID := uuid.MustParse("eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee")
	submitEventID := uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff")
	expectedPackageVersion := int64(1)
	expectedDecisionVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 1, CreatedAt: now, UpdatedAt: now},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha", ReleasePolicyRef: "policy:stable"},
			Status:              enum.ReleaseDecisionPackageStatusReady,
		},
	}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &fixedIDs{ids: []uuid.UUID{decisionID, requestEventID, submitEventID}},
		Authorizer:  AllowAllAuthorizer{},
	})

	decision, pkg, err := service.RequestReleaseDecision(context.Background(), RequestReleaseDecisionInput{
		ReleaseDecisionPackageID: packageID,
		RequestGateIfRequired:    true,
		Meta: CommandMeta{
			CommandID:       &requestCommandID,
			ExpectedVersion: &expectedPackageVersion,
			Actor:           value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if err != nil {
		t.Fatalf("RequestReleaseDecision(): %v", err)
	}
	if decision.Status != enum.ReleaseDecisionStatusRequested || pkg.Status != enum.ReleaseDecisionPackageStatusDecisionRequested || pkg.Version != 2 {
		t.Fatalf("decision/package = %+v/%+v, want requested package v2", decision, pkg)
	}
	if repository.events[0].EventType != governanceevents.EventReleaseDecisionRequested {
		t.Fatalf("event type = %q, want release decision requested", repository.events[0].EventType)
	}

	decision, pkg, err = service.SubmitReleaseDecision(context.Background(), SubmitReleaseDecisionInput{
		ReleaseDecisionPackageID: packageID,
		Outcome:                  enum.ReleaseDecisionOutcomeHold,
		DecisionActorRef:         "user:release-owner",
		DecisionPolicyRef:        "policy:stable",
		Reason:                   "waiting for rollout window",
		Meta: CommandMeta{
			CommandID:       &submitCommandID,
			ExpectedVersion: &expectedDecisionVersion,
			Actor:           value.Actor{Type: "user", ID: "release-owner"},
		},
	})
	if err != nil {
		t.Fatalf("SubmitReleaseDecision(): %v", err)
	}
	if decision.Status != enum.ReleaseDecisionStatusResolved || decision.Outcome != enum.ReleaseDecisionOutcomeHold || decision.Version != 2 {
		t.Fatalf("decision = %+v, want resolved hold v2", decision)
	}
	if pkg.Status != enum.ReleaseDecisionPackageStatusClosed || pkg.Version != 3 {
		t.Fatalf("package = %+v, want closed v3", pkg)
	}
	if payload := string(repository.events[0].Payload); strings.Contains(payload, "waiting for rollout") || strings.Contains(payload, "raw_diff") {
		t.Fatalf("release decision event leaked unsafe text: %s", payload)
	}
}

func TestReleaseDecisionBlocksGoWhenRiskRequiresGate(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	assessmentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	decisionID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 2},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			RiskAssessmentID:    &assessmentID,
			Status:              enum.ReleaseDecisionPackageStatusDecisionRequested,
		},
		releaseDecision: entity.ReleaseDecision{
			VersionedBase:            entity.VersionedBase{ID: decisionID, Version: 1},
			ReleaseDecisionPackageID: packageID,
			Status:                   enum.ReleaseDecisionStatusRequested,
		},
		assessment: entity.RiskAssessment{
			VersionedBase:      entity.VersionedBase{ID: assessmentID, Version: 1},
			EffectiveRiskClass: enum.RiskClassR3,
		},
	}
	service := newTestService(repository)

	_, _, err := service.SubmitReleaseDecision(context.Background(), SubmitReleaseDecisionInput{
		ReleaseDecisionPackageID: packageID,
		Outcome:                  enum.ReleaseDecisionOutcomeGo,
		DecisionActorRef:         "user:owner",
		Reason:                   "release",
		Meta: CommandMeta{
			CommandID:       ptrUUID(uuid.MustParse("dddddddd-dddd-4ddd-dddd-dddddddddddd")),
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "user", ID: "owner"},
		},
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("SubmitReleaseDecision() error = %v, want ErrPreconditionFailed", err)
	}
	if repository.mutationCalls != 0 {
		t.Fatalf("mutation calls = %d, want 0", repository.mutationCalls)
	}
}

func TestReleaseDecisionBlocksGoWhenActiveSignalExists(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	decisionID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 2},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			Status:              enum.ReleaseDecisionPackageStatusDecisionRequested,
		},
		releaseDecision: entity.ReleaseDecision{
			VersionedBase:            entity.VersionedBase{ID: decisionID, Version: 1},
			ReleaseDecisionPackageID: packageID,
			Status:                   enum.ReleaseDecisionStatusRequested,
		},
		blockingSignals: []entity.BlockingSignal{{
			VersionedBase: entity.VersionedBase{ID: uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc"), Version: 1},
			Target:        value.ExternalRef{Type: "release_candidate", Ref: "release:v1.0.0"},
			Status:        enum.BlockingSignalStatusActive,
			Severity:      enum.SignalSeverityBlocking,
		}},
	}
	service := newTestService(repository)

	_, _, err := service.SubmitReleaseDecision(context.Background(), SubmitReleaseDecisionInput{
		ReleaseDecisionPackageID: packageID,
		Outcome:                  enum.ReleaseDecisionOutcomeGoWithConditions,
		DecisionActorRef:         "user:owner",
		Reason:                   "release",
		Meta: CommandMeta{
			CommandID:       ptrUUID(uuid.MustParse("dddddddd-dddd-4ddd-dddd-dddddddddddd")),
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "user", ID: "owner"},
		},
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("SubmitReleaseDecision() error = %v, want ErrPreconditionFailed", err)
	}
}

func TestReleaseDecisionExpectedVersionConflict(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 2},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
			Status:              enum.ReleaseDecisionPackageStatusReady,
		},
	}
	service := newTestService(repository)

	_, _, err := service.RequestReleaseDecision(context.Background(), RequestReleaseDecisionInput{
		ReleaseDecisionPackageID: packageID,
		Meta: CommandMeta{
			CommandID:       ptrUUID(uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")),
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "agent-manager"},
		},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RequestReleaseDecision() error = %v, want ErrConflict", err)
	}
}

func TestReleaseReadAccessDeniedBeforeRepositoryRead(t *testing.T) {
	t.Parallel()

	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository: repository,
		Authorizer: authorizerFunc(func(context.Context, AuthorizationRequest) error {
			return errs.ErrForbidden
		}),
	})

	_, err := service.GetReleaseDecisionPackage(context.Background(), GetReleaseDecisionPackageInput{
		ReleaseDecisionPackageID: packageID,
		Meta:                     QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("GetReleaseDecisionPackage() error = %v, want ErrForbidden", err)
	}
	if repository.releasePackageReads != 0 {
		t.Fatalf("release package reads = %d, want 0 before access allow", repository.releasePackageReads)
	}
}

func TestBlockingSignalLifecycleAndSafeEventPayload(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	signalID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	recordCommandID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	resolveCommandID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	recordEventID := uuid.MustParse("dddddddd-dddd-4ddd-dddd-dddddddddddd")
	resolveEventID := uuid.MustParse("eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee")
	expectedVersion := int64(1)
	repository := &fakeRepository{ready: true}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &fixedIDs{ids: []uuid.UUID{signalID, recordEventID, resolveEventID}},
		Authorizer:  AllowAllAuthorizer{},
	})

	signal, err := service.RecordBlockingSignal(context.Background(), RecordBlockingSignalInput{
		Target:     value.ExternalRef{Type: "release_candidate", Ref: "release:v1.0.0"},
		SourceType: enum.BlockingSignalSourceTypeRuntime,
		SourceRef:  "runtime:job:1",
		Severity:   enum.SignalSeverityCritical,
		Summary:    "safe bounded summary without logs",
		Meta:       CommandMeta{CommandID: &recordCommandID, Actor: value.Actor{Type: "service", ID: "runtime-manager"}},
	})
	if err != nil {
		t.Fatalf("RecordBlockingSignal(): %v", err)
	}
	if signal.Status != enum.BlockingSignalStatusActive || signal.Version != 1 {
		t.Fatalf("signal = %+v, want active v1", signal)
	}
	if payload := string(repository.events[0].Payload); strings.Contains(payload, "bounded summary") || strings.Contains(payload, "runtime:job:1") {
		t.Fatalf("blocking signal event leaked signal details: %s", payload)
	}
	signal, err = service.ResolveBlockingSignal(context.Background(), ResolveBlockingSignalInput{
		BlockingSignalID:  signalID,
		TerminalStatus:    enum.BlockingSignalStatusResolved,
		ResolutionSummary: "fixed",
		Meta: CommandMeta{
			CommandID:       &resolveCommandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "runtime-manager"},
		},
	})
	if err != nil {
		t.Fatalf("ResolveBlockingSignal(): %v", err)
	}
	if signal.Status != enum.BlockingSignalStatusResolved || signal.ResolvedAt == nil || signal.Version != 2 {
		t.Fatalf("resolved signal = %+v, want resolved v2", signal)
	}
}

func TestReleaseSafetyStateCreateAndUpdate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	packageID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa")
	stateID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	createCommandID := uuid.MustParse("cccccccc-cccc-4ccc-cccc-cccccccccccc")
	updateCommandID := uuid.MustParse("dddddddd-dddd-4ddd-dddd-dddddddddddd")
	createEventID := uuid.MustParse("eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee")
	updateEventID := uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		ready: true,
		releasePackage: entity.ReleaseDecisionPackage{
			VersionedBase:       entity.VersionedBase{ID: packageID, Version: 3},
			ReleaseCandidateRef: "release:v1.0.0",
			ProjectContext:      value.ProjectContextRef{ProjectRef: "project:alpha"},
		},
		blockingSignals: []entity.BlockingSignal{{
			VersionedBase: entity.VersionedBase{ID: uuid.MustParse("99999999-9999-4999-8999-999999999999"), Version: 1},
			Target:        value.ExternalRef{Type: "release_candidate", Ref: "release:v1.0.0"},
			Status:        enum.BlockingSignalStatusActive,
			Severity:      enum.SignalSeverityBlocking,
		}},
	}
	service := NewWithConfig(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &fixedIDs{ids: []uuid.UUID{stateID, createEventID, updateEventID}},
		Authorizer:  AllowAllAuthorizer{},
	})

	state, err := service.RecordReleaseSafetyState(context.Background(), RecordReleaseSafetyStateInput{
		ReleaseDecisionPackageID: packageID,
		CurrentState:             enum.ReleaseSafetyStateKindAwaitingReleaseGate,
		LastStateReason:          "active blocker",
		Meta:                     CommandMeta{CommandID: &createCommandID, Actor: value.Actor{Type: "service", ID: "runtime-manager"}},
	})
	if err != nil {
		t.Fatalf("RecordReleaseSafetyState(create): %v", err)
	}
	if state.Version != 1 || state.BlockingSignalCount != 1 {
		t.Fatalf("state = %+v, want v1 with one active blocker", state)
	}
	state, err = service.RecordReleaseSafetyState(context.Background(), RecordReleaseSafetyStateInput{
		ReleaseDecisionPackageID: packageID,
		CurrentState:             enum.ReleaseSafetyStateKindStable,
		RuntimeJobRef:            "runtime:job:stable",
		LastStateReason:          "postdeploy healthy",
		Meta: CommandMeta{
			CommandID:       &updateCommandID,
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: "runtime-manager"},
		},
	})
	if err != nil {
		t.Fatalf("RecordReleaseSafetyState(update): %v", err)
	}
	if state.Version != 2 || state.CurrentState != enum.ReleaseSafetyStateKindStable {
		t.Fatalf("state = %+v, want stable v2", state)
	}
}

type fakeRepository struct {
	governancerepo.Repository
	ready                    bool
	hasCommandResult         bool
	commandResult            entity.CommandResult
	profile                  entity.RiskProfile
	profileVersion           entity.RiskProfileVersion
	assessment               entity.RiskAssessment
	assessmentReads          int
	riskFactors              []entity.RiskFactor
	riskAssessmentListCalls  int
	riskFactorListCalls      int
	assessmentUpdateCalls    int
	reviewSignal             entity.ReviewSignal
	reviewSignals            []entity.ReviewSignal
	reviewSignalListCalls    int
	gateRequest              entity.GateRequest
	gateRequestErr           error
	gateRequestReads         int
	gateRequestListCalls     int
	gateDecision             entity.GateDecision
	gateDecisionErr          error
	gateDecisionReads        int
	gateDecisionListCalls    int
	releasePackage           entity.ReleaseDecisionPackage
	releasePackageReads      int
	releaseDecision          entity.ReleaseDecision
	releaseDecisionReads     int
	releaseDecisionListCalls int
	releaseSafetyState       entity.ReleaseSafetyState
	releaseSafetyStateErr    error
	blockingSignal           entity.BlockingSignal
	blockingSignals          []entity.BlockingSignal
	blockingSignalReads      int
	blockingSignalListCalls  int
	result                   entity.CommandResult
	events                   []entity.OutboxEvent
	mutationCalls            int
}

func newTestService(repository *fakeRepository) *Service {
	return NewWithConfig(Config{
		Repository:  repository,
		Clock:       systemClock{},
		IDGenerator: uuidGenerator{},
		Authorizer:  AllowAllAuthorizer{},
	})
}

func (repository *fakeRepository) Ready() bool {
	return repository.ready
}

func (repository *fakeRepository) GetCommandResult(_ context.Context, _ query.CommandIdentity) (entity.CommandResult, error) {
	if !repository.hasCommandResult {
		return entity.CommandResult{}, errs.ErrNotFound
	}
	return repository.commandResult, nil
}

func (repository *fakeRepository) CreateRiskProfile(_ context.Context, profile entity.RiskProfile, result entity.CommandResult) error {
	repository.mutationCalls++
	repository.profile = profile
	repository.result = result
	return nil
}

func (repository *fakeRepository) CreateRiskProfileVersion(_ context.Context, version entity.RiskProfileVersion, result entity.CommandResult) error {
	repository.mutationCalls++
	repository.profileVersion = version
	repository.result = result
	return nil
}

func (repository *fakeRepository) ActivateRiskProfileVersion(_ context.Context, profile entity.RiskProfile, _ int64, version entity.RiskProfileVersion, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.profile = profile
	repository.profileVersion = version
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) ArchiveRiskProfile(_ context.Context, profile entity.RiskProfile, _ int64, result entity.CommandResult) error {
	repository.mutationCalls++
	repository.profile = profile
	repository.result = result
	return nil
}

func (repository *fakeRepository) GetRiskProfile(_ context.Context, _ uuid.UUID) (entity.RiskProfile, error) {
	return repository.profile, nil
}

func (repository *fakeRepository) GetRiskProfileVersion(_ context.Context, _ uuid.UUID, _ int64) (entity.RiskProfileVersion, error) {
	return repository.profileVersion, nil
}

func (repository *fakeRepository) CreateRiskAssessment(_ context.Context, assessment entity.RiskAssessment, factors []entity.RiskFactor, result entity.CommandResult, events []entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.assessment = assessment
	repository.riskFactors = factors
	repository.result = result
	repository.events = events
	return nil
}

func (repository *fakeRepository) UpdateRiskAssessment(_ context.Context, assessment entity.RiskAssessment, factors []entity.RiskFactor, _ int64, result entity.CommandResult, events []entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.assessmentUpdateCalls++
	repository.assessment = assessment
	repository.riskFactors = factors
	repository.result = result
	repository.events = events
	return nil
}

func (repository *fakeRepository) GetRiskAssessment(_ context.Context, _ uuid.UUID) (entity.RiskAssessment, error) {
	repository.assessmentReads++
	return repository.assessment, nil
}

func (repository *fakeRepository) ListRiskAssessments(_ context.Context, _ query.RiskAssessmentFilter) ([]entity.RiskAssessment, query.PageResult, error) {
	repository.riskAssessmentListCalls++
	return nil, query.PageResult{}, nil
}

func (repository *fakeRepository) ListRiskFactors(_ context.Context, _ query.RiskFactorFilter) ([]entity.RiskFactor, query.PageResult, error) {
	repository.riskFactorListCalls++
	return repository.riskFactors, query.PageResult{}, nil
}

func (repository *fakeRepository) RecordReviewSignal(_ context.Context, signal entity.ReviewSignal, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.reviewSignal = signal
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) GetReviewSignal(_ context.Context, _ uuid.UUID) (entity.ReviewSignal, error) {
	return repository.reviewSignal, nil
}

func (repository *fakeRepository) ListReviewSignals(_ context.Context, _ query.ReviewSignalFilter) ([]entity.ReviewSignal, query.PageResult, error) {
	repository.reviewSignalListCalls++
	return repository.reviewSignals, query.PageResult{}, nil
}

func (repository *fakeRepository) CreateGateRequest(_ context.Context, request entity.GateRequest, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.gateRequest = request
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) UpdateGateRequestStatus(_ context.Context, request entity.GateRequest, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.gateRequest = request
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) UpdateGateRequestWithDecision(_ context.Context, request entity.GateRequest, _ int64, decision entity.GateDecision, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.gateRequest = request
	repository.gateDecision = decision
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) GetGateRequest(_ context.Context, _ uuid.UUID) (entity.GateRequest, error) {
	repository.gateRequestReads++
	if repository.gateRequestErr != nil {
		return entity.GateRequest{}, repository.gateRequestErr
	}
	return repository.gateRequest, nil
}

func (repository *fakeRepository) GetGateDecision(_ context.Context, _ uuid.UUID) (entity.GateDecision, error) {
	repository.gateDecisionReads++
	if repository.gateDecisionErr != nil {
		return entity.GateDecision{}, repository.gateDecisionErr
	}
	return repository.gateDecision, nil
}

func (repository *fakeRepository) ListGateRequests(_ context.Context, _ query.GateRequestFilter) ([]entity.GateRequest, query.PageResult, error) {
	repository.gateRequestListCalls++
	return nil, query.PageResult{}, nil
}

func (repository *fakeRepository) ListGateDecisions(_ context.Context, _ query.GateDecisionFilter) ([]entity.GateDecision, query.PageResult, error) {
	repository.gateDecisionListCalls++
	return nil, query.PageResult{}, nil
}

func (repository *fakeRepository) CreateReleaseDecisionPackage(_ context.Context, item entity.ReleaseDecisionPackage, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.releasePackage = item
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) UpdateReleaseDecisionPackageStatus(_ context.Context, item entity.ReleaseDecisionPackage, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.releasePackage = item
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) GetReleaseDecisionPackage(_ context.Context, _ uuid.UUID) (entity.ReleaseDecisionPackage, error) {
	repository.releasePackageReads++
	return repository.releasePackage, nil
}

func (repository *fakeRepository) ListReleaseDecisionPackages(_ context.Context, _ query.ReleaseDecisionPackageFilter) ([]entity.ReleaseDecisionPackage, query.PageResult, error) {
	return []entity.ReleaseDecisionPackage{repository.releasePackage}, query.PageResult{}, nil
}

func (repository *fakeRepository) CreateReleaseDecision(_ context.Context, pkg entity.ReleaseDecisionPackage, _ int64, decision entity.ReleaseDecision, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.releasePackage = pkg
	repository.releaseDecision = decision
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) UpdateReleaseDecision(_ context.Context, pkg entity.ReleaseDecisionPackage, _ int64, decision entity.ReleaseDecision, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.releasePackage = pkg
	repository.releaseDecision = decision
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) GetReleaseDecision(_ context.Context, _ uuid.UUID) (entity.ReleaseDecision, error) {
	repository.releaseDecisionReads++
	return repository.releaseDecision, nil
}

func (repository *fakeRepository) GetReleaseDecisionByPackage(_ context.Context, _ uuid.UUID) (entity.ReleaseDecision, error) {
	repository.releaseDecisionReads++
	if repository.releaseDecision.ID == uuid.Nil {
		return entity.ReleaseDecision{}, errs.ErrNotFound
	}
	return repository.releaseDecision, nil
}

func (repository *fakeRepository) ListReleaseDecisions(_ context.Context, _ query.ReleaseDecisionFilter) ([]entity.ReleaseDecision, query.PageResult, error) {
	repository.releaseDecisionListCalls++
	return []entity.ReleaseDecision{repository.releaseDecision}, query.PageResult{}, nil
}

func (repository *fakeRepository) RecordReleaseSafetyState(_ context.Context, state entity.ReleaseSafetyState, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.releaseSafetyState = state
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) UpdateReleaseSafetyState(_ context.Context, state entity.ReleaseSafetyState, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.releaseSafetyState = state
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) GetReleaseSafetyStateByPackage(_ context.Context, _ uuid.UUID) (entity.ReleaseSafetyState, error) {
	if repository.releaseSafetyStateErr != nil {
		return entity.ReleaseSafetyState{}, repository.releaseSafetyStateErr
	}
	if repository.releaseSafetyState.ID == uuid.Nil {
		return entity.ReleaseSafetyState{}, errs.ErrNotFound
	}
	return repository.releaseSafetyState, nil
}

func (repository *fakeRepository) RecordBlockingSignal(_ context.Context, signal entity.BlockingSignal, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.blockingSignal = signal
	repository.blockingSignals = append(repository.blockingSignals, signal)
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) UpdateBlockingSignal(_ context.Context, signal entity.BlockingSignal, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.blockingSignal = signal
	repository.result = result
	repository.events = []entity.OutboxEvent{event}
	return nil
}

func (repository *fakeRepository) GetBlockingSignal(_ context.Context, _ uuid.UUID) (entity.BlockingSignal, error) {
	repository.blockingSignalReads++
	return repository.blockingSignal, nil
}

func (repository *fakeRepository) ListBlockingSignals(_ context.Context, filter query.BlockingSignalFilter) ([]entity.BlockingSignal, query.PageResult, error) {
	repository.blockingSignalListCalls++
	if len(repository.blockingSignals) == 0 {
		return nil, query.PageResult{}, nil
	}
	items := make([]entity.BlockingSignal, 0, len(repository.blockingSignals))
	for _, signal := range repository.blockingSignals {
		if externalRefProvided(filter.Target) && !sameExternalRef(signal.Target, filter.Target) {
			continue
		}
		if filter.Status != "" && signal.Status != filter.Status {
			continue
		}
		if filter.Severity != "" && signal.Severity != filter.Severity {
			continue
		}
		items = append(items, signal)
	}
	return items, query.PageResult{}, nil
}

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time {
	return clock.now
}

type fixedIDs struct {
	ids []uuid.UUID
}

func (generator *fixedIDs) New() uuid.UUID {
	if len(generator.ids) == 0 {
		return uuid.Nil
	}
	id := generator.ids[0]
	generator.ids = generator.ids[1:]
	return id
}

func ptrUUID(id uuid.UUID) *uuid.UUID {
	return &id
}

type authorizerFunc func(context.Context, AuthorizationRequest) error

func (fn authorizerFunc) Authorize(ctx context.Context, request AuthorizationRequest) error {
	return fn(ctx, request)
}
