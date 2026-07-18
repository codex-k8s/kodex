package migrations_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var migrationSchemaSequence atomic.Uint64

const exactNMinusOneSHA = "e61d5aec0829de38aeaf71a0ee7d9e5fc6126a82"

type runtimeGuardMutationBarrier struct {
	name         string
	input        securityrepo.ClusterAdminBindingInput
	mutationSQL  string
	mutationArgs []any
}

func TestPublicBoundaryMigrationFresh(t *testing.T) {
	dsn := isolatedMigrationDSN(t, "fresh")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("fresh migrations: %v", err)
	}
	pool := openMigrationPool(t, ctx, dsn)
	defer pool.Close()

	rows, err := pool.Query(ctx, `select column_name from information_schema.columns where table_schema = current_schema() and table_name = 'matter_codex_interaction_capabilities' order by column_name`)
	if err != nil {
		t.Fatalf("чтение колонок capability: %v", err)
	}
	columns := map[string]bool{}
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("чтение колонки capability: %v", err)
		}
		columns[column] = true
	}
	rows.Close()
	if !columns["token_hash"] || columns["token"] || columns["capability"] {
		t.Fatalf("небезопасная схема capability: %#v", columns)
	}

	repository := postgresrepo.NewRepository(pool)
	allowed, err := repository.AdmitExistingClusterAdminBinding(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: 1, ProjectID: 1, ChatID: 1, ChatSlug: "missing-admin-chat", MattermostChannelID: "missing-channel",
		ActorUser: "test", Operation: "test.fresh.binding.admission",
	})
	if err != nil || allowed {
		t.Fatalf("fresh install создал cluster-admin binding: allowed=%t error=%v", allowed, err)
	}
	rawToken := "synthetic-capability-never-persisted"
	tokenHash := sha256.Sum256([]byte(rawToken))
	contextHash := sha256.Sum256([]byte("synthetic-context"))
	now := time.Now().UTC()
	issue := securityrepo.IssueCapabilityInput{
		TokenHash: tokenHash[:], Kind: "action", Operation: "action;view=projects",
		ResourceType: "project", ResourceID: "17", ChannelID: "channel-1", PostBinding: "post-1",
		ActorUserID: "owner-id", ActorUserName: "owner", InstallationScope: "single-installation",
		WorkspaceScope: "workspace-1", SessionScope: "session-1", ContextHash: contextHash[:],
		IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := repository.IssueInteractionCapability(ctx, issue); err != nil {
		t.Fatalf("IssueInteractionCapability() error = %v", err)
	}
	var rawStored bool
	if err := pool.QueryRow(ctx, `select exists(select 1 from matter_codex_interaction_capabilities where token_hash = convert_to($1, 'UTF8'))`, rawToken).Scan(&rawStored); err != nil {
		t.Fatalf("проверка хранения capability: %v", err)
	}
	if rawStored {
		t.Fatal("в хранилище обнаружено исходное значение capability")
	}
	consume := securityrepo.ConsumeCapabilityInput{
		TokenHash: tokenHash[:], Kind: issue.Kind, Operation: issue.Operation,
		ResourceType: issue.ResourceType, ResourceID: issue.ResourceID, ChannelID: issue.ChannelID,
		PostBinding: issue.PostBinding, ActorUserID: issue.ActorUserID, ContextHash: contextHash[:], Now: now,
	}
	if _, err := repository.ConsumeInteractionCapability(ctx, consume); err != nil {
		t.Fatalf("ConsumeInteractionCapability() error = %v", err)
	}
	if _, err := repository.ConsumeInteractionCapability(ctx, consume); !errors.Is(err, securityrepo.ErrCapabilityConsumed) {
		t.Fatalf("повторное потребление error = %v", err)
	}
	concurrentTokenHash := sha256.Sum256([]byte("synthetic-concurrent-capability"))
	issue.TokenHash = concurrentTokenHash[:]
	if err := repository.IssueInteractionCapability(ctx, issue); err != nil {
		t.Fatalf("IssueInteractionCapability(concurrent) error = %v", err)
	}
	consume.TokenHash = concurrentTokenHash[:]
	poolOne := openMigrationSingleConnectionPool(t, ctx, dsn)
	defer poolOne.Close()
	poolTwo := openMigrationSingleConnectionPool(t, ctx, dsn)
	defer poolTwo.Close()
	repositoryOne := postgresrepo.NewRepository(poolOne)
	repositoryTwo := postgresrepo.NewRepository(poolTwo)
	checkResults := make(chan error, 2)
	checkStart := make(chan struct{})
	checkReady := make(chan struct{}, 2)
	for _, concurrentRepository := range []*postgresrepo.Repository{repositoryOne, repositoryTwo} {
		go func(repo *postgresrepo.Repository) {
			checkReady <- struct{}{}
			<-checkStart
			_, checkErr := repo.CheckInteractionCapability(ctx, consume)
			checkResults <- checkErr
		}(concurrentRepository)
	}
	<-checkReady
	<-checkReady
	close(checkStart)
	for range 2 {
		if checkErr := <-checkResults; checkErr != nil {
			t.Fatalf("двухconnection pre-admission check: %v", checkErr)
		}
	}
	var checkedState string
	if err := pool.QueryRow(ctx, `select status from matter_codex_interaction_capabilities where token_hash = $1`, concurrentTokenHash[:]).Scan(&checkedState); err != nil || checkedState != "unused" {
		t.Fatalf("denied/indeterminate check изменил capability: status=%q error=%v", checkedState, err)
	}

	results := make(chan error, 2)
	start := make(chan struct{})
	ready := make(chan struct{}, 2)
	for _, concurrentRepository := range []*postgresrepo.Repository{repositoryOne, repositoryTwo} {
		go func(repo *postgresrepo.Repository) {
			ready <- struct{}{}
			<-start
			_, consumeErr := repo.ConsumeInteractionCapability(ctx, consume)
			results <- consumeErr
		}(concurrentRepository)
	}
	<-ready
	<-ready
	close(start)
	successes := 0
	replays := 0
	for range 2 {
		consumeErr := <-results
		switch {
		case consumeErr == nil:
			successes++
		case errors.Is(consumeErr, securityrepo.ErrCapabilityConsumed):
			replays++
		default:
			t.Fatalf("конкурентное consume error = %v", consumeErr)
		}
	}
	if successes != 1 || replays != 1 {
		t.Fatalf("конкурентное consume: successes=%d replays=%d", successes, replays)
	}
	pendingTokenHash := sha256.Sum256([]byte("synthetic-pending-card-capability"))
	issue.TokenHash = pendingTokenHash[:]
	issue.State = securityrepo.CapabilityStatePending
	if err := repository.IssueInteractionCapability(ctx, issue); err != nil {
		t.Fatalf("IssueInteractionCapability(pending) error = %v", err)
	}
	consume.TokenHash = pendingTokenHash[:]
	if _, err := repository.CheckInteractionCapability(ctx, consume); !errors.Is(err, securityrepo.ErrCapabilityInactive) {
		t.Fatalf("pending capability usable before card update: %v", err)
	}
	if err := repository.TransitionInteractionCapabilities(ctx, securityrepo.TransitionCapabilitiesInput{
		TokenHashes: [][]byte{pendingTokenHash[:]}, From: securityrepo.CapabilityStatePending, To: securityrepo.CapabilityStateUnused,
	}); err != nil {
		t.Fatalf("activate pending capability: %v", err)
	}
	if _, err := repository.CheckInteractionCapability(ctx, consume); err != nil {
		t.Fatalf("activated capability unusable: %v", err)
	}
	if err := repository.TransitionInteractionCapabilities(ctx, securityrepo.TransitionCapabilitiesInput{
		TokenHashes: [][]byte{pendingTokenHash[:]}, From: securityrepo.CapabilityStateUnused, To: securityrepo.CapabilityStateRevoked,
	}); err != nil {
		t.Fatalf("revoke active capability: %v", err)
	}
	if _, err := repository.CheckInteractionCapability(ctx, consume); !errors.Is(err, securityrepo.ErrCapabilityInactive) {
		t.Fatalf("revoked capability remained usable: %v", err)
	}

	if _, _, err := repository.UpsertAgentProfile(ctx, adminrepo.UpsertAgentProfileInput{
		Name: "new-admin", Role: "admin", KubernetesAccess: "cluster-admin", Enabled: true,
	}); !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
		t.Fatalf("новый cluster-admin profile error = %v", err)
	}
	var freshProjectID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Fresh', 'fresh') returning id`).Scan(&freshProjectID); err != nil {
		t.Fatalf("подготовка fresh project: %v", err)
	}
	if _, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: freshProjectID, Name: "new-admin", RoleType: "admin", PromptMode: "template",
		KubernetesAccess: "cluster-admin", SandboxMode: "danger-full-access", AdvancedSettings: "{}", Enabled: true,
	}); !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
		t.Fatalf("новая fresh cluster-admin role error = %v", err)
	}
	var freshRoleCount int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_agent_roles where project_id = $1`, freshProjectID).Scan(&freshRoleCount); err != nil || freshRoleCount != 0 {
		t.Fatalf("fresh install сохранил новый cluster-admin grant: count=%d error=%v", freshRoleCount, err)
	}

	testCapabilityCleanupRetention(t, ctx, pool, repository, now)
}

func TestRuntimeRoleNMinusOneAndAdversarialBoundary(t *testing.T) {
	ownerDSN := isolatedMigrationDSN(t, "runtime_role")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	roleName := "mc_runtime_" + strconv.FormatUint(migrationSchemaSequence.Add(1), 36)
	rolePassword := "synthetic-runtime-password"
	helperRoleName := "mc_runtime_helper_" + strconv.FormatUint(migrationSchemaSequence.Add(1), 36)
	helperRoleCreated := false
	if err := postgresrepo.ProvisionRuntimeDatabaseRole(ctx, ownerDSN, roleName, rolePassword); err != nil {
		t.Fatalf("provision runtime role: %v", err)
	}
	baseDSN := requiredMigrationDSN(t)
	roleIdentifier := pgx.Identifier{roleName}.Sanitize()
	helperRoleIdentifier := pgx.Identifier{helperRoleName}.Sanitize()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		cleanupPool := openMigrationPool(t, cleanupCtx, baseDSN)
		defer cleanupPool.Close()
		_, _ = cleanupPool.Exec(cleanupCtx, "drop owned by "+roleIdentifier)
		_, _ = cleanupPool.Exec(cleanupCtx, "drop role "+roleIdentifier)
		if helperRoleCreated {
			_, _ = cleanupPool.Exec(cleanupCtx, "drop owned by "+helperRoleIdentifier)
			_, _ = cleanupPool.Exec(cleanupCtx, "drop role "+helperRoleIdentifier)
		}
		var databaseName string
		if err := cleanupPool.QueryRow(cleanupCtx, `select current_database()`).Scan(&databaseName); err == nil {
			_, _ = cleanupPool.Exec(cleanupCtx, "grant temporary on database "+pgx.Identifier{databaseName}.Sanitize()+" to public")
		}
	})
	if err := migrations.RunTo(ctx, ownerDSN, 23); err != nil {
		t.Fatalf("runtime-role migration through v23: %v", err)
	}
	ownerPool := openMigrationPool(t, ctx, ownerDSN)
	var projectID int64
	if err := ownerPool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Runtime proof', 'runtime-proof') returning id`).Scan(&projectID); err != nil {
		ownerPool.Close()
		t.Fatalf("seed runtime proof project: %v", err)
	}
	if _, err := ownerPool.Exec(ctx, `
insert into matter_codex_agent_roles(project_id, name, role_type, kubernetes_access)
values ($1, 'n-minus-one', 'worker', 'read-only')
`, projectID); err != nil {
		ownerPool.Close()
		t.Fatalf("seed N-1 role: %v", err)
	}
	if tag, err := ownerPool.Exec(ctx, `
update matter_codex_agent_profiles
set kubernetes_access = 'cluster-admin', sandbox_mode = 'danger-full-access',
	openai_account_name = '', github_account_name = ''
where name = 'developer'
`); err != nil || tag.RowsAffected() != 1 {
		ownerPool.Close()
		t.Fatalf("seed frozen profile: rows=%d error=%v", tag.RowsAffected(), err)
	}
	ownerPool.Close()
	if err := migrations.RunForRuntimeRole(ctx, ownerDSN, roleName); err != nil {
		t.Fatalf("runtime-role migration through v25: %v", err)
	}
	runtimeDSN := migrationDSNForRole(t, ownerDSN, roleName, rolePassword)
	runtimePool := openMigrationPool(t, ctx, runtimeDSN)
	defer runtimePool.Close()
	if err := postgresrepo.ValidateRuntimeDatabaseRole(ctx, runtimePool); err != nil {
		t.Fatalf("runtime role attributes: %v", err)
	}
	runtimeRepository := postgresrepo.NewRepository(runtimePool)
	profiles, err := runtimeRepository.ListAgentProfiles(ctx)
	if err != nil || len(profiles) == 0 {
		t.Fatalf("N-1 profiles visibility: count=%d error=%v", len(profiles), err)
	}
	roles, err := runtimeRepository.ListAgentRoles(ctx, projectID)
	if err != nil || len(roles) == 0 {
		t.Fatalf("N-1 roles visibility: count=%d error=%v", len(roles), err)
	}
	if _, _, err := runtimeRepository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: projectID, Name: "runtime-dml", RoleType: "worker", PromptMode: "template",
		KubernetesAccess: "read-only", SandboxMode: "danger-full-access", AdvancedSettings: "{}", Enabled: true,
	}); err != nil {
		t.Fatalf("N-1 repository DML: %v", err)
	}
	if _, err := runtimePool.Exec(ctx, `select set_config('matter_codex.cluster_admin_freeze_writer', 'v24', false)`); err != nil {
		t.Fatalf("self-declared marker compatibility: %v", err)
	}
	if _, err := runtimePool.Exec(ctx, `create temporary table matter_codex_agent_profiles(id bigint)`); err == nil {
		t.Fatal("runtime role создала pg_temp shadow relation")
	}
	if _, err := runtimePool.Exec(ctx, `create table runtime_ddl_forbidden(id bigint)`); err == nil {
		t.Fatal("runtime role получила CREATE в service schema")
	}
	if _, err := runtimePool.Exec(ctx, `alter table matter_codex_agent_profiles disable trigger user`); err == nil {
		t.Fatal("runtime role отключила защитные триггеры")
	}
	if _, err := runtimePool.Exec(ctx, `delete from matter_codex_cluster_admin_subjects where subject_type = 'agent_profile'`); err == nil {
		t.Fatal("runtime role получила DELETE для immutable freeze snapshot")
	}
	if _, err := runtimePool.Exec(ctx, `update matter_codex_cluster_admin_subjects set privilege_state = '{}'::jsonb`); err == nil {
		t.Fatal("runtime role получила UPDATE для immutable freeze snapshot")
	}
	if tag, err := runtimePool.Exec(ctx, `update matter_codex_agent_profiles set description = 'mutated' where name = 'developer'`); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("frozen profile direct DML: rows=%d error=%v", tag.RowsAffected(), err)
	}
	if tag, err := runtimePool.Exec(ctx, `
insert into matter_codex_agent_profiles(name, role, kubernetes_access, sandbox_mode)
values ('forged-admin', 'admin', 'cluster-admin', 'danger-full-access')
`); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("forged cluster-admin direct DML: rows=%d error=%v", tag.RowsAffected(), err)
	}

	ownerPool = openMigrationPool(t, ctx, ownerDSN)
	var schemaName string
	if err := ownerPool.QueryRow(ctx, `select current_schema()`).Scan(&schemaName); err != nil {
		ownerPool.Close()
		t.Fatalf("read runtime schema: %v", err)
	}
	if _, err := ownerPool.Exec(ctx, "create role "+helperRoleIdentifier+" nologin"); err != nil {
		ownerPool.Close()
		t.Fatalf("create assumable helper role: %v", err)
	}
	helperRoleCreated = true
	for _, statement := range []string{
		"grant usage on schema " + pgx.Identifier{schemaName}.Sanitize() + " to " + helperRoleIdentifier,
		"grant select, update on matter_codex_cluster_admin_subjects to " + helperRoleIdentifier,
		"grant " + helperRoleIdentifier + " to " + roleIdentifier,
	} {
		if _, err := ownerPool.Exec(ctx, statement); err != nil {
			ownerPool.Close()
			t.Fatalf("grant assumable helper role: %v", err)
		}
	}
	ownerPool.Close()

	tx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin SET ROLE proof: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "set local role "+helperRoleIdentifier); err != nil {
		t.Fatalf("SET ROLE helper: %v", err)
	}
	if tag, err := tx.Exec(ctx, `
update matter_codex_cluster_admin_subjects
set privilege_state = privilege_state || '{"membership_proof":true}'::jsonb
where subject_type = 'agent_profile'
`); err != nil || tag.RowsAffected() == 0 {
		t.Fatalf("SET ROLE adversarial DML proof: rows=%d error=%v", tag.RowsAffected(), err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback SET ROLE proof: %v", err)
	}
	if err := postgresrepo.ValidateRuntimeDatabaseRole(ctx, runtimePool); err == nil {
		t.Fatal("runtime validator принял assumable membership")
	}
	if err := postgresrepo.ProvisionRuntimeDatabaseRole(ctx, ownerDSN, roleName, rolePassword); err == nil {
		t.Fatal("runtime provisioning не обнаружил старый membership drift")
	}
}

func TestExactNMinusOneBinaryBootstrapsAfterV22V23V24Upgrade(t *testing.T) {
	ownerDSN := isolatedMigrationDSN(t, "exact_n_minus_one")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	binary := buildExactNMinusOneBinary(t, ctx)
	runExactNMinusOneBinary(t, ctx, binary, ownerDSN, true)
	if version, err := migrations.Version(ctx, ownerDSN); err != nil || version != 22 {
		t.Fatalf("exact N-1 bootstrap schema version = %d, error=%v", version, err)
	}

	roleName := "mc_exact_n_minus_one_" + strconv.FormatUint(migrationSchemaSequence.Add(1), 36)
	rolePassword := "synthetic-exact-n-minus-one-password"
	if err := postgresrepo.ProvisionRuntimeDatabaseRole(ctx, ownerDSN, roleName, rolePassword); err != nil {
		t.Fatalf("provision exact N-1 runtime role: %v", err)
	}
	roleIdentifier := pgx.Identifier{roleName}.Sanitize()
	baseDSN := requiredMigrationDSN(t)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		cleanupPool := openMigrationPool(t, cleanupCtx, baseDSN)
		defer cleanupPool.Close()
		_, _ = cleanupPool.Exec(cleanupCtx, "drop owned by "+roleIdentifier)
		_, _ = cleanupPool.Exec(cleanupCtx, "drop role "+roleIdentifier)
	})
	if err := migrations.RunTo(ctx, ownerDSN, 23); err != nil {
		t.Fatalf("upgrade exact N-1 database through v23: %v", err)
	}
	stagingPool := openMigrationPool(t, ctx, ownerDSN)
	for _, statement := range []string{
		`insert into matter_codex_mattermost_bot_identities(
			project_id, role_id, username, mattermost_user_id, token_secret_ref, status
		)
		select role.project_id, role.id, 'synthetic-admin-' || role.id::text,
			'synthetic-admin-user-' || role.id::text, 'synthetic-admin-token-' || role.id::text, 'active'
		from matter_codex_agent_roles role
		where lower(trim(role.kubernetes_access)) = 'cluster-admin'
		on conflict (role_id) do nothing`,
		`update matter_codex_credentials
		set secret_content_sha256 = repeat('a', 64), secret_resource_uid = 'synthetic-credential-uid', secret_resource_version = '1'
		where trim(secret_ref) <> ''`,
		`update matter_codex_mattermost_bot_identities
		set secret_content_sha256 = repeat('b', 64), secret_resource_uid = 'synthetic-bot-uid', secret_resource_version = '1'
		where trim(token_secret_ref) <> ''`,
		`update matter_codex_project_runtime_variables
		set secret_content_sha256 = repeat('c', 64), secret_resource_uid = 'synthetic-variable-uid', secret_resource_version = '1'
		where trim(secret_ref) <> ''`,
	} {
		if _, err := stagingPool.Exec(ctx, statement); err != nil {
			stagingPool.Close()
			t.Fatalf("stage synthetic exact N-1 Secret integrity: %v", err)
		}
	}
	stagingPool.Close()
	if err := migrations.RunForRuntimeRole(ctx, ownerDSN, roleName); err != nil {
		t.Fatalf("upgrade exact N-1 database v22->v23->v24->v25: %v", err)
	}
	if version, err := migrations.Version(ctx, ownerDSN); err != nil || version != 25 {
		t.Fatalf("upgraded exact N-1 schema version = %d, error=%v", version, err)
	}

	ownerPool := openMigrationPool(t, ctx, ownerDSN)
	defer ownerPool.Close()
	var roleID int64
	if err := ownerPool.QueryRow(ctx, `
select id
from matter_codex_agent_roles
where name = 'mattercodex-admin' and kubernetes_access = 'cluster-admin'
`).Scan(&roleID); err != nil {
		t.Fatalf("find exact N-1 frozen role: %v", err)
	}
	var subjectsBefore, dependenciesBefore, deniedBefore int
	if err := ownerPool.QueryRow(ctx, `select count(*) from matter_codex_cluster_admin_subjects`).Scan(&subjectsBefore); err != nil {
		t.Fatalf("count frozen subjects before exact N-1: %v", err)
	}
	if err := ownerPool.QueryRow(ctx, `select count(*) from matter_codex_cluster_admin_dependencies`).Scan(&dependenciesBefore); err != nil {
		t.Fatalf("count frozen dependencies before exact N-1: %v", err)
	}
	if err := ownerPool.QueryRow(ctx, `select count(*) from matter_codex_audit_events where event_type = 'cluster_admin.freeze.denied'`).Scan(&deniedBefore); err != nil {
		t.Fatalf("count freeze denials before exact N-1: %v", err)
	}

	runtimeDSN := migrationDSNForRole(t, ownerDSN, roleName, rolePassword)
	runExactNMinusOneBinary(t, ctx, binary, runtimeDSN, false)
	var subjectsAfter, dependenciesAfter, deniedAfter int
	if err := ownerPool.QueryRow(ctx, `select count(*) from matter_codex_cluster_admin_subjects`).Scan(&subjectsAfter); err != nil {
		t.Fatalf("count frozen subjects after exact N-1: %v", err)
	}
	if err := ownerPool.QueryRow(ctx, `select count(*) from matter_codex_cluster_admin_dependencies`).Scan(&dependenciesAfter); err != nil {
		t.Fatalf("count frozen dependencies after exact N-1: %v", err)
	}
	if err := ownerPool.QueryRow(ctx, `select count(*) from matter_codex_audit_events where event_type = 'cluster_admin.freeze.denied'`).Scan(&deniedAfter); err != nil {
		t.Fatalf("count freeze denials after exact N-1: %v", err)
	}
	if subjectsAfter != subjectsBefore || dependenciesAfter != dependenciesBefore {
		t.Fatalf(
			"exact N-1 expanded freeze state: subjects=%d/%d dependencies=%d/%d",
			subjectsBefore, subjectsAfter, dependenciesBefore, dependenciesAfter,
		)
	}
	if deniedAfter != deniedBefore {
		t.Fatalf("exact N-1 produced freeze denials: before=%d after=%d", deniedBefore, deniedAfter)
	}
	var exact bool
	if err := ownerPool.QueryRow(ctx, `select matter_codex_cluster_admin_role_exact($1)`, roleID).Scan(&exact); err != nil || !exact {
		var revocations string
		if queryErr := ownerPool.QueryRow(ctx, `
select coalesce(string_agg(resource_type || ':' || resource_key || ':' || reason, ', ' order by revoked_at, resource_type, resource_key), '')
from matter_codex_cluster_admin_revocations
`).Scan(&revocations); queryErr != nil {
			t.Fatalf("read exact N-1 revocations: %v", queryErr)
		}
		t.Fatalf("exact N-1 expanded frozen role: exact=%t error=%v revocations=%s", exact, err, revocations)
	}
}

func buildExactNMinusOneBinary(t *testing.T, ctx context.Context) string {
	t.Helper()
	repositoryRootOutput, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("locate repository for exact N-1: %v", err)
	}
	repositoryRoot := strings.TrimSpace(string(repositoryRootOutput))
	worktreeRoot := filepath.Join(t.TempDir(), "source")
	command := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", worktreeRoot, exactNMinusOneSHA)
	command.Dir = repositoryRoot
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create exact N-1 worktree: %v: %s", err, strings.TrimSpace(string(output)))
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		cleanup := exec.CommandContext(cleanupCtx, "git", "worktree", "remove", "--force", worktreeRoot)
		cleanup.Dir = repositoryRoot
		if output, err := cleanup.CombinedOutput(); err != nil {
			t.Errorf("remove exact N-1 worktree: %v: %s", err, strings.TrimSpace(string(output)))
		}
	})
	binary := filepath.Join(t.TempDir(), "bot-service-exact-n-minus-one")
	command = exec.CommandContext(ctx, "go", "build", "-o", binary, "./services/external/bot-service/cmd/bot-service")
	command.Dir = worktreeRoot
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build exact N-1 %s: %v: %s", exactNMinusOneSHA, err, strings.TrimSpace(string(output)))
	}
	return binary
}

func runExactNMinusOneBinary(t *testing.T, ctx context.Context, binary string, dsn string, migrationsEnabled bool) {
	t.Helper()
	command := exec.CommandContext(ctx, binary)
	command.Env = append(os.Environ(),
		"MATTERCODEX_DATABASE_DSN="+dsn,
		"MATTERCODEX_STORAGE_MIGRATIONS_ENABLED="+strconv.FormatBool(migrationsEnabled),
		"MATTERCODEX_RUNTIME_ENABLED=false",
		"MATTERCODEX_BOT_SERVICE_HTTP_ADDR=127.0.0.1:0",
		"MATTERCODEX_LOCALE=en",
	)
	reader, writer := io.Pipe()
	command.Stdout = writer
	command.Stderr = writer
	lineCh := make(chan string, 64)
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
		close(lineCh)
	}()
	if err := command.Start(); err != nil {
		_ = writer.Close()
		t.Fatalf("start exact N-1 binary: %v", err)
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- command.Wait()
		_ = writer.Close()
	}()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	var output strings.Builder
	signaled := false
	for {
		select {
		case line, ok := <-lineCh:
			if ok {
				output.WriteString(line)
				output.WriteByte('\n')
				if !signaled && strings.Contains(line, "bot-service listening") {
					signaled = true
					if err := command.Process.Signal(os.Interrupt); err != nil {
						t.Fatalf("stop exact N-1 binary: %v", err)
					}
				}
			}
		case err := <-waitCh:
			logs := output.String()
			if err != nil {
				t.Fatalf("exact N-1 binary exited with error: %v; logs=%s", err, logs)
			}
			if !signaled {
				t.Fatalf("exact N-1 binary did not reach listen state; logs=%s", logs)
			}
			if strings.Contains(logs, "system agent role bootstrap failed") {
				t.Fatalf("exact N-1 bootstrap degraded after upgrade; logs=%s", logs)
			}
			return
		case <-timer.C:
			_ = command.Process.Kill()
			t.Fatalf("exact N-1 binary did not start before timeout")
		case <-ctx.Done():
			_ = command.Process.Kill()
			t.Fatalf("exact N-1 binary context ended: %v", ctx.Err())
		}
	}
}

func TestForwardOnlyDownKeepsVersionAndUpIsIdempotent(t *testing.T) {
	dsn := isolatedMigrationDSN(t, "forward_only")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("initial up: %v", err)
	}
	if err := migrations.DownOne(ctx, dsn); err == nil {
		t.Fatal("v25 down unexpectedly succeeded")
	}
	version, err := migrations.Version(ctx, dsn)
	if err != nil || version != 25 {
		t.Fatalf("version after failed down = %d, error=%v", version, err)
	}
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("repeated up after failed down: %v", err)
	}
	version, err = migrations.Version(ctx, dsn)
	if err != nil || version != 25 {
		t.Fatalf("version after repeated up = %d, error=%v", version, err)
	}
}

func TestV25DuplicateSessionKeyDiagnosticIsSanitizedAndAtomic(t *testing.T) {
	const privateCanary = "synthetic-private-v25-session-key"
	dsn := isolatedMigrationDSN(t, "v25_duplicate_privacy")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.RunTo(ctx, dsn, 24); err != nil {
		t.Fatalf("migrations through v24: %v", err)
	}
	pool := openMigrationPool(t, ctx, dsn)
	defer pool.Close()
	if _, err := pool.Exec(ctx, `
insert into matter_codex_cluster_admin_session_bindings(
	role_id, project_id, chat_id, session_key, mattermost_channel_id, privilege_state
) values
	(91001, 92001, 93001, $1, 'synthetic-channel-a', '{}'::jsonb),
	(91002, 92002, 93002, $1, 'synthetic-channel-b', '{}'::jsonb)
`, privateCanary); err != nil {
		t.Fatal("seed v24 duplicate session key")
	}
	var functionBefore string
	if err := pool.QueryRow(ctx, `select pg_get_functiondef('matter_codex_guard_cluster_admin_session()'::regprocedure)`).Scan(&functionBefore); err != nil {
		t.Fatalf("read v24 guard function: %v", err)
	}
	migrationErr, stderr := captureMigrationStderr(t, func() error { return migrations.Run(ctx, dsn) })
	if migrationErr == nil {
		t.Fatal("v25 accepted duplicate session key")
	}
	diagnostic := migrationErr.Error() + stderr
	if strings.Contains(diagnostic, privateCanary) {
		t.Fatal("v25 diagnostic disclosed a private session key")
	}
	if !strings.Contains(diagnostic, "MCV25_DUPLICATE_SESSION_KEY_GROUPS") || !strings.Contains(diagnostic, "duplicate_group_count=1") {
		t.Fatal("v25 diagnostic did not contain only the safe remediation code and count")
	}
	version, err := migrations.Version(ctx, dsn)
	if err != nil || version != 24 {
		t.Fatalf("schema version after rejected v25 = %d, error=%v", version, err)
	}
	var indexExists bool
	if err := pool.QueryRow(ctx, `select to_regclass('matter_codex_cluster_admin_session_bindings_session_key_uq') is not null`).Scan(&indexExists); err != nil || indexExists {
		t.Fatalf("v25 unique index remained after rollback: exists=%t error=%v", indexExists, err)
	}
	var functionAfter string
	if err := pool.QueryRow(ctx, `select pg_get_functiondef('matter_codex_guard_cluster_admin_session()'::regprocedure)`).Scan(&functionAfter); err != nil || functionAfter != functionBefore {
		t.Fatal("v25 guard function changed after rejected migration")
	}
	var inventoryCount int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_cluster_admin_revocations where resource_type = 'session_key'`).Scan(&inventoryCount); err != nil || inventoryCount != 0 {
		t.Fatalf("v25 inventory remained after rollback: count=%d error=%v", inventoryCount, err)
	}
	var auditDisclosure bool
	if err := pool.QueryRow(ctx, `select exists(select 1 from matter_codex_audit_events where summary like '%' || $1 || '%')`, privateCanary).Scan(&auditDisclosure); err != nil || auditDisclosure {
		t.Fatalf("v25 diagnostic leaked into audit: disclosed=%t error=%v", auditDisclosure, err)
	}
}

func TestV25PreflightLocksOutConcurrentDuplicateWriter(t *testing.T) {
	const privateCanary = "synthetic-private-v25-concurrent-key"
	dsn := isolatedMigrationDSN(t, "v25_duplicate_race")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.RunTo(ctx, dsn, 24); err != nil {
		t.Fatalf("migrations through v24: %v", err)
	}
	pool := openMigrationPool(t, ctx, dsn)
	defer pool.Close()
	if _, err := pool.Exec(ctx, `
insert into matter_codex_cluster_admin_session_bindings(
	role_id, project_id, chat_id, session_key, mattermost_channel_id, privilege_state
) values (91101, 92101, 93101, $1, 'synthetic-channel-a', '{}'::jsonb)
`, privateCanary); err != nil {
		t.Fatal("seed v24 session binding")
	}
	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin concurrent writer: %v", err)
	}
	defer func() { _ = writer.Rollback(ctx) }()
	if _, err := writer.Exec(ctx, `
insert into matter_codex_cluster_admin_session_bindings(
	role_id, project_id, chat_id, session_key, mattermost_channel_id, privilege_state
) values (91102, 92102, 93102, $1, 'synthetic-channel-b', '{}'::jsonb)
`, privateCanary); err != nil {
		t.Fatal("stage concurrent duplicate")
	}
	result := make(chan error, 1)
	go func() { result <- migrations.Run(ctx, dsn) }()
	select {
	case <-result:
		t.Fatal("v25 preflight did not wait for the concurrent writer")
	case <-time.After(200 * time.Millisecond):
	}
	if err := writer.Commit(ctx); err != nil {
		t.Fatalf("commit concurrent duplicate: %v", err)
	}
	migrationErr := <-result
	if migrationErr == nil {
		t.Fatal("v25 accepted a concurrently committed duplicate")
	}
	if strings.Contains(migrationErr.Error(), privateCanary) || !strings.Contains(migrationErr.Error(), "MCV25_DUPLICATE_SESSION_KEY_GROUPS") {
		t.Fatal("concurrent v25 diagnostic was not sanitized")
	}
	version, err := migrations.Version(ctx, dsn)
	if err != nil || version != 24 {
		t.Fatalf("schema version after concurrent duplicate = %d, error=%v", version, err)
	}
	var indexExists bool
	if err := pool.QueryRow(ctx, `select to_regclass('matter_codex_cluster_admin_session_bindings_session_key_uq') is not null`).Scan(&indexExists); err != nil || indexExists {
		t.Fatalf("v25 index remained after concurrent duplicate: exists=%t error=%v", indexExists, err)
	}
}

func captureMigrationStderr(t *testing.T, fn func() error) (error, string) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr capture: %v", err)
	}
	previous := os.Stderr
	os.Stderr = writer
	output := make(chan []byte, 1)
	go func() {
		captured, _ := io.ReadAll(reader)
		output <- captured
	}()
	runErr := fn()
	_ = writer.Close()
	os.Stderr = previous
	captured := <-output
	_ = reader.Close()
	return runErr, string(captured)
}

func TestPublicBoundaryMigrationUpgradePreservesConfiguredClusterAdmin(t *testing.T) {
	dsn := isolatedMigrationDSN(t, "upgrade")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.RunTo(ctx, dsn, 21); err != nil {
		t.Fatalf("migrations through historical base v21: %v", err)
	}
	pool := openMigrationPool(t, ctx, dsn)
	if _, err := pool.Exec(ctx, `update matter_codex_agent_profiles set kubernetes_access = 'cluster-admin' where name = 'developer'`); err != nil {
		t.Fatalf("подготовка существующего профиля: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_agent_profiles(name, role, enabled, kubernetes_access) values ('disabled-admin-profile', 'admin', false, 'cluster-admin')`); err != nil {
		t.Fatalf("подготовка выключенного профиля: %v", err)
	}
	var projectID, roleID, downgradeRoleID, disabledRoleID, chatID, runtimeVariableID int64
	var repositoryID, projectRepositoryID, chatRepositoryID int64
	var botBarrierRoleID, botBarrierChatID, runtimeBarrierRoleID, runtimeBarrierChatID, runtimeBarrierVariableID int64
	var repositoryBarrierRoleID, repositoryBarrierChatID, repositoryBarrierRepositoryID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Existing', 'existing') returning id`).Scan(&projectID); err != nil {
		t.Fatalf("подготовка проекта: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into matter_codex_agent_roles(
			project_id, name, role_type, openai_account_name, github_account_name, kubernetes_access
		) values ($1, 'existing-admin', 'admin', 'primary', 'agent', 'cluster-admin') returning id
	`, projectID).Scan(&roleID); err != nil {
		t.Fatalf("подготовка существующей роли: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into matter_codex_repositories(
			provider, owner, name, default_branch, github_account_name
		) values ('github', 'synthetic-owner', 'synthetic-repository', 'main', 'agent') returning id
	`).Scan(&repositoryID); err != nil {
		t.Fatalf("подготовка frozen repository: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into matter_codex_project_repositories(project_id, repository_id, is_default)
		values ($1, $2, true) returning id
	`, projectID, repositoryID).Scan(&projectRepositoryID); err != nil {
		t.Fatalf("подготовка frozen project repository binding: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into matter_codex_mattermost_bot_identities(
			project_id, role_id, username, mattermost_user_id, token_secret_ref, status
		) values ($1, $2, 'existing-admin-bot', 'synthetic-existing-admin-user', 'synthetic-existing-admin-token-ref', 'active')
	`, projectID, roleID); err != nil {
		t.Fatalf("подготовка существующей bot identity: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, kubernetes_access) values ($1, 'downgrade-admin', 'admin', 'cluster-admin') returning id`, projectID).Scan(&downgradeRoleID); err != nil {
		t.Fatalf("подготовка роли для downgrade: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into matter_codex_mattermost_bot_identities(
			project_id, role_id, username, mattermost_user_id, token_secret_ref, status
		) values ($1, $2, 'downgrade-admin-bot', 'synthetic-downgrade-admin-user', 'synthetic-downgrade-admin-token-ref', 'active')
	`, projectID, downgradeRoleID); err != nil {
		t.Fatalf("подготовка bot identity для downgrade: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, kubernetes_access, enabled) values ($1, 'disabled-admin', 'admin', 'cluster-admin', false) returning id`, projectID).Scan(&disabledRoleID); err != nil {
		t.Fatalf("подготовка выключенной роли: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into matter_codex_mattermost_bot_identities(
			project_id, role_id, username, mattermost_user_id, token_secret_ref, status
		) values ($1, $2, 'disabled-admin-bot', 'synthetic-disabled-admin-user', 'synthetic-disabled-admin-token-ref', 'active')
	`, projectID, disabledRoleID); err != nil {
		t.Fatalf("подготовка bot identity выключенной роли: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug) values ($1, 'channel-existing', 'Existing admin chat', 'existing-admin-chat') returning id`, projectID).Scan(&chatID); err != nil {
		t.Fatalf("подготовка существующего чата: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_chat_participants(chat_id, role_id) values ($1, $2)`, chatID, roleID); err != nil {
		t.Fatalf("подготовка существующей привязки: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into matter_codex_chat_repositories(chat_id, repository_id)
		values ($1, $2) returning id
	`, chatID, repositoryID).Scan(&chatRepositoryID); err != nil {
		t.Fatalf("подготовка frozen chat repository binding: %v", err)
	}
	seedRuntimeGuardBarrier := func(roleName string, channelID string, chatSlug string) (int64, int64) {
		t.Helper()
		var barrierRoleID int64
		if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, kubernetes_access) values ($1, $2, 'admin', 'cluster-admin') returning id`, projectID, roleName).Scan(&barrierRoleID); err != nil {
			t.Fatalf("подготовка barrier role %s: %v", roleName, err)
		}
		if _, err := pool.Exec(ctx, `
			insert into matter_codex_mattermost_bot_identities(
				project_id, role_id, username, mattermost_user_id, token_secret_ref, status
			) values ($1, $2, $3, $4, $5, 'active')
		`, projectID, barrierRoleID, roleName+"-bot", "synthetic-"+roleName+"-user", "synthetic-"+roleName+"-token-ref"); err != nil {
			t.Fatalf("подготовка barrier bot %s: %v", roleName, err)
		}
		var barrierChatID int64
		if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug) values ($1, $2, $3, $4) returning id`, projectID, channelID, roleName+" chat", chatSlug).Scan(&barrierChatID); err != nil {
			t.Fatalf("подготовка barrier chat %s: %v", roleName, err)
		}
		if _, err := pool.Exec(ctx, `insert into matter_codex_chat_participants(chat_id, role_id) values ($1, $2)`, barrierChatID, barrierRoleID); err != nil {
			t.Fatalf("подготовка barrier participant %s: %v", roleName, err)
		}
		return barrierRoleID, barrierChatID
	}
	botBarrierRoleID, botBarrierChatID = seedRuntimeGuardBarrier("bot-barrier-admin", "channel-bot-barrier", "bot-barrier-chat")
	runtimeBarrierRoleID, runtimeBarrierChatID = seedRuntimeGuardBarrier("runtime-barrier-admin", "channel-runtime-barrier", "runtime-barrier-chat")
	repositoryBarrierRoleID, repositoryBarrierChatID = seedRuntimeGuardBarrier("repository-barrier-admin", "channel-repository-barrier", "repository-barrier-chat")
	if err := pool.QueryRow(ctx, `
		insert into matter_codex_repositories(provider, owner, name, default_branch)
		values ('github', 'synthetic-owner', 'repository-barrier', 'main') returning id
	`).Scan(&repositoryBarrierRepositoryID); err != nil {
		t.Fatalf("подготовка repository barrier: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_chat_repositories(chat_id, repository_id) values ($1, $2)`, repositoryBarrierChatID, repositoryBarrierRepositoryID); err != nil {
		t.Fatalf("подготовка repository barrier binding: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into matter_codex_project_runtime_variables(
			project_id, name, slug, description, secret_ref, secret_key, sensitive, enabled
		) values ($1, 'SYNTHETIC_BARRIER_KEY', 'synthetic-barrier-key', 'Синтетическая barrier variable',
			'synthetic-barrier-secret-ref', 'value', true, true)
		returning id
	`, projectID).Scan(&runtimeBarrierVariableID); err != nil {
		t.Fatalf("подготовка barrier runtime variable: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_agent_role_runtime_variables(role_id, variable_id) values ($1, $2)`, runtimeBarrierRoleID, runtimeBarrierVariableID); err != nil {
		t.Fatalf("подготовка barrier runtime binding: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into matter_codex_project_runtime_variables(
			project_id, name, slug, description, secret_ref, secret_key, sensitive, enabled
		) values ($1, 'SYNTHETIC_ADMIN_KEY', 'synthetic-admin-key', 'Синтетическая переменная',
			'synthetic-runtime-secret-ref', 'value', true, true)
		returning id
	`, projectID).Scan(&runtimeVariableID); err != nil {
		t.Fatalf("подготовка существующей runtime variable: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_agent_role_runtime_variables(role_id, variable_id) values ($1, $2)`, roleID, runtimeVariableID); err != nil {
		t.Fatalf("подготовка существующей runtime binding: %v", err)
	}
	const existingSessionKey = "existing-admin-session"
	const rebindSessionKey = "rebind-admin-session"
	if _, err := pool.Exec(ctx, `
		insert into matter_codex_agent_sessions(
			session_key, project_id, chat_id, role_id, session_scope, mattermost_channel_id,
			mattermost_root_post_id, ttl_seconds, expires_at
		) values
			($1, $3, $4, $5, 'thread', 'channel-existing', 'root-existing', 3600, now() + interval '1 hour'),
			($2, $3, $4, $5, 'thread', 'channel-existing', 'root-rebind', 3600, now() + interval '1 hour')
	`, existingSessionKey, rebindSessionKey, projectID, chatID, roleID); err != nil {
		t.Fatalf("подготовка существующей session binding: %v", err)
	}
	const blockedSessionKey = "blocked-admin-session"
	if _, err := pool.Exec(ctx, `
		insert into matter_codex_agent_sessions(
			session_key, project_id, chat_id, role_id, session_scope, mattermost_channel_id,
			mattermost_root_post_id, status, ttl_seconds, expires_at
		) values ($1, $2, $3, $4, 'thread', 'channel-existing', 'root-blocked', 'blocked', 3600, now() + interval '1 hour')
	`, blockedSessionKey, projectID, chatID, roleID); err != nil {
		t.Fatalf("подготовка provider-blocked session binding: %v", err)
	}
	pool.Close()
	if err := migrations.RunTo(ctx, dsn, 22); err != nil {
		t.Fatalf("upgrade historical base v21->current main v22: %v", err)
	}
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("upgrade v22->v23->v24->v25: %v", err)
	}
	pool = openMigrationPool(t, ctx, dsn)
	defer pool.Close()
	repository := postgresrepo.NewRepository(pool)
	for provider, accountName := range map[string]string{"openai": "primary", "github": "agent"} {
		var frozen bool
		var dependencyErr error
		if provider == "openai" {
			frozen, dependencyErr = repository.IsFrozenClusterAdminOpenAIAccount(ctx, accountName)
		} else {
			frozen, dependencyErr = repository.IsFrozenClusterAdminGitHubAccount(ctx, accountName)
		}
		if dependencyErr != nil || !frozen {
			t.Fatalf("frozen %s account dependency: frozen=%t error=%v", provider, frozen, dependencyErr)
		}
	}
	allowed, err := repository.AdmitExistingClusterAdmin(ctx, securityrepo.ClusterAdminAdmissionInput{
		SubjectType: "agent_profile", SubjectKey: "developer", ProfileName: "developer", ActorUser: "test", Operation: "test.profile.admission",
	})
	if err != nil || !allowed {
		t.Fatalf("существующий profile grant: allowed=%t error=%v", allowed, err)
	}
	allowed, err = repository.AdmitExistingClusterAdmin(ctx, securityrepo.ClusterAdminAdmissionInput{
		SubjectType: "agent_role", SubjectKey: strconv.FormatInt(roleID, 10), ProjectID: projectID,
		ProfileName: "spoofed-name", ActorUser: "test", Operation: "test.role.admission",
	})
	if err != nil || allowed {
		t.Fatalf("wrong-binding role grant: allowed=%t error=%v", allowed, err)
	}
	allowed, err = repository.AdmitExistingClusterAdminBinding(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: roleID, ProjectID: projectID, ChatID: chatID, ChatSlug: "existing-admin-chat",
		MattermostChannelID: "channel-existing", ActorUser: "test", Operation: "test.binding.admission",
	})
	if err != nil || !allowed {
		t.Fatalf("существующая точная привязка: allowed=%t error=%v", allowed, err)
	}
	allowed, err = repository.AdmitExistingClusterAdminBinding(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: roleID, ProjectID: projectID, ChatID: 0, ChatSlug: "new-admin-chat",
		MattermostChannelID: "channel-new", ActorUser: "test", Operation: "test.binding.admission",
	})
	if err != nil || allowed {
		t.Fatalf("новая привязка: allowed=%t error=%v", allowed, err)
	}
	allowed, err = repository.AdmitExistingClusterAdminBinding(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: roleID, ProjectID: projectID, ChatID: chatID, ChatSlug: "existing-admin-chat",
		MattermostChannelID: "channel-remapped", ActorUser: "test", Operation: "test.binding.channel-remap",
	})
	if err != nil || allowed {
		t.Fatalf("channel remap: allowed=%t error=%v", allowed, err)
	}
	allowed, err = repository.AdmitExistingClusterAdminBinding(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: roleID, ProjectID: projectID, ChatID: chatID, ChatSlug: "existing-admin-chat",
		MattermostChannelID: "channel-existing", SessionKey: existingSessionKey, ActorUser: "test", Operation: "test.binding.existing-session",
	})
	if err != nil || !allowed {
		t.Fatalf("существующая session binding: allowed=%t error=%v", allowed, err)
	}
	requiresGuard, err := repository.RequiresClusterAdminSessionGuard(ctx, roleID, existingSessionKey)
	if err != nil || !requiresGuard {
		t.Fatalf("классификация frozen cluster-admin session: required=%t error=%v", requiresGuard, err)
	}
	testFrozenSessionKeyRebindDenied(t, ctx, pool, repository, projectID, chatID, rebindSessionKey)
	persistenceCallback := false
	err = repository.WithExistingClusterAdminPersistenceGuard(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: roleID, ProjectID: projectID, ChatID: chatID, ChatSlug: "existing-admin-chat",
		MattermostChannelID: "channel-existing", SessionKey: existingSessionKey,
		ActorUser: "test", Operation: "test.binding.existing-session-persistence",
	}, func(guardedStore adminrepo.Repository) error {
		persistenceCallback = true
		_, updateErr := guardedStore.UpdateAgentSessionRuntime(ctx, adminrepo.UpdateAgentSessionRuntimeInput{SessionKey: existingSessionKey})
		return updateErr
	})
	if err != nil || !persistenceCallback {
		t.Fatalf("существующая persistence binding: callback=%t error=%v", persistenceCallback, err)
	}
	allowed, err = repository.AdmitExistingClusterAdminBinding(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: roleID, ProjectID: projectID, ChatID: chatID, ChatSlug: "existing-admin-chat",
		MattermostChannelID: "channel-existing", SessionKey: blockedSessionKey, ActorUser: "test", Operation: "test.binding.blocked-session",
	})
	if err != nil || allowed {
		t.Fatalf("provider-blocked session binding: allowed=%t error=%v", allowed, err)
	}
	if tag, err := pool.Exec(ctx, `update matter_codex_agent_sessions set status = 'idle' where session_key = $1`, blockedSessionKey); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("повторное включение provider-blocked session: rows=%d error=%v", tag.RowsAffected(), err)
	}
	allowed, err = repository.AdmitExistingClusterAdminBinding(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: roleID, ProjectID: projectID, ChatID: chatID, ChatSlug: "existing-admin-chat",
		MattermostChannelID: "channel-existing", SessionKey: "new-admin-session", ActorUser: "test", Operation: "test.binding.new-session",
	})
	if err != nil || allowed {
		t.Fatalf("новая session binding: allowed=%t error=%v", allowed, err)
	}
	if _, _, err := repository.UpsertAgentProfile(ctx, adminrepo.UpsertAgentProfileInput{
		Name: "developer", Role: "developer", Description: "Generic implementation and review-fix agent profile seed", Enabled: true,
		OpenAIAccountName: "primary", GitHubAccountName: "agent", KubernetesAccess: "cluster-admin",
		SandboxMode: "danger-full-access",
	}); err != nil {
		t.Fatalf("обновление существующего profile grant: %v", err)
	}
	if _, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: projectID, Name: "existing-admin", RoleType: "admin", PromptMode: "template",
		OpenAIAccountName: "primary", GitHubAccountName: "agent",
		KubernetesAccess: "cluster-admin", SandboxMode: "danger-full-access", AdvancedSettings: "{}", Enabled: true,
	}); err != nil {
		t.Fatalf("обновление существующего role grant: %v", err)
	}
	if _, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: projectID, Name: "new-admin", RoleType: "admin", PromptMode: "template",
		KubernetesAccess: "cluster-admin", SandboxMode: "danger-full-access", AdvancedSettings: "{}", Enabled: true,
	}); !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
		t.Fatalf("новый cluster-admin role error = %v", err)
	}
	if _, _, err := repository.CreateChat(ctx, adminrepo.CreateChatInput{
		ProjectID: projectID, MattermostChannelID: "channel-existing", Name: "Existing admin chat", Slug: "existing-admin-chat",
		ChatType: "custom", Settings: "{}", RoleIDs: []int64{roleID}, RepositoryIDs: []int64{repositoryID},
	}); err != nil {
		t.Fatalf("точный frozen CreateChat: %v", err)
	}
	var frozenChatRepositoryCount int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_chat_repositories where chat_id = $1 and repository_id = $2`, chatID, repositoryID).Scan(&frozenChatRepositoryCount); err != nil || frozenChatRepositoryCount != 1 {
		t.Fatalf("точный CreateChat изменил frozen repository binding: count=%d error=%v", frozenChatRepositoryCount, err)
	}
	if _, _, err := repository.CreateChat(ctx, adminrepo.CreateChatInput{
		ProjectID: projectID, MattermostChannelID: "channel-new", Name: "New admin chat", Slug: "new-admin-chat",
		ChatType: "single_custom", Settings: "{}", RoleIDs: []int64{roleID},
	}); !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
		t.Fatalf("новый cluster-admin chat binding error = %v", err)
	}
	var newChatCount int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_chats where project_id = $1 and slug = 'new-admin-chat'`, projectID).Scan(&newChatCount); err != nil || newChatCount != 0 {
		t.Fatalf("новая запрещённая привязка создала чат: count=%d error=%v", newChatCount, err)
	}
	if _, _, err := repository.CreateChat(ctx, adminrepo.CreateChatInput{
		ProjectID: projectID, MattermostChannelID: "channel-remapped", Name: "Existing admin chat", Slug: "existing-admin-chat",
		ChatType: "single_custom", Settings: "{}", RoleIDs: []int64{roleID},
	}); !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
		t.Fatalf("channel remap cluster-admin chat error = %v", err)
	}
	var persistedChannel string
	if err := pool.QueryRow(ctx, `select mattermost_channel_id from matter_codex_chats where id = $1`, chatID).Scan(&persistedChannel); err != nil || persistedChannel != "channel-existing" {
		t.Fatalf("channel remap изменил чат: channel=%q error=%v", persistedChannel, err)
	}
	testFrozenBindingTwoConnectionNegatives(t, ctx, dsn, securityrepo.ClusterAdminBindingInput{
		RoleID: roleID, ProjectID: projectID, ChatID: chatID, ChatSlug: "existing-admin-chat",
		MattermostChannelID: "channel-existing", SessionKey: existingSessionKey,
		ActorUser: "test", Operation: "test.binding.two-connection",
	})
	testInteractionResourceScopeAdmission(t, ctx, repository, projectID)
	testRuntimeGuardPrivilegeMutationBarriers(t, ctx, dsn, []runtimeGuardMutationBarrier{
		{
			name: "bot delete", input: securityrepo.ClusterAdminBindingInput{
				RoleID: botBarrierRoleID, ProjectID: projectID, ChatID: botBarrierChatID,
				ChatSlug: "bot-barrier-chat", MattermostChannelID: "channel-bot-barrier",
			},
			mutationSQL: `delete from matter_codex_mattermost_bot_identities where role_id = $1`, mutationArgs: []any{botBarrierRoleID},
		},
		{
			name: "runtime variable disable", input: securityrepo.ClusterAdminBindingInput{
				RoleID: runtimeBarrierRoleID, ProjectID: projectID, ChatID: runtimeBarrierChatID,
				ChatSlug: "runtime-barrier-chat", MattermostChannelID: "channel-runtime-barrier",
			},
			mutationSQL: `update matter_codex_project_runtime_variables set enabled = false where id = $1`, mutationArgs: []any{runtimeBarrierVariableID},
		},
		{
			name: "repository delete", input: securityrepo.ClusterAdminBindingInput{
				RoleID: repositoryBarrierRoleID, ProjectID: projectID, ChatID: repositoryBarrierChatID,
				ChatSlug: "repository-barrier-chat", MattermostChannelID: "channel-repository-barrier",
			},
			mutationSQL: `delete from matter_codex_repositories where id = $1`, mutationArgs: []any{repositoryBarrierRepositoryID},
		},
	})
	tag, err := pool.Exec(ctx, `update matter_codex_project_runtime_variables set enabled = true where id = $1`, runtimeBarrierVariableID)
	if err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("повторное включение revoked runtime variable: rows=%d error=%v", tag.RowsAffected(), err)
	}
	var runtimeBarrierEnabled bool
	if err := pool.QueryRow(ctx, `select enabled from matter_codex_project_runtime_variables where id = $1`, runtimeBarrierVariableID).Scan(&runtimeBarrierEnabled); err != nil || runtimeBarrierEnabled {
		t.Fatalf("revoked runtime variable повторно включена: enabled=%t error=%v", runtimeBarrierEnabled, err)
	}
	testFrozenPrivilegeMutationMatrix(
		t, ctx, pool, projectID, roleID, chatID, runtimeVariableID,
		repositoryID, projectRepositoryID, chatRepositoryID, existingSessionKey,
	)
	testNMinusOneRepositoryGuard(t, ctx, pool, projectID, roleID, chatID)
	secretMismatchSideEffect := false
	if err := repository.WithExistingClusterAdminRuntimeGuard(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: roleID, ProjectID: projectID, ChatID: chatID, ChatSlug: "existing-admin-chat",
		MattermostChannelID: "channel-existing", SessionKey: existingSessionKey,
		ActorUser: "test", Operation: "test.runtime.secret_integrity_denied",
	}, func() error {
		secretMismatchSideEffect = true
		return adminrepo.ErrClusterAdminAdmissionDenied
	}); !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) || !secretMismatchSideEffect {
		t.Fatalf("secret integrity guard denial: callback=%t error=%v", secretMismatchSideEffect, err)
	}
	var secretMismatchDeniedAudit int
	if err := pool.QueryRow(ctx, `
		select count(*) from matter_codex_audit_events
		where event_type = 'cluster_admin.runtime.denied'
			and summary = 'test.runtime.secret_integrity_denied: denied'
	`).Scan(&secretMismatchDeniedAudit); err != nil || secretMismatchDeniedAudit != 1 {
		t.Fatalf("secret integrity denied audit=%d error=%v", secretMismatchDeniedAudit, err)
	}
	if tag, err := pool.Exec(ctx, `update matter_codex_agent_sessions set status = 'blocked' where session_key = $1`, existingSessionKey); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("монотонная блокировка frozen session: rows=%d error=%v", tag.RowsAffected(), err)
	}
	if tag, err := pool.Exec(ctx, `update matter_codex_agent_sessions set status = 'idle' where session_key = $1`, existingSessionKey); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("повторное включение blocked frozen session: rows=%d error=%v", tag.RowsAffected(), err)
	}
	testMonotonicRoleRevocations(t, ctx, pool, repository, projectID, downgradeRoleID, disabledRoleID)
	if _, _, err := repository.UpsertAgentProfile(ctx, adminrepo.UpsertAgentProfileInput{
		Name: "disabled-admin-profile", Role: "admin", Enabled: true,
		OpenAIAccountName: "primary", GitHubAccountName: "primary", KubernetesAccess: "cluster-admin",
		SandboxMode: "danger-full-access",
	}); !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
		t.Fatalf("включение frozen disabled profile error = %v", err)
	}
	var disabledProfileEnabled bool
	if err := pool.QueryRow(ctx, `select enabled from matter_codex_agent_profiles where name = 'disabled-admin-profile'`).Scan(&disabledProfileEnabled); err != nil || disabledProfileEnabled {
		t.Fatalf("disabled profile включён: enabled=%t error=%v", disabledProfileEnabled, err)
	}

	testAtomicProfileDowngradeBarrier(t, ctx, dsn, "developer")
	runtimeBinding := securityrepo.ClusterAdminBindingInput{
		RoleID: roleID, ProjectID: projectID, ChatID: chatID, ChatSlug: "existing-admin-chat",
		MattermostChannelID: "channel-existing", SessionKey: existingSessionKey,
		ActorUser: "test", Operation: "test.runtime.side_effect",
	}
	testAtomicCreateChatRevocationBarrier(t, ctx, dsn, runtimeBinding)
	runtimeSideEffect := false
	if err := repository.WithExistingClusterAdminRuntimeGuard(ctx, runtimeBinding, func() error {
		runtimeSideEffect = true
		return nil
	}); !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) || runtimeSideEffect {
		t.Fatalf("runtime guard после committed participant disable: side_effect=%t error=%v", runtimeSideEffect, err)
	}
	testAtomicRoleDeleteBarrier(t, ctx, dsn, projectID, roleID)
	runtimeSideEffect = false
	if err := repository.WithExistingClusterAdminRuntimeGuard(ctx, runtimeBinding, func() error {
		runtimeSideEffect = true
		return nil
	}); !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) || runtimeSideEffect {
		t.Fatalf("runtime guard после committed delete: side_effect=%t error=%v", runtimeSideEffect, err)
	}
	var runtimeDeniedAuditCount int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_audit_events where event_type = 'cluster_admin.runtime.denied'`).Scan(&runtimeDeniedAuditCount); err != nil || runtimeDeniedAuditCount < 2 {
		t.Fatalf("runtime denial audit count = %d, error=%v", runtimeDeniedAuditCount, err)
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_audit_events where event_type in ('cluster_admin.admission.allowed', 'cluster_admin.admission.denied')`).Scan(&auditCount); err != nil {
		t.Fatalf("чтение cluster-admin audit: %v", err)
	}
	if auditCount < 4 {
		t.Fatalf("cluster-admin audit count = %d, want at least 4", auditCount)
	}
	testMonotonicAccountDependencyRevocations(t, ctx, pool)
}

func testMonotonicAccountDependencyRevocations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, account := range []struct {
		name  string
		table string
	}{
		{name: "primary", table: "matter_codex_openai_accounts"},
		{name: "agent", table: "matter_codex_github_accounts"},
	} {
		var originalStatus string
		if err := pool.QueryRow(ctx, "select status from "+account.table+" where name = $1", account.name).Scan(&originalStatus); err != nil {
			t.Fatalf("read %s account status: %v", account.name, err)
		}
		if tag, err := pool.Exec(ctx, "update "+account.table+" set status = 'disabled' where name = $1", account.name); err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("disable frozen %s account: rows=%d error=%v", account.name, tag.RowsAffected(), err)
		}
		if tag, err := pool.Exec(ctx, "update "+account.table+" set status = $2 where name = $1", account.name, originalStatus); err != nil || tag.RowsAffected() != 0 {
			t.Fatalf("restore frozen %s account: rows=%d error=%v", account.name, tag.RowsAffected(), err)
		}
	}
	var credentialID int64
	var originalCredentialStatus string
	if err := pool.QueryRow(ctx, `
		select credential.id, credential.status
		from matter_codex_credentials credential
		join matter_codex_openai_accounts account on account.credential_id = credential.id
		where account.name = 'primary'
	`).Scan(&credentialID, &originalCredentialStatus); err != nil {
		t.Fatalf("read frozen credential status: %v", err)
	}
	if tag, err := pool.Exec(ctx, `update matter_codex_credentials set status = 'disabled' where id = $1`, credentialID); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("disable frozen credential: rows=%d error=%v", tag.RowsAffected(), err)
	}
	if tag, err := pool.Exec(ctx, `update matter_codex_credentials set status = $2 where id = $1`, credentialID, originalCredentialStatus); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("restore frozen credential: rows=%d error=%v", tag.RowsAffected(), err)
	}
	var dependencyRevocations int
	if err := pool.QueryRow(ctx, `
		select count(*)
		from matter_codex_cluster_admin_revocations
		where resource_type = 'profile_dependency'
			and split_part(resource_key, ':', 1) = 'developer'
	`).Scan(&dependencyRevocations); err != nil || dependencyRevocations < 3 {
		t.Fatalf("profile dependency revocations=%d error=%v", dependencyRevocations, err)
	}
	var profileExact bool
	if err := pool.QueryRow(ctx, `select matter_codex_cluster_admin_profile_exact('developer')`).Scan(&profileExact); err != nil || profileExact {
		t.Fatalf("profile admission survived dependency revocation: exact=%t error=%v", profileExact, err)
	}
}

func testFrozenSessionKeyRebindDenied(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *postgresrepo.Repository,
	projectID int64,
	chatID int64,
	sessionKey string,
) {
	t.Helper()
	var ordinaryRoleID int64
	if err := pool.QueryRow(ctx, `
		insert into matter_codex_agent_roles(
			project_id, name, role_type, kubernetes_access, sandbox_mode, enabled
		) values ($1, 'ordinary-rebind', 'worker', 'read-only', 'workspace-write', true)
		returning id
	`, projectID).Scan(&ordinaryRoleID); err != nil {
		t.Fatalf("создание обычной роли для session_key regression: %v", err)
	}
	if tag, err := pool.Exec(ctx, `delete from matter_codex_agent_sessions where session_key = $1`, sessionKey); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("delete frozen session перед rebind: rows=%d error=%v", tag.RowsAffected(), err)
	}
	requiresGuard, err := repository.RequiresClusterAdminSessionGuard(ctx, ordinaryRoleID, sessionKey)
	if err != nil || !requiresGuard {
		t.Fatalf("rebound frozen key classifier: required=%t error=%v", requiresGuard, err)
	}
	insertSQL := `
		insert into matter_codex_agent_sessions(
			session_key, project_id, chat_id, role_id, session_scope, mattermost_channel_id,
			mattermost_root_post_id, ttl_seconds, expires_at
		) values ($1, $2, $3, $4, 'thread', $5, $6, 3600, now() + interval '1 hour')
	`
	if tag, err := pool.Exec(ctx, insertSQL, sessionKey, projectID, chatID, ordinaryRoleID, "channel-existing", "root-rebound"); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("INSERT повторно использовал frozen session_key: rows=%d error=%v", tag.RowsAffected(), err)
	}
	if tag, err := pool.Exec(ctx, insertSQL+` on conflict (session_key) do update set role_id = excluded.role_id`, sessionKey, projectID, chatID, ordinaryRoleID, "channel-forged", "root-forged"); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("UPSERT повторно использовал frozen session_key: rows=%d error=%v", tag.RowsAffected(), err)
	}
	const ordinarySessionKey = "ordinary-unique-session"
	if tag, err := pool.Exec(ctx, insertSQL, ordinarySessionKey, projectID, chatID, ordinaryRoleID, "channel-existing", "root-ordinary"); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("обычный уникальный session_key не создан: rows=%d error=%v", tag.RowsAffected(), err)
	}
	if required, err := repository.RequiresClusterAdminSessionGuard(ctx, ordinaryRoleID, ordinarySessionKey); err != nil || required {
		t.Fatalf("обычный уникальный key ошибочно guarded: required=%t error=%v", required, err)
	}
	if tag, err := pool.Exec(ctx, `update matter_codex_agent_sessions set session_key = $1 where session_key = $2`, sessionKey, ordinarySessionKey); err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("UPDATE привязал ordinary row к frozen session_key: rows=%d error=%v", tag.RowsAffected(), err)
	}
	var reboundRows int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_agent_sessions where session_key = $1`, sessionKey).Scan(&reboundRows); err != nil || reboundRows != 0 {
		t.Fatalf("frozen session_key восстановлен после revoke: rows=%d error=%v", reboundRows, err)
	}
}

func testFrozenPrivilegeMutationMatrix(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	projectID int64,
	roleID int64,
	chatID int64,
	runtimeVariableID int64,
	repositoryID int64,
	projectRepositoryID int64,
	chatRepositoryID int64,
	sessionKey string,
) {
	t.Helper()
	var auditBefore int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_audit_events where event_type = 'cluster_admin.freeze.denied'`).Scan(&auditBefore); err != nil {
		t.Fatalf("чтение исходного freeze audit: %v", err)
	}
	tests := []struct {
		name string
		sql  string
		args []any
	}{
		{name: "profile name", sql: `update matter_codex_agent_profiles set name = 'mutated-developer' where name = 'developer'`},
		{name: "profile role", sql: `update matter_codex_agent_profiles set role = 'reviewer' where name = 'developer'`},
		{name: "profile description", sql: `update matter_codex_agent_profiles set description = 'mutated' where name = 'developer'`},
		{name: "profile OpenAI account", sql: `update matter_codex_agent_profiles set openai_account_name = 'other' where name = 'developer'`},
		{name: "profile GitHub account", sql: `update matter_codex_agent_profiles set github_account_name = 'other' where name = 'developer'`},
		{name: "profile sandbox", sql: `update matter_codex_agent_profiles set sandbox_mode = 'workspace-write' where name = 'developer'`},
		{name: "profile config", sql: `update matter_codex_agent_profiles set config_overlay = 'mutated' where name = 'developer'`},
		{name: "profile prompt template insert", sql: `insert into matter_codex_agent_prompt_templates(profile_name, template_key, body) values ('developer', 'system', 'mutated instruction')`},
		{name: "role project", sql: `update matter_codex_agent_roles set project_id = project_id + 999 where id = $1`, args: []any{roleID}},
		{name: "role name", sql: `update matter_codex_agent_roles set name = 'mutated-admin' where id = $1`, args: []any{roleID}},
		{name: "role type", sql: `update matter_codex_agent_roles set role_type = 'developer' where id = $1`, args: []any{roleID}},
		{name: "role description", sql: `update matter_codex_agent_roles set description = 'mutated' where id = $1`, args: []any{roleID}},
		{name: "role prompt", sql: `update matter_codex_agent_roles set prompt_template = 'mutated' where id = $1`, args: []any{roleID}},
		{name: "role GitHub account", sql: `update matter_codex_agent_roles set github_account_name = 'other' where id = $1`, args: []any{roleID}},
		{name: "role OpenAI account", sql: `update matter_codex_agent_roles set openai_account_name = 'other' where id = $1`, args: []any{roleID}},
		{name: "role sandbox", sql: `update matter_codex_agent_roles set sandbox_mode = 'workspace-write' where id = $1`, args: []any{roleID}},
		{name: "role config", sql: `update matter_codex_agent_roles set config_overlay = 'mutated' where id = $1`, args: []any{roleID}},
		{name: "role advanced settings", sql: `update matter_codex_agent_roles set advanced_settings = '{"mutated":true}'::jsonb where id = $1`, args: []any{roleID}},
		{name: "role bot identity", sql: `update matter_codex_agent_roles set bot_identity = 'other' where id = $1`, args: []any{roleID}},
		{name: "project GitHub binding", sql: `update matter_codex_projects set github_account_name = 'other' where id = $1`, args: []any{projectID}},
		{name: "project name", sql: `update matter_codex_projects set name = 'Mutated project' where id = $1`, args: []any{projectID}},
		{name: "project description", sql: `update matter_codex_projects set description = 'mutated instruction context' where id = $1`, args: []any{projectID}},
		{name: "project advanced settings", sql: `update matter_codex_projects set advanced_settings = '{"instruction":"mutated"}'::jsonb where id = $1`, args: []any{projectID}},
		{name: "project slug", sql: `update matter_codex_projects set slug = 'mutated-project' where id = $1`, args: []any{projectID}},
		{name: "project Mattermost team", sql: `update matter_codex_projects set mattermost_team_id = 'mutated-team' where id = $1`, args: []any{projectID}},
		{name: "project GitHub owner", sql: `update matter_codex_projects set github_owner = 'mutated-owner' where id = $1`, args: []any{projectID}},
		{name: "project GitHub owner type", sql: `update matter_codex_projects set github_owner_type = 'user' where id = $1`, args: []any{projectID}},
		{name: "OpenAI account", sql: `update matter_codex_openai_accounts set model_policy = 'mutated' where name = 'primary'`},
		{name: "OpenAI credential", sql: `update matter_codex_credentials set secret_ref = 'mutated-openai-ref' where id = (select credential_id from matter_codex_openai_accounts where name = 'primary')`},
		{name: "OpenAI credential same-ref content", sql: `update matter_codex_credentials set secret_content_sha256 = 'mutated-content' where id = (select credential_id from matter_codex_openai_accounts where name = 'primary')`},
		{name: "GitHub account", sql: `update matter_codex_github_accounts set secret_ref = 'mutated-github-ref' where name = 'agent'`},
		{name: "GitHub credential", sql: `update matter_codex_credentials set secret_ref = 'mutated-github-credential-ref' where id = (select credential_id from matter_codex_github_accounts where name = 'agent')`},
		{name: "GitHub credential same-ref UID", sql: `update matter_codex_credentials set secret_resource_uid = 'mutated-uid' where id = (select credential_id from matter_codex_github_accounts where name = 'agent')`},
		{name: "repository branch", sql: `update matter_codex_repositories set default_branch = 'mutated' where id = $1`, args: []any{repositoryID}},
		{name: "repository account", sql: `update matter_codex_repositories set github_account_name = 'primary' where id = $1`, args: []any{repositoryID}},
		{name: "project repository binding", sql: `update matter_codex_project_repositories set is_default = false where id = $1`, args: []any{projectRepositoryID}},
		{name: "chat repository binding", sql: `update matter_codex_chat_repositories set repository_id = repository_id + 999 where id = $1`, args: []any{chatRepositoryID}},
		{name: "bot project", sql: `update matter_codex_mattermost_bot_identities set project_id = project_id + 999 where role_id = $1`, args: []any{roleID}},
		{name: "bot username", sql: `update matter_codex_mattermost_bot_identities set username = 'mutated-admin-bot' where role_id = $1`, args: []any{roleID}},
		{name: "bot display name", sql: `update matter_codex_mattermost_bot_identities set display_name = 'mutated' where role_id = $1`, args: []any{roleID}},
		{name: "bot user binding", sql: `update matter_codex_mattermost_bot_identities set mattermost_user_id = 'mutated-admin-user' where role_id = $1`, args: []any{roleID}},
		{name: "bot token binding", sql: `update matter_codex_mattermost_bot_identities set token_secret_ref = 'mutated-token-ref' where role_id = $1`, args: []any{roleID}},
		{name: "bot token same-ref content", sql: `update matter_codex_mattermost_bot_identities set secret_content_sha256 = 'mutated-content' where role_id = $1`, args: []any{roleID}},
		{name: "bot status", sql: `update matter_codex_mattermost_bot_identities set status = 'pending' where role_id = $1`, args: []any{roleID}},
		{name: "runtime variable project", sql: `update matter_codex_project_runtime_variables set project_id = project_id + 999 where id = $1`, args: []any{runtimeVariableID}},
		{name: "runtime variable", sql: `update matter_codex_project_runtime_variables set secret_ref = 'mutated-runtime-ref' where id = $1`, args: []any{runtimeVariableID}},
		{name: "runtime variable same-ref content", sql: `update matter_codex_project_runtime_variables set secret_content_sha256 = 'mutated-content' where id = $1`, args: []any{runtimeVariableID}},
		{name: "runtime binding remap", sql: `update matter_codex_agent_role_runtime_variables set variable_id = variable_id + 999 where role_id = $1 and variable_id = $2`, args: []any{roleID, runtimeVariableID}},
		{name: "chat project", sql: `update matter_codex_chats set project_id = project_id + 999 where id = $1`, args: []any{chatID}},
		{name: "chat channel", sql: `update matter_codex_chats set mattermost_channel_id = 'mutated-channel' where id = $1`, args: []any{chatID}},
		{name: "chat name", sql: `update matter_codex_chats set name = 'Mutated chat' where id = $1`, args: []any{chatID}},
		{name: "chat slug", sql: `update matter_codex_chats set slug = 'mutated-chat' where id = $1`, args: []any{chatID}},
		{name: "chat description", sql: `update matter_codex_chats set description = 'mutated instruction context' where id = $1`, args: []any{chatID}},
		{name: "chat type", sql: `update matter_codex_chats set chat_type = 'mutated' where id = $1`, args: []any{chatID}},
		{name: "chat root issue", sql: `update matter_codex_chats set root_github_issue = 'mutated#1' where id = $1`, args: []any{chatID}},
		{name: "chat work policy", sql: `update matter_codex_chats set work_policy = 'mutated policy' where id = $1`, args: []any{chatID}},
		{name: "chat settings", sql: `update matter_codex_chats set settings = '{"instruction":"mutated"}'::jsonb where id = $1`, args: []any{chatID}},
		{name: "chat system purpose", sql: `update matter_codex_chats set system_purpose = 'manager' where id = $1`, args: []any{chatID}},
		{name: "participant remap", sql: `update matter_codex_chat_participants set chat_id = chat_id + 999 where chat_id = $1 and role_id = $2`, args: []any{chatID, roleID}},
		{name: "session key", sql: `update matter_codex_agent_sessions set session_key = 'mutated-session' where session_key = $1`, args: []any{sessionKey}},
		{name: "session project", sql: `update matter_codex_agent_sessions set project_id = project_id + 999 where session_key = $1`, args: []any{sessionKey}},
		{name: "session chat", sql: `update matter_codex_agent_sessions set chat_id = chat_id + 999 where session_key = $1`, args: []any{sessionKey}},
		{name: "session role", sql: `update matter_codex_agent_sessions set role_id = role_id + 999 where session_key = $1`, args: []any{sessionKey}},
		{name: "session channel", sql: `update matter_codex_agent_sessions set mattermost_channel_id = 'mutated-channel' where session_key = $1`, args: []any{sessionKey}},
		{name: "session root", sql: `update matter_codex_agent_sessions set mattermost_root_post_id = 'mutated-root' where session_key = $1`, args: []any{sessionKey}},
		{name: "session OpenAI account", sql: `update matter_codex_agent_sessions set openai_account_name = 'other' where session_key = $1`, args: []any{sessionKey}},
		{name: "session Kubernetes namespace", sql: `update matter_codex_agent_sessions set kubernetes_namespace = 'mutated-namespace' where session_key = $1`, args: []any{sessionKey}},
		{name: "session pod", sql: `update matter_codex_agent_sessions set pod_name = 'mutated-pod' where session_key = $1`, args: []any{sessionKey}},
		{name: "session PVC", sql: `update matter_codex_agent_sessions set pvc_name = 'mutated-pvc' where session_key = $1`, args: []any{sessionKey}},
		{name: "session token Secret binding", sql: `update matter_codex_agent_sessions set token_secret_ref = 'mutated-session-token-ref' where session_key = $1`, args: []any{sessionKey}},
		{name: "session token same-ref version", sql: `update matter_codex_agent_sessions set secret_resource_version = 'mutated-version' where session_key = $1`, args: []any{sessionKey}},
		{name: "session capabilities", sql: `update matter_codex_agent_sessions set capabilities = '["mutated"]'::jsonb where session_key = $1`, args: []any{sessionKey}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tag, err := pool.Exec(ctx, test.sql, test.args...)
			if err != nil {
				t.Fatalf("запрещённая mutation завершилась ошибкой вместо fail-closed no-op: %v", err)
			}
			if tag.RowsAffected() != 0 {
				t.Fatalf("запрещённая mutation изменила %d строк", tag.RowsAffected())
			}
			var exact bool
			if err := pool.QueryRow(ctx, `select matter_codex_cluster_admin_role_exact($1)`, roleID).Scan(&exact); err != nil || !exact {
				t.Fatalf("mutation нарушила точное frozen state: exact=%t error=%v", exact, err)
			}
			if err := pool.QueryRow(ctx, `select matter_codex_cluster_admin_profile_exact('developer')`).Scan(&exact); err != nil || !exact {
				t.Fatalf("mutation нарушила точный frozen profile: exact=%t error=%v", exact, err)
			}
		})
	}
	var newVariableID int64
	if err := pool.QueryRow(ctx, `
		insert into matter_codex_project_runtime_variables(
			project_id, name, slug, secret_ref, secret_key, sensitive, enabled
		) values ($1, 'SYNTHETIC_EXTRA_KEY', 'synthetic-extra-key', 'synthetic-extra-ref', 'value', true, true)
		returning id
	`, projectID).Scan(&newVariableID); err != nil {
		t.Fatalf("подготовка новой runtime variable: %v", err)
	}
	tag, err := pool.Exec(ctx, `insert into matter_codex_agent_role_runtime_variables(role_id, variable_id) values ($1, $2)`, roleID, newVariableID)
	if err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("новая runtime binding: rows=%d error=%v", tag.RowsAffected(), err)
	}
	var newRepositoryID int64
	if err := pool.QueryRow(ctx, `
		insert into matter_codex_repositories(provider, owner, name, default_branch, github_account_name)
		values ('github', 'synthetic-owner', 'synthetic-extra-repository', 'main', 'agent') returning id
	`).Scan(&newRepositoryID); err != nil {
		t.Fatalf("подготовка новой repository dependency: %v", err)
	}
	for _, binding := range []struct {
		name string
		sql  string
		args []any
	}{
		{name: "project repository", sql: `insert into matter_codex_project_repositories(project_id, repository_id) values ($1, $2)`, args: []any{projectID, newRepositoryID}},
		{name: "chat repository", sql: `insert into matter_codex_chat_repositories(chat_id, repository_id) values ($1, $2)`, args: []any{chatID, newRepositoryID}},
	} {
		tag, err := pool.Exec(ctx, binding.sql, binding.args...)
		if err != nil || tag.RowsAffected() != 0 {
			t.Fatalf("новая %s binding: rows=%d error=%v", binding.name, tag.RowsAffected(), err)
		}
	}
	var auditAfter int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_audit_events where event_type = 'cluster_admin.freeze.denied'`).Scan(&auditAfter); err != nil {
		t.Fatalf("чтение итогового freeze audit: %v", err)
	}
	if auditAfter-auditBefore != len(tests)+3 {
		t.Fatalf("freeze audit delta=%d, want=%d", auditAfter-auditBefore, len(tests)+3)
	}
	var unsafeAuditCount int
	if err := pool.QueryRow(ctx, `
		select count(*)
		from matter_codex_audit_events
		where event_type = 'cluster_admin.freeze.denied'
			and (
				summary like '%synthetic-existing-admin-token-ref%'
				or summary like '%synthetic-runtime-secret-ref%'
				or resource_name like '%synthetic-existing-admin-token-ref%'
				or resource_name like '%synthetic-runtime-secret-ref%'
			)
	`).Scan(&unsafeAuditCount); err != nil || unsafeAuditCount != 0 {
		t.Fatalf("freeze audit содержит значение synthetic secret: count=%d error=%v", unsafeAuditCount, err)
	}
}

func testMonotonicRoleRevocations(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *postgresrepo.Repository,
	projectID int64,
	downgradeRoleID int64,
	disabledRoleID int64,
) {
	t.Helper()
	tag, err := pool.Exec(ctx, `update matter_codex_agent_roles set kubernetes_access = 'read-only' where id = $1`, downgradeRoleID)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("монотонный downgrade: rows=%d error=%v", tag.RowsAffected(), err)
	}
	if _, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: projectID, Name: "downgrade-admin", RoleType: "admin", PromptMode: "template",
		KubernetesAccess: "cluster-admin", SandboxMode: "danger-full-access", AdvancedSettings: "{}", Enabled: true,
	}); !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
		t.Fatalf("восстановление downgraded роли error = %v", err)
	}
	if _, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: projectID, Name: "disabled-admin", RoleType: "admin", PromptMode: "template",
		KubernetesAccess: "cluster-admin", SandboxMode: "danger-full-access", AdvancedSettings: "{}", Enabled: true,
	}); !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
		t.Fatalf("включение frozen disabled роли error = %v", err)
	}
	var access string
	var enabled bool
	if err := pool.QueryRow(ctx, `select kubernetes_access from matter_codex_agent_roles where id = $1`, downgradeRoleID).Scan(&access); err != nil || access != "read-only" {
		t.Fatalf("downgraded роль восстановлена: access=%q error=%v", access, err)
	}
	if err := pool.QueryRow(ctx, `select enabled from matter_codex_agent_roles where id = $1`, disabledRoleID).Scan(&enabled); err != nil || enabled {
		t.Fatalf("disabled роль включена: enabled=%t error=%v", enabled, err)
	}
}

func testNMinusOneRepositoryGuard(t *testing.T, ctx context.Context, pool *pgxpool.Pool, projectID int64, roleID int64, chatID int64) {
	t.Helper()
	roleName := "mc_n_minus_one_" + strconv.FormatUint(migrationSchemaSequence.Add(1), 36)
	roleIdentifier := pgx.Identifier{roleName}.Sanitize()
	var schemaName string
	if err := pool.QueryRow(ctx, `select current_schema()`).Scan(&schemaName); err != nil {
		t.Fatalf("current schema для N-1 proof: %v", err)
	}
	schemaIdentifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := pool.Exec(ctx, "create role "+roleIdentifier+" nologin"); err != nil {
		t.Fatalf("создание N-1 роли: %v", err)
	}
	cleanupDSN := pool.Config().ConnString()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cleanupPool, err := pgxpool.New(cleanupCtx, cleanupDSN)
		if err != nil {
			t.Errorf("подключение для очистки N-1 роли: %v", err)
			return
		}
		defer cleanupPool.Close()
		if _, err := cleanupPool.Exec(cleanupCtx, "drop owned by "+roleIdentifier); err != nil {
			t.Errorf("очистка прав N-1 роли: %v", err)
			return
		}
		if _, err := cleanupPool.Exec(cleanupCtx, "drop role "+roleIdentifier); err != nil {
			t.Errorf("удаление N-1 роли: %v", err)
		}
	})
	for _, statement := range []string{
		"grant usage on schema " + schemaIdentifier + " to " + roleIdentifier,
		"grant select, insert, update, delete on all tables in schema " + schemaIdentifier + " to " + roleIdentifier,
		"grant usage, select on all sequences in schema " + schemaIdentifier + " to " + roleIdentifier,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("выдача синтетических N-1 прав: %v", err)
		}
	}
	runAsNMinusOne := func(operation func(pgx.Tx) error) error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, "set local role "+roleIdentifier); err != nil {
			return err
		}
		return operation(tx)
	}
	if err := runAsNMinusOne(func(tx pgx.Tx) error {
		var roleCount, profileCount int
		if err := tx.QueryRow(ctx, `select count(*) from matter_codex_agent_roles`).Scan(&roleCount); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `select count(*) from matter_codex_agent_profiles`).Scan(&profileCount); err != nil {
			return err
		}
		if roleCount == 0 || profileCount == 0 {
			return errors.New("N-1 runtime не видит профили или роли, нужные для bootstrap")
		}
		return nil
	}); err != nil {
		t.Fatalf("N-1 bootstrap visibility: %v", err)
	}
	var repositoryID, projectRepositoryID int64
	var repositoryProvider, repositoryOwner, repositoryName, defaultBranch, githubAccountName, mattermostChannel string
	var projectRepositoryMetadata string
	var projectRepositoryDefault bool
	if err := pool.QueryRow(ctx, `
select repository.id, binding.id, repository.provider, repository.owner, repository.name,
	repository.default_branch, repository.github_account_name, repository.mattermost_channel,
	binding.is_default, binding.metadata::text
from matter_codex_project_repositories binding
join matter_codex_repositories repository on repository.id = binding.repository_id
where binding.project_id = $1
order by binding.id
limit 1
`, projectID).Scan(
		&repositoryID, &projectRepositoryID, &repositoryProvider, &repositoryOwner, &repositoryName,
		&defaultBranch, &githubAccountName, &mattermostChannel, &projectRepositoryDefault, &projectRepositoryMetadata,
	); err != nil {
		t.Fatalf("load frozen repository for N-1 upsert: %v", err)
	}
	exactRepositoryUpsert := `
	insert into matter_codex_repositories(provider, owner, name, default_branch, github_account_name, mattermost_channel)
	values ($1, $2, $3, $4, $5, $6)
	on conflict (provider, owner, name) do update set
		default_branch = excluded.default_branch,
		github_account_name = excluded.github_account_name,
		mattermost_channel = excluded.mattermost_channel,
		updated_at = now()
	returning id
`
	exactProjectRepositoryUpsert := `
	insert into matter_codex_project_repositories(project_id, repository_id, is_default, metadata)
	values ($1, $2, $3, $4::jsonb)
	on conflict (project_id, repository_id) do update set
		is_default = excluded.is_default,
		metadata = excluded.metadata,
		updated_at = now()
	returning id
`
	if err := runAsNMinusOne(func(tx pgx.Tx) error {
		var actualRepositoryID, actualBindingID int64
		if err := tx.QueryRow(
			ctx, exactRepositoryUpsert,
			repositoryProvider, repositoryOwner, repositoryName, defaultBranch, githubAccountName, mattermostChannel,
		).Scan(&actualRepositoryID); err != nil {
			return err
		}
		if err := tx.QueryRow(
			ctx, exactProjectRepositoryUpsert,
			projectID, repositoryID, projectRepositoryDefault, projectRepositoryMetadata,
		).Scan(&actualBindingID); err != nil {
			return err
		}
		if actualRepositoryID != repositoryID || actualBindingID != projectRepositoryID {
			return fmt.Errorf("exact ON CONFLICT returned repository=%d binding=%d", actualRepositoryID, actualBindingID)
		}
		return nil
	}); err != nil {
		t.Fatalf("exact N-1 repository ON CONFLICT: %v", err)
	}
	if err := runAsNMinusOne(func(tx pgx.Tx) error {
		var ignored int64
		return tx.QueryRow(
			ctx, exactRepositoryUpsert,
			repositoryProvider, repositoryOwner, repositoryName, defaultBranch+"-remapped", githubAccountName, mattermostChannel,
		).Scan(&ignored)
	}); err == nil {
		t.Fatal("N-1 repository remap расширил frozen state")
	}
	if err := runAsNMinusOne(func(tx pgx.Tx) error {
		var newRepositoryID, ignored int64
		if err := tx.QueryRow(ctx, `
			insert into matter_codex_repositories(provider, owner, name, default_branch)
			values ('github', 'synthetic-owner', 'n-minus-one-expansion', 'main')
			returning id
		`).Scan(&newRepositoryID); err != nil {
			return err
		}
		return tx.QueryRow(
			ctx, exactProjectRepositoryUpsert,
			projectID, newRepositoryID, false, `{}`,
		).Scan(&ignored)
	}); err == nil {
		t.Fatal("N-1 project repository expansion прошла freeze guard")
	}
	if err := func() error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, "set local role "+roleIdentifier); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `select set_config('matter_codex.cluster_admin_freeze_writer', 'v24', true)`); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `select count(*) from matter_codex_agent_roles`).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return errors.New("самодекларируемый GUC изменил обычную видимость runtime")
		}
		return nil
	}(); err != nil {
		t.Fatalf("self-declared GUC isolation: %v", err)
	}
	baseRoleUpsert := `
		insert into matter_codex_agent_roles(
			project_id, name, role_type, description, prompt_template, prompt_mode,
			github_account_name, openai_account_name, kubernetes_access, sandbox_mode,
			config_overlay, advanced_settings, enabled, bot_identity
		) values ($1, $2, 'admin', '', null, 'template', '', '', 'cluster-admin',
			'danger-full-access', '', '{}'::jsonb, true, '')
		on conflict (project_id, name) do update set
			role_type = excluded.role_type, kubernetes_access = excluded.kubernetes_access,
			enabled = excluded.enabled, updated_at = now()
		returning id
	`
	for _, name := range []string{"existing-admin", "n-minus-one-new-admin"} {
		err := runAsNMinusOne(func(tx pgx.Tx) error {
			var id int64
			return tx.QueryRow(ctx, baseRoleUpsert, projectID, name).Scan(&id)
		})
		if err == nil {
			t.Fatalf("N-1 role upsert %q расширил frozen state", name)
		}
	}
	baseProfileUpsert := `
		insert into matter_codex_agent_profiles(
			name, role, description, enabled, openai_account_name, github_account_name,
			kubernetes_access, sandbox_mode, config_overlay
		) values ('developer', 'developer', '', true, 'primary', 'agent',
			'cluster-admin', 'danger-full-access', '')
		on conflict (name) do update set kubernetes_access = excluded.kubernetes_access,
			enabled = excluded.enabled, updated_at = now()
		returning id
	`
	if err := runAsNMinusOne(func(tx pgx.Tx) error {
		var id int64
		return tx.QueryRow(ctx, baseProfileUpsert).Scan(&id)
	}); err == nil {
		t.Fatal("N-1 profile upsert расширил frozen state")
	}
	var chatRepositoryID, chatRepositoryRepositoryID int64
	if err := pool.QueryRow(ctx, `
		select id, repository_id
		from matter_codex_chat_repositories
		where chat_id = $1
		order by id
		limit 1
	`, chatID).Scan(&chatRepositoryID, &chatRepositoryRepositoryID); err != nil {
		t.Fatalf("N-1 frozen chat repository: %v", err)
	}
	if err := func() error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, "set local role "+roleIdentifier); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from matter_codex_chat_participants where chat_id = $1`, chatID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from matter_codex_chat_repositories where chat_id = $1`, chatID); err != nil {
			return err
		}
		var restoredParticipantID, restoredChatRepositoryID int64
		if err := tx.QueryRow(ctx, `
			insert into matter_codex_chat_participants(chat_id, role_id)
			values ($1, $2)
			on conflict (chat_id, role_id) do update set enabled = true
			returning id
		`, chatID, roleID).Scan(&restoredParticipantID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			insert into matter_codex_chat_repositories(chat_id, repository_id)
			values ($1, $2)
			on conflict (chat_id, repository_id) do nothing
			returning id
		`, chatID, chatRepositoryRepositoryID).Scan(&restoredChatRepositoryID); err != nil {
			return err
		}
		if restoredParticipantID <= 0 || restoredChatRepositoryID != chatRepositoryID {
			return fmt.Errorf(
				"exact N-1 CreateChat restored participant=%d chat_repository=%d, want repository=%d",
				restoredParticipantID, restoredChatRepositoryID, chatRepositoryID,
			)
		}
		return tx.Commit(ctx)
	}(); err != nil {
		t.Fatalf("exact N-1 CreateChat delete/reinsert: %v", err)
	}
	var roleCount int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_agent_roles where project_id = $1 and name = 'n-minus-one-new-admin'`, projectID).Scan(&roleCount); err != nil || roleCount != 0 {
		t.Fatalf("N-1 оставил новый grant: count=%d error=%v", roleCount, err)
	}
	var exact bool
	if err := pool.QueryRow(ctx, `select matter_codex_cluster_admin_role_exact($1)`, roleID).Scan(&exact); err != nil || !exact {
		t.Fatalf("N-1 proof изменил точный grant: exact=%t error=%v", exact, err)
	}
}

func testAtomicCreateChatRevocationBarrier(t *testing.T, ctx context.Context, dsn string, input securityrepo.ClusterAdminBindingInput) {
	t.Helper()
	requestPool := openMigrationSingleConnectionPool(t, ctx, dsn)
	defer requestPool.Close()
	mutationPool := openMigrationSingleConnectionPool(t, ctx, dsn)
	defer mutationPool.Close()
	observationPool := openMigrationSingleConnectionPool(t, ctx, dsn)
	defer observationPool.Close()
	applicationName := "mc_create_chat_barrier_" + strconv.FormatUint(migrationSchemaSequence.Add(1), 36)
	if _, err := requestPool.Exec(ctx, `select set_config('application_name', $1, false)`, applicationName); err != nil {
		t.Fatalf("application_name для barrier: %v", err)
	}
	mutationTx, err := mutationPool.Begin(ctx)
	if err != nil {
		t.Fatalf("participant revocation transaction: %v", err)
	}
	if _, err := mutationTx.Exec(ctx, `update matter_codex_chat_participants set enabled = false where chat_id = $1 and role_id = $2`, input.ChatID, input.RoleID); err != nil {
		_ = mutationTx.Rollback(ctx)
		t.Fatalf("participant revocation barrier: %v", err)
	}
	result := make(chan error, 1)
	go func() {
		_, _, createErr := postgresrepo.NewRepository(requestPool).CreateChat(ctx, adminrepo.CreateChatInput{
			ProjectID: input.ProjectID, MattermostChannelID: input.MattermostChannelID,
			Name: "Existing admin chat", Slug: input.ChatSlug, ChatType: "single_custom",
			Settings: "{}", RoleIDs: []int64{input.RoleID},
		})
		result <- createErr
	}()
	waitForPostgresLockWait(t, ctx, observationPool, applicationName, "CreateChat participant")
	if err := mutationTx.Commit(ctx); err != nil {
		t.Fatalf("commit participant revocation: %v", err)
	}
	if err := <-result; !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
		t.Fatalf("CreateChat после committed revocation error = %v", err)
	}
	var channel string
	var participantEnabled bool
	if err := observationPool.QueryRow(ctx, `select mattermost_channel_id from matter_codex_chats where id = $1`, input.ChatID).Scan(&channel); err != nil || channel != input.MattermostChannelID {
		t.Fatalf("CreateChat восстановил/изменил chat: channel=%q error=%v", channel, err)
	}
	if err := observationPool.QueryRow(ctx, `select enabled from matter_codex_chat_participants where chat_id = $1 and role_id = $2`, input.ChatID, input.RoleID).Scan(&participantEnabled); err != nil || participantEnabled {
		t.Fatalf("CreateChat восстановил participant: enabled=%t error=%v", participantEnabled, err)
	}
}

func testRuntimeGuardPrivilegeMutationBarriers(t *testing.T, ctx context.Context, dsn string, tests []runtimeGuardMutationBarrier) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestPool := openMigrationSingleConnectionPool(t, ctx, dsn)
			defer requestPool.Close()
			mutationPool := openMigrationSingleConnectionPool(t, ctx, dsn)
			defer mutationPool.Close()
			observationPool := openMigrationSingleConnectionPool(t, ctx, dsn)
			defer observationPool.Close()
			applicationName := "mc_runtime_guard_barrier_" + strconv.FormatUint(migrationSchemaSequence.Add(1), 36)
			if _, err := requestPool.Exec(ctx, `select set_config('application_name', $1, false)`, applicationName); err != nil {
				t.Fatalf("application_name для runtime barrier: %v", err)
			}
			repository := postgresrepo.NewRepository(requestPool)
			allowed, err := repository.AdmitExistingClusterAdminBinding(ctx, test.input)
			if err != nil || !allowed {
				t.Fatalf("исходная runtime binding: allowed=%t error=%v", allowed, err)
			}
			initialSideEffect := false
			if err := repository.WithExistingClusterAdminRuntimeGuard(ctx, test.input, func() error {
				initialSideEffect = true
				return nil
			}); err != nil || !initialSideEffect {
				t.Fatalf("исходный runtime guard: side_effect=%t error=%v", initialSideEffect, err)
			}
			mutationTx, err := mutationPool.Begin(ctx)
			if err != nil {
				t.Fatalf("privilege mutation transaction: %v", err)
			}
			defer func() { _ = mutationTx.Rollback(ctx) }()
			tag, err := mutationTx.Exec(ctx, test.mutationSQL, test.mutationArgs...)
			if err != nil || tag.RowsAffected() != 1 {
				_ = mutationTx.Rollback(ctx)
				t.Fatalf("privilege mutation barrier: rows=%d error=%v", tag.RowsAffected(), err)
			}
			input := test.input
			input.ActorUser = "test"
			input.Operation = "test.runtime.barrier." + strings.ReplaceAll(test.name, " ", "_")
			sideEffect := false
			result := make(chan error, 1)
			go func() {
				result <- repository.WithExistingClusterAdminRuntimeGuard(ctx, input, func() error {
					sideEffect = true
					return nil
				})
			}()
			waitForPostgresLockWaitOrResult(t, ctx, observationPool, applicationName, test.name, result)
			if err := mutationTx.Commit(ctx); err != nil {
				t.Fatalf("commit privilege mutation: %v", err)
			}
			if err := <-result; !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) || sideEffect {
				t.Fatalf("runtime guard после committed mutation: side_effect=%t error=%v", sideEffect, err)
			}
			var auditCount int
			if err := observationPool.QueryRow(ctx, `
				select count(*) from matter_codex_audit_events
				where event_type = 'cluster_admin.runtime.denied' and summary = $1
			`, input.Operation+": denied").Scan(&auditCount); err != nil || auditCount != 1 {
				t.Fatalf("runtime denied audit: count=%d error=%v", auditCount, err)
			}
		})
	}
}

func waitForPostgresLockWait(t *testing.T, ctx context.Context, pool *pgxpool.Pool, applicationName string, label string) {
	t.Helper()
	waitDeadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `
			select exists(
				select 1 from pg_stat_activity
				where application_name = $1 and wait_event_type = 'Lock'
			)
		`, applicationName).Scan(&waiting); err != nil {
			t.Fatalf("наблюдение %s lock barrier: %v", label, err)
		}
		if waiting {
			return
		}
		if time.Now().After(waitDeadline) {
			t.Fatalf("%s не достиг lock barrier", label)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForPostgresLockWaitOrResult(t *testing.T, ctx context.Context, pool *pgxpool.Pool, applicationName string, label string, result <-chan error) {
	t.Helper()
	waitDeadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-result:
			t.Fatalf("%s завершился до lock barrier: %v", label, err)
		default:
		}
		var waiting bool
		if err := pool.QueryRow(ctx, `
			select exists(
				select 1 from pg_stat_activity
				where application_name = $1 and wait_event_type = 'Lock'
			)
		`, applicationName).Scan(&waiting); err != nil {
			t.Fatalf("наблюдение %s lock barrier: %v", label, err)
		}
		if waiting {
			return
		}
		if time.Now().After(waitDeadline) {
			t.Fatalf("%s не достиг lock barrier", label)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func testInteractionResourceScopeAdmission(t *testing.T, ctx context.Context, repository *postgresrepo.Repository, projectID int64) {
	t.Helper()
	base := securityrepo.InteractionResourceAdmissionInput{
		ActionKey: "mattermost.callback.action", Operation: "action;kind=agents_menu;action=show;view=projects",
		ResourceType: "project", ResourceID: strconv.FormatInt(projectID, 10), ActorUserID: "owner-id",
		ChannelID: "channel-existing", PostID: "post-1", Installation: "single-installation", Workspace: "installation-root",
	}
	allowed, err := repository.AdmitInteractionResource(ctx, base)
	if err != nil || !allowed {
		t.Fatalf("точная resource admission: allowed=%t error=%v", allowed, err)
	}
	for _, mutate := range []func(*securityrepo.InteractionResourceAdmissionInput){
		func(input *securityrepo.InteractionResourceAdmissionInput) { input.ChannelID = "unknown-channel" },
		func(input *securityrepo.InteractionResourceAdmissionInput) { input.PostID = "" },
		func(input *securityrepo.InteractionResourceAdmissionInput) { input.Installation = "other-installation" },
		func(input *securityrepo.InteractionResourceAdmissionInput) { input.Workspace = "999999" },
		func(input *securityrepo.InteractionResourceAdmissionInput) { input.ResourceID = "999999" },
		func(input *securityrepo.InteractionResourceAdmissionInput) { input.Session = "missing-session" },
	} {
		input := base
		mutate(&input)
		allowed, err := repository.AdmitInteractionResource(ctx, input)
		if err != nil {
			t.Fatalf("отрицательная resource admission: %v", err)
		}
		if allowed {
			t.Fatalf("resource admission разрешил mismatch: %#v", input)
		}
	}
	projectScoped := base
	projectScoped.Workspace = strconv.FormatInt(projectID, 10)
	allowed, err = repository.AdmitInteractionResource(ctx, projectScoped)
	if err != nil || !allowed {
		t.Fatalf("project workspace admission: allowed=%t error=%v", allowed, err)
	}
}

func testCapabilityCleanupRetention(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repository *postgresrepo.Repository, now time.Time) {
	t.Helper()
	insert := func(label string, status string, issuedAt time.Time, expiresAt time.Time, consumedAt any) {
		hash := sha256.Sum256([]byte("cleanup-" + label))
		contextHash := sha256.Sum256([]byte("cleanup-context-" + label))
		if _, err := pool.Exec(ctx, `
			insert into matter_codex_interaction_capabilities(
				token_hash, kind, operation, channel_id, post_binding, actor_user_id,
				installation_scope, workspace_scope, context_hash, status, issued_at, expires_at, consumed_at
			) values ($1, 'action', 'action;kind=agents_menu;view=main', 'channel-1', 'post-1', 'owner-id',
				'single-installation', 'installation-root', $2, $3, $4, $5, $6)
		`, hash[:], contextHash[:], status, issuedAt, expiresAt, consumedAt); err != nil {
			t.Fatalf("подготовка cleanup row %s: %v", label, err)
		}
	}
	insert("old-unused", "unused", now.Add(-72*time.Hour), now.Add(-70*time.Hour), nil)
	insert("old-consumed", "consumed", now.Add(-72*time.Hour), now.Add(-69*time.Hour), now.Add(-71*time.Hour))
	insert("old-revoked", "revoked", now.Add(-72*time.Hour), now.Add(-68*time.Hour), nil)
	insert("grace", "unused", now.Add(-3*time.Hour), now.Add(-2*time.Hour), nil)
	insert("valid", "unused", now.Add(-time.Hour), now.Add(time.Hour), nil)

	deleted, err := repository.CleanupInteractionCapabilities(ctx, securityrepo.CapabilityCleanupInput{DeleteBefore: now.Add(-24 * time.Hour), Limit: 2})
	if err != nil || deleted != 2 {
		t.Fatalf("первый bounded cleanup: deleted=%d error=%v", deleted, err)
	}
	deleted, err = repository.CleanupInteractionCapabilities(ctx, securityrepo.CapabilityCleanupInput{DeleteBefore: now.Add(-24 * time.Hour), Limit: 2})
	if err != nil || deleted != 1 {
		t.Fatalf("второй bounded cleanup: deleted=%d error=%v", deleted, err)
	}
	deleted, err = repository.CleanupInteractionCapabilities(ctx, securityrepo.CapabilityCleanupInput{DeleteBefore: now.Add(-24 * time.Hour), Limit: 2})
	if err != nil || deleted != 0 {
		t.Fatalf("идемпотентный cleanup: deleted=%d error=%v", deleted, err)
	}
	var survivors int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_interaction_capabilities where operation = 'action;kind=agents_menu;view=main' and expires_at >= $1`, now.Add(-24*time.Hour)).Scan(&survivors); err != nil || survivors != 2 {
		t.Fatalf("cleanup удалил действующие/grace rows: survivors=%d error=%v", survivors, err)
	}
}

func testFrozenBindingTwoConnectionNegatives(t *testing.T, ctx context.Context, dsn string, input securityrepo.ClusterAdminBindingInput) {
	t.Helper()
	poolOne := openMigrationSingleConnectionPool(t, ctx, dsn)
	defer poolOne.Close()
	poolTwo := openMigrationSingleConnectionPool(t, ctx, dsn)
	defer poolTwo.Close()
	channelRemap := input
	channelRemap.MattermostChannelID = "channel-remapped"
	newSession := input
	newSession.SessionKey = "new-admin-session"
	inputs := []securityrepo.ClusterAdminBindingInput{channelRemap, newSession}
	repositories := []*postgresrepo.Repository{postgresrepo.NewRepository(poolOne), postgresrepo.NewRepository(poolTwo)}
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	results := make(chan error, 2)
	for index := range repositories {
		go func(repo *postgresrepo.Repository, candidate securityrepo.ClusterAdminBindingInput) {
			ready <- struct{}{}
			<-start
			allowed, err := repo.AdmitExistingClusterAdminBinding(ctx, candidate)
			if err != nil {
				results <- err
				return
			}
			if allowed {
				results <- errors.New("запрещённая cluster-admin binding разрешена")
				return
			}
			results <- nil
		}(repositories[index], inputs[index])
	}
	<-ready
	<-ready
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("two-connection frozen binding: %v", err)
		}
	}
	var remappedChats, newSessions int
	if err := poolOne.QueryRow(ctx, `select count(*) from matter_codex_chats where id = $1 and mattermost_channel_id = 'channel-remapped'`, input.ChatID).Scan(&remappedChats); err != nil {
		t.Fatalf("проверка channel side effects: %v", err)
	}
	if err := poolOne.QueryRow(ctx, `select count(*) from matter_codex_agent_sessions where session_key = 'new-admin-session'`).Scan(&newSessions); err != nil {
		t.Fatalf("проверка session side effects: %v", err)
	}
	if remappedChats != 0 || newSessions != 0 {
		t.Fatalf("frozen binding создала DB side effects: remapped_chats=%d new_sessions=%d", remappedChats, newSessions)
	}
}

func testAtomicProfileDowngradeBarrier(t *testing.T, ctx context.Context, dsn string, profileName string) {
	t.Helper()
	lockerPool := openMigrationSingleConnectionPool(t, ctx, dsn)
	defer lockerPool.Close()
	requestPool := openMigrationSingleConnectionPool(t, ctx, dsn)
	defer requestPool.Close()
	locker, err := lockerPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("profile locker connection: %v", err)
	}
	defer locker.Release()
	tx, err := locker.Begin(ctx)
	if err != nil {
		t.Fatalf("profile downgrade transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `update matter_codex_agent_profiles set kubernetes_access = 'read-only' where name = $1`, profileName); err != nil {
		t.Fatalf("profile downgrade barrier: %v", err)
	}
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		_, _, upsertErr := postgresrepo.NewRepository(requestPool).UpsertAgentProfile(ctx, adminrepo.UpsertAgentProfileInput{
			Name: profileName, Role: "developer", KubernetesAccess: "cluster-admin", Enabled: true,
		})
		result <- upsertErr
	}()
	<-started
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit profile downgrade: %v", err)
	}
	if err := <-result; !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
		t.Fatalf("concurrent profile restore error = %v", err)
	}
	var access string
	if err := locker.QueryRow(ctx, `select kubernetes_access from matter_codex_agent_profiles where name = $1`, profileName).Scan(&access); err != nil || access != "read-only" {
		t.Fatalf("profile restore outcome: access=%q error=%v", access, err)
	}
}

func testAtomicRoleDeleteBarrier(t *testing.T, ctx context.Context, dsn string, projectID int64, roleID int64) {
	t.Helper()
	lockerPool := openMigrationSingleConnectionPool(t, ctx, dsn)
	defer lockerPool.Close()
	requestPool := openMigrationSingleConnectionPool(t, ctx, dsn)
	defer requestPool.Close()
	locker, err := lockerPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("role locker connection: %v", err)
	}
	defer locker.Release()
	tx, err := locker.Begin(ctx)
	if err != nil {
		t.Fatalf("role delete transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `delete from matter_codex_agent_roles where id = $1`, roleID); err != nil {
		t.Fatalf("role delete barrier: %v", err)
	}
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		_, _, upsertErr := postgresrepo.NewRepository(requestPool).UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
			ProjectID: projectID, Name: "existing-admin", RoleType: "admin", PromptMode: "template",
			KubernetesAccess: "cluster-admin", SandboxMode: "danger-full-access", AdvancedSettings: "{}", Enabled: true,
		})
		result <- upsertErr
	}()
	<-started
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit role delete: %v", err)
	}
	if err := <-result; !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
		t.Fatalf("concurrent role restore error = %v", err)
	}
	var count int
	if err := locker.QueryRow(ctx, `select count(*) from matter_codex_agent_roles where project_id = $1 and name = 'existing-admin'`, projectID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("role restored after delete: count=%d error=%v", count, err)
	}
}

func requiredMigrationDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN"))
	if dsn != "" {
		return dsn
	}
	if os.Getenv("MATTERCODEX_POSTGRES_TEST_REQUIRED") == "1" {
		t.Fatal("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN обязателен в required-режиме PostgreSQL-тестов")
	}
	t.Skip("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN не задан")
	return ""
}

func isolatedMigrationDSN(t *testing.T, label string) string {
	t.Helper()
	baseDSN := requiredMigrationDSN(t)
	schema := "mc_migration_" + label + "_" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36) + "_" + strconv.FormatUint(migrationSchemaSequence.Add(1), 36)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool := openMigrationPool(t, ctx, baseDSN)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "create schema "+identifier); err != nil {
		pool.Close()
		t.Fatalf("создание изолированной схемы PostgreSQL: %v", err)
	}
	pool.Close()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		cleanupPool := openMigrationPool(t, cleanupCtx, baseDSN)
		defer cleanupPool.Close()
		if _, err := cleanupPool.Exec(cleanupCtx, "drop schema "+identifier+" cascade"); err != nil {
			t.Errorf("очистка схемы PostgreSQL: %v", err)
		}
	})
	return migrationDSNWithSearchPath(t, baseDSN, schema)
}

func migrationDSNWithSearchPath(t *testing.T, dsn string, schema string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	if strings.ContainsAny(schema, " '=") {
		t.Fatal("небезопасное имя тестовой схемы")
	}
	return strings.TrimSpace(dsn) + " search_path=" + schema
}

func migrationDSNForRole(t *testing.T, dsn string, roleName string, password string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("runtime-role PostgreSQL proof requires a URL DSN")
	}
	parsed.User = url.UserPassword(roleName, password)
	return parsed.String()
}

func openMigrationPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("создание подключения к PostgreSQL: %v", err)
	}
	return pool
}

func openMigrationSingleConnectionPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("разбор PostgreSQL DSN: %v", err)
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("создание односессионного подключения к PostgreSQL: %v", err)
	}
	return pool
}
