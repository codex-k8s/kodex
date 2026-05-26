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

type fakeRepository struct {
	governancerepo.Repository
	ready                 bool
	hasCommandResult      bool
	commandResult         entity.CommandResult
	profile               entity.RiskProfile
	profileVersion        entity.RiskProfileVersion
	assessment            entity.RiskAssessment
	reviewSignal          entity.ReviewSignal
	gateRequest           entity.GateRequest
	gateRequestErr        error
	gateRequestReads      int
	gateRequestListCalls  int
	gateDecision          entity.GateDecision
	gateDecisionErr       error
	gateDecisionReads     int
	gateDecisionListCalls int
	releasePackage        entity.ReleaseDecisionPackage
	result                entity.CommandResult
	events                []entity.OutboxEvent
	mutationCalls         int
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

func (repository *fakeRepository) CreateRiskAssessment(_ context.Context, assessment entity.RiskAssessment, _ []entity.RiskFactor, result entity.CommandResult, events []entity.OutboxEvent) error {
	repository.mutationCalls++
	repository.assessment = assessment
	repository.result = result
	repository.events = events
	return nil
}

func (repository *fakeRepository) GetRiskAssessment(_ context.Context, _ uuid.UUID) (entity.RiskAssessment, error) {
	return repository.assessment, nil
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

func (repository *fakeRepository) GetReleaseDecisionPackage(_ context.Context, _ uuid.UUID) (entity.ReleaseDecisionPackage, error) {
	return repository.releasePackage, nil
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

type authorizerFunc func(context.Context, AuthorizationRequest) error

func (fn authorizerFunc) Authorize(ctx context.Context, request AuthorizationRequest) error {
	return fn(ctx, request)
}
