package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testScheduleMaterializedPreview(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner value.Principal, schedule *entity.Schedule, continued bool) {
	t.Helper()
	current, err := service.GetSchedule(ctx, owner, schedule.Ref)
	if err != nil {
		t.Fatal(err)
	}
	input := command.ScheduleInput{ProjectRef: current.ProjectRef, Name: current.Name, Target: current.Target,
		Preset: current.Preset, CronExpression: current.CronExpression, Timezone: current.Timezone, TimeOfDay: current.TimeOfDay, DayOfWeek: current.DayOfWeek,
		DSTGapPolicy: current.DSTGapPolicy, DSTFoldPolicy: current.DSTFoldPolicy, MisfirePolicy: current.MisfirePolicy, OverlapPolicy: current.OverlapPolicy,
		Input: current.Input, PromptInputs: current.PromptInputs, AutomationText: current.AutomationText, SessionPolicy: current.SessionPolicy, NotificationPolicy: current.NotificationPolicy}
	principal := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.query.schedules.preview"}, "control-api-gateway")
	when := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	if !continued && current.Target.Type == "AGENT" {
		testSchedulePreviewAuthority(t, ctx, r, service, owner, input, current)
	}
	preview, pin, variables, err := service.PreviewScheduleMaterialization(ctx, principal, input, current.Ref, current.Version, when, "", false, "CURRENT_REVISION")
	if err != nil || !preview.Complete || !pin.RevisionAvailable || pin.RevisionRef != current.CurrentRevision.Ref || pin.Continuation != continued || len(variables) != 6 || preview.ContextPin.Digest == "" {
		t.Fatalf("saved Automation preview: complete=%v pin=%+v variables=%d err=%v", preview.Complete, pin, len(variables), err)
	}
	for _, v := range variables {
		if !v.Available {
			t.Fatalf("saved variable unavailable: %s", v.Name)
		}
	}
	if continued && preview.RuntimeDiff == nil {
		t.Fatal("continuation preview lost actual runtime diff")
	}
	if continued && current.Target.Type == "AGENT" {
		viewer, viewerRef := previewAuthorityActor(t, ctx, r, service, owner, "automation-bound-viewer", []string{"project.view", "agent.view", "run.view", "schedule.manage"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: input.ProjectRef})
		view, viewPin, _, err := service.PreviewScheduleMaterialization(ctx, viewer, input, current.Ref, current.Version, when, "", false, "CURRENT_REVISION")
		if err != nil || !view.Complete || view.RuntimeDiff == nil || viewPin.ExecutionActorRef == viewerRef || viewPin.ExecutionActorRef != pin.ExecutionActorRef || viewPin.SessionRef != pin.SessionRef {
			t.Fatalf("bound Session preview substituted viewer actor: %+v %v", viewPin, err)
		}
	}
	draft, draftPin, _, err := service.PreviewScheduleMaterialization(ctx, principal, input, current.Ref, current.Version, when, "", false, "")
	if err != nil || !draft.Complete || draftPin.Mode != "DRAFT" || draftPin.RevisionAvailable || draftPin.BaseRevisionRef != current.CurrentRevision.Ref || draftPin.ExecutionActorRef != pin.ExecutionActorRef || draft.ContextPin.Digest == preview.ContextPin.Digest {
		t.Fatalf("identical draft fabricated current revision: %+v %v", draftPin, err)
	}
	fresh := principal
	fresh.CredentialAuthenticatedAt = time.Now().UTC()
	fresh.CredentialACR = "urn:kodex:acr:interactive"
	fresh.CredentialAMR = []string{"pwd"}
	full, _, _, err := service.PreviewScheduleMaterialization(ctx, fresh, input, current.Ref, current.Version, when, "", true, "CURRENT_REVISION")
	if err != nil || !full.Complete || !strings.Contains(full.Prompt, input.AutomationText) {
		t.Fatalf("full materialized Automation: complete=%v err=%v", full.Complete, err)
	}
	if _, _, _, err := service.PreviewScheduleMaterialization(ctx, principal, input, current.Ref, current.Version, when, strings.Repeat("f", 64), false, "CURRENT_REVISION"); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale preview digest: %v", err)
	}
	if _, _, _, err := service.PreviewScheduleMaterialization(ctx, principal, input, current.Ref, current.Version+1, when, "", false, "CURRENT_REVISION"); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale Schedule version: %v", err)
	}
	input.AutomationText += " Changed draft."
	if _, _, _, err := service.PreviewScheduleMaterialization(ctx, principal, input, current.Ref, current.Version, when, "", false, "CURRENT_REVISION"); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("current revision accepted changed specification: %v", err)
	}
	changed, changedPin, changedVars, err := service.PreviewScheduleMaterialization(ctx, principal, input, current.Ref, current.Version, when, "", false, "DRAFT")
	if err != nil || !changed.Complete || changedPin.RevisionAvailable || changedPin.RevisionRef != "" || changed.ContextPin.Digest == preview.ContextPin.Digest {
		t.Fatalf("changed draft preview: %+v %v", changedPin, err)
	}
	for _, v := range changedVars {
		if v.Name == "automation.revision" && (v.Available || v.Reason != "REVISION_NOT_SAVED") {
			t.Fatal("draft fabricated immutable revision")
		}
	}
	for _, policy := range []string{"NEW_EACH_RUN", "CONTINUE_ONE"} {
		input.SessionPolicy = policy
		first, firstPin, _, err := service.PreviewScheduleMaterialization(ctx, principal, input, "", 0, when, "", false, "DRAFT")
		if err != nil || !first.Complete || firstPin.ScheduleRef != "" || firstPin.RevisionAvailable || firstPin.Continuation {
			t.Fatalf("new Automation preview %s: %+v %v", policy, firstPin, err)
		}
	}
	readback, err := service.GetSchedule(ctx, owner, current.Ref)
	if err != nil || readback.Version != current.Version || readback.CurrentRevision.Ref != current.CurrentRevision.Ref || readback.ContinueSessionRef != current.ContinueSessionRef {
		t.Fatal("preview changed Schedule state")
	}
}
