//go:build postgres

package migrations_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	instructionsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/instructions"
	workspacesrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/workspaces"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestUniversalModelMigrationFreshAndNMinusOneBackfill(t *testing.T) {
	dsn := isolatedMigrationDSN(t, "universal_model_backfill")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrations.RunTo(ctx, dsn, 29); err != nil {
		t.Fatalf("подготовка N-1 schema: %v", err)
	}
	pool := openMigrationPool(t, ctx, dsn)
	defer pool.Close()
	var projectID, roleID, chatID int64
	if err := pool.QueryRow(ctx, `
insert into matter_codex_projects(name, slug, mattermost_team_id, description)
values ('Негитовая область', 'no-git-workspace', 'team-no-git', 'Без репозитория')
returning id
`).Scan(&projectID); err != nil {
		t.Fatalf("seed legacy project: %v", err)
	}
	legacyMarkdown := "# V1\n\nРаботай без Git."
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_roles(
	project_id, name, role_type, description, prompt_template, prompt_mode,
	github_account_name, openai_account_name, enabled
) values ($1, 'assistant', 'worker', 'Негитовый агент', $2, 'raw', '', 'primary', true)
returning id
`, projectID, legacyMarkdown).Scan(&roleID); err != nil {
		t.Fatalf("seed legacy role: %v", err)
	}
	if err := pool.QueryRow(ctx, `
insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug, description, chat_type)
values ($1, 'channel-no-git', 'Рабочая комната', 'work-room', 'Без репозитория', 'custom')
returning id
`, projectID).Scan(&chatID); err != nil {
		t.Fatalf("seed legacy chat: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_chat_participants(chat_id, role_id) values ($1, $2)`, chatID, roleID); err != nil {
		t.Fatalf("seed legacy participant: %v", err)
	}
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("upgrade N-1 -> universal model: %v", err)
	}
	if version, err := migrations.Version(ctx, dsn); err != nil || version != 30 {
		t.Fatalf("universal model schema version = %d, error=%v", version, err)
	}

	var workspaces, rooms, definitions, agents, assignments, sets, versions int
	if err := pool.QueryRow(ctx, `select
	(select count(*) from matter_codex_workspaces),
	(select count(*) from matter_codex_rooms),
	(select count(*) from matter_codex_role_definitions),
	(select count(*) from matter_codex_agents),
	(select count(*) from matter_codex_agent_assignments),
	(select count(*) from matter_codex_instruction_sets),
	(select count(*) from matter_codex_instruction_versions)
`).Scan(&workspaces, &rooms, &definitions, &agents, &assignments, &sets, &versions); err != nil {
		t.Fatalf("read universal backfill counts: %v", err)
	}
	if workspaces != 1 || rooms != 1 || definitions != 1 || agents != 1 || assignments != 2 || sets != 1 || versions != 1 {
		t.Fatalf("unexpected backfill counts: workspace=%d room=%d definition=%d agent=%d assignment=%d set=%d version=%d", workspaces, rooms, definitions, agents, assignments, sets, versions)
	}
	expectedDigest := sha256.Sum256([]byte(legacyMarkdown))
	var storedMarkdown string
	var storedDigest []byte
	if err := pool.QueryRow(ctx, `select markdown, content_sha256 from matter_codex_instruction_versions where instruction_set_id = (select instruction_set_id from matter_codex_agents where legacy_agent_role_id = $1)`, roleID).Scan(&storedMarkdown, &storedDigest); err != nil {
		t.Fatalf("read backfilled instruction version: %v", err)
	}
	if storedMarkdown != legacyMarkdown || string(storedDigest) != string(expectedDigest[:]) {
		t.Fatalf("backfilled instruction mismatch: markdown=%q sha=%x", storedMarkdown, storedDigest)
	}

	repository := postgresrepo.NewRepository(pool)
	project, created, err := repository.UpsertProject(ctx, adminrepo.UpsertProjectInput{
		Name: "Вторая область", Slug: "second-workspace", MattermostTeamID: "team-second",
		Description: "Без GitHub account и repository", AdvancedSettings: "{}",
	})
	if err != nil || !created || project.GitHubAccountName != "" {
		t.Fatalf("no-repository workspace dual-write: project=%#v created=%t error=%v", project, created, err)
	}
	if _, err := repository.GetWorkspaceByLegacyProjectID(ctx, project.ID); err != nil {
		t.Fatalf("workspace target missing: %v", err)
	}

	v2 := "# V2\n\nСледующая версия."
	role, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: projectID, Name: "assistant", RoleType: "worker", Description: "Негитовый агент",
		PromptTemplate: v2, PromptMode: "raw", OpenAIAccountName: "primary",
		KubernetesAccess: "read-only", SandboxMode: "danger-full-access", Enabled: true,
	})
	if err != nil {
		t.Fatalf("publish V2 dual-write: %v", err)
	}
	snapshot, err := repository.GetAgentInstructionSnapshot(ctx, role.ID)
	if err != nil || snapshot.InstructionVersion.Version != 2 || snapshot.InstructionVersion.Markdown != v2 {
		t.Fatalf("V2 snapshot = %#v, error=%v", snapshot.InstructionVersion, err)
	}
	var mirroredPrompt string
	if err := pool.QueryRow(ctx, `select coalesce(prompt_template, '') from matter_codex_agent_roles where id = $1`, role.ID).Scan(&mirroredPrompt); err != nil || mirroredPrompt != v2 {
		t.Fatalf("legacy prompt mirror = %q, error=%v", mirroredPrompt, err)
	}
	initialRecordVersion := snapshot.InstructionSet.RecordVersion
	const renamedRole = "assistant-renamed"
	if _, err := pool.Exec(ctx, `update matter_codex_agent_roles set name = $2 where id = $1`, role.ID, renamedRole); err != nil {
		t.Fatalf("rename legacy role before same-digest synchronization: %v", err)
	}
	role, _, err = repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: projectID, Name: renamedRole, RoleType: "worker", Description: "Негитовый агент",
		PromptTemplate: v2, PromptMode: "raw", OpenAIAccountName: "primary",
		KubernetesAccess: "read-only", SandboxMode: "danger-full-access", Enabled: false,
	})
	if err != nil {
		t.Fatalf("same-digest metadata synchronization: %v", err)
	}
	snapshot, err = repository.GetAgentInstructionSnapshot(ctx, role.ID)
	if err != nil {
		t.Fatalf("read same-digest metadata snapshot: %v", err)
	}
	if snapshot.InstructionVersion.Version != 2 || snapshot.InstructionSet.Name != renamedRole || snapshot.InstructionSet.Status != "disabled" || snapshot.InstructionSet.SourceRevision != fmt.Sprintf("legacy-agent-role:%d:version:2", role.ID) {
		t.Fatalf("same-digest target metadata = %#v", snapshot.InstructionSet)
	}
	if snapshot.InstructionSet.RecordVersion != initialRecordVersion+1 {
		t.Fatalf("same-digest metadata record version = %d, want %d", snapshot.InstructionSet.RecordVersion, initialRecordVersion+1)
	}
	idempotentRecordVersion := snapshot.InstructionSet.RecordVersion
	for range 2 {
		if _, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
			ProjectID: projectID, Name: renamedRole, RoleType: "worker", Description: "Негитовый агент",
			PromptTemplate: v2, PromptMode: "raw", OpenAIAccountName: "primary",
			KubernetesAccess: "read-only", SandboxMode: "danger-full-access", Enabled: false,
		}); err != nil {
			t.Fatalf("idempotent same-digest retry: %v", err)
		}
	}
	sameDigestStart := make(chan struct{})
	sameDigestErrors := make(chan error, 2)
	for range 2 {
		go func() {
			<-sameDigestStart
			_, _, sameDigestErr := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
				ProjectID: projectID, Name: renamedRole, RoleType: "worker", Description: "Негитовый агент",
				PromptTemplate: v2, PromptMode: "raw", OpenAIAccountName: "primary",
				KubernetesAccess: "read-only", SandboxMode: "danger-full-access", Enabled: false,
			})
			sameDigestErrors <- sameDigestErr
		}()
	}
	close(sameDigestStart)
	for range 2 {
		if sameDigestErr := <-sameDigestErrors; sameDigestErr != nil {
			t.Fatalf("concurrent same-digest publish: %v", sameDigestErr)
		}
	}
	snapshot, err = repository.GetAgentInstructionSnapshot(ctx, role.ID)
	if err != nil || snapshot.InstructionVersion.Version != 2 || snapshot.InstructionSet.RecordVersion != idempotentRecordVersion {
		t.Fatalf("idempotent same-digest snapshot=%#v error=%v", snapshot, err)
	}

	concurrentErrors := make(chan error, 2)
	for index := range 2 {
		index := index
		go func() {
			_, _, publishErr := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
				ProjectID: projectID, Name: renamedRole, RoleType: "worker",
				PromptTemplate: fmt.Sprintf("# Concurrent V%d", index+3), PromptMode: "raw",
				OpenAIAccountName: "primary", KubernetesAccess: "read-only",
				SandboxMode: "danger-full-access", Enabled: true,
			})
			concurrentErrors <- publishErr
		}()
	}
	for range 2 {
		if publishErr := <-concurrentErrors; publishErr != nil {
			t.Fatalf("concurrent instruction publish: %v", publishErr)
		}
	}
	var versionNumbers []int64
	rows, err := pool.Query(ctx, `select version from matter_codex_instruction_versions where instruction_set_id = $1 order by version`, snapshot.InstructionSet.ID)
	if err != nil {
		t.Fatalf("list concurrent versions: %v", err)
	}
	for rows.Next() {
		var number int64
		if err := rows.Scan(&number); err != nil {
			rows.Close()
			t.Fatalf("scan concurrent version: %v", err)
		}
		versionNumbers = append(versionNumbers, number)
	}
	rows.Close()
	if fmt.Sprint(versionNumbers) != "[1 2 3 4]" {
		t.Fatalf("concurrent versions = %v", versionNumbers)
	}

	otherRole, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: project.ID, Name: "other-agent", RoleType: "worker",
		PromptTemplate: "# Other V1", PromptMode: "raw", OpenAIAccountName: "primary",
		KubernetesAccess: "read-only", SandboxMode: "danger-full-access", Enabled: true,
	})
	if err != nil {
		t.Fatalf("publish other instruction set: %v", err)
	}
	otherSnapshot, err := repository.GetAgentInstructionSnapshot(ctx, otherRole.ID)
	if err != nil {
		t.Fatalf("read other instruction set: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_instruction_sets set current_version_id = $2 where id = $1`, snapshot.InstructionSet.ID, otherSnapshot.InstructionVersion.ID); err == nil {
		t.Fatal("instruction set accepted current version from another set")
	}
	if _, err := pool.Exec(ctx, `
insert into matter_codex_instruction_versions(
	organization_scope, instruction_set_id, version, markdown, content_sha256, actor_ref
)
select organization_scope, id, 100, repeat('x', 65536), public.digest(repeat('x', 65536), 'sha256'), 'test'
from matter_codex_instruction_sets where id = $1
`, otherSnapshot.InstructionSet.ID); err != nil {
		t.Fatalf("instruction version rejected exact 65536-byte storage boundary: %v", err)
	}
	if _, err := pool.Exec(ctx, `
insert into matter_codex_instruction_versions(
	organization_scope, instruction_set_id, version, markdown, content_sha256, actor_ref
)
select organization_scope, id, 101, repeat('x', 65537), public.digest(repeat('x', 65537), 'sha256'), 'test'
from matter_codex_instruction_sets where id = $1
`, otherSnapshot.InstructionSet.ID); err == nil {
		t.Fatal("instruction version accepted Markdown above 65536-byte storage boundary")
	}
	if _, err := pool.Exec(ctx, `
insert into matter_codex_instruction_versions(
	organization_scope, instruction_set_id, version, markdown, content_sha256, actor_ref
)
select organization_scope, id, 102, 'wrong digest', decode(repeat('00', 32), 'hex'), 'test'
from matter_codex_instruction_sets where id = $1
`, otherSnapshot.InstructionSet.ID); err == nil {
		t.Fatal("instruction version accepted mismatched SHA-256")
	}
	if _, err := pool.Exec(ctx, `update matter_codex_instruction_versions set markdown = 'mutated' where id = $1`, snapshot.InstructionVersion.ID); err == nil {
		t.Fatal("immutable instruction version accepted update")
	}
	if _, err := pool.Exec(ctx, `delete from matter_codex_instruction_versions where id = $1`, snapshot.InstructionVersion.ID); err == nil {
		t.Fatal("immutable instruction version accepted delete")
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_agent_assignments(organization_scope, agent_id, workspace_id) select 'foreign-scope', id, (select id from matter_codex_workspaces limit 1) from matter_codex_agents limit 1`); err == nil {
		t.Fatal("cross-scope assignment was accepted")
	}
	deleteTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin legacy cascade check: %v", err)
	}
	if _, err := deleteTx.Exec(ctx, `delete from matter_codex_projects where id = $1`, projectID); err != nil {
		_ = deleteTx.Rollback(ctx)
		t.Fatalf("universal projection blocked legacy project delete: %v", err)
	}
	var remainingWorkspaceProjections int
	if err := deleteTx.QueryRow(ctx, `select count(*) from matter_codex_workspaces where legacy_project_id = $1`, projectID).Scan(&remainingWorkspaceProjections); err != nil {
		_ = deleteTx.Rollback(ctx)
		t.Fatalf("read cascaded workspace projection: %v", err)
	}
	if remainingWorkspaceProjections != 0 {
		_ = deleteTx.Rollback(ctx)
		t.Fatalf("legacy project cascade left %d workspace projections", remainingWorkspaceProjections)
	}
	if err := deleteTx.Rollback(ctx); err != nil {
		t.Fatalf("rollback legacy cascade check: %v", err)
	}

	if _, err := pool.Exec(ctx, `update matter_codex_instruction_sets set managed_by = 'git', source_type = 'git', source_ref = '{"path":"AGENTS.md"}', source_revision = 'synthetic-revision', provenance = '{"origin":{"kind":"legacy-chain"}}' where id = $1`, snapshot.InstructionSet.ID); err != nil {
		t.Fatalf("stage Git-managed instruction set: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agents set managed_by = 'git', source_ref = '{"agent":"source"}', source_revision = 'agent-revision', provenance = '{"agent_origin":"legacy-chain"}' where legacy_agent_role_id = $1`, role.ID); err != nil {
		t.Fatalf("stage Git-managed agent: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_role_definitions set managed_by = 'git', source_ref = '{"role":"source"}', source_revision = 'role-revision', provenance = '{"role_origin":"legacy-chain"}' where legacy_agent_role_id = $1`, role.ID); err != nil {
		t.Fatalf("stage Git-managed role definition: %v", err)
	}
	var promptBeforeDenied string
	if err := pool.QueryRow(ctx, `select coalesce(prompt_template, '') from matter_codex_agent_roles where id = $1`, role.ID).Scan(&promptBeforeDenied); err != nil {
		t.Fatalf("read prompt before denied mutation: %v", err)
	}
	if _, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: projectID, Name: renamedRole, RoleType: "worker", PromptTemplate: "# Denied",
		PromptMode: "raw", OpenAIAccountName: "primary", KubernetesAccess: "read-only",
		SandboxMode: "danger-full-access", Enabled: true,
	}); !errors.Is(err, instructionsrepo.ErrManagedByGit) {
		t.Fatalf("Git-managed mutation error = %v", err)
	}
	var promptAfterDenied string
	if err := pool.QueryRow(ctx, `select coalesce(prompt_template, '') from matter_codex_agent_roles where id = $1`, role.ID).Scan(&promptAfterDenied); err != nil || promptAfterDenied != promptBeforeDenied {
		t.Fatalf("denied mutation changed legacy projection: before=%q after=%q error=%v", promptBeforeDenied, promptAfterDenied, err)
	}
	detached, err := repository.DetachInstructionSet(ctx, instructionsrepo.DetachInstructionSetInput{LegacyAgentRoleID: role.ID, ActorRef: "owner"})
	if err != nil || detached.InstructionSet.ManagedBy != "ui" || detached.Agent.ManagedBy != "ui" || detached.RoleDefinition.ManagedBy != "ui" ||
		!strings.Contains(detached.InstructionSet.Provenance, "synthetic-revision") ||
		!strings.Contains(detached.InstructionSet.Provenance, "legacy-chain") ||
		!strings.Contains(detached.InstructionSet.Provenance, "previous_provenance") ||
		!strings.Contains(detached.InstructionSet.Provenance, "detach_history") ||
		!strings.Contains(detached.Agent.Provenance, "agent-revision") ||
		!strings.Contains(detached.Agent.Provenance, "agent_origin") ||
		!strings.Contains(detached.Agent.Provenance, "detach_history") ||
		!strings.Contains(detached.RoleDefinition.Provenance, "role-revision") ||
		!strings.Contains(detached.RoleDefinition.Provenance, "role_origin") ||
		!strings.Contains(detached.RoleDefinition.Provenance, "detach_history") {
		t.Fatalf("detach result = %#v, error=%v", detached.InstructionSet, err)
	}
	var detachAudits int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_audit_events where event_type = 'instruction_set.detached' and resource_name = $1`, fmt.Sprintf("legacy-agent-role:%d", role.ID)).Scan(&detachAudits); err != nil || detachAudits != 1 {
		t.Fatalf("detach audit count = %d, error=%v", detachAudits, err)
	}
	afterDetach := "# After detach"
	if _, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: projectID, Name: renamedRole, RoleType: "worker", PromptTemplate: afterDetach,
		PromptMode: "raw", OpenAIAccountName: "primary", KubernetesAccess: "read-only",
		SandboxMode: "danger-full-access", Enabled: true,
	}); err != nil {
		t.Fatalf("publish after explicit detach: %v", err)
	}
	detachedSnapshot, err := repository.GetAgentInstructionSnapshot(ctx, role.ID)
	if err != nil || detachedSnapshot.InstructionVersion.Version != 5 || detachedSnapshot.InstructionVersion.Markdown != afterDetach {
		t.Fatalf("post-detach snapshot = %#v, error=%v", detachedSnapshot.InstructionVersion, err)
	}

	if _, err := pool.Exec(ctx, `update matter_codex_workspaces set managed_by = 'git', source_revision = 'workspace-git-revision' where legacy_project_id = $1`, project.ID); err != nil {
		t.Fatalf("stage Git-managed workspace: %v", err)
	}
	if _, _, err := repository.UpsertProject(ctx, adminrepo.UpsertProjectInput{
		Name: "Изменённая область", Slug: project.Slug, MattermostTeamID: project.MattermostTeamID,
	}); !errors.Is(err, workspacesrepo.ErrManagedByGit) {
		t.Fatalf("Git-managed workspace mutation error = %v", err)
	}
	var unchangedWorkspaceName string
	if err := pool.QueryRow(ctx, `select name from matter_codex_projects where id = $1`, project.ID).Scan(&unchangedWorkspaceName); err != nil || unchangedWorkspaceName != project.Name {
		t.Fatalf("denied workspace mutation changed legacy projection: name=%q error=%v", unchangedWorkspaceName, err)
	}

	if _, _, err := repository.UpsertProject(ctx, adminrepo.UpsertProjectInput{
		Name: "Коллизия", Slug: "team-collision", MattermostTeamID: "team-second", AdvancedSettings: "{}",
	}); err == nil {
		t.Fatal("target unique fault injection unexpectedly succeeded")
	}
	var leakedLegacy int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_projects where slug = 'team-collision'`).Scan(&leakedLegacy); err != nil || leakedLegacy != 0 {
		t.Fatalf("target failure leaked legacy write: rows=%d error=%v", leakedLegacy, err)
	}

	room, _, err := repository.CreateChat(ctx, adminrepo.CreateChatInput{
		ProjectID: projectID, MattermostChannelID: "no-repository-room", Name: "Без репозитория",
		Slug: "no-repository-room", ChatType: "custom", RoleIDs: []int64{role.ID},
	})
	if err != nil {
		t.Fatalf("no-repository room dual-write: %v", err)
	}
	if _, err := repository.GetRoomByLegacyChatID(ctx, room.ID); err != nil {
		t.Fatalf("room target missing: %v", err)
	}
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("repeated Up: %v", err)
	}
}

func TestUniversalModelMigrationPreflightRejectsDuplicateExternalBindings(t *testing.T) {
	dsn := isolatedMigrationDSN(t, "universal_model_preflight")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migrations.RunTo(ctx, dsn, 29); err != nil {
		t.Fatalf("подготовка N-1 schema: %v", err)
	}
	pool := openMigrationPool(t, ctx, dsn)
	defer pool.Close()
	if _, err := pool.Exec(ctx, `insert into matter_codex_projects(name, slug, mattermost_team_id) values ('A', 'duplicate-a', 'duplicate-team'), ('B', 'duplicate-b', 'duplicate-team')`); err != nil {
		t.Fatalf("seed duplicate external binding: %v", err)
	}
	err := migrations.Run(ctx, dsn)
	if err == nil || !strings.Contains(err.Error(), "MCV30_DUPLICATE_MATTERMOST_TEAM_BINDINGS") {
		t.Fatalf("duplicate preflight error = %v", err)
	}
	if version, versionErr := migrations.Version(ctx, dsn); versionErr != nil || version != 29 {
		t.Fatalf("version after rejected preflight = %d, error=%v", version, versionErr)
	}
}

func TestUniversalModelMigrationPreflightRejectsCrossProjectLegacyOwnership(t *testing.T) {
	tests := []struct {
		name string
		code string
		seed func(context.Context, migrationExecutor, *testing.T)
	}{
		{
			name: "chat participant",
			code: "MCV30_CROSS_PROJECT_CHAT_PARTICIPANT",
			seed: func(ctx context.Context, db migrationExecutor, t *testing.T) {
				var firstProjectID, secondProjectID, roleID, chatID int64
				if err := db.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('A', 'ownership-a') returning id`).Scan(&firstProjectID); err != nil {
					t.Fatalf("seed first project: %v", err)
				}
				if err := db.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('B', 'ownership-b') returning id`).Scan(&secondProjectID); err != nil {
					t.Fatalf("seed second project: %v", err)
				}
				if err := db.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type) values ($1, 'worker-a', 'worker') returning id`, firstProjectID).Scan(&roleID); err != nil {
					t.Fatalf("seed first project role: %v", err)
				}
				if err := db.QueryRow(ctx, `insert into matter_codex_chats(project_id, name, slug) values ($1, 'Room B', 'room-b') returning id`, secondProjectID).Scan(&chatID); err != nil {
					t.Fatalf("seed second project chat: %v", err)
				}
				if _, err := db.Exec(ctx, `insert into matter_codex_chat_participants(chat_id, role_id) values ($1, $2)`, chatID, roleID); err != nil {
					t.Fatalf("seed cross-project participant: %v", err)
				}
			},
		},
		{
			name: "bot identity",
			code: "MCV30_CROSS_PROJECT_BOT_IDENTITY",
			seed: func(ctx context.Context, db migrationExecutor, t *testing.T) {
				var firstProjectID, secondProjectID, roleID int64
				if err := db.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('A', 'bot-owner-a') returning id`).Scan(&firstProjectID); err != nil {
					t.Fatalf("seed first project: %v", err)
				}
				if err := db.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('B', 'bot-owner-b') returning id`).Scan(&secondProjectID); err != nil {
					t.Fatalf("seed second project: %v", err)
				}
				if err := db.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type) values ($1, 'worker-a', 'worker') returning id`, firstProjectID).Scan(&roleID); err != nil {
					t.Fatalf("seed first project role: %v", err)
				}
				if _, err := db.Exec(ctx, `insert into matter_codex_mattermost_bot_identities(project_id, role_id, username) values ($1, $2, 'cross-owner-bot')`, secondProjectID, roleID); err != nil {
					t.Fatalf("seed cross-project bot identity: %v", err)
				}
			},
		},
		{
			name: "instruction storage limit",
			code: "MCV30_INSTRUCTION_MARKDOWN_TOO_LARGE",
			seed: func(ctx context.Context, db migrationExecutor, t *testing.T) {
				var projectID int64
				if err := db.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('A', 'large-prompt') returning id`).Scan(&projectID); err != nil {
					t.Fatalf("seed project: %v", err)
				}
				if _, err := db.Exec(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, prompt_template) values ($1, 'worker-a', 'worker', repeat('x', 65537))`, projectID); err != nil {
					t.Fatalf("seed oversized prompt: %v", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dsn := isolatedMigrationDSN(t, "universal_preflight_"+strings.ReplaceAll(test.name, " ", "_"))
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if err := migrations.RunTo(ctx, dsn, 29); err != nil {
				t.Fatalf("подготовка N-1 schema: %v", err)
			}
			pool := openMigrationPool(t, ctx, dsn)
			defer pool.Close()
			test.seed(ctx, pool, t)
			err := migrations.Run(ctx, dsn)
			if err == nil || !strings.Contains(err.Error(), test.code) {
				t.Fatalf("preflight error = %v, want code %s", err, test.code)
			}
			if version, versionErr := migrations.Version(ctx, dsn); versionErr != nil || version != 29 {
				t.Fatalf("version after rejected preflight = %d, error=%v", version, versionErr)
			}
			var targetTable *string
			if err := pool.QueryRow(ctx, `select to_regclass(current_schema() || '.matter_codex_workspaces')::text`).Scan(&targetTable); err != nil || targetTable != nil {
				t.Fatalf("rejected preflight left partial target schema: table=%v error=%v", targetTable, err)
			}
		})
	}
}

type migrationExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func TestUniversalBotIdentityBindingRequiresExactUIManagedOwnership(t *testing.T) {
	dsn := isolatedMigrationDSN(t, "universal_bot_binding")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("fresh migrations: %v", err)
	}
	pool := openMigrationPool(t, ctx, dsn)
	defer pool.Close()
	repository := postgresrepo.NewRepository(pool)
	firstProject, _, err := repository.UpsertProject(ctx, adminrepo.UpsertProjectInput{Name: "First", Slug: "bot-first"})
	if err != nil {
		t.Fatalf("create first project: %v", err)
	}
	secondProject, _, err := repository.UpsertProject(ctx, adminrepo.UpsertProjectInput{Name: "Second", Slug: "bot-second"})
	if err != nil {
		t.Fatalf("create second project: %v", err)
	}
	gitRole, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: firstProject.ID, Name: "git-agent", RoleType: "worker", PromptTemplate: "# Git agent",
		PromptMode: "raw", KubernetesAccess: "read-only", SandboxMode: "danger-full-access", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create Git candidate role: %v", err)
	}
	gitSnapshot, err := repository.GetAgentInstructionSnapshot(ctx, gitRole.ID)
	if err != nil {
		t.Fatalf("read Git candidate snapshot: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_instruction_sets set managed_by = 'git', source_type = 'git' where id = $1`, gitSnapshot.InstructionSet.ID); err != nil {
		t.Fatalf("stage Git-managed instruction set: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agents set managed_by = 'git' where id = $1`, gitSnapshot.Agent.ID); err != nil {
		t.Fatalf("stage Git-managed agent: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_role_definitions set managed_by = 'git' where id = $1`, gitSnapshot.RoleDefinition.ID); err != nil {
		t.Fatalf("stage Git-managed role definition: %v", err)
	}
	var agentVersionBefore int64
	if err := pool.QueryRow(ctx, `select record_version from matter_codex_agents where id = $1`, gitSnapshot.Agent.ID).Scan(&agentVersionBefore); err != nil {
		t.Fatalf("read agent version before denied binding: %v", err)
	}
	var auditsBefore int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_audit_events`).Scan(&auditsBefore); err != nil {
		t.Fatalf("read audit count before denied binding: %v", err)
	}
	if _, _, err := repository.UpsertMattermostBotIdentity(ctx, adminrepo.UpsertMattermostBotIdentityInput{
		ProjectID: firstProject.ID, RoleID: gitRole.ID, Username: "git-agent-bot", Status: "configured",
	}); !errors.Is(err, adminrepo.ErrBotIdentityBindingDenied) {
		t.Fatalf("Git-managed bot binding error = %v", err)
	}
	var deniedIdentityCount int
	var agentVersionAfter int64
	var auditsAfter int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_mattermost_bot_identities where role_id = $1`, gitRole.ID).Scan(&deniedIdentityCount); err != nil {
		t.Fatalf("read denied identity count: %v", err)
	}
	if err := pool.QueryRow(ctx, `select record_version from matter_codex_agents where id = $1`, gitSnapshot.Agent.ID).Scan(&agentVersionAfter); err != nil {
		t.Fatalf("read agent version after denied binding: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_audit_events`).Scan(&auditsAfter); err != nil {
		t.Fatalf("read audit count after denied binding: %v", err)
	}
	if deniedIdentityCount != 0 || agentVersionAfter != agentVersionBefore || auditsAfter != auditsBefore {
		t.Fatalf("denied binding leaked partial write: identities=%d version=%d/%d audits=%d/%d", deniedIdentityCount, agentVersionAfter, agentVersionBefore, auditsAfter, auditsBefore)
	}
	if _, err := repository.DetachInstructionSet(ctx, instructionsrepo.DetachInstructionSetInput{LegacyAgentRoleID: gitRole.ID, ActorRef: "owner"}); err != nil {
		t.Fatalf("explicit detach before bot binding: %v", err)
	}
	identity, created, err := repository.UpsertMattermostBotIdentity(ctx, adminrepo.UpsertMattermostBotIdentityInput{
		ProjectID: firstProject.ID, RoleID: gitRole.ID, Username: "git-agent-bot", Status: "configured",
	})
	if err != nil || !created || identity.ProjectID != firstProject.ID || identity.RoleID != gitRole.ID {
		t.Fatalf("post-detach bot binding=%#v created=%t error=%v", identity, created, err)
	}

	uiRole, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: firstProject.ID, Name: "ui-agent", RoleType: "worker", PromptTemplate: "# UI agent",
		PromptMode: "raw", KubernetesAccess: "read-only", SandboxMode: "danger-full-access", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create UI-managed role: %v", err)
	}
	if _, _, err := repository.UpsertMattermostBotIdentity(ctx, adminrepo.UpsertMattermostBotIdentityInput{
		ProjectID: secondProject.ID, RoleID: uiRole.ID, Username: "cross-project-bot", Status: "configured",
	}); !errors.Is(err, adminrepo.ErrBotIdentityBindingDenied) {
		t.Fatalf("cross-project bot binding error = %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_mattermost_bot_identities where role_id = $1`, uiRole.ID).Scan(&deniedIdentityCount); err != nil || deniedIdentityCount != 0 {
		t.Fatalf("cross-project binding leaked identity rows=%d error=%v", deniedIdentityCount, err)
	}
}

func TestUniversalPromptSeedStartupDualWrite(t *testing.T) {
	repositoryRoot := universalModelRepositoryRoot(t)
	previousBody := readPromptSeedBody(t, filepath.Join(repositoryRoot, "services/external/bot-service/internal/domain/service/prompt_seeds/history/v1/manager_coordinate_task.md"))
	currentBody := readPromptSeedBody(t, filepath.Join(repositoryRoot, "services/external/bot-service/internal/domain/service/prompt_seeds/manager_coordinate_task.md"))

	t.Run("fresh and restart", func(t *testing.T) {
		dsn := isolatedMigrationDSN(t, "universal_seed_fresh")
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := migrations.Run(ctx, dsn); err != nil {
			t.Fatalf("fresh migrations: %v", err)
		}
		pool := openMigrationPool(t, ctx, dsn)
		defer pool.Close()
		repository := postgresrepo.NewRepository(pool)
		changed, err := statusservice.SeedDefaultAgentPromptTemplates(ctx, repository)
		if err != nil || changed == 0 {
			t.Fatalf("fresh prompt seed changed=%d error=%v", changed, err)
		}
		restartedChanged, err := statusservice.SeedDefaultAgentPromptTemplates(ctx, repository)
		if err != nil || restartedChanged != 0 {
			t.Fatalf("restart prompt seed changed=%d error=%v", restartedChanged, err)
		}
		var roles, instructionVersions int
		if err := pool.QueryRow(ctx, `select (select count(*) from matter_codex_agent_roles), (select count(*) from matter_codex_instruction_versions)`).Scan(&roles, &instructionVersions); err != nil || roles != 0 || instructionVersions != 0 {
			t.Fatalf("fresh prompt seed created agent configuration: roles=%d versions=%d error=%v", roles, instructionVersions, err)
		}
	})

	t.Run("N-1 upgrade and idempotent restart", func(t *testing.T) {
		dsn := isolatedMigrationDSN(t, "universal_seed_upgrade")
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := migrations.RunTo(ctx, dsn, 29); err != nil {
			t.Fatalf("подготовка N-1 schema: %v", err)
		}
		pool := openMigrationPool(t, ctx, dsn)
		defer pool.Close()
		roleID := seedManagerPromptUpgradeFixture(t, ctx, pool, previousBody)
		if err := migrations.Run(ctx, dsn); err != nil {
			t.Fatalf("upgrade N-1 -> universal model: %v", err)
		}
		repository := postgresrepo.NewRepository(pool)
		before, err := repository.GetAgentInstructionSnapshot(ctx, roleID)
		if err != nil || before.InstructionVersion.Version != 1 || before.InstructionVersion.Markdown != previousBody {
			t.Fatalf("pre-startup seed snapshot=%#v error=%v", before, err)
		}
		changed, err := statusservice.SeedDefaultAgentPromptTemplates(ctx, repository)
		if err != nil || changed == 0 {
			t.Fatalf("upgrade prompt seed changed=%d error=%v", changed, err)
		}
		after, err := repository.GetAgentInstructionSnapshot(ctx, roleID)
		if err != nil || after.InstructionVersion.Version != 2 || after.InstructionVersion.Markdown != currentBody || after.InstructionVersion.ActorRef != "startup-prompt-seed" {
			t.Fatalf("post-startup seed snapshot=%#v error=%v", after, err)
		}
		var legacyBody, templateBody string
		if err := pool.QueryRow(ctx, `select coalesce(prompt_template, '') from matter_codex_agent_roles where id = $1`, roleID).Scan(&legacyBody); err != nil {
			t.Fatalf("read upgraded legacy prompt: %v", err)
		}
		if err := pool.QueryRow(ctx, `select body from matter_codex_agent_prompt_templates where profile_name = 'manager' and template_key = 'coordinate_task'`).Scan(&templateBody); err != nil {
			t.Fatalf("read upgraded prompt template: %v", err)
		}
		if legacyBody != currentBody || templateBody != currentBody {
			t.Fatalf("startup seed projections diverged: legacy=%t template=%t", legacyBody == currentBody, templateBody == currentBody)
		}
		restartedChanged, err := statusservice.SeedDefaultAgentPromptTemplates(ctx, repository)
		if err != nil || restartedChanged != 0 {
			t.Fatalf("idempotent restart changed=%d error=%v", restartedChanged, err)
		}
		restarted, err := repository.GetAgentInstructionSnapshot(ctx, roleID)
		if err != nil || restarted.InstructionVersion.Version != 2 || restarted.InstructionSet.RecordVersion != after.InstructionSet.RecordVersion {
			t.Fatalf("restart snapshot=%#v error=%v", restarted, err)
		}
	})

	t.Run("Git-managed fail-closed rollback", func(t *testing.T) {
		dsn := isolatedMigrationDSN(t, "universal_seed_git_denied")
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := migrations.RunTo(ctx, dsn, 29); err != nil {
			t.Fatalf("подготовка N-1 schema: %v", err)
		}
		pool := openMigrationPool(t, ctx, dsn)
		defer pool.Close()
		roleID := seedManagerPromptUpgradeFixture(t, ctx, pool, previousBody)
		if err := migrations.Run(ctx, dsn); err != nil {
			t.Fatalf("upgrade N-1 -> universal model: %v", err)
		}
		repository := postgresrepo.NewRepository(pool)
		before, err := repository.GetAgentInstructionSnapshot(ctx, roleID)
		if err != nil {
			t.Fatalf("read Git-managed candidate snapshot: %v", err)
		}
		if _, err := pool.Exec(ctx, `update matter_codex_instruction_sets set managed_by = 'git', source_type = 'git' where id = $1`, before.InstructionSet.ID); err != nil {
			t.Fatalf("stage Git-managed instruction set: %v", err)
		}
		if _, err := pool.Exec(ctx, `update matter_codex_agents set managed_by = 'git' where id = $1`, before.Agent.ID); err != nil {
			t.Fatalf("stage Git-managed agent: %v", err)
		}
		if _, err := pool.Exec(ctx, `update matter_codex_role_definitions set managed_by = 'git' where id = $1`, before.RoleDefinition.ID); err != nil {
			t.Fatalf("stage Git-managed role definition: %v", err)
		}
		if _, err := statusservice.SeedDefaultAgentPromptTemplates(ctx, repository); !errors.Is(err, instructionsrepo.ErrManagedByGit) {
			t.Fatalf("Git-managed startup seed error = %v", err)
		}
		after, err := repository.GetAgentInstructionSnapshot(ctx, roleID)
		if err != nil || after.InstructionVersion.Version != 1 || after.InstructionVersion.Markdown != previousBody || after.InstructionSet.RecordVersion != before.InstructionSet.RecordVersion {
			t.Fatalf("denied startup seed snapshot=%#v error=%v", after, err)
		}
		var legacyBody, templateBody string
		if err := pool.QueryRow(ctx, `select coalesce(prompt_template, '') from matter_codex_agent_roles where id = $1`, roleID).Scan(&legacyBody); err != nil {
			t.Fatalf("read denied legacy prompt: %v", err)
		}
		if err := pool.QueryRow(ctx, `select body from matter_codex_agent_prompt_templates where profile_name = 'manager' and template_key = 'coordinate_task'`).Scan(&templateBody); err != nil {
			t.Fatalf("read denied prompt template: %v", err)
		}
		if legacyBody != previousBody || templateBody != previousBody {
			t.Fatalf("denied startup seed leaked partial write: legacy=%t template=%t", legacyBody == previousBody, templateBody == previousBody)
		}
	})
}

func readPromptSeedBody(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение prompt seed %s: %v", filepath.Base(path), err)
	}
	return strings.TrimSpace(string(body)) + "\n"
}

func seedManagerPromptUpgradeFixture(t *testing.T, ctx context.Context, db migrationExecutor, previousBody string) int64 {
	t.Helper()
	var projectID, roleID int64
	if err := db.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Seed Project', 'seed-project') returning id`).Scan(&projectID); err != nil {
		t.Fatalf("seed prompt project: %v", err)
	}
	if err := db.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, prompt_template, prompt_mode, kubernetes_access, sandbox_mode) values ($1, 'manager', 'manager', $2, 'template', 'read-only', 'danger-full-access') returning id`, projectID, previousBody).Scan(&roleID); err != nil {
		t.Fatalf("seed previous agent prompt: %v", err)
	}
	if _, err := db.Exec(ctx, `insert into matter_codex_agent_prompt_templates(profile_name, template_key, body) values ('manager', 'coordinate_task', $1)`, previousBody); err != nil {
		t.Fatalf("seed previous prompt template: %v", err)
	}
	return roleID
}
