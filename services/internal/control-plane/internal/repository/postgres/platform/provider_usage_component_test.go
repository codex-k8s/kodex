package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

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

//go:embed testdata/provider_usage_lock_account.sql
var providerUsageLockAccountSQL string

//go:embed testdata/provider_usage_disable_account.sql
var providerUsageDisableAccountSQL string

//go:embed testdata/provider_usage_waiting_account.sql
var providerUsageWaitingAccountSQL string

//go:embed testdata/provider_usage_authorize_isolated_account.sql
var providerUsageAuthorizeIsolatedAccountSQL string

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
