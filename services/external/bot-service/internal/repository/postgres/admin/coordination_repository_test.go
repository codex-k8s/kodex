//go:build postgres

package admin_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	domainrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCoordinationRepositoryLifecycle(t *testing.T) {
	dsn := isolatedPostgresTestDSN(t, "coordination")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer pool.Close()

	suffix := time.Now().UTC().UnixNano()
	var projectID int64
	if err := pool.QueryRow(ctx, "insert into matter_codex_projects(name, slug) values ($1, $2) returning id",
		"Coordination Test", fmt.Sprintf("coordination-test-%d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "delete from matter_codex_projects where id = $1", projectID)
	})

	directorID := insertCoordinationTestRole(t, ctx, pool, projectID, "portfolio", "custom-top")
	managerID := insertCoordinationTestRole(t, ctx, pool, projectID, "wave", "custom-wave")
	workerID := insertCoordinationTestRole(t, ctx, pool, projectID, "worker", "custom-worker")

	repository := postgresrepo.NewRepository(pool)
	if err := repository.ApplyCoordinationPolicyPreset(ctx, projectID, directorID, []int64{managerID}); err != nil {
		t.Fatalf("ApplyCoordinationPolicyPreset() error = %v", err)
	}
	assertCapability(t, ctx, repository, 0, projectID, directorID, entity.CoordinationCapabilityStartAgents, true)
	assertCapability(t, ctx, repository, 0, projectID, workerID, entity.CoordinationCapabilityUpdateOwnWork, true)
	assertRelationship(t, ctx, repository, 0, projectID, directorID, entity.CoordinationActionStart, managerID, true)
	assertRelationship(t, ctx, repository, 0, projectID, managerID, entity.CoordinationActionStart, workerID, true)
	assertRelationship(t, ctx, repository, 0, projectID, workerID, entity.CoordinationActionCallback, managerID, true)
	assertRelationship(t, ctx, repository, 0, projectID, workerID, entity.CoordinationActionStart, managerID, false)

	chatID := insertCoordinationTestChat(t, ctx, pool, projectID)
	rootSessionID := insertCoordinationTestSession(t, ctx, pool, projectID, chatID, directorID, fmt.Sprintf("root-session-%d", suffix))
	childSessionID := insertCoordinationTestSession(t, ctx, pool, projectID, chatID, managerID, fmt.Sprintf("child-session-%d", suffix))
	rootTurnID := insertCoordinationTestTurn(t, ctx, pool, rootSessionID, fmt.Sprintf("root-run-%d", suffix), "root-post")
	childTurnID := insertCoordinationTestTurn(t, ctx, pool, childSessionID, fmt.Sprintf("child-run-%d", suffix), "child-post")

	rootProcess, err := repository.EnsureTurnProcess(ctx, domainrepo.EnsureTurnProcessInput{
		TurnID: rootTurnID, ProjectID: projectID, RoleID: directorID,
		InitiatorUserID: "owner-id", InitiatorUserName: "owner", TriggerPostID: "root-post",
		MattermostChannelID: "channel", MattermostRootPostID: "root-post",
	})
	if err != nil {
		t.Fatalf("EnsureTurnProcess(root) error = %v", err)
	}
	childProcess, err := repository.EnsureTurnProcess(ctx, domainrepo.EnsureTurnProcessInput{
		TurnID: childTurnID, ParentTurnID: rootTurnID, ProjectID: projectID, RoleID: managerID,
		InitiatorUserID: "director-bot", InitiatorUserName: "portfolio", TriggerPostID: "child-post",
		MattermostChannelID: "channel", MattermostRootPostID: "root-post",
	})
	if err != nil {
		t.Fatalf("EnsureTurnProcess(child) error = %v", err)
	}
	if childProcess.ProcessRunID != rootProcess.ProcessRunID || childProcess.RootInitiatorUserName != "owner" {
		t.Fatalf("child process = %#v, root process = %#v", childProcess, rootProcess)
	}
	lineage, err := repository.GetTurnLineage(ctx, childTurnID)
	if err != nil || len(lineage) != 2 || lineage[0].TurnID != rootTurnID || lineage[1].TurnID != childTurnID {
		t.Fatalf("GetTurnLineage() lineage=%#v error=%v", lineage, err)
	}

	claim, err := repository.UpdateWorkClaim(ctx, domainrepo.UpdateWorkClaimInput{
		TurnID: childTurnID, Summary: "Проектирование", Domains: []string{"coordination"}, ResourceKeys: []string{"issue-1"},
	})
	if err != nil || claim.Summary != "Проектирование" {
		t.Fatalf("UpdateWorkClaim() claim=%#v error=%v", claim, err)
	}
	claims, err := repository.ListActiveWork(ctx, 0, projectID, 20)
	if err != nil || len(claims) != 2 {
		t.Fatalf("ListActiveWork() claims=%#v error=%v", claims, err)
	}

	projectMemory, err := repository.RememberMemory(ctx, domainrepo.RememberMemoryInput{
		ProjectID: projectID, Scope: "project", CreatedByRoleID: directorID, SourceTurnID: rootTurnID,
		Title: "Решение", Content: "Использовать настраиваемую политику", Importance: "high",
	})
	if err != nil {
		t.Fatalf("RememberMemory(project) error = %v", err)
	}
	_, err = repository.RememberMemory(ctx, domainrepo.RememberMemoryInput{
		ProjectID: projectID, Scope: "role", RoleID: managerID, CreatedByRoleID: managerID, SourceTurnID: childTurnID,
		Title: "Опыт роли", Content: "проверять активные работы", Importance: "normal",
	})
	if err != nil {
		t.Fatalf("RememberMemory(role) error = %v", err)
	}
	memories, err := repository.SearchMemory(ctx, domainrepo.SearchMemoryInput{ProjectID: projectID, RoleID: managerID, Query: "проверять", Limit: 10})
	if err != nil || len(memories) != 1 || memories[0].Scope != "role" {
		t.Fatalf("SearchMemory(role) memories=%#v error=%v", memories, err)
	}
	memories, err = repository.SearchMemory(ctx, domainrepo.SearchMemoryInput{ProjectID: projectID, RoleID: workerID, Query: "настраиваемую", Limit: 10})
	if err != nil || len(memories) != 1 || memories[0].ID != projectMemory.ID {
		t.Fatalf("SearchMemory(project) memories=%#v error=%v", memories, err)
	}

	attention, created, err := repository.CreateOwnerAttention(ctx, domainrepo.CreateOwnerAttentionInput{
		ProcessRunID: rootProcess.ProcessRunID, TurnID: childTurnID, Severity: "urgent",
		Summary: "Нужно решение", PauseScope: "wave", IdempotencyKey: "decision-1",
	})
	if err != nil || !created {
		t.Fatalf("CreateOwnerAttention() item=%#v created=%t error=%v", attention, created, err)
	}
	duplicate, created, err := repository.CreateOwnerAttention(ctx, domainrepo.CreateOwnerAttentionInput{
		ProcessRunID: rootProcess.ProcessRunID, TurnID: childTurnID, Severity: "urgent",
		Summary: "Нужно решение", PauseScope: "wave", IdempotencyKey: "decision-1",
	})
	if err != nil || created || duplicate.ID != attention.ID {
		t.Fatalf("CreateOwnerAttention(duplicate) item=%#v created=%t error=%v", duplicate, created, err)
	}
	assertCoordinationTransactionRepository(t, ctx, repository, projectID, managerID, childTurnID, rootProcess.ProcessRunID, suffix)

	waitingProcess, err := repository.GetTurnProcess(ctx, childTurnID)
	if err != nil || waitingProcess.Status != "waiting_owner" {
		t.Fatalf("GetTurnProcess(waiting) process=%#v error=%v", waitingProcess, err)
	}

	ownerReplyTurnID := insertCoordinationTestTurn(t, ctx, pool, rootSessionID, "owner-reply-run", "owner-reply-post")
	resumedProcess, err := repository.EnsureTurnProcess(ctx, domainrepo.EnsureTurnProcessInput{
		TurnID: ownerReplyTurnID, ProjectID: projectID, RoleID: directorID,
		InitiatorUserID: "owner-id", InitiatorUserName: "owner", TriggerPostID: "owner-reply-post",
		MattermostChannelID: "channel", MattermostRootPostID: "root-post",
	})
	if err != nil {
		t.Fatalf("EnsureTurnProcess(owner reply) error = %v", err)
	}
	if resumedProcess.ProcessRunID != rootProcess.ProcessRunID || resumedProcess.Status != "running" {
		t.Fatalf("owner reply process = %#v, root process = %#v", resumedProcess, rootProcess)
	}
	var attentionStatus, resolvedByUserID, resolvedByPostID string
	if err := pool.QueryRow(ctx, `
		select status, resolved_by_user_id, resolved_by_post_id
		from matter_codex_owner_attention_requests where id = $1
	`, attention.ID).Scan(&attentionStatus, &resolvedByUserID, &resolvedByPostID); err != nil {
		t.Fatalf("read resolved attention: %v", err)
	}
	if attentionStatus != "resolved" || resolvedByUserID != "owner-id" || resolvedByPostID != "owner-reply-post" {
		t.Fatalf("resolved attention status=%q user=%q post=%q", attentionStatus, resolvedByUserID, resolvedByPostID)
	}
	if _, err := pool.Exec(ctx, `
		update matter_codex_agent_session_turns set status = 'succeeded'
		where id = any($1::bigint[])
	`, []int64{rootTurnID, childTurnID, ownerReplyTurnID}); err != nil {
		t.Fatalf("complete process turns: %v", err)
	}
	if err := repository.ReconcileProcessRun(ctx, ownerReplyTurnID); err != nil {
		t.Fatalf("ReconcileProcessRun() error = %v", err)
	}
	completedProcess, err := repository.GetTurnProcess(ctx, ownerReplyTurnID)
	if err != nil || completedProcess.Status != "completed" {
		t.Fatalf("GetTurnProcess(completed) process=%#v error=%v", completedProcess, err)
	}

	if _, err := pool.Exec(ctx, "update matter_codex_policy_revisions set status = 'archived' where id = $1", rootProcess.PolicyRevisionID); err != nil {
		t.Fatalf("archive policy revision: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_policy_revisions(project_id, version, status, activated_at)
		values ($1, 2, 'active', now())`, projectID); err != nil {
		t.Fatalf("create replacement policy revision: %v", err)
	}
	assertCapability(t, ctx, repository, rootTurnID, projectID, directorID, entity.CoordinationCapabilityStartAgents, true)
	assertCapability(t, ctx, repository, 0, projectID, directorID, entity.CoordinationCapabilityStartAgents, false)
}

func assertCoordinationTransactionRepository(
	t *testing.T,
	ctx context.Context,
	repository *postgresrepo.Repository,
	projectID int64,
	roleID int64,
	turnID int64,
	processRunID int64,
	suffix int64,
) {
	t.Helper()
	tokenHash := sha256.Sum256([]byte(fmt.Sprintf("coordination-transaction-token-%d", suffix)))
	contextHash := sha256.Sum256([]byte(fmt.Sprintf("coordination-transaction-context-%d", suffix)))
	now := time.Now().UTC()
	issue := securityrepo.IssueCapabilityInput{
		TokenHash: tokenHash[:], Kind: "dialog", Operation: "coordination.transaction",
		ResourceType: "work_context", ResourceID: fmt.Sprintf("%d", turnID), ChannelID: "channel",
		PostBinding: "post", ActorUserID: "owner-id", ActorUserName: "owner",
		InstallationScope: "single-installation", WorkspaceScope: fmt.Sprintf("%d", projectID),
		ContextHash: contextHash[:], IssuedAt: now, ExpiresAt: now.Add(time.Hour), State: securityrepo.CapabilityStateUnused,
	}
	if err := repository.IssueInteractionCapability(ctx, issue); err != nil {
		t.Fatalf("IssueInteractionCapability(transaction repository) error = %v", err)
	}
	_, err := repository.ConsumeInteractionCapabilityWithMutation(ctx, securityrepo.ConsumeCapabilityInput{
		TokenHash: tokenHash[:], Kind: issue.Kind, Operation: issue.Operation,
		ResourceType: issue.ResourceType, ResourceID: issue.ResourceID, ChannelID: issue.ChannelID,
		PostBinding: issue.PostBinding, ActorUserID: issue.ActorUserID, ContextHash: contextHash[:], Now: now.Add(time.Minute),
	}, func(store domainrepo.Repository) error {
		coordinationStore, ok := store.(domainrepo.CoordinationRepository)
		if !ok {
			return fmt.Errorf("transaction repository does not implement coordination repository")
		}
		if _, err := coordinationStore.GetTurnProcess(ctx, turnID); err != nil {
			return err
		}
		if _, err := coordinationStore.GetTurnLineage(ctx, turnID); err != nil {
			return err
		}
		if _, err := coordinationStore.UpdateWorkClaim(ctx, domainrepo.UpdateWorkClaimInput{TurnID: turnID, Summary: "Транзакционный guarded callback"}); err != nil {
			return err
		}
		if _, err := coordinationStore.ListActiveWork(ctx, processRunID, projectID, 10); err != nil {
			return err
		}
		if _, err := coordinationStore.RememberMemory(ctx, domainrepo.RememberMemoryInput{
			ProjectID: projectID, Scope: "role", RoleID: roleID, CreatedByRoleID: roleID, SourceTurnID: turnID,
			Title: "Транзакционный контекст", Content: "Проверять транзакционную repository привязку", Importance: "normal",
		}); err != nil {
			return err
		}
		if _, err := coordinationStore.SearchMemory(ctx, domainrepo.SearchMemoryInput{ProjectID: projectID, RoleID: roleID, Query: "транзакционную", Limit: 10}); err != nil {
			return err
		}
		request, _, err := coordinationStore.CreateOwnerAttention(ctx, domainrepo.CreateOwnerAttentionInput{
			ProcessRunID: processRunID, TurnID: turnID, Severity: "normal", Summary: "Транзакционная проверка",
			PauseScope: "turn", IdempotencyKey: fmt.Sprintf("transaction-context-%d", suffix),
		})
		if err != nil {
			return err
		}
		_, err = coordinationStore.SetOwnerAttentionPost(ctx, request.ID, "transaction-post")
		return err
	})
	if err != nil {
		t.Fatalf("ConsumeInteractionCapabilityWithMutation(coordination repository) error = %v", err)
	}
}

func insertCoordinationTestRole(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID int64, name string, roleType string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type)
		values ($1,$2,$3) returning id`, projectID, name, roleType).Scan(&id); err != nil {
		t.Fatalf("create role %s: %v", name, err)
	}
	return id
}

func insertCoordinationTestChat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID int64) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, name, slug)
		values ($1,'Coordination','coordination-test') returning id`, projectID).Scan(&id); err != nil {
		t.Fatalf("create chat: %v", err)
	}
	return id
}

func insertCoordinationTestSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID int64, chatID int64, roleID int64, key string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_sessions(
		session_key, project_id, chat_id, role_id, session_scope, ttl_seconds, expires_at
	) values ($1,$2,$3,$4,'thread_role',3600,now() + interval '1 hour') returning id`, key, projectID, chatID, roleID).Scan(&id); err != nil {
		t.Fatalf("create session %s: %v", key, err)
	}
	return id
}

func insertCoordinationTestTurn(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionID int64, runID string, postID string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
		session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message
	) values ($1,$2,'channel','root-post',$3,'test') returning id`, sessionID, runID, postID).Scan(&id); err != nil {
		t.Fatalf("create turn %s: %v", runID, err)
	}
	return id
}

func assertCapability(t *testing.T, ctx context.Context, repository *postgresrepo.Repository, turnID int64, projectID int64, roleID int64, capability string, expected bool) {
	t.Helper()
	actual, err := repository.IsRoleCapabilityAllowed(ctx, turnID, projectID, roleID, capability)
	if err != nil || actual != expected {
		t.Fatalf("capability %s allowed=%t expected=%t error=%v", capability, actual, expected, err)
	}
}

func assertRelationship(t *testing.T, ctx context.Context, repository *postgresrepo.Repository, turnID int64, projectID int64, sourceRoleID int64, action string, targetRoleID int64, expected bool) {
	t.Helper()
	actual, err := repository.IsRoleRelationshipAllowed(ctx, turnID, projectID, sourceRoleID, action, targetRoleID)
	if err != nil || actual != expected {
		t.Fatalf("relationship %s %d->%d allowed=%t expected=%t error=%v", action, sourceRoleID, targetRoleID, actual, expected, err)
	}
}
