package resource

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

func TestFirstScheduleRunUsesOwnerAnchorAndTimezone(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 34, 56, 789000000, time.UTC)
	interval, err := firstScheduleRun(entity.ScheduleSpec{Interval: 90 * time.Minute}, now)
	if err != nil || !interval.Equal(time.Date(2026, 8, 5, 14, 4, 56, 789000000, time.UTC)) {
		t.Fatalf("interval first run = %s, err=%v", interval, err)
	}
	cronRun, err := firstScheduleRun(entity.ScheduleSpec{
		Cron: "0 9 * * *", Timezone: "America/New_York",
	}, now)
	if err != nil || !cronRun.Equal(time.Date(2026, 8, 5, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("timezone cron first run = %s, err=%v", cronRun, err)
	}
}

func TestUnboundApplicationGrantRotationKeepsSemanticIdentity(t *testing.T) {
	base := value.Principal{ActorID: "actor", OrganizationID: "organization", ProjectID: "project",
		Permission: "control.schedule.claim", PolicyRevision: 4, AuthorityGeneration: 7,
		CallerWorkload: "automation-scheduler", CallerSPIFFEID: "spiffe://mattercodex/scheduler",
		AuthoritySource: "APPLICATION_GRANT", AuthorityReference: "jti-one",
		AuthorityRevision: 11, AuthorityDigest: hashString("grant-one")}
	rotated := base
	rotated.AuthorityReference, rotated.AuthorityRevision, rotated.AuthorityDigest =
		"jti-two", 12, hashString("grant-two")
	if !reflect.DeepEqual(identity(base), identity(rotated)) {
		t.Fatal("short-lived application grant rotation changed semantic intent")
	}
	rotated.AuthorityGrantGeneration = 1
	if reflect.DeepEqual(identity(base), identity(rotated)) {
		t.Fatal("bound execution grant generation was removed from semantic identity")
	}
}

func TestAutomationAuthorityRotatesServerOwnedProjectPartitions(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	secondProjectID := uuid.NewString()
	secondProject, err := entity.New(secondProjectID, fixture.organization, secondProjectID,
		"", fixture.actor, enum.KindProject, "Second project", entity.ProjectSpec{
			Slug: "second-project", Locale: "ru",
			Ownership: entity.ConfigurationOwnership{ManagedBy: "UI"},
		}, fixture.tx.now)
	if err != nil {
		t.Fatalf("create second project: %v", err)
	}
	fixture.tx.resources[secondProject.ID] = secondProject
	principal := fixture.principalFor(permissionClaimSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		fixture.turnID, 1, fixture.inputSHA256, fixture.grant)
	requestHash, err := semanticCommandHash(principal, struct{ Operation string }{"DUE"})
	if err != nil {
		t.Fatalf("hash automation partition intent: %v", err)
	}
	first, err := fixture.service.selectAutomationProject(context.Background(), principal,
		"DUE", "multi-project-first", requestHash)
	if err != nil {
		t.Fatalf("select first project: %v", err)
	}
	replayed, err := fixture.service.selectAutomationProject(context.Background(), principal,
		"DUE", "multi-project-first", requestHash)
	if err != nil || replayed != first {
		t.Fatalf("project partition replay changed: %q -> %q, err=%v", first, replayed, err)
	}
	second, err := fixture.service.selectAutomationProject(context.Background(), principal,
		"DUE", "multi-project-second", requestHash)
	if err != nil || second == first {
		t.Fatalf("server-owned partition did not reach another project: %q -> %q, err=%v",
			first, second, err)
	}
	principal.ProjectID = first
	_, err = fixture.service.ClaimDueSchedules(context.Background(), ClaimDueSchedulesInput{
		Principal: principal, IdempotencyKey: "caller-owned-project", Limit: 1,
	})
	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("caller-owned scheduler project was accepted: %v", err)
	}
}

func TestRunScheduleNowKeepsPlannedWatermark(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	spec := produced.schedule.Spec.(entity.ScheduleSpec)
	spec.OverlapPolicy = "FORBID"
	principal := fixture.principal(
		permissionManageSchedule, controlAPIGatewayWorkload, controlAPIGatewaySPIFFEID,
	)
	schedule, err := fixture.service.ManageSchedule(context.Background(), ManageScheduleInput{
		Principal: principal, IdempotencyKey: "create-manual-run-schedule", Action: "CREATE",
		Name: "Manual run schedule", Spec: spec,
	})
	if err != nil {
		t.Fatalf("create manual schedule: %v", err)
	}
	watermark := schedule.Spec.(entity.ScheduleSpec).NextRunAt
	fixture.tx.now = fixture.tx.now.Add(17 * time.Minute)
	manual, err := fixture.service.RunScheduleNow(context.Background(), RunScheduleNowInput{
		Principal: principal, IdempotencyKey: "manual-occurrence-one",
		ScheduleID: schedule.ID, ExpectedVersion: schedule.Version,
	})
	if err != nil || manual.ScheduleID != schedule.ID || !manual.ScheduledFor.Equal(fixture.tx.now) {
		t.Fatalf("manual occurrence: %v %+v", err, manual)
	}
	current := fixture.tx.resources[schedule.ID]
	if current.Version != schedule.Version ||
		!current.Spec.(entity.ScheduleSpec).NextRunAt.Equal(watermark) {
		t.Fatalf("manual run moved planned watermark: before=%s after=%+v", watermark, current)
	}
	replayed, err := fixture.service.RunScheduleNow(context.Background(), RunScheduleNowInput{
		Principal: principal, IdempotencyKey: "manual-occurrence-one",
		ScheduleID: schedule.ID, ExpectedVersion: schedule.Version,
	})
	if err != nil || replayed.ID != manual.ID || len(fixture.tx.occurrences) != 2 {
		t.Fatalf("manual occurrence replay repeated effect: %v %+v", err, replayed)
	}
}

func TestScheduledDeliveryEligibilityIsClosed(t *testing.T) {
	cases := []struct {
		policy, outcome string
		want            bool
	}{
		{"ALWAYS", "action_taken", true},
		{"ALWAYS", "no_action", false},
		{"ON_ACTION", "action_taken", true},
		{"ON_ACTION", "failed", false},
		{"ON_FAILURE", "failed", true},
		{"ON_ACTION_OR_FAILURE", "action_taken", true},
		{"ON_ACTION_OR_FAILURE", "failed", true},
		{"AUDIT_ONLY", "action_taken", false},
		{"ALWAYS", "requires_human", false},
	}
	for _, test := range cases {
		got, err := scheduledDeliveryEligible(test.policy, test.outcome)
		if err != nil || got != test.want {
			t.Fatalf("eligibility %s/%s = %v, err=%v", test.policy, test.outcome, got, err)
		}
	}
	if _, err := scheduledDeliveryEligible("ALWAYS", "free-form"); err == nil {
		t.Fatal("free-form scheduled outcome was accepted")
	}
	terminalCases := []struct {
		state, outcome, want string
	}{
		{"SUCCEEDED", "no_action", "no_action"},
		{"SUCCEEDED", "action_taken", "action_taken"},
		{"SUCCEEDED", "owner_gate_approved", "action_taken"},
		{"FAILED", "runtime_error", "failed"},
		{"CANCELLED", "owner_cancelled", "failed"},
		{"EXPIRED", "watchdog", "failed"},
	}
	for _, test := range terminalCases {
		got, err := closedScheduledTerminalOutcome(test.state, test.outcome)
		if err != nil || got != test.want {
			t.Fatalf("closed terminal outcome %s/%s = %q, err=%v",
				test.state, test.outcome, got, err)
		}
	}
	if _, err := closedScheduledTerminalOutcome("SUCCEEDED", "free-form"); err == nil {
		t.Fatal("free-form successful scheduled outcome was accepted")
	}
}

func TestScheduledContinuationKeepsOccurrenceLineage(t *testing.T) {
	occurrenceID := uuid.NewString()
	resolved := resolvedExecution{
		TurnSpec:    entity.TurnSpec{SourceRef: "owner-gate-continuation:" + uuid.NewString()},
		ProcessSpec: entity.ProcessRunSpec{OccurrenceID: occurrenceID},
	}
	got, err := resolvedScheduleOccurrenceID(resolved)
	if err != nil || got != occurrenceID {
		t.Fatalf("scheduled continuation lost occurrence lineage: %q, err=%v", got, err)
	}
	resolved.TurnSpec.SourceRef = "schedule-occurrence:" + uuid.NewString()
	if _, err := resolvedScheduleOccurrenceID(resolved); err == nil {
		t.Fatal("conflicting scheduled lineage was accepted")
	}
}
