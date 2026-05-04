package access

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

var sqlHeaderPattern = regexp.MustCompile(`^-- name: ([a-z0-9_]+__[a-z0-9_]+) :(one|many|exec)$`)

const testServiceCreateOrganizationOperation = "domain.Service.CreateOrganization"

func TestSQLFilesHaveNamedHeaders(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(SQLFiles, "sql/*.sql")
	if err != nil {
		t.Fatalf("glob sql files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected embedded SQL files")
	}

	for _, file := range files {
		contentBytes, err := SQLFiles.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		firstLine, _, _ := strings.Cut(string(contentBytes), "\n")
		match := sqlHeaderPattern.FindStringSubmatch(firstLine)
		if match == nil {
			t.Fatalf("%s has invalid named query header: %q", file, firstLine)
		}
		queryName := strings.TrimSuffix(filepath.Base(file), ".sql")
		if match[1] != queryName {
			t.Fatalf("%s header query name = %s, want %s", file, match[1], queryName)
		}
	}
}

func TestRepositoryLoadsEverySQLFile(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(SQLFiles, "sql/*.sql")
	if err != nil {
		t.Fatalf("glob sql files: %v", err)
	}
	for _, file := range files {
		queryName := strings.TrimSuffix(filepath.Base(file), ".sql")
		query, err := loadQuery(queryName)
		if err != nil {
			t.Fatalf("load query %s: %v", queryName, err)
		}
		if strings.TrimSpace(query) == "" {
			t.Fatalf("query %s is empty", queryName)
		}
	}
}

func TestWrapErrorMapsPostgresErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want error
	}{
		{name: "not found", err: pgx.ErrNoRows, want: errs.ErrNotFound},
		{name: "unique", err: &pgconn.PgError{Code: postgresUniqueViolation}, want: errs.ErrAlreadyExists},
		{name: "foreign key", err: &pgconn.PgError{Code: postgresForeignKeyViolation}, want: errs.ErrPreconditionFailed},
		{name: "check", err: &pgconn.PgError{Code: postgresCheckViolation}, want: errs.ErrInvalidArgument},
		{name: "serialization", err: &pgconn.PgError{Code: postgresSerialization}, want: errs.ErrConflict},
		{name: "deadlock", err: &pgconn.PgError{Code: postgresDeadlock}, want: errs.ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := wrapError("test operation", tc.err); !errors.Is(got, tc.want) {
				t.Fatalf("wrapError() = %v, want %v", got, tc.want)
			}
			var pgErr *pgconn.PgError
			if errors.As(tc.err, &pgErr) && !errors.As(wrapError("test operation", tc.err), &pgErr) {
				t.Fatalf("wrapError() lost postgres cause")
			}
		})
	}
}

func TestRepositoryIntegrationMutationReadAndOutbox(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	organization := entity.Organization{
		Base:        entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Kind:        enum.OrganizationKindOwner,
		Slug:        "kodex",
		DisplayName: "KODEX",
		Status:      enum.OrganizationStatusActive,
	}

	commandID := uuid.New()
	if err := repository.CreateOrganization(
		ctx,
		organization,
		testEvent("access.organization.created", "organization", organization.ID, now),
		testCommandResult(commandID, testServiceCreateOrganizationOperation, "organization", organization.ID, now),
	); err != nil {
		t.Fatalf("create organization: %v", err)
	}
	stored, err := repository.GetOrganization(ctx, organization.ID)
	if err != nil {
		t.Fatalf("get organization: %v", err)
	}
	if stored.ID != organization.ID || stored.Slug != organization.Slug {
		t.Fatalf("stored organization = %#v, want %#v", stored, organization)
	}
	if got := countTableRows(t, ctx, pool, "access_outbox_events"); got != 1 {
		t.Fatalf("outbox events = %d, want 1", got)
	}
	result, err := repository.GetCommandResult(ctx, query.CommandIdentity{CommandID: commandID})
	if err != nil {
		t.Fatalf("get command result: %v", err)
	}
	if result.AggregateID != organization.ID || result.Operation != testServiceCreateOrganizationOperation {
		t.Fatalf("command result = %#v, want aggregate %s operation %s", result, organization.ID, testServiceCreateOrganizationOperation)
	}
	conflictingOrganization := organization
	conflictingOrganization.ID = uuid.New()
	conflictingOrganization.Kind = enum.OrganizationKindClient
	conflictingOrganization.Slug = "kodex-duplicate-command"
	err = repository.CreateOrganization(
		ctx,
		conflictingOrganization,
		testEvent("access.organization.created", "organization", conflictingOrganization.ID, now),
		testCommandResult(commandID, testServiceCreateOrganizationOperation, "organization", conflictingOrganization.ID, now),
	)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("duplicate command err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestRepositoryIntegrationUpsertKeepsStableAggregate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	entry := entity.AllowlistEntry{
		Base:          entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		MatchType:     enum.AllowlistMatchEmail,
		Value:         "owner@example.com",
		DefaultStatus: enum.UserStatusActive,
		Status:        enum.AllowlistStatusActive,
	}
	if err := repository.PutAllowlistEntry(ctx, entry, testEvent("access.allowlist_entry.created", "allowlist_entry", entry.ID, now)); err != nil {
		t.Fatalf("put allowlist entry: %v", err)
	}

	updated := entry
	updated.Version = 2
	updated.UpdatedAt = now.Add(time.Minute)
	updated.DefaultStatus = enum.UserStatusPending
	if err := repository.PutAllowlistEntry(ctx, updated, testEvent("access.allowlist_entry.updated", "allowlist_entry", entry.ID, updated.UpdatedAt)); err != nil {
		t.Fatalf("update allowlist entry: %v", err)
	}
	stored, err := repository.FindAllowlistEntry(ctx, entry.MatchType, entry.Value)
	if err != nil {
		t.Fatalf("find allowlist entry: %v", err)
	}
	if stored.ID != entry.ID || !stored.CreatedAt.Equal(entry.CreatedAt) || stored.Version != 2 {
		t.Fatalf("stored allowlist identity/version = %s/%s/%d, want %s/%s/2", stored.ID, stored.CreatedAt, stored.Version, entry.ID, entry.CreatedAt)
	}

	conflicting := updated
	conflicting.ID = uuid.New()
	conflicting.Version = 3
	err = repository.PutAllowlistEntry(ctx, conflicting, testEvent("access.allowlist_entry.updated", "allowlist_entry", conflicting.ID, updated.UpdatedAt))
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("conflict err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestRepositoryIntegrationUserLifecycleAndPendingAccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	user := entity.User{
		Base:         entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		PrimaryEmail: "pending@example.com",
		Status:       enum.UserStatusPending,
	}
	identity := entity.UserIdentity{
		ID:           uuid.New(),
		UserID:       user.ID,
		Provider:     enum.IdentityProviderKeycloak,
		Subject:      "kc-pending",
		EmailAtLogin: user.PrimaryEmail,
	}
	if err := repository.CreateUser(ctx, user, identity, testEvent("access.user.created", "user", user.ID, now)); err != nil {
		t.Fatalf("create user: %v", err)
	}
	items, err := repository.ListPendingAccess(ctx, query.PendingAccessFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list pending access: %v", err)
	}
	if len(items) != 1 || items[0].ItemID != user.ID.String() || items[0].Status != string(enum.UserStatusPending) {
		t.Fatalf("pending items = %+v, want pending user %s", items, user.ID)
	}

	updated := user
	updated.Status = enum.UserStatusActive
	updated.Version = 2
	updated.UpdatedAt = now.Add(time.Minute)
	if err := repository.UpdateUser(ctx, updated, user.Version, testEvent("access.user.status_changed", "user", user.ID, updated.UpdatedAt), nil); err != nil {
		t.Fatalf("update user: %v", err)
	}
	stored, err := repository.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if stored.Status != enum.UserStatusActive || stored.Version != 2 {
		t.Fatalf("stored user = %+v, want active version 2", stored)
	}
	err = repository.UpdateUser(ctx, updated, user.Version, testEvent("access.user.status_changed", "user", user.ID, updated.UpdatedAt), nil)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale update err = %v, want %v", err, errs.ErrConflict)
	}
	audit := entity.AccessDecisionAudit{
		ID:        uuid.New(),
		Subject:   value.SubjectRef{Type: "user", ID: user.ID.String()},
		ActionKey: "access.pending_access.list",
		Resource:  value.ResourceRef{Type: "pending_access"},
		Scope:     value.ScopeRef{Type: "global"},
		RequestContext: value.RequestContext{
			Source: "test",
		},
		Decision:      enum.AccessDecisionPending,
		ReasonCode:    "subject_pending",
		PolicyVersion: 1,
		Explanation: value.DecisionExplanation{
			Decision:      string(enum.AccessDecisionPending),
			ReasonCode:    "subject_pending",
			PolicyVersion: 1,
		},
		CreatedAt: now.Add(2 * time.Minute),
	}
	if err := repository.RecordAccessDecision(ctx, audit, nil); err != nil {
		t.Fatalf("record audit: %v", err)
	}
	items, err = repository.ListPendingAccess(ctx, query.PendingAccessFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list pending access after activation: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("pending items after activation = %+v, want no historical audit rows", items)
	}
}

func TestRepositoryIntegrationGetAllowlistEntryByID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	entry := entity.AllowlistEntry{
		Base:          entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		MatchType:     enum.AllowlistMatchEmail,
		Value:         "owner@example.com",
		DefaultStatus: enum.UserStatusActive,
		Status:        enum.AllowlistStatusActive,
	}
	if err := repository.PutAllowlistEntry(ctx, entry, testEvent("access.allowlist_entry.created", "allowlist_entry", entry.ID, now)); err != nil {
		t.Fatalf("put allowlist entry: %v", err)
	}
	stored, err := repository.GetAllowlistEntry(ctx, entry.ID)
	if err != nil {
		t.Fatalf("get allowlist entry: %v", err)
	}
	if stored.ID != entry.ID || stored.Value != entry.Value {
		t.Fatalf("stored entry = %+v, want %+v", stored, entry)
	}
	disabled := stored
	disabled.Status = enum.AllowlistStatusDisabled
	disabled.Version = stored.Version + 1
	disabled.UpdatedAt = now.Add(time.Minute)
	if err := repository.UpdateAllowlistEntry(ctx, disabled, stored.Version, testEvent("access.allowlist_entry.disabled", "allowlist_entry", stored.ID, disabled.UpdatedAt), nil); err != nil {
		t.Fatalf("update allowlist entry: %v", err)
	}
	err = repository.UpdateAllowlistEntry(ctx, disabled, stored.Version, testEvent("access.allowlist_entry.disabled", "allowlist_entry", stored.ID, disabled.UpdatedAt), nil)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale allowlist update err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestRepositoryIntegrationExternalAccountLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	provider := entity.ExternalProvider{
		Base:         entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Slug:         "github",
		ProviderKind: enum.ExternalProviderRepository,
		DisplayName:  "GitHub",
		Status:       enum.ExternalProviderStatusActive,
	}
	if err := repository.PutExternalProvider(ctx, provider, testEvent("access.external_provider.created", "external_provider", provider.ID, now)); err != nil {
		t.Fatalf("put provider: %v", err)
	}
	disabledProvider := provider
	disabledProvider.Status = enum.ExternalProviderStatusDisabled
	disabledProvider.Version = 2
	disabledProvider.UpdatedAt = now.Add(time.Minute)
	providerCommandID := uuid.New()
	providerResult := testCommandResult(providerCommandID, "domain.Service.UpdateExternalProvider", "external_provider", provider.ID, disabledProvider.UpdatedAt)
	if err := repository.UpdateExternalProvider(ctx, disabledProvider, provider.Version, testEvent("access.external_provider.disabled", "external_provider", provider.ID, disabledProvider.UpdatedAt), &providerResult); err != nil {
		t.Fatalf("update provider: %v", err)
	}
	storedProvider, err := repository.GetExternalProvider(ctx, provider.ID)
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}
	if storedProvider.Status != enum.ExternalProviderStatusDisabled || storedProvider.Version != 2 {
		t.Fatalf("stored provider = %+v, want disabled version 2", storedProvider)
	}
	if _, err := repository.GetCommandResult(ctx, query.CommandIdentity{CommandID: providerCommandID}); err != nil {
		t.Fatalf("get provider command result: %v", err)
	}

	secret := entity.SecretBindingRef{
		Base:      entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		StoreType: enum.SecretStoreVault,
		StoreRef:  "kv/kodex/github/bot",
	}
	if err := repository.PutSecretBindingRef(ctx, secret, testEvent("access.secret_binding_ref.created", "secret_binding_ref", secret.ID, now)); err != nil {
		t.Fatalf("put secret ref: %v", err)
	}
	account := entity.ExternalAccount{
		Base:               entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ExternalProviderID: provider.ID,
		AccountType:        enum.ExternalAccountBot,
		DisplayName:        "kodex-agent",
		OwnerScopeType:     enum.ExternalAccountScopeGlobal,
		Status:             enum.ExternalAccountStatusPending,
		SecretBindingRefID: &secret.ID,
	}
	if err := repository.RegisterExternalAccount(ctx, account, testEvent("access.external_account.created", "external_account", account.ID, now), testCommandResult(uuid.New(), "domain.Service.RegisterExternalAccount", "external_account", account.ID, now)); err != nil {
		t.Fatalf("register account: %v", err)
	}
	updatedAccount := account
	updatedAccount.Status = enum.ExternalAccountStatusActive
	updatedAccount.Version = 2
	updatedAccount.UpdatedAt = now.Add(2 * time.Minute)
	if err := repository.UpdateExternalAccount(ctx, updatedAccount, account.Version, testEvent("access.external_account.status_changed", "external_account", account.ID, updatedAccount.UpdatedAt), nil); err != nil {
		t.Fatalf("update account: %v", err)
	}
	storedAccount, err := repository.GetExternalAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if storedAccount.Status != enum.ExternalAccountStatusActive || storedAccount.Version != 2 {
		t.Fatalf("stored account = %+v, want active version 2", storedAccount)
	}

	binding := entity.ExternalAccountBinding{
		Base:              entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ExternalAccountID: account.ID,
		UsageScopeType:    enum.ExternalAccountScopeProject,
		UsageScopeID:      "project-1",
		AllowedActionKeys: []string{"provider.issue.write"},
		Status:            enum.ExternalAccountBindingStatusActive,
	}
	if err := repository.BindExternalAccount(ctx, binding, testEvent("access.external_account_binding.created", "external_account_binding", binding.ID, now)); err != nil {
		t.Fatalf("bind account: %v", err)
	}
	disabledBinding := binding
	disabledBinding.Status = enum.ExternalAccountBindingStatusDisabled
	disabledBinding.Version = 2
	disabledBinding.UpdatedAt = now.Add(3 * time.Minute)
	if err := repository.UpdateExternalAccountBinding(ctx, disabledBinding, binding.Version, testEvent("access.external_account_binding.disabled", "external_account_binding", binding.ID, disabledBinding.UpdatedAt), nil); err != nil {
		t.Fatalf("update binding: %v", err)
	}
	storedBinding, err := repository.GetExternalAccountBinding(ctx, binding.ID)
	if err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if storedBinding.Status != enum.ExternalAccountBindingStatusDisabled || storedBinding.Version != 2 {
		t.Fatalf("stored binding = %+v, want disabled version 2", storedBinding)
	}
	err = repository.UpdateExternalAccountBinding(ctx, disabledBinding, binding.Version, testEvent("access.external_account_binding.disabled", "external_account_binding", binding.ID, disabledBinding.UpdatedAt), nil)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale binding update err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestRepositoryIntegrationAccessRuleIdentityIsUnique(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	action := entity.AccessAction{
		Base:         entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Key:          "project.read",
		DisplayName:  "Project read",
		ResourceType: "project",
		Status:       enum.AccessActionStatusActive,
	}
	if err := repository.PutAccessAction(ctx, action, testEvent("access.access_action.created", "access_action", action.ID, now)); err != nil {
		t.Fatalf("put action: %v", err)
	}

	rule := entity.AccessRule{
		Base:         entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Effect:       enum.AccessEffectAllow,
		SubjectType:  enum.AccessSubjectUser,
		SubjectID:    uuid.NewString(),
		ActionKey:    action.Key,
		ResourceType: "project",
		ScopeType:    "global",
		Status:       enum.AccessRuleStatusActive,
	}
	if err := repository.PutAccessRule(ctx, rule, testEvent("access.access_rule.created", "access_rule", rule.ID, now)); err != nil {
		t.Fatalf("put rule: %v", err)
	}
	updated := rule
	updated.Version = 2
	updated.Priority = 10
	updated.UpdatedAt = now.Add(time.Minute)
	if err := repository.PutAccessRule(ctx, updated, testEvent("access.access_rule.updated", "access_rule", rule.ID, updated.UpdatedAt)); err != nil {
		t.Fatalf("update rule: %v", err)
	}
	if got := countTableRows(t, ctx, pool, "access_rules"); got != 1 {
		t.Fatalf("access rules = %d, want 1", got)
	}
}

func TestRepositoryIntegrationAccessDecisionAuditRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	ruleID := uuid.New()
	audit := entity.AccessDecisionAudit{
		ID:        uuid.New(),
		Subject:   value.SubjectRef{Type: "user", ID: uuid.NewString()},
		ActionKey: "project.read",
		Resource:  value.ResourceRef{Type: "project", ID: "project-1"},
		Scope:     value.ScopeRef{Type: "project", ID: "project-1"},
		RequestContext: value.RequestContext{
			Source:       "staff-gateway",
			TraceID:      "trace-1",
			SessionID:    "session-1",
			ClientIPHash: "hash-1",
		},
		Decision:      enum.AccessDecisionAllow,
		ReasonCode:    "explicit_allow",
		PolicyVersion: 2,
		Explanation: value.DecisionExplanation{
			Decision:      string(enum.AccessDecisionAllow),
			ReasonCode:    "explicit_allow",
			PolicyVersion: 2,
			MatchedRules: []value.RuleExplanation{{
				RuleID:     ruleID,
				Effect:     string(enum.AccessEffectAllow),
				Subject:    value.SubjectRef{Type: "user", ID: "user-1"},
				ActionKey:  "project.read",
				Scope:      value.ScopeRef{Type: "project", ID: "project-1"},
				Priority:   10,
				ReasonCode: "explicit_allow",
			}},
		},
		CreatedAt: now,
	}
	if err := repository.RecordAccessDecision(ctx, audit, nil); err != nil {
		t.Fatalf("record audit: %v", err)
	}
	stored, err := repository.GetAccessDecisionAudit(ctx, audit.ID)
	if err != nil {
		t.Fatalf("get audit: %v", err)
	}
	if stored.ID != audit.ID || stored.Decision != audit.Decision || stored.ReasonCode != audit.ReasonCode {
		t.Fatalf("stored audit = %#v, want %#v", stored, audit)
	}
	if stored.Scope != audit.Scope || stored.RequestContext.TraceID != audit.RequestContext.TraceID {
		t.Fatalf("stored context = %+v/%+v, want %+v/%+v", stored.Scope, stored.RequestContext, audit.Scope, audit.RequestContext)
	}
	if len(stored.Explanation.MatchedRules) != 1 || stored.Explanation.MatchedRules[0].RuleID != ruleID {
		t.Fatalf("stored explanation = %+v, want rule %s", stored.Explanation, ruleID)
	}
}

func openIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("KODEX_ACCESS_MANAGER_TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("set KODEX_ACCESS_MANAGER_TEST_DATABASE_DSN to run PostgreSQL repository integration tests")
	}
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := "access_repo_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE")
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	applyMigrations(t, ctx, pool)
	return pool
}

func applyMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	files, err := filepath.Glob("../../../../cmd/cli/migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(files)
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		for _, statement := range splitSQLStatements(upMigrationSQL(t, string(content), file)) {
			if _, err := pool.Exec(ctx, statement); err != nil {
				t.Fatalf("apply migration %s statement %q: %v", file, statement, err)
			}
		}
	}
}

func upMigrationSQL(t *testing.T, content string, file string) string {
	t.Helper()

	upIndex := strings.Index(content, "-- +goose Up")
	downIndex := strings.Index(content, "-- +goose Down")
	if upIndex < 0 || downIndex < 0 || downIndex < upIndex {
		t.Fatalf("invalid goose migration markers in %s", file)
	}
	return content[upIndex+len("-- +goose Up") : downIndex]
}

func splitSQLStatements(content string) []string {
	parts := strings.Split(content, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		statement := strings.TrimSpace(part)
		if statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}

func countTableRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) int {
	t.Helper()

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func testEvent(eventType string, aggregateType string, aggregateID uuid.UUID, occurredAt time.Time) entity.OutboxEvent {
	return entity.OutboxEvent{
		ID:            uuid.New(),
		EventType:     eventType,
		SchemaVersion: 1,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		Payload:       []byte(`{}`),
		OccurredAt:    occurredAt,
	}
}

func testCommandResult(commandID uuid.UUID, operation string, aggregateType string, aggregateID uuid.UUID, createdAt time.Time) entity.CommandResult {
	return entity.CommandResult{
		Key:           operation + ":" + commandID.String(),
		CommandID:     commandID,
		Operation:     operation,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		CreatedAt:     createdAt,
	}
}
