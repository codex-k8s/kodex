package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governancerepo "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/repository/governance"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

func TestBacklogOperationReturnsNotImplemented(t *testing.T) {
	t.Parallel()

	service := New(&fakeRepository{ready: true})
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
	if !New(&fakeRepository{ready: true}).Ready() {
		t.Fatal("Ready() = false for ready repository, want true")
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

type fakeRepository struct {
	governancerepo.Repository
	ready      bool
	assessment entity.RiskAssessment
	result     entity.CommandResult
	events     []entity.OutboxEvent
}

func (repository *fakeRepository) Ready() bool {
	return repository.ready
}

func (repository *fakeRepository) CreateRiskAssessment(_ context.Context, assessment entity.RiskAssessment, _ []entity.RiskFactor, result entity.CommandResult, events []entity.OutboxEvent) error {
	repository.assessment = assessment
	repository.result = result
	repository.events = events
	return nil
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
