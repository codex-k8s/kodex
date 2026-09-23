package platform

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProjectTrashBoundaryComponent(t *testing.T) {
	dsn := os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("KODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("open disposable PostgreSQL")
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "retired-bootstrap-model", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1,
		PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute,
		MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles",
		DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64),
		LeaseSigningKey:       []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID:  "20000000-0000-4000-8000-000000000001",
		ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload:   "control-api-gateway", Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "trash-boundary-project"},
		Payload:  command.ProjectInput{Name: "Trash boundary", Language: "en"},
	})
	if err != nil || created.Project == nil {
		t.Fatalf("create project: %v", err)
	}
	projectRef := created.Project.Ref
	var projectID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM control_plane.projects WHERE ref=$1`, projectRef).Scan(&projectID); err != nil {
		t.Fatalf("read project authority id: %v", err)
	}
	scopedOwner := owner
	scopedOwner.ProjectRef = projectID
	wrongScope := owner
	wrongScope.ProjectRef = "90000000-0000-4000-8000-000000000097"
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.TrashProject, Principal: wrongScope,
		Mutation: value.Mutation{IdempotencyKey: "trash-boundary-wrong-scope", ExpectedVersion: &created.Project.Version},
		Payload: command.ProjectLifecycleInput{Ref: projectRef},
	}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign project proof accepted for trash: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, projectRef, "trash-boundary-agent", "Trash boundary agent")
	schedule, err := service.Execute(ctx, command.Command{
		Kind: command.CreateSchedule, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "trash-boundary-schedule"},
		Payload: command.ScheduleInput{ProjectRef: projectRef, Name: "Trash boundary schedule",
			Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Preset: "CUSTOM",
			CronExpression: "0 * * * *", Timezone: "UTC", Input: map[string]any{},
			AutomationText: "Prepare a synthetic report.", SessionPolicy: "NEW_EACH_RUN",
			NotificationPolicy: "CONTROL_CENTER_ONLY"},
	})
	if err != nil || schedule.Schedule == nil {
		t.Fatalf("create schedule: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.schedules SET next_run_at=next_run_at-interval '7 days' WHERE ref=$1`, schedule.Schedule.Ref); err != nil {
		t.Fatalf("make schedule due: %v", err)
	}
	scheduler := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "automation-scheduler", Operation: "platform.runtime.schedules.claim",
	}, "automation-scheduler")
	claims, err := service.ClaimDueSchedules(ctx, scheduler, "project-trash-boundary", 1)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim schedule before trash: count=%d err=%v", len(claims), err)
	}
	assertProjectLockFence(t, ctx, pool, owner.AuthorityTenant, projectRef)
	if _, err := pool.Exec(ctx, `UPDATE control_plane.projects SET lifecycle='TRASHED' WHERE ref=$1`, projectRef); err == nil {
		t.Fatal("trash without deletion metadata was accepted")
	}
	trashed, err := service.Execute(ctx, command.Command{
		Kind: command.TrashProject, Principal: scopedOwner,
		Mutation: value.Mutation{IdempotencyKey: "trash-boundary-delete", ExpectedVersion: &created.Project.Version},
		Payload:  command.ProjectLifecycleInput{Ref: projectRef},
	})
	if err != nil || trashed.Project == nil || trashed.Project.Lifecycle != "TRASHED" ||
		trashed.Project.DeletedAt == nil || trashed.Project.PurgeAfter == nil ||
		trashed.Project.PurgeAfter.Sub(*trashed.Project.DeletedAt) != 30*24*time.Hour {
		t.Fatalf("trash project: project=%#v err=%v", trashed.Project, err)
	}
	var occurrenceState, attemptState string
	var leaseCleared bool
	if err := pool.QueryRow(ctx, `SELECT occurrence.state, attempt.state, occurrence.lease_ref IS NULL
		FROM control_plane.schedule_occurrences occurrence
		JOIN control_plane.schedule_occurrence_attempts attempt ON attempt.occurrence_id=occurrence.id
		WHERE occurrence.ref=$1`, stringMap(claims[0], "occurrenceRef")).Scan(&occurrenceState, &attemptState, &leaseCleared); err != nil ||
		occurrenceState != "CANCELLED" || attemptState != "CANCELLED" || !leaseCleared {
		t.Fatalf("claimed schedule survived project trash: occurrence=%s attempt=%s lease_cleared=%t err=%v", occurrenceState, attemptState, leaseCleared, err)
	}
	if claims, err := service.ClaimDueSchedules(ctx, scheduler, "project-trash-after", 1); err != nil || len(claims) != 0 {
		t.Fatalf("trashed project schedule was claimed: count=%d err=%v", len(claims), err)
	}
	trashPage, next, err := service.ListTrashedProjects(ctx, owner, query.Page{Size: 1})
	if err != nil || len(trashPage) != 1 || next != "" || trashPage[0].Ref != projectRef ||
		trashPage[0].DeletedAt == nil || trashPage[0].PurgeAfter == nil || trashPage[0].AgentCount != 1 {
		t.Fatalf("trash catalog readback: items=%#v next=%q err=%v", trashPage, next, err)
	}
	if _, _, err := service.ListTrashedProjects(ctx, owner, query.Page{Token: "v1.invalid"}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("malformed trash cursor was accepted: %v", err)
	}
	assertProjectTrashHidden(t, ctx, service, owner, projectRef, agent.Ref)
	var catalogTargets int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.catalog_access_targets WHERE ref=$1`, agent.Ref).Scan(&catalogTargets); err != nil || catalogTargets != 0 {
		t.Fatalf("trashed agent remains a catalog target: count=%d err=%v", catalogTargets, err)
	}
	_, err = service.Execute(ctx, command.Command{
		Kind: command.UpdateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "trash-boundary-update", ExpectedVersion: &created.Project.Version},
		Payload:  command.ProjectInput{Ref: projectRef, Name: "Changed after trash", Language: "en"},
	})
	if err == nil {
		t.Fatal("trashed project accepted ordinary update")
	}
	_, err = service.Execute(ctx, command.Command{
		Kind: command.UpdateAgent, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "trash-boundary-agent-update", ExpectedVersion: &agent.Version},
		Payload:  command.AgentInput{Ref: agent.Ref, ProjectRef: projectRef, Name: "Changed agent"},
	})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("trashed agent command target remains accessible: %v", err)
	}
	restored, err := service.Execute(ctx, command.Command{
		Kind: command.RestoreProject, Principal: scopedOwner,
		Mutation: value.Mutation{IdempotencyKey: "trash-boundary-restore", ExpectedVersion: &trashed.Project.Version},
		Payload:  command.ProjectLifecycleInput{Ref: projectRef},
	})
	if err != nil || restored.Project == nil || restored.Project.Lifecycle != "ACTIVE" ||
		restored.Project.DeletedAt != nil || restored.Project.PurgeAfter != nil {
		t.Fatalf("restore project: project=%#v err=%v", restored.Project, err)
	}
	if _, err := service.GetProject(ctx, owner, projectRef); err != nil {
		t.Fatalf("restored project is hidden: %v", err)
	}
	if _, err := service.GetAgent(ctx, owner, agent.Ref); err != nil {
		t.Fatalf("restored agent is hidden: %v", err)
	}
	if trashPage, _, err := service.ListTrashedProjects(ctx, owner, query.Page{}); err != nil || len(trashPage) != 0 {
		t.Fatalf("restored project remains in trash: count=%d err=%v", len(trashPage), err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.catalog_access_targets WHERE ref=$1`, agent.Ref).Scan(&catalogTargets); err != nil || catalogTargets != 1 {
		t.Fatalf("restored agent catalog target: count=%d err=%v", catalogTargets, err)
	}
	retrashed, err := service.Execute(ctx, command.Command{
		Kind: command.TrashProject, Principal: scopedOwner,
		Mutation: value.Mutation{IdempotencyKey: "trash-boundary-retrash", ExpectedVersion: &restored.Project.Version},
		Payload: command.ProjectLifecycleInput{Ref: projectRef},
	})
	if err != nil || retrashed.Project == nil {
		t.Fatalf("trash restored project: %v", err)
	}
	pending, err := service.Execute(ctx, command.Command{
		Kind: command.PurgeProject, Principal: scopedOwner,
		Mutation: value.Mutation{IdempotencyKey: "trash-boundary-purge", ExpectedVersion: &retrashed.Project.Version},
		Payload: command.ProjectLifecycleInput{Ref: projectRef},
	})
	if err != nil || pending.Project == nil || pending.Project.Lifecycle != "PURGE_PENDING" {
		t.Fatalf("request project purge: project=%#v err=%v", pending.Project, err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.RestoreProject, Principal: scopedOwner,
		Mutation: value.Mutation{IdempotencyKey: "trash-boundary-restore-pending", ExpectedVersion: &pending.Project.Version},
		Payload: command.ProjectLifecycleInput{Ref: projectRef},
	}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("restore accepted after purge requested: %v", err)
	}
	assertProjectTrashHidden(t, ctx, service, owner, projectRef, agent.Ref)
	var emptyObjectsDigest string
	if err := pool.QueryRow(ctx, `SELECT control_plane.project_purge_inventory_digest($1::uuid,$2::uuid)`,
		owner.AuthorityTenant, projectID).Scan(&emptyObjectsDigest); err != nil {
		t.Fatalf("read project external inventory digest: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.project_purge_receipts
		SET state='OBJECTS_CLEARED',objects_digest=$2,objects_cleared_at=statement_timestamp()
		WHERE project_id=$1::uuid AND state='PENDING'`, projectID, emptyObjectsDigest); err != nil {
		t.Fatalf("prepare synthetic external cleanup receipt: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT control_plane.purge_project_database($1::uuid,$2::uuid,$3)`,
		owner.AuthorityTenant, projectID, strings.Repeat("f", 64)).Scan(new(int)); err == nil {
		t.Fatal("project purge accepted an unverified external cleanup digest")
	}
	var deletedRows int
	if err := pool.QueryRow(ctx, `SELECT control_plane.purge_project_database($1::uuid,$2::uuid,$3)`,
		owner.AuthorityTenant, projectID, emptyObjectsDigest).Scan(&deletedRows); err != nil || deletedRows == 0 {
		t.Fatalf("purge project graph: rows=%d err=%v", deletedRows, err)
	}
	if err := pool.QueryRow(ctx, `SELECT control_plane.purge_project_database($1::uuid,$2::uuid,$3)`,
		owner.AuthorityTenant, projectID, emptyObjectsDigest).Scan(&deletedRows); err != nil || deletedRows == 0 {
		t.Fatalf("replay project purge receipt: rows=%d err=%v", deletedRows, err)
	}
	var remainingProjects, remainingAgents, remainingSchedules, remainingTargets int
	var receiptState string
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM control_plane.projects WHERE id=$1::uuid),
		(SELECT count(*) FROM control_plane.agents WHERE project_id=$1::uuid),
		(SELECT count(*) FROM control_plane.schedules WHERE project_id=$1::uuid),
		(SELECT count(*) FROM control_plane.project_purge_targets),
		(SELECT state FROM control_plane.project_purge_receipts WHERE project_id=$1::uuid)`,
		projectID).Scan(&remainingProjects, &remainingAgents, &remainingSchedules, &remainingTargets, &receiptState); err != nil ||
		remainingProjects != 0 || remainingAgents != 0 || remainingSchedules != 0 || remainingTargets != 0 || receiptState != "DONE" {
		t.Fatalf("project purge left working rows: projects=%d agents=%d schedules=%d targets=%d receipt=%s err=%v",
			remainingProjects, remainingAgents, remainingSchedules, remainingTargets, receiptState, err)
	}
	retained, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "trash-retention-project"},
		Payload: command.ProjectInput{Name: "Retention boundary", Language: "en"},
	})
	if err != nil || retained.Project == nil {
		t.Fatalf("create retention fixture: %v", err)
	}
	retainedTrash, err := service.Execute(ctx, command.Command{
		Kind: command.TrashProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "trash-retention-request", ExpectedVersion: &retained.Project.Version},
		Payload: command.ProjectLifecycleInput{Ref: retained.Project.Ref},
	})
	if err != nil || retainedTrash.Project == nil {
		t.Fatalf("trash retention fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.projects
		SET deleted_at=statement_timestamp()-interval '31 days',purge_after=statement_timestamp()-interval '1 day'
		WHERE ref=$1`, retained.Project.Ref); err != nil {
		t.Fatalf("age retention fixture: %v", err)
	}
	if err := repository.PromoteDueProjectPurges(ctx, 8); err != nil {
		t.Fatalf("promote due project purge: %v", err)
	}
	pendingItems, err := repository.ListPendingProjectPurges(ctx, 8)
	if err != nil || len(pendingItems) != 1 || pendingItems[0].ProjectRef != retained.Project.Ref {
		t.Fatalf("due purge queue: items=%#v err=%v", pendingItems, err)
	}
	inventory, err := repository.ProjectPurgeInventory(ctx, pendingItems[0])
	if err != nil || len(inventory) != 0 {
		t.Fatalf("retention fixture external inventory: items=%#v err=%v", inventory, err)
	}
	if err := repository.FinalizeProjectPurge(ctx, pendingItems[0], inventory); err != nil {
		t.Fatalf("finalize due project purge: %v", err)
	}
	if err := repository.FinalizeProjectPurge(ctx, pendingItems[0], inventory); err != nil {
		t.Fatalf("replay due project purge: %v", err)
	}
}

func assertProjectLockFence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID, projectRef string) {
	t.Helper()
	deleter, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = deleter.Rollback(ctx) }()
	var id string
	if err := deleter.QueryRow(ctx, `SELECT id::text FROM control_plane.projects WHERE organization_id=$1::uuid AND ref=$2 FOR UPDATE`, organizationID, projectRef).Scan(&id); err != nil {
		t.Fatalf("lock project for trash: %v", err)
	}
	launcher, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = launcher.Rollback(ctx) }()
	if _, err := launcher.Exec(ctx, `SET LOCAL lock_timeout='100ms'`); err != nil {
		t.Fatal(err)
	}
	err = launcher.QueryRow(ctx, queryCommandsMustprojectidSelectProjectsOrganizationIdRefLifecycle, organizationID, projectRef).Scan(&id)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "55P03" {
		t.Fatalf("project launch did not wait for trash fence: %v", err)
	}
}

func assertProjectTrashHidden(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, projectRef, agentRef string) {
	t.Helper()
	if _, err := service.GetProject(ctx, owner, projectRef); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("trashed project direct read: %v", err)
	}
	if _, err := service.GetAgent(ctx, owner, agentRef); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("trashed agent direct read: %v", err)
	}
	projects, _, _, err := service.ListProjects(ctx, owner, query.Filter{Query: "Trash boundary"})
	if err != nil || len(projects) != 0 {
		t.Fatalf("trashed project catalog read: count=%d err=%v", len(projects), err)
	}
	agents, _, err := service.ListAgents(ctx, owner, query.Filter{ProjectRef: projectRef})
	if err != nil || len(agents) != 0 {
		t.Fatalf("trashed agent catalog read: count=%d err=%v", len(agents), err)
	}
	matches, total, _, err := service.Search(ctx, owner, query.Filter{Query: "Trash boundary"})
	if err != nil || len(matches) != 0 || total != 0 {
		t.Fatalf("trashed project global search: count=%d total=%d err=%v", len(matches), total, err)
	}
}
