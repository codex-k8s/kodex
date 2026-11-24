package platform

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/providercredentialclient"
)

// TestPersonalModelAccountLaunchComponent verifies ownership before launch and at claim on disposable PostgreSQL using synthetic provider metadata.
// TestPersonalModelAccountLaunchComponent проверяет владение перед запуском и при выдаче исполнения на временной PostgreSQL с синтетическим провайдером.
func TestPersonalModelAccountLaunchComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	defer pool.Close()
	repository, catalog := newTwoRouteProviderFixture(t, ctx, pool, "vendor/alternate")
	provider := catalog.Providers[0]
	model := provider.Models[0].ID
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.provider-accounts.create",
	}, "control-api-gateway")
	peer := owner
	if err := pool.QueryRow(ctx, `
		INSERT INTO control_plane.subjects(organization_id,ref,issuer,external_subject_digest,display_name)
		VALUES ($1::uuid,'usr_personal_model_peer','https://identity.example.invalid',repeat('d',64),'Second fixture user')
		RETURNING id::text`, owner.AuthorityTenant).Scan(&peer.ActorID); err != nil {
		t.Fatal(err)
	}
	execute := newComponentCommandExecutor(t, ctx, service, "personal-model-account")
	create := func(principal value.Principal, id string) entity.ProviderAccount {
		t.Helper()
		return *execute(command.CreateProviderAccount, principal, id, nil, command.ProviderAccountInput{
			Name: "Personal model key", Personal: true, DefinitionKey: provider.DefinitionKey(),
		}).ProviderAccount
	}
	self := create(owner, "own")
	other := create(peer, "peer")
	if !self.Personal || !other.Personal || self.Ref == other.Ref {
		t.Fatal("personal account ownership was lost or merged")
	}
	list, _, actions, err := service.ListProviderAccounts(ctx, peer, query.Filter{PersonalProviderAccounts: true})
	if err != nil || len(list) != 1 || list[0].Ref != other.Ref || !slices.Contains(actions, "CREATE_CONNECTION") {
		t.Fatal("personal account collection exposed another subject or refused self-service", err)
	}
	shared, _, _, err := service.ListProviderAccounts(ctx, owner, query.Filter{})
	if err != nil || slices.ContainsFunc(shared, func(item entity.ProviderAccount) bool { return item.Personal }) {
		t.Fatal("shared account collection exposed a personal account", err)
	}
	if _, err := service.GetProviderAccount(ctx, owner, other.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("owner saw another user's personal account", err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.RevokeProviderAccount, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "account-owner-revoke-peer", ExpectedVersion: &other.Version},
		Payload:  command.ProviderAccountInput{AccountRef: other.Ref},
	}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("owner could revoke another user's personal account", err)
	}
	// The fixture records credential metadata only; model discovery uses the normal owner task path.
	if _, err := pool.Exec(ctx, `
		INSERT INTO control_plane.provider_credential_revisions
		 (ref,organization_id,provider_account_id,revision_number,secret_name,secret_uid,secret_resource_version,content_sha256,observed_at)
		SELECT 'pcred_personal_model_fixture',organization_id,id,1,'fixture-model',gen_random_uuid(),'1',repeat('b',64),clock_timestamp()
		FROM control_plane.provider_accounts WHERE ref=$1`, self.Ref); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE control_plane.provider_accounts SET enabled=true, state='AUTHORIZED',
		 current_credential_revision_id=(SELECT id FROM control_plane.provider_credential_revisions WHERE ref='pcred_personal_model_fixture')
		WHERE ref=$1`, self.Ref); err != nil {
		t.Fatal(err)
	}
	tasks, err := repository.ClaimProviderModelCatalogTasks(ctx, "personal-catalog-fixture", 16, &providercredentialclient.Client{})
	if err != nil || len(tasks) != 3 {
		t.Fatalf("expected shared and personal catalog tasks, got %d: %v", len(tasks), err)
	}
	for _, task := range tasks {
		selectedModel := model
		if task.ProviderDefinitionKey != provider.DefinitionKey() {
			selectedModel = catalog.Providers[1].Models[0].ID
		}
		err := repository.CompleteProviderModelCatalogTask(ctx, task, platformrepo.ProviderModelCatalogObservation{
			AccountRef: task.AccountRef, CredentialRef: task.CredentialRef, Source: "REMOTE_API",
			Failure: "NONE", ObservedAt: time.Now(), Models: []platformrepo.ProviderModelCatalogRecord{{ID: selectedModel}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Personal model routing", Language: "en"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "personal-model-agent", "Fixture worker")
	view, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil {
		t.Fatal(err)
	}
	selected := execute(command.PublishAgentRuntimeConfig, owner, "policy", &view.AgentVersion, command.AgentRuntimeConfigurationInput{
		AgentRef: agent.Ref, RuntimeProfileRef: modelProviderProfileKey(provider, model), Model: model, ProviderPolicyMode: "PERSONAL",
	})
	if selected.RuntimeConfiguration.Configuration.ProviderPolicy.Mode != "PERSONAL" {
		t.Fatal("personal policy not materialized")
	}
	input := command.LaunchRunInput{ProjectRef: project.Ref, Task: "Return a synthetic result.", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}
	launch := execute(command.LaunchRun, owner, "launch", nil, input).Run
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	items := execute(command.ClaimExecution, worker, "claim", nil, command.LeaseInput{WorkloadInstance: "runtime-model-owner-fixture", Limit: 1}).RuntimeItems
	if len(items) != 1 || stringMap(items[0], "runRef") != launch.Ref || stringMap(items[0], "providerAccountRef") != self.Ref {
		t.Fatalf("claim did not retain the launching owner's selected account: %d", len(items))
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, err = repository.selectReadyProviderAccountForAgent(ctx, tx, owner.AuthorityTenant, agent.Ref, peer.ActorID)
	if !errors.Is(err, errs.ErrPersonalModelAccountRequired) {
		t.Fatal("user without a ready account borrowed a shared or peer account", err)
	}
	_, err = repository.selectReadyProviderAccountForAgent(ctx, tx, owner.AuthorityTenant, agent.Ref, "")
	if !errors.Is(err, errs.ErrPersonalModelAccountRequired) {
		t.Fatal("unattended launch borrowed a personal account", err)
	}
}
