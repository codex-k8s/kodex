package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

//go:embed testdata/sql/resumable_session_state.sql
var queryResumableSessionFixtureState string

//go:embed testdata/sql/resumable_agent_enabled.sql
var queryResumableAgentFixtureEnabled string

func testResumableTargetChange(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, run entity.Run) {
	t.Helper()
	if _, err := repository.pool.Exec(ctx, queryResumableAgentFixtureEnabled, run.Target.Ref, false); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := repository.pool.Exec(context.WithoutCancel(ctx), queryResumableAgentFixtureEnabled, run.Target.Ref, true); err != nil {
			t.Error(err)
		}
	}()
	testResumableSessionCatalog(t, ctx, service, owner, run, false)
	item, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil || containsString(item.NextActions, "ADD_TURN") {
		t.Fatalf("disabled target remained actionable: err=%v actions=%v", err, item.NextActions)
	}
}

func testResumableSessionPagination(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner, worker, reader value.Principal, original entity.Run) {
	t.Helper()
	other := createLifecycleAgent(t, ctx, service, owner, original.ProjectRef, "file-target-other-continuation-agent", "Other continuation target")
	if _, err := service.GetRunAttachmentEligibility(ctx, owner, original.ProjectRef, entity.RunTarget{Type: "AGENT", Ref: other.Ref}, original.Ref); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("continuation accepted substituted target: %v", err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "resumable-pagination-launch"}, Payload: command.LaunchRunInput{
			ProjectRef: original.ProjectRef, Task: "Verify immutable provider account affinity.", Target: original.Target}})
	if err != nil || created.Run == nil {
		t.Fatalf("create second resumable Session: %v", err)
	}
	closeSession := func() {
		if _, err := repository.pool.Exec(context.WithoutCancel(ctx), queryResumableSessionFixtureState, created.Run.SessionRef, "CLOSED"); err != nil {
			t.Errorf("close pagination fixture Session: %v", err)
		}
	}
	defer closeSession()
	complete := func(key string) {
		claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: key + "-claim"}, Payload: command.LeaseInput{WorkloadInstance: "resumable-pagination-worker", Limit: 1}})
		if err != nil || len(claimed.RuntimeItems) != 1 {
			t.Fatalf("claim pagination turn: %v", err)
		}
		completeClaimedExecution(t, ctx, service, worker, claimed.RuntimeItems[0], key, false)
	}
	complete("resumable-pagination-first")
	continued, err := service.Execute(ctx, command.Command{Kind: command.AddSessionTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "resumable-pagination-next"}, Payload: command.SessionTurnInput{
			SessionRef: created.Run.SessionRef, RunRef: created.Run.Ref, Task: "Verify immutable provider account affinity."}})
	if err != nil || continued.Run == nil {
		t.Fatalf("continue pagination Session: %v", err)
	}
	complete("resumable-pagination-second")
	filter := query.Filter{ProjectRef: original.ProjectRef, Query: "Verify immutable provider account affinity.", ResumableSessionsOnly: true, Page: query.Page{Size: 1}}
	filter.TargetType, filter.TargetRef = original.Target.Type, original.Target.Ref
	first, total, cursor, err := service.ListRuns(ctx, owner, filter)
	if err != nil || total != 2 || len(first) != 1 || cursor == "" {
		t.Fatalf("distinct first page: total=%d items=%d cursor=%q err=%v", total, len(first), cursor, err)
	}
	filter.Page.Token = cursor
	if _, _, _, err := service.ListRuns(ctx, reader, filter); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("different actor accepted cursor: %v", err)
	}
	second, secondTotal, next, err := service.ListRuns(ctx, owner, filter)
	if err != nil || secondTotal != 2 || len(second) != 1 || next != "" || second[0].SessionRef == first[0].SessionRef {
		t.Fatalf("distinct second page: total=%d items=%d err=%v", secondTotal, len(second), err)
	}
	for _, item := range append(first, second...) {
		if item.SessionRef == created.Run.SessionRef && item.Ref != continued.Run.Ref {
			t.Fatal("catalog selected old Run for continued Session")
		}
	}
	filter.ResumableSessionsOnly = false
	if _, _, _, err := service.ListRuns(ctx, owner, filter); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("ordinary mode accepted Session cursor: %v", err)
	}
	filter.ResumableSessionsOnly = true
	filter.Query = "other query"
	if _, _, _, err := service.ListRuns(ctx, owner, filter); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("changed query accepted cursor: %v", err)
	}
	filter.Query = "Verify immutable provider account affinity."
	filter.TargetType, filter.TargetRef = "", ""
	if _, _, _, err := service.ListRuns(ctx, owner, filter); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("Home mode accepted target-bound cursor: %v", err)
	}
	filter.TargetType, filter.TargetRef = original.Target.Type, original.Target.Ref
	closeSession()
	if _, _, _, err := service.ListRuns(ctx, owner, filter); !errors.Is(err, domainerrs.ErrVersionMismatch) {
		t.Fatalf("terminal Session did not invalidate cursor: %v", err)
	}
	filter.Page.Token = ""
	if items, total, _, err := service.ListRuns(ctx, owner, filter); err != nil || total != 1 || len(items) != 1 || items[0].SessionRef != original.SessionRef {
		t.Fatalf("fresh page retained terminal Session: total=%d err=%v", total, err)
	}
}

func testResumableSessionAuthority(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, run entity.Run) value.Principal {
	t.Helper()
	input := platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000008499", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Resumable Run reader", CallerWorkload: "control-api-gateway", Operation: "platform.query.projects.get", ProjectRef: run.ProjectRef}
	if _, err := repository.ResolveProofAuthority(ctx, input); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("unbound resumable reader: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: input.ExternalDisplayName, Page: query.Page{Size: 20}}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("resumable reader subject: %v", err)
	}
	role := createRoleImageAccessRole(t, ctx, service, owner, "resumable-reader-role", input.ExternalDisplayName, []string{"project.view", "run.view"}, []string{"PROJECT"})
	createRoleImageAccessBinding(t, ctx, service, owner, "resumable-reader-binding", subjects[0].Ref, role.CurrentVersion.Ref, entity.AccessScope{Kind: "PROJECT", ProjectRef: run.ProjectRef})
	reader := resolvedTestPrincipal(t, ctx, repository, input, "control-api-gateway")
	filter := query.Filter{ProjectRef: run.ProjectRef, Query: "Verify immutable provider account affinity.", Page: query.Page{Size: 10}}
	if items, total, _, err := service.ListRuns(ctx, reader, filter); err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("reader cannot see ordinary run: total=%d err=%v", total, err)
	}
	filter.ResumableSessionsOnly = true
	if items, total, _, err := service.ListRuns(ctx, reader, filter); err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("run.view granted continuation: total=%d err=%v", total, err)
	}
	if item, err := service.GetRun(ctx, reader, run.Ref); err == nil && containsString(item.NextActions, "ADD_TURN") {
		t.Fatal("single Run granted continuation without target launch")
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.AddSessionTurn, Principal: reader,
		Mutation: value.Mutation{IdempotencyKey: "resumable-reader-denied-turn"},
		Payload:  command.SessionTurnInput{RunRef: run.Ref, SessionRef: run.SessionRef, Task: "Must not launch without target authority."}})
	if !errors.Is(err, domainerrs.ErrNotFound) && !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("run.view launched nested turn: %v", err)
	}
	return reader
}

func testResumableSessionCatalog(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, run entity.Run, available bool) {
	t.Helper()
	filter := query.Filter{ProjectRef: run.ProjectRef, Query: "Verify immutable provider account affinity.", ResumableSessionsOnly: true, Page: query.Page{Size: 1}}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if items, total, next, err := service.ListRuns(cancelled, owner, filter); err == nil || len(items) != 0 || total != 0 || next != "" {
		t.Fatalf("cancelled catalog returned partial success: total=%d err=%v", total, err)
	}
	items, total, next, err := service.ListRuns(ctx, owner, filter)
	if err != nil {
		t.Fatalf("read resumable session catalog: %v", err)
	}
	if available {
		if total != 1 || len(items) != 1 || items[0].SessionRef != run.SessionRef || len(items[0].NextActions) != 2 || items[0].NextActions[1] != "ADD_TURN" || next != "" {
			t.Fatalf("eligible Session missing or incorrectly counted: total=%d items=%#v next=%q", total, items, next)
		}
		item, err := service.GetRun(ctx, owner, run.Ref)
		if err != nil || !containsString(item.NextActions, "ADD_TURN") {
			t.Fatalf("single Run disagrees with eligible catalog: %v", err)
		}
	} else if total != 0 || len(items) != 0 || next != "" {
		t.Fatalf("ineligible Session leaked: total=%d items=%#v", total, items)
	}
	attachment, err := service.GetRunAttachmentEligibility(ctx, owner, run.ProjectRef, run.Target, run.Ref)
	if err != nil {
		t.Fatalf("continuation attachment projection: %v", err)
	}
	if attachment.RunRef != run.Ref || attachment.RunVersion == 0 || attachment.Digest == "" {
		t.Fatalf("continuation lost exact Run metadata: %+v", attachment)
	}
	if available && attachment.Reason != fileTargetAvailable && attachment.Reason != fileTargetCapabilityRequired {
		t.Fatalf("eligible Session was replaced with current catalog: %+v", attachment)
	}
	if !available && (attachment.Eligible || attachment.Reason != runAttachmentSessionUnavailable) {
		t.Fatalf("ineligible Session allowed attachments: %+v", attachment)
	}
	events, _, _, err := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: run.Ref, Limit: 500})
	if err != nil || len(events) == 0 {
		t.Fatalf("read continuation event projection: %v", err)
	}
	for _, event := range events {
		if event.Delta.Run != nil && containsString(event.Delta.Run.NextActions, "ADD_TURN") != (available && event.Delta.Run.State == "SUCCEEDED") {
			t.Fatalf("event disagrees with current continuation eligibility: state=%s available=%v actions=%v", event.Delta.Run.State, available, event.Delta.Run.NextActions)
		}
	}
	filter.States = []string{"SUCCEEDED"}
	if _, _, _, err := service.ListRuns(ctx, owner, filter); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("resumable mode accepted states: %v", err)
	}
	filter.States = nil
	for _, target := range []entity.RunTarget{{Type: "AGENT"}, {Ref: run.Target.Ref}, {Type: "SYSTEM_ASSISTANT", Ref: run.Target.Ref}} {
		invalid := filter
		invalid.TargetType, invalid.TargetRef = target.Type, target.Ref
		if _, _, _, err := service.ListRuns(ctx, owner, invalid); !errors.Is(err, domainerrs.ErrInvalid) {
			t.Fatalf("invalid target filter accepted: %v", err)
		}
	}
	targetFilter := filter
	targetFilter.TargetType, targetFilter.TargetRef = run.Target.Type, run.Target.Ref
	targetFilter.ProjectRef = "prj_hidden_resumable"
	if _, _, _, err := service.ListRuns(ctx, owner, targetFilter); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("foreign target project accepted: %v", err)
	}
	targetFilter.ProjectRef = run.ProjectRef
	targetFilter.TargetRef = "agt_hidden_resumable"
	if _, _, _, err := service.ListRuns(ctx, owner, targetFilter); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("unknown target accepted: %v", err)
	}
	targetFilter.TargetRef = run.Target.Ref
	targetFilter.ResumableSessionsOnly = false
	if _, _, _, err := service.ListRuns(ctx, owner, targetFilter); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("ordinary mode accepted target filter: %v", err)
	}
	filter.ProjectRef = "prj_hidden_resumable"
	if items, total, _, err := service.ListRuns(ctx, owner, filter); err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("foreign project leaked: total=%d err=%v", total, err)
	}
	filter.ProjectRef = run.ProjectRef
	filter.ResumableSessionsOnly = false
	if items, total, _, err := service.ListRuns(ctx, owner, filter); err != nil || total < 1 || len(items) != 1 {
		t.Fatalf("ordinary Run catalog changed: total=%d err=%v", total, err)
	}
}
