package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func testIntegrationDefinitionBindingRead(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "binding-read-create-connection"},
		Payload:  command.ConnectionInput{DefinitionKey: "synthetic", Name: "Binding read fixture", PublicConfiguration: map[string]any{"journal": "binding-read"}}})
	if err != nil || created.Connection == nil {
		t.Fatalf("create connection: %v", err)
	}
	ref := created.Connection.Ref
	if created.Connection.DefinitionConfigurationBinding != nil {
		t.Fatal("command receipt promised authoritative binding")
	}
	read := func(principal value.Principal) entity.IntegrationConnection {
		t.Helper()
		connection, err := service.GetIntegrationConnection(ctx, principal, ref)
		if err != nil {
			t.Fatalf("read connection: %v", err)
		}
		return connection
	}
	if binding := read(owner).DefinitionConfigurationBinding; binding == nil || binding.State != "ABSENT" || binding.BindingVersion != 0 || binding.RevisionRef != "" || binding.ConfigurationRef != "" {
		t.Fatalf("absence was not proven: %#v", binding)
	}
	publish := func(name string) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: command.CreateIntegrationDefinition, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: name + "-create"}, Payload: command.ManagedConfigurationInput{Name: name, ContentFormat: "JSON", Content: string(asJSON(repository.integrationDefinitions["synthetic"]))}})
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []command.Kind{command.ValidateIntegrationDefinition, command.PublishIntegrationDefinition} {
			version := result.ManagedConfiguration.Version
			result, err = service.Execute(ctx, command.Command{Kind: kind, Principal: owner,
				Mutation: value.Mutation{IdempotencyKey: name + "-" + string(kind), ExpectedVersion: &version},
				Payload:  command.ManagedConfigurationInput{ConfigurationRef: result.ManagedConfiguration.Ref, RevisionRef: result.ManagedRevision.Ref}})
			if err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	a, b := publish("binding-read-a"), publish("binding-read-b")
	rebind := func(key string, target command.Result, expected entity.ManagedConfigurationConsumer) (command.Result, error) {
		t.Helper()
		impact, err := service.GetManagedConfigurationImpact(ctx, owner, target.ManagedConfiguration.Ref, target.ManagedRevision.Ref, query.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		version := target.ManagedConfiguration.Version
		return service.Execute(ctx, command.Command{Kind: command.RebindIntegrationDefinition, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &version},
			Payload: command.ManagedConfigurationInput{ConfigurationRef: target.ManagedConfiguration.Ref, RevisionRef: target.ManagedRevision.Ref,
				ImpactDigest: impact.Digest, Consumers: []entity.ManagedConfigurationConsumer{expected}}})
	}
	absent := entity.ManagedConfigurationConsumer{Kind: "INTEGRATION_CONNECTION", Ref: ref, ExpectedAbsent: true}
	a, err = rebind("binding-read-bind-a", a, absent)
	if err != nil {
		t.Fatal(err)
	}
	pins := read(owner).DefinitionConfigurationBinding
	if pins == nil || pins.State != "MATCH" || pins.ConfigurationRef != a.ManagedConfiguration.Ref || pins.RevisionRef != a.ManagedRevision.Ref || pins.BindingVersion != 1 {
		t.Fatalf("current binding lost exact pins: %#v", pins)
	}
	if _, err := rebind("binding-read-stale-absence", b, absent); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale absence accepted: %v", err)
	}
	b, err = rebind("binding-read-move-b", b, entity.ManagedConfigurationConsumer{Kind: "INTEGRATION_CONNECTION", Ref: ref, RevisionRef: pins.RevisionRef, Version: pins.BindingVersion})
	if err != nil {
		t.Fatalf("cross-set exact rebind: %v", err)
	}
	current := read(owner).DefinitionConfigurationBinding
	if current == nil || current.ConfigurationRef != b.ManagedConfiguration.Ref || current.RevisionRef != b.ManagedRevision.Ref || current.BindingVersion != pins.BindingVersion+1 {
		t.Fatalf("cross-set read kept stale pins: %#v", current)
	}
	items, _, err := service.ListIntegrationConnections(ctx, owner, query.Filter{Query: "Binding read fixture"})
	if err != nil || len(items) != 1 || items[0].DefinitionConfigurationBinding != nil {
		t.Fatalf("catalog promised binding: %v", err)
	}
	proof := platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000004797", ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: "Binding read manager", CallerWorkload: "control-api-gateway", Operation: "platform.query.integration-grant-candidates.connections.list"}
	if _, err := repository.ResolveProofAuthority(ctx, proof); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("unbound reader: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: proof.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("reader subject: %v", err)
	}
	grantRole := func(key string, permissions []string, target entity.AccessScope) {
		t.Helper()
		role, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-role"}, Payload: command.AccessRoleInput{Name: key, PermissionKeys: permissions, AllowedScopes: []string{target.Kind}, ChangeComment: "Binding read fixture"}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-binding"}, Payload: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: role.AccessRole.CurrentVersion.Ref, Scope: target}})
		if err != nil {
			t.Fatal(err)
		}
	}
	grantRole("binding-read-connection", []string{"integration.view", "integration.manage"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: ref})
	actor := resolvedTestPrincipal(t, ctx, repository, proof, "control-api-gateway")
	if read(actor).DefinitionConfigurationBinding != nil {
		t.Fatal("connection manager learned organization binding")
	}
	grantRole("binding-read-org-manage", []string{"organization.manage"}, entity.AccessScope{Kind: "ORGANIZATION"})
	if read(actor).DefinitionConfigurationBinding != nil {
		t.Fatal("hidden configuration pins leaked without view")
	}
	grantRole("binding-read-org-view", []string{"organization.view"}, entity.AccessScope{Kind: "ORGANIZATION"})
	if binding := read(actor).DefinitionConfigurationBinding; binding == nil || *binding != *current {
		t.Fatalf("eligible reader lost current pins: %#v", binding)
	}
	foreign := actor
	foreign.AuthorityTenant = "90000000-0000-4000-8000-000000000099"
	if connection, err := service.GetIntegrationConnection(ctx, foreign, ref); err == nil || connection.DefinitionConfigurationBinding != nil {
		t.Fatal("foreign tenant read binding")
	}
	// Доказательство ABSENT не подменяет фильтрованное NoRows: найденная
	// непригодная связь закрывает выдачу, а не разрешает повтор INSERT.
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE control_plane.integration_connections SET lifecycle_state='DELETED' WHERE organization_id=$1::uuid AND ref=$2`, scope.organizationID, ref); err != nil {
		t.Fatal(err)
	}
	connection := entity.IntegrationConnection{Ref: ref}
	if err := repository.attachIntegrationDefinitionBinding(ctx, tx, scope, &connection); !errors.Is(err, errs.ErrUnavailable) || connection.DefinitionConfigurationBinding != nil {
		t.Fatalf("ineligible existing binding became absence: %v", err)
	}
}
