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
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, consumeErr := repository.ConsumeInteractionCapability(ctx, consume)
			results <- consumeErr
		}()
	}
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
	var projectID, roleID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Existing', 'existing') returning id`).Scan(&projectID); err != nil {
		t.Fatalf("подготовка проекта: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, kubernetes_access) values ($1, 'existing-admin', 'admin', 'cluster-admin') returning id`, projectID).Scan(&roleID); err != nil {
		t.Fatalf("подготовка существующей роли: %v", err)
	}
	pool.Close()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("upgrade migrations: %v", err)
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
	var auditCount int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_audit_events where event_type in ('cluster_admin.admission.allowed', 'cluster_admin.admission.denied')`).Scan(&auditCount); err != nil {
		t.Fatalf("чтение cluster-admin audit: %v", err)
	}
	if auditCount < 4 {
		t.Fatalf("cluster-admin audit count = %d, want at least 4", auditCount)
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
