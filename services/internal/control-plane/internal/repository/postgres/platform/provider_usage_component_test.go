package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestProviderUsageManagedWorkspaceProjectionComponent reads the real SQL/Scan projection from
// pinned immutable environment versions. Synthetic changes stay in rolled-back transactions;
// no runtime, credential, native tool or external effect is created by this read-side check.
// TestProviderUsageManagedWorkspaceProjectionComponent читает настоящую SQL/Scan-проекцию точных
// immutable версий окружения. Искусственные изменения откатываются транзакциями; проверка чтения
// не создаёт runtime, credential, native tool или внешний эффект.
func TestProviderUsageManagedWorkspaceProjectionComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	defer pool.Close()
	repository := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	createProject := func(key string) entity.Project {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.ProjectInput{Name: key, Language: "en"}})
		if err != nil || result.Project == nil {
			t.Fatal("create projection project", err)
		}
		return *result.Project
	}
	project := createProject("workspace-projection")
	other := createProject("workspace-projection-other")
	otherAgent := createLifecycleAgent(t, ctx, service, owner, other.Ref, "projection-other-agent", "Other workspace")
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "projection-agent", "Workspace projection")
	helper, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name, mutation string
		assistant      bool
		managed        bool
		want           bool
		count          int32
	}{
		{name: "pinned-managed-version", managed: true, want: true, count: 2},
		{name: "profile-off", count: 2},
		{name: "capability-revoked", managed: true, count: 2, mutation: "UPDATE control_plane.agents SET capabilities='{}' WHERE ref=$1"},
		{name: "disabled-environment", managed: true, count: 2, mutation: "UPDATE control_plane.runtime_environment_sets SET state='DISABLED' WHERE id=(SELECT environment_set_id FROM control_plane.agent_runtime_environment_bindings WHERE agent_id=(SELECT id FROM control_plane.agents WHERE ref=$1))"},
		{name: "foreign-project", managed: true, count: -1, mutation: "UPDATE control_plane.agent_runtime_environment_bindings b SET environment_set_id=other.environment_set_id, environment_version_id=other.environment_version_id FROM control_plane.agent_runtime_environment_bindings other JOIN control_plane.agents a ON a.id=other.agent_id WHERE a.ref=$2 AND b.agent_id=(SELECT id FROM control_plane.agents WHERE ref=$1)"},
		{name: "missing-binding", managed: true, count: -1, mutation: "DELETE FROM control_plane.agent_runtime_environment_bindings WHERE agent_id=(SELECT id FROM control_plane.agents WHERE ref=$1)"},
		{name: "system-assistant", assistant: true, managed: true, count: 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			ref := agent.Ref
			if scenario.assistant {
				ref = helper.Ref
			}
			// A new test-only version leaves the set's current pointer unchanged: the agent's exact
			// version pin must win. Immutable rows and their guards are never edited or disabled.
			// Новая test-only версия не меняет current pointer набора: должна читаться точная версия
			// binding агента. Immutable строки и их guards не изменяются и не отключаются.
			_, err = tx.Exec(ctx, `WITH version AS (
 INSERT INTO control_plane.runtime_environment_versions
 (ref,organization_id,environment_set_id,version_number,parent_version_id,non_secret_values,
 secret_descriptors,role_image_artifact_id,selected_tools,core_digest,managed_workspace,
 resource_policy,volume_policy,network_policy,kubernetes_access_profile,resources_digest,
 volumes_digest,network_digest,rbac_digest,digest,created_by)
 SELECT 'renvv_projection0001',v.organization_id,v.environment_set_id,v.version_number+1,v.id,
 v.non_secret_values,v.secret_descriptors,v.role_image_artifact_id,
 '[{"name":"Bash","command":"bash"},{"name":"Go","command":"go"}]'::jsonb,v.core_digest,$2,
 v.resource_policy,v.volume_policy,v.network_policy,v.kubernetes_access_profile,v.resources_digest,
 v.volumes_digest,v.network_digest,v.rbac_digest,v.digest,v.created_by
 FROM control_plane.agents a
 JOIN control_plane.agent_runtime_environment_bindings b ON b.agent_id=a.id
 JOIN control_plane.runtime_environment_sets e ON e.id=b.environment_set_id
 JOIN control_plane.runtime_environment_versions v ON v.id=COALESCE(b.environment_version_id,e.current_version_id)
 WHERE a.ref=$1 RETURNING id
 ) UPDATE control_plane.agent_runtime_environment_bindings SET environment_version_id=version.id
 FROM version WHERE agent_id=(SELECT id FROM control_plane.agents WHERE ref=$1)`, ref, scenario.managed)
			if err != nil {
				t.Fatal("create immutable projection fixture", err)
			}
			if _, err := tx.Exec(ctx, "UPDATE control_plane.agents SET capabilities=$2 WHERE ref=$1", ref, []string{runtimecontract.ArtifactCapability}); err != nil {
				t.Fatal(err)
			}
			if scenario.mutation != "" {
				args := []any{ref}
				if scenario.name == "foreign-project" {
					args = append(args, otherAgent.Ref)
				}
				_, err := tx.Exec(ctx, scenario.mutation, args...)
				if scenario.name == "foreign-project" {
					var rejected *pgconn.PgError
					if !errors.As(err, &rejected) || rejected.Code != "23514" {
						t.Fatal("foreign binding escaped immutable owner guard", err)
					}
					return
				}
				if err != nil {
					t.Fatal("mutate disposable eligibility fixture", err)
				}
			}
			agents, err := readProviderUsageAgents(ctx, tx, owner.AuthorityTenant, []string{ref})
			if err != nil || agents[ref].NativeToolCount != scenario.count || agents[ref].ManagedWorkspaceTools != scenario.want {
				t.Fatalf("wrong workspace projection: agents=%+v err=%v", agents, err)
			}
			hidden, err := readProviderUsageAgents(ctx, tx, "20000000-0000-4000-8000-000000009999", []string{ref})
			if err != nil || len(hidden) != 0 {
				t.Fatal("projection crossed organization boundary", err)
			}
		})
	}
}

// testProviderUsageProjection checks PostgreSQL-backed single/list usage agreement, actor-bound
// cursors, separate configure/launch permissions, revocation and hidden assistant boundaries using
// seeded catalog observations.
// testProviderUsageProjection проверяет согласованность единичной и списочной проекций
// использования в PostgreSQL, курсоры с привязкой к actor, раздельные права настройки и запуска,
// отзыв и границы скрытого ассистента на подготовленных наблюдениях каталога.
func testProviderUsageProjection(t *testing.T, ctx context.Context, repository *Repository) {
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(input command.Command) command.Result {
		t.Helper()
		input.Principal = owner
		result, err := service.Execute(ctx, input)
		if err != nil {
			t.Fatalf("provider usage fixture %s: %v", input.Kind, err)
		}
		return result
	}
	project := execute(command.Command{Kind: command.CreateProject, Mutation: value.Mutation{IdempotencyKey: "usage-project"}, Payload: command.ProjectInput{Name: "Provider usage", Purpose: "Account admission dimensions", Language: "en"}}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "usage-agent", "Usage agent")
	view, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil || len(view.Configuration.ProviderPolicy.AccountCandidates) == 0 {
		t.Fatalf("usage runtime fixture: %v", err)
	}
	accountRef := view.Configuration.ProviderPolicy.AccountCandidates[0].AccountRef
	configure := &entity.ProviderAccountUsageContext{Purpose: "CONFIGURE", AgentRef: agent.Ref, ProviderDefinitionKey: view.Configuration.Provider, Model: view.Configuration.Model, RuntimeProfileRef: view.Configuration.RuntimeProfileRef}
	read := func(p value.Principal, context *entity.ProviderAccountUsageContext) entity.ProviderAccount {
		t.Helper()
		item, err := service.GetProviderAccountWithUsage(ctx, p, accountRef, context)
		if err != nil || item.Usage == nil {
			t.Fatalf("provider usage read: %v", err)
		}
		return item
	}
	first := read(owner, configure)
	if !first.Usage.AllowedToSubmit || first.Usage.ProviderHealth.State != "READY" || first.Usage.Credential.State != "READY" {
		t.Fatal("configured account dimensions incorrect")
	}
	admin := read(owner, nil)
	if admin.Usage.ActorEligibility.State != "NOT_EVALUATED" || admin.Usage.ModelCompatibility.State != "NOT_EVALUATED" || admin.Usage.AllowedToSubmit {
		t.Fatal("admin read invented usage authority")
	}
	beforeModel := *configure
	beforeModel.Model = ""
	selection := read(owner, &beforeModel)
	if !selection.Usage.EligibleForSelection || selection.Usage.AllowedToSubmit || selection.Usage.ModelCompatibility.Reason != "MODEL_REQUIRED" {
		t.Fatal("account selection depends on nonexistent model")
	}
	launch := &entity.ProviderAccountUsageContext{Purpose: "LAUNCH", AgentRef: agent.Ref}
	if !read(owner, launch).Usage.AllowedToSubmit {
		t.Fatal("actual configured launch account denied")
	}
	invalid := *launch
	invalid.Model = configure.Model
	if _, err := service.GetProviderAccountWithUsage(ctx, owner, accountRef, &invalid); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("LAUNCH accepted caller model")
	}
	page, _, _, err := service.ListProviderAccounts(ctx, owner, query.Filter{ProviderUsage: configure, Page: query.Page{Size: 100}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range page {
		if item.Ref == accountRef {
			found = true
			if item.Usage.ContextDigest != first.Usage.ContextDigest || item.Usage.ModelCompatibility != first.Usage.ModelCompatibility {
				t.Fatal("single/list sources differ")
			}
		}
	}
	if !found {
		t.Fatal("selected account absent")
	}
	// Создание pending account обеспечивает вторую страницу без provider effects.
	execute(command.Command{Kind: command.CreateProviderAccount, Mutation: value.Mutation{IdempotencyKey: "usage-pending-account"}, Payload: command.ProviderAccountInput{Name: "Usage pending", DefinitionKey: view.Configuration.Provider}})
	_, cursor, _, err := service.ListProviderAccounts(ctx, owner, query.Filter{ProviderUsage: configure, Page: query.Page{Size: 1}})
	if err != nil || cursor == "" {
		t.Fatalf("provider cursor fixture: %v", err)
	}
	changed := *configure
	changed.Model = "other-model"
	if _, _, _, err := service.ListProviderAccounts(ctx, owner, query.Filter{ProviderUsage: &changed, Page: query.Page{Size: 1, Token: cursor}}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("cursor crossed usage context")
	}
	input := platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000007719", ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: "Provider usage reader", CallerWorkload: "control-api-gateway", Operation: "platform.query.provider-accounts.list"}
	if _, err := repository.ResolveProofAuthority(ctx, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("unbound provider actor: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: input.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("provider actor registration: %v", err)
	}
	bind := func(key string, permissions []string, target entity.AccessScope) entity.AccessBinding {
		t.Helper()
		role := execute(command.Command{Kind: command.CreateAccessRole, Mutation: value.Mutation{IdempotencyKey: key + "-role"}, Payload: command.AccessRoleInput{Name: key, PermissionKeys: permissions, AllowedScopes: []string{target.Kind}, ChangeComment: "Usage projection fixture"}}).AccessRole
		return *execute(command.Command{Kind: command.CreateAccessBinding, Mutation: value.Mutation{IdempotencyKey: key + "-bind"}, Payload: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: role.CurrentVersion.Ref, Scope: target}}).AccessBinding
	}
	bind("usage-view-accounts", []string{"provider.account.view"}, entity.AccessScope{Kind: "RESOURCE_KIND", ResourceKind: "PROVIDER_ACCOUNT"})
	target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: project.Ref, ResourceKind: "AGENT", ResourceRef: agent.Ref}
	bind("usage-view-agent", []string{"agent.view"}, target)
	reader := resolvedTestPrincipal(t, ctx, repository, input, "control-api-gateway")
	if read(reader, configure).Usage.ActorEligibility.Reason != "PERMISSION_REQUIRED" {
		t.Fatal("view became configure authority")
	}
	if _, _, _, err := service.ListProviderAccounts(ctx, reader, query.Filter{ProviderUsage: configure, Page: query.Page{Size: 1, Token: cursor}}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("cursor crossed actor")
	}
	bind("usage-manage-agent", []string{"agent.manage"}, target)
	if !read(reader, configure).Usage.AllowedToSubmit || read(reader, launch).Usage.ActorEligibility.State != "BLOCKED" {
		t.Fatal("configure and launch permissions conflated")
	}
	launchBinding := bind("usage-launch-agent", []string{"agent.launch"}, target)
	if !read(reader, launch).Usage.AllowedToSubmit {
		t.Fatal("exact launch permission denied")
	}
	execute(command.Command{Kind: command.RevokeAccessBinding, Mutation: value.Mutation{IdempotencyKey: "usage-revoke-launch", ExpectedVersion: &launchBinding.Version}, Payload: command.AccessBindingInput{BindingRef: launchBinding.Ref}})
	if read(reader, launch).Usage.AllowedToSubmit || !read(reader, configure).Usage.AllowedToSubmit {
		t.Fatal("revoked launch authority retained or unrelated configure lost")
	}
	wrongProfile := *configure
	wrongProfile.RuntimeProfileRef = "missing-runtime-profile"
	if usage := read(owner, &wrongProfile).Usage; usage.AllowedToSubmit || usage.ModelCompatibility.Reason != "RUNTIME_PROFILE_UNAVAILABLE" {
		t.Fatal("candidate profile substituted with saved profile")
	}
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	hidden := *configure
	hidden.AgentRef = assistant.Ref
	if _, err := service.GetProviderAccountWithUsage(ctx, reader, accountRef, &hidden); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("system assistant crossed project authority: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service.GetProviderAccountWithUsage(canceled, owner, accountRef, configure); err == nil {
		t.Fatal("canceled usage read succeeded")
	}
}

// providerUsageLockAccountSQL locks an enabled authorized fixture account and returns its
// organization to coordinate selection contention.
// providerUsageLockAccountSQL блокирует включённый авторизованный тестовый аккаунт и возвращает
// организацию для согласования конкурирующего выбора.
//
//go:embed testdata/provider_usage_lock_account.sql
var providerUsageLockAccountSQL string

// providerUsageDisableAccountSQL disables the locked fixture account and advances its version
// before the waiting selector resumes.
// providerUsageDisableAccountSQL отключает заблокированный тестовый аккаунт и увеличивает версию
// до возобновления ожидающего выбора.
//
//go:embed testdata/provider_usage_disable_account.sql
var providerUsageDisableAccountSQL string

// providerUsageWaitingAccountSQL checks the exact reader/writer backend pair with pg_blocking_pids
// instead of assuming a lock wait from elapsed time.
// providerUsageWaitingAccountSQL проверяет точную пару backend читателя и писателя через
// pg_blocking_pids, не выводя ожидание блокировки из прошедшего времени.
//
//go:embed testdata/provider_usage_waiting_account.sql
var providerUsageWaitingAccountSQL string

// providerUsageAuthorizeIsolatedAccountSQL binds a synthetic credential revision to the isolated
// pending account without creating an external Secret.
// providerUsageAuthorizeIsolatedAccountSQL связывает искусственную ревизию credential с
// изолированным ожидающим аккаунтом без создания внешнего Secret.
//
//go:embed testdata/provider_usage_authorize_isolated_account.sql
var providerUsageAuthorizeIsolatedAccountSQL string

// testProviderSelectionAfterLockWait proves the selector actually waits for the fixture row lock,
// then rejects the concurrently disabled account after commit and joins the reader before
// connection release.
// testProviderSelectionAfterLockWait доказывает ожидание блокировки строки, затем отклонение
// одновременно отключённого аккаунта после commit; завершает читателя до освобождения соединения.
func testProviderSelectionAfterLockWait(t *testing.T, parent context.Context, repository *Repository) {
	owner := resolvedTestPrincipal(t, parent, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(parent, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "usage-lock-project"}, Payload: command.ProjectInput{Name: "Account lock", Purpose: "Concurrent account selection", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := createLifecycleAgent(t, parent, service, owner, project.Project.Ref, "usage-lock-agent", "Account lock agent")
	view, err := service.GetAgentRuntimeConfiguration(parent, owner, agent.Ref)
	if err != nil {
		t.Fatalf("account lock configuration: %v", err)
	}
	created, err := service.Execute(parent, command.Command{Kind: command.CreateProviderAccount, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "usage-lock-account"},
		Payload:  command.ProviderAccountInput{Name: "Isolated lock account", DefinitionKey: view.Configuration.Provider}})
	if err != nil || created.ProviderAccount == nil {
		t.Fatalf("create isolated lock account: %v", err)
	}
	agentRef, accountRef := agent.Ref, created.ProviderAccount.Ref
	if result, err := repository.pool.Exec(parent, providerUsageAuthorizeIsolatedAccountSQL, accountRef); err != nil || result.RowsAffected() != 1 {
		t.Fatalf("authorize isolated lock account: %v", err)
	}
	seedObservedCatalogFixture(t, parent, repository)
	catalog, err := service.ListModelCatalog(parent, owner, view.Configuration.Provider, accountRef, query.Filter{Page: query.Page{Size: 100}})
	if err != nil {
		t.Fatalf("isolated account catalog: %v", err)
	}
	_, err = service.Execute(parent, command.Command{Kind: command.PublishAgentRuntimeConfig, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "usage-lock-fixed-policy", ExpectedVersion: &view.AgentVersion},
		Payload: command.AgentRuntimeConfigurationInput{AgentRef: agentRef, RuntimeProfileRef: view.Configuration.RuntimeProfileRef,
			Model: "gpt-5", ProviderPolicyMode: "FIXED", ProviderAccounts: []entity.ProviderAccountCandidate{{AccountRef: accountRef,
				Weight: 1, ProviderDefinitionKey: view.Configuration.Provider, CatalogRevision: catalog.Revision, CatalogDigest: catalog.Digest}}}})
	if err != nil {
		t.Fatalf("publish isolated lock policy: %v", err)
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	preflight, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer preflight.Rollback(parent)
	var organizationID string
	if err := preflight.QueryRow(ctx, providerUsageLockAccountSQL, accountRef).Scan(&organizationID); err != nil {
		t.Fatal(err)
	}
	if selected, err := repository.selectProviderAccountForAgent(ctx, preflight, organizationID, agentRef); err != nil || selected == "" {
		t.Fatalf("isolated account selection before contention: %v", err)
	}
	if err := preflight.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	writer, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(parent)
	if err := writer.QueryRow(ctx, providerUsageLockAccountSQL, accountRef).Scan(&organizationID); err != nil {
		t.Fatal(err)
	}
	reader, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Rollback(parent)
	readerPID := reader.Conn().PgConn().PID()
	writerPID := writer.Conn().PgConn().PID()
	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := repository.selectProviderAccountForAgent(ctx, reader, organizationID, agentRef)
		result <- err
	}()
	// При любом исходе завершаем запрос до освобождения его connection.
	defer func() { cancel(); <-done }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := repository.pool.QueryRow(ctx, providerUsageWaitingAccountSQL, readerPID, writerPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("account selector completed before lock wait: %v", err)
		case <-ctx.Done():
			t.Fatal("account selector did not wait for the account lock")
		case <-ticker.C:
		}
	}
	if _, err := writer.Exec(ctx, providerUsageDisableAccountSQL, accountRef); err != nil {
		t.Fatal(err)
	}
	if err := writer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	err = <-result
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("account selector accepted concurrent disable: %v", err)
	}
}
