package resource

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
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

func TestUnboundAutomationOccurrenceGrantRotationKeepsSemanticIdentity(t *testing.T) {
	base := value.Principal{ActorID: "actor", OrganizationID: "organization", ProjectID: "project",
		Permission: "control.schedule.claim", PolicyRevision: 4, AuthorityGeneration: 7,
		CallerWorkload: "automation-scheduler", CallerSPIFFEID: "spiffe://mattercodex/scheduler",
		AuthoritySource: "AUTOMATION_OCCURRENCE", AuthorityReference: "jti-one",
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

func TestWorkloadReadinessGrantRotationKeepsSemanticIdentity(t *testing.T) {
	base := value.Principal{ActorID: "actor", OrganizationID: "organization",
		Permission: "controlplane.readiness.check", PolicyRevision: 4, AuthorityGeneration: 7,
		CallerWorkload: "control-api-gateway", CallerSPIFFEID: "spiffe://mattercodex/control-api-gateway",
		AuthoritySource: "WORKLOAD_READINESS", AuthorityReference: "jti-one",
		AuthorityRevision: 11, AuthorityDigest: hashString("grant-one"), AuthorityGrantGeneration: 11}
	rotated := base
	rotated.PolicyRevision, rotated.AuthorityGeneration = 5, 8
	rotated.AuthorityReference, rotated.AuthorityRevision, rotated.AuthorityDigest =
		"jti-two", 12, hashString("grant-two")
	rotated.AuthorityGrantGeneration = 12
	if !reflect.DeepEqual(identity(base), identity(rotated)) {
		t.Fatal("short-lived readiness grant rotation changed semantic intent")
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

func TestAutomationSchedulerTreatsEmptyProjectCatalogAsNoWork(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	for resourceID, current := range fixture.tx.resources {
		if current.Kind == enum.KindProject {
			delete(fixture.tx.resources, resourceID)
		}
	}
	principal := fixture.principalFor(
		permissionClaimSchedule,
		"scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		fixture.turnID,
		1,
		fixture.inputSHA256,
		fixture.grant,
	)
	due, err := fixture.service.ClaimDueSchedules(
		context.Background(),
		ClaimDueSchedulesInput{
			Principal: principal, IdempotencyKey: "empty-project-catalog-due", Limit: 1,
		},
	)
	if err != nil || len(due.Occurrences) != 0 {
		t.Fatalf("empty project catalog due result = %+v, err=%v", due, err)
	}
	_, err = fixture.service.ClaimScheduleOccurrence(
		context.Background(),
		ClaimScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "empty-project-catalog-claim",
		},
	)
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("empty project catalog claim error = %v, want ErrNotFound", err)
	}
}

func TestInteractionGatewayReadinessAuthorityRotatesServerOwnedProjectPartitions(t *testing.T) {
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
	principal := value.Principal{
		ActorID: fixture.actor, OrganizationID: fixture.organization,
		Permission: permissionDeliverInteraction, PolicyRevision: 1, AuthorityGeneration: 1,
		CallerWorkload:  fixture.service.ownerGateDeliveryWorkload,
		CallerSPIFFEID:  fixture.service.ownerGateDeliverySPIFFEID,
		AuthoritySource: interactionGatewayAuthoritySource,
	}
	first, err := fixture.service.selectInteractionGatewayProject(context.Background(), principal,
		"DELIVERY_CLAIM", "interaction-first")
	if err != nil {
		t.Fatalf("select first interaction project: %v", err)
	}
	replayed, err := fixture.service.selectInteractionGatewayProject(context.Background(), principal,
		"DELIVERY_CLAIM", "interaction-first")
	if err != nil || replayed.ProjectID != first.ProjectID {
		t.Fatalf("interaction project replay changed: %q -> %q, err=%v", first.ProjectID, replayed.ProjectID, err)
	}
	second, err := fixture.service.selectInteractionGatewayProject(context.Background(), principal,
		"DELIVERY_CLAIM", "interaction-second")
	if err != nil || second.ProjectID == first.ProjectID {
		t.Fatalf("interaction partition did not rotate: %q -> %q, err=%v", first.ProjectID, second.ProjectID, err)
	}
	principal.ProjectID = first.ProjectID
	if _, err := fixture.service.selectInteractionGatewayProject(context.Background(), principal,
		"DELIVERY_CLAIM", "caller-project"); !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("caller-owned interaction project was accepted: %v", err)
	}
}

func TestScheduledCapabilitiesAreExactOneTimeOwnerAuthority(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	var materialize, completeFound bool
	for _, capability := range fixture.tx.capabilities {
		if capability.OccurrenceID != produced.claim.Occurrence.ID ||
			capability.ProjectID != fixture.project || capability.Attempt != produced.claim.Occurrence.Attempt ||
			capability.WorkloadID != "scheduler" ||
			capability.CallerSPIFFEID != "spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler" {
			continue
		}
		switch capability.FullMethod {
		case materializeScheduleOccurrenceMethod:
			materialize = capability.State == "CONSUMED" && !capability.ConsumedAt.IsZero()
		case completeScheduleOccurrenceMethod:
			completeFound = capability.State == "ISSUED" &&
				capability.TokenSHA256 == hashString(produced.claim.CompletionCapability)
		}
	}
	if !materialize || !completeFound {
		t.Fatalf("exact materialize/complete capability lifecycle is incomplete: %+v", fixture.tx.capabilities)
	}
	principal := fixture.principalFor(permissionUseScheduleCapability, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		produced.claim.Occurrence.ID, uint64(produced.claim.Occurrence.Attempt),
		produced.claim.Occurrence.EffectiveInputSHA256, fixture.grant)
	_, err := fixture.service.MaterializeScheduleOccurrence(context.Background(),
		MaterializeScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "cross-occurrence-capability",
			OccurrenceID: uuid.NewString(), ProjectID: fixture.project,
			ExpectedAttempt:           produced.claim.Occurrence.Attempt,
			MaterializationCapability: strings.Repeat("a", 64),
		})
	if !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrPermissionDenied) {
		t.Fatalf("cross-occurrence capability was not rejected closed: %v", err)
	}
}

func TestScheduleCompletionReplaySurvivesActualBearerRotation(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	fixture.failScheduledTurn(t, produced, "completion-rotation")
	principal := fixture.principalFor(permissionUseScheduleCapability, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		produced.claim.Occurrence.ID, uint64(produced.claim.Occurrence.Attempt),
		produced.claim.Occurrence.EffectiveInputSHA256, fixture.grant)
	first, err := fixture.service.CompleteScheduleOccurrence(context.Background(),
		CompleteScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "complete-after-rotated-bearer",
			OccurrenceID: produced.claim.Occurrence.ID, ProjectID: fixture.project,
			ExpectedAttempt:      produced.claim.Occurrence.Attempt,
			CompletionCapability: produced.claim.CompletionCapability,
		})
	if err != nil {
		t.Fatalf("complete before bearer rotation: %v", err)
	}
	principal.CorrelationID, principal.AuthorityReference = uuid.NewString(), uuid.NewString()
	principal.AuthorityRevision, principal.AuthorityDigest = 102, hashString("rotated-complete-grant")
	replayed, err := fixture.service.CompleteScheduleOccurrence(context.Background(),
		CompleteScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: "complete-after-rotated-bearer",
			OccurrenceID: produced.claim.Occurrence.ID, ProjectID: fixture.project,
			ExpectedAttempt:      produced.claim.Occurrence.Attempt,
			CompletionCapability: produced.claim.CompletionCapability,
		})
	if err != nil || replayed != first {
		t.Fatalf("completion rotation replay changed effect: %v first=%+v replay=%+v", err, first, replayed)
	}
}

func TestRecoveryBlockedOwnerRepairUsesExactReadbackProof(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	occurrence := fixture.tx.occurrences[produced.claim.Occurrence.ID]
	fixture.tx.now = occurrence.LeaseExpiresAt.Add(time.Microsecond)
	occurrence.LeaseExpiresAt = fixture.tx.now.Add(-time.Microsecond)
	fixture.tx.occurrences[occurrence.ID] = occurrence
	scheduler := fixture.principalFor(permissionClaimSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		occurrence.ID, uint64(occurrence.Attempt), occurrence.EffectiveInputSHA256, fixture.grant)
	if err := fixture.service.recordBlockedScheduleRecovery(context.Background(), domainrepo.Scope{
		OrganizationID: fixture.organization, ProjectID: fixture.project, ActorID: fixture.actor,
	}, scheduler, occurrence); err != nil {
		t.Fatalf("record RECOVERY_BLOCKED: %v", err)
	}
	blocked := fixture.tx.occurrences[occurrence.ID]
	if blocked.State != "RECOVERY_BLOCKED" || blocked.RecoveryEvidenceSHA256 == "" ||
		blocked.Version <= occurrence.Version {
		t.Fatalf("recovery incident readback is incomplete: %+v", blocked)
	}
	owner := fixture.principal(permissionRecoverSchedule,
		controlAPIGatewayWorkload, controlAPIGatewaySPIFFEID)
	wrong := blocked
	_, err := fixture.service.ResolveScheduleRecovery(context.Background(),
		ResolveScheduleRecoveryInput{
			Principal: owner, IdempotencyKey: "repair-blocked-wrong-proof",
			ScheduleID: blocked.ScheduleID, OccurrenceID: blocked.ID,
			ExpectedVersion: blocked.Version, ExpectedAttempt: blocked.Attempt,
			Action: "REPAIR", EvidenceSHA256: hashString("wrong-proof"), ReasonCode: "owner_repair",
		})
	if !errors.Is(err, errs.ErrStateConflict) || fixture.tx.occurrences[blocked.ID] != wrong {
		t.Fatalf("wrong recovery proof changed owner graph: %v", err)
	}
	repaired, err := fixture.service.ResolveScheduleRecovery(context.Background(),
		ResolveScheduleRecoveryInput{
			Principal: owner, IdempotencyKey: "repair-blocked-exact-proof",
			ScheduleID: blocked.ScheduleID, OccurrenceID: blocked.ID,
			ExpectedVersion: blocked.Version, ExpectedAttempt: blocked.Attempt,
			Action: "REPAIR", EvidenceSHA256: blocked.RecoveryEvidenceSHA256,
			ReasonCode: "owner_repair",
		})
	if err != nil || repaired.State != "CLAIMED" || repaired.RecoveryEvidenceSHA256 != "" ||
		repaired.ExecutionTurnID != produced.turn.ID || !repaired.LeaseExpiresAt.After(fixture.tx.now) ||
		repaired.AuthorityGeneration == 0 || repaired.TokenHash == "" ||
		repaired.ClaimKeySHA256 == "" {
		t.Fatalf("exact recovery repair did not restore canonical graph: %v %+v", err, repaired)
	}
	repairCapability, ok := fixture.tx.capabilities[repaired.TokenHash]
	if !ok || repairCapability.State != "ISSUED" ||
		repairCapability.FullMethod != completeScheduleOccurrenceMethod ||
		repairCapability.OccurrenceID != repaired.ID ||
		repairCapability.Attempt != repaired.Attempt ||
		repairCapability.AuthorityGeneration != repaired.AuthorityGeneration ||
		repairCapability.ImmutableInputSHA256 != produced.turn.Spec.(entity.TurnSpec).EffectiveInputSHA256 {
		t.Fatalf("recovery repair did not materialize an exact watchdog fence: %+v", repairCapability)
	}
	replayed, err := fixture.service.ResolveScheduleRecovery(context.Background(),
		ResolveScheduleRecoveryInput{
			Principal: owner, IdempotencyKey: "repair-blocked-exact-proof",
			ScheduleID: blocked.ScheduleID, OccurrenceID: blocked.ID,
			ExpectedVersion: blocked.Version, ExpectedAttempt: blocked.Attempt,
			Action: "REPAIR", EvidenceSHA256: blocked.RecoveryEvidenceSHA256,
			ReasonCode: "owner_repair",
		})
	if err != nil || replayed != repaired {
		t.Fatalf("recovery repair replay changed result: %v %+v", err, replayed)
	}
}

func TestRecoveryBlockedOwnerCancelClosesMalformedCurrentGraph(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	occurrence := fixture.tx.occurrences[produced.claim.Occurrence.ID]
	runKey := turnAttemptMapKey(occurrence.ID, occurrence.Attempt)
	run := fixture.tx.runs[runKey]
	run.CurrentTurnVersion += 7
	fixture.tx.runs[runKey] = run
	fixture.tx.now = occurrence.LeaseExpiresAt.Add(time.Microsecond)
	occurrence.LeaseExpiresAt = fixture.tx.now.Add(-time.Microsecond)
	fixture.tx.occurrences[occurrence.ID] = occurrence
	scheduler := fixture.principalFor(permissionClaimSchedule, "scheduler",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/scheduler",
		occurrence.ID, uint64(occurrence.Attempt), occurrence.EffectiveInputSHA256, fixture.grant)
	if err := fixture.service.recordBlockedScheduleRecovery(context.Background(), domainrepo.Scope{
		OrganizationID: fixture.organization, ProjectID: fixture.project, ActorID: fixture.actor,
	}, scheduler, occurrence); err != nil {
		t.Fatalf("record malformed RECOVERY_BLOCKED: %v", err)
	}
	blocked := fixture.tx.occurrences[occurrence.ID]
	resolved, err := fixture.service.ResolveScheduleRecovery(context.Background(),
		ResolveScheduleRecoveryInput{
			Principal: fixture.principal(permissionRecoverSchedule,
				controlAPIGatewayWorkload, controlAPIGatewaySPIFFEID),
			IdempotencyKey: "cancel-malformed-recovery", ScheduleID: blocked.ScheduleID,
			OccurrenceID: blocked.ID, ExpectedVersion: blocked.Version,
			ExpectedAttempt: blocked.Attempt, Action: "CANCEL",
			EvidenceSHA256: blocked.RecoveryEvidenceSHA256, ReasonCode: "owner_cancel",
		})
	if err != nil || resolved.State != "CANCELLED" ||
		fixture.tx.runs[runKey].State != "CANCELLED" ||
		fixture.tx.resources[produced.turn.ID].State != enum.StateCancelled ||
		fixture.tx.resources[produced.process.ID].State != enum.StateCancelled {
		t.Fatalf("owner recovery cancel left a partial graph: %v occurrence=%+v run=%+v turn=%+v process=%+v",
			err, resolved, fixture.tx.runs[runKey], fixture.tx.resources[produced.turn.ID],
			fixture.tx.resources[produced.process.ID])
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

func TestScheduledDeliveryRouteUsesExactRoomForEveryEligibleOutput(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	turn := fixture.tx.resources[produced.turn.ID]
	spec := turn.Spec.(entity.TurnSpec)
	spec.Outcome = "action_taken"
	route, eligible, err := scheduledDeliveryRoute(context.Background(), fixture.tx, turn, spec,
		produced.claim.Occurrence.ID)
	if err != nil || !eligible || route.RoomID != fixture.chatID ||
		route.NotificationPolicy != "ON_ACTION_OR_FAILURE" {
		t.Fatalf("eligible scheduled route is not exact: eligible=%t err=%v route=%+v",
			eligible, err, route)
	}
	spec.Outcome = "no_action"
	if _, eligible, err = scheduledDeliveryRoute(context.Background(), fixture.tx, turn, spec,
		produced.claim.Occurrence.ID); err != nil || eligible {
		t.Fatalf("no_action unexpectedly created a delivery route: eligible=%t err=%v", eligible, err)
	}
}

func TestSecondaryRuntimeOutputsInheritSingleScheduledRoute(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	produced := fixture.produceScheduledGraph(t)
	turn := produced.turn
	turn.State = enum.StateSucceeded
	spec := turn.Spec.(entity.TurnSpec)
	spec.Outcome = "action_taken"
	primaryPayload := []byte("Выполнение завершено.")
	primarySHA256 := hashString(string(primaryPayload))
	primaryID := uuid.NewString()
	spec.ResultArtifactID, spec.ResultArtifactVersion, spec.ResultArtifactSHA256 =
		primaryID, 1, primarySHA256
	turn.Spec = spec

	fileID, imageID := uuid.NewString(), uuid.NewString()
	fileSHA256, imageSHA256 := hashString("file-output"), hashString("image-output")
	for _, artifact := range []struct {
		id, name, kind, storage, mediaType, digest string
	}{
		{fileID, "report.txt", "runtime-output-file", "s3://runtime/report.txt", "text/plain", fileSHA256},
		{imageID, "chart.png", "runtime-output-image", "s3://runtime/chart.png", "image/png", imageSHA256},
	} {
		fixture.tx.resources[artifact.id] = entity.Resource{
			ID: artifact.id, OrganizationID: fixture.organization, ProjectID: fixture.project,
			ParentID: turn.ID, OwnerActorID: turn.OwnerActorID, Kind: enum.KindArtifact,
			Name: artifact.name, State: enum.StateActive, Version: 1,
			Spec: entity.ArtifactSpec{ArtifactKind: artifact.kind, Direction: "OUTPUT",
				StorageRef: artifact.storage, SizeBytes: 11, MediaType: artifact.mediaType,
				SHA256: artifact.digest, ScanStatus: "CLEAN"},
		}
	}
	outputs := []RuntimeOutput{
		{Kind: "FINAL_MARKDOWN", ArtifactID: primaryID, ArtifactVersion: 1,
			ArtifactSHA256: primarySHA256, ArtifactName: "result.md",
			ArtifactMediaType: "text/markdown", ArtifactPayload: primaryPayload,
			ArtifactSizeBytes: uint64(len(primaryPayload)), Sequence: 1, Total: 1},
		{Kind: "FILE", ArtifactID: fileID, ArtifactVersion: 1, ArtifactSHA256: fileSHA256,
			ArtifactName: "report.txt", ArtifactStorageRef: "s3://runtime/report.txt",
			ArtifactSizeBytes: 11, ArtifactMediaType: "text/plain", Sequence: 1, Total: 1},
		{Kind: "IMAGE", ArtifactID: imageID, ArtifactVersion: 1, ArtifactSHA256: imageSHA256,
			ArtifactName: "chart.png", ArtifactStorageRef: "s3://runtime/chart.png",
			ArtifactSizeBytes: 11, ArtifactMediaType: "image/png", Sequence: 1, Total: 1},
	}
	execution := RuntimeExecution{
		ScheduleOccurrenceID: produced.claim.Occurrence.ID, TurnID: turn.ID,
		RuntimeRevisionID: produced.runtime.ID, RuntimeRevisionVersion: produced.runtime.Version,
		ImmutableInputSHA256: spec.EffectiveInputSHA256,
	}
	principal := fixture.principal(
		permissionRuntimeComplete, fixture.runtimeWorker, fixture.runtimeSPIFFE,
	)
	if err := fixture.service.materializeRuntimeOutputs(context.Background(), fixture.tx,
		principal, execution, fixture.tx.resources[fixture.sessionID], turn, spec, outputs,
		fixture.tx.now); err != nil {
		t.Fatalf("materialize eligible scheduled outputs: %v", err)
	}
	if len(fixture.tx.deliveries) != 6 {
		t.Fatalf("expected primary and secondary delivery work, got %d", len(fixture.tx.deliveries))
	}
	for _, work := range fixture.tx.deliveries {
		if work.NotificationRoomID != fixture.chatID ||
			work.NotificationPolicy != "ON_ACTION_OR_FAILURE" ||
			work.ScheduledOutcome != "action_taken" {
			t.Fatalf("scheduled output escaped exact route: %+v", work)
		}
	}
	beforeSuppressed := len(fixture.tx.deliveries)
	spec.Outcome = "no_action"
	if err := fixture.service.materializeRuntimeOutputs(context.Background(), fixture.tx,
		principal, execution, fixture.tx.resources[fixture.sessionID], turn, spec, outputs,
		fixture.tx.now); err != nil {
		t.Fatalf("suppress no_action scheduled outputs: %v", err)
	}
	if len(fixture.tx.deliveries) != beforeSuppressed {
		t.Fatal("no_action created primary or secondary delivery work")
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
