//go:build postgres

package migrations_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	instructionsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/instructions"
	workspacesrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/workspaces"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
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

	concurrentErrors := make(chan error, 2)
	for index := range 2 {
		index := index
		go func() {
			_, _, publishErr := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
				ProjectID: projectID, Name: "assistant", RoleType: "worker",
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
select organization_scope, id, 100, repeat('x', 262145), public.digest(repeat('x', 262145), 'sha256'), 'test'
from matter_codex_instruction_sets where id = $1
`, snapshot.InstructionSet.ID); err == nil {
		t.Fatal("instruction version accepted Markdown above 256 KiB")
	}
	if _, err := pool.Exec(ctx, `
insert into matter_codex_instruction_versions(
	organization_scope, instruction_set_id, version, markdown, content_sha256, actor_ref
)
select organization_scope, id, 100, 'wrong digest', decode(repeat('00', 32), 'hex'), 'test'
from matter_codex_instruction_sets where id = $1
`, snapshot.InstructionSet.ID); err == nil {
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
	if _, err := pool.Exec(ctx, `delete from matter_codex_projects where id = $1`, projectID); err == nil {
		t.Fatal("legacy project delete bypassed universal RESTRICT binding")
	}

	if _, err := pool.Exec(ctx, `update matter_codex_instruction_sets set managed_by = 'git', source_type = 'git', source_ref = '{"path":"AGENTS.md"}', source_revision = 'synthetic-revision' where id = $1`, snapshot.InstructionSet.ID); err != nil {
		t.Fatalf("stage Git-managed instruction set: %v", err)
	}
	var promptBeforeDenied string
	if err := pool.QueryRow(ctx, `select coalesce(prompt_template, '') from matter_codex_agent_roles where id = $1`, role.ID).Scan(&promptBeforeDenied); err != nil {
		t.Fatalf("read prompt before denied mutation: %v", err)
	}
	if _, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: projectID, Name: "assistant", RoleType: "worker", PromptTemplate: "# Denied",
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
	if err != nil || detached.InstructionSet.ManagedBy != "ui" || !strings.Contains(detached.InstructionSet.Provenance, "synthetic-revision") {
		t.Fatalf("detach result = %#v, error=%v", detached.InstructionSet, err)
	}
	var detachAudits int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_audit_events where event_type = 'instruction_set.detached' and resource_name = $1`, fmt.Sprintf("legacy-agent-role:%d", role.ID)).Scan(&detachAudits); err != nil || detachAudits != 1 {
		t.Fatalf("detach audit count = %d, error=%v", detachAudits, err)
	}
	afterDetach := "# After detach"
	if _, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: projectID, Name: "assistant", RoleType: "worker", PromptTemplate: afterDetach,
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
