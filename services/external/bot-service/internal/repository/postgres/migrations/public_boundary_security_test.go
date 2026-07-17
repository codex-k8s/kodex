package migrations_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/url"
	"os"
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

	if _, _, err := repository.UpsertAgentProfile(ctx, adminrepo.UpsertAgentProfileInput{
		Name: "new-admin", Role: "admin", KubernetesAccess: "cluster-admin", Enabled: true,
	}); !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
		t.Fatalf("новый cluster-admin profile error = %v", err)
	}

	testCapabilityCleanupRetention(t, ctx, pool, repository, now)
}

func TestPublicBoundaryMigrationUpgradePreservesConfiguredClusterAdmin(t *testing.T) {
	dsn := isolatedMigrationDSN(t, "upgrade")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.RunTo(ctx, dsn, 20); err != nil {
		t.Fatalf("migrations through v20: %v", err)
	}
	pool := openMigrationPool(t, ctx, dsn)
	if _, err := pool.Exec(ctx, `update matter_codex_agent_profiles set kubernetes_access = 'cluster-admin' where name = 'developer'`); err != nil {
		t.Fatalf("подготовка существующего профиля: %v", err)
	}
	var projectID, roleID, chatID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Existing', 'existing') returning id`).Scan(&projectID); err != nil {
		t.Fatalf("подготовка проекта: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, kubernetes_access) values ($1, 'existing-admin', 'admin', 'cluster-admin') returning id`, projectID).Scan(&roleID); err != nil {
		t.Fatalf("подготовка существующей роли: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug) values ($1, 'channel-existing', 'Existing admin chat', 'existing-admin-chat') returning id`, projectID).Scan(&chatID); err != nil {
		t.Fatalf("подготовка существующего чата: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_chat_participants(chat_id, role_id) values ($1, $2)`, chatID, roleID); err != nil {
		t.Fatalf("подготовка существующей привязки: %v", err)
	}
	pool.Close()
	if err := migrations.RunTo(ctx, dsn, 21); err != nil {
		t.Fatalf("upgrade v20->v21: %v", err)
	}
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("upgrade v21->latest: %v", err)
	}
	pool = openMigrationPool(t, ctx, dsn)
	defer pool.Close()
	repository := postgresrepo.NewRepository(pool)
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
		ActorUser: "test", Operation: "test.binding.admission",
	})
	if err != nil || !allowed {
		t.Fatalf("существующая точная привязка: allowed=%t error=%v", allowed, err)
	}
	allowed, err = repository.AdmitExistingClusterAdminBinding(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: roleID, ProjectID: projectID, ChatID: 0, ChatSlug: "new-admin-chat",
		ActorUser: "test", Operation: "test.binding.admission",
	})
	if err != nil || allowed {
		t.Fatalf("новая привязка: allowed=%t error=%v", allowed, err)
	}
	if _, _, err := repository.UpsertAgentProfile(ctx, adminrepo.UpsertAgentProfileInput{
		Name: "developer", Role: "developer", KubernetesAccess: "cluster-admin", Enabled: true,
	}); err != nil {
		t.Fatalf("обновление существующего profile grant: %v", err)
	}
	if _, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: projectID, Name: "existing-admin", RoleType: "admin", PromptMode: "template",
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
		ProjectID: projectID, MattermostChannelID: "channel-new", Name: "New admin chat", Slug: "new-admin-chat",
		ChatType: "single_custom", Settings: "{}", RoleIDs: []int64{roleID},
	}); !errors.Is(err, adminrepo.ErrClusterAdminAdmissionDenied) {
		t.Fatalf("новый cluster-admin chat binding error = %v", err)
	}
	var newChatCount int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_chats where project_id = $1 and slug = 'new-admin-chat'`, projectID).Scan(&newChatCount); err != nil || newChatCount != 0 {
		t.Fatalf("новая запрещённая привязка создала чат: count=%d error=%v", newChatCount, err)
	}
	testInteractionResourceScopeAdmission(t, ctx, repository, projectID)

	testAtomicProfileDowngradeBarrier(t, ctx, dsn, "developer")
	testAtomicRoleDeleteBarrier(t, ctx, dsn, projectID, roleID)
	var auditCount int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_audit_events where event_type in ('cluster_admin.admission.allowed', 'cluster_admin.admission.denied')`).Scan(&auditCount); err != nil {
		t.Fatalf("чтение cluster-admin audit: %v", err)
	}
	if auditCount < 4 {
		t.Fatalf("cluster-admin audit count = %d, want at least 4", auditCount)
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
