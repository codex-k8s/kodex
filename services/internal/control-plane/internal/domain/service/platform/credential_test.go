package platform

import (
	"context"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

type integrationCredentialRepository struct {
	platformrepo.Repository
	resolveCalls  int
	readPrincipal value.Principal
	executed      command.Command
}

func (repository *integrationCredentialRepository) ResolvePrincipal(_ context.Context, principal value.Principal) (value.Principal, error) {
	repository.resolveCalls++
	if principal.ActorID != "external-actor" || principal.AuthorityTenant != "external-tenant" {
		return value.Principal{}, errs.ErrForbidden
	}
	principal.ActorID = "usr_owner"
	principal.AuthorityTenant = "org_owner"
	return principal, nil
}

func (repository *integrationCredentialRepository) GetIntegrationConnection(_ context.Context, principal value.Principal, ref string) (entity.IntegrationConnection, error) {
	repository.readPrincipal = principal
	return entity.IntegrationConnection{
		Ref: ref, DefinitionKey: "github", Version: 3, CredentialSecretKey: "token", NextActions: []string{"CONFIGURE_CREDENTIAL"},
	}, nil
}

func (repository *integrationCredentialRepository) Execute(_ context.Context, input command.Command) (command.Result, error) {
	repository.executed = input
	connection := entity.IntegrationConnection{Ref: input.Payload.(command.ConnectionInput).Ref, Version: 4}
	return command.Result{Connection: &connection}, nil
}

type integrationCredentialMaterializer struct{}

func (integrationCredentialMaterializer) Materialize(_ context.Context, _ string, _ []byte) (MaterializedCredential, error) {
	return MaterializedCredential{
		SecretRef: "integration-credentials", SecretUID: "secret-uid", SecretResourceVersion: "12",
		ContentSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, nil
}

func TestConfigureIntegrationCredentialDoesNotResolveOpaquePrincipalAgain(t *testing.T) {
	t.Parallel()
	repository := &integrationCredentialRepository{}
	service, err := New(repository, WithCredentialMaterializer(integrationCredentialMaterializer{}))
	if err != nil {
		t.Fatal(err)
	}
	version := int64(3)
	principal := value.Principal{
		ActorID: "external-actor", AuthorityTenant: "external-tenant", Permission: "platform.command.integrations.configure-credential",
		CorrelationRef: "correlation-test", CallerWorkload: "control-api-gateway", CredentialRevision: 1,
	}
	connection, err := service.ConfigureIntegrationCredential(
		context.Background(), principal,
		value.Mutation{IdempotencyKey: "integration-credential-test", ExpectedVersion: &version},
		"intconn_test", []byte("synthetic-token"),
	)
	if err != nil {
		t.Fatalf("configure credential: %v", err)
	}
	if connection.Ref != "intconn_test" || repository.resolveCalls != 1 {
		t.Fatalf("connection/resolve calls = %q/%d", connection.Ref, repository.resolveCalls)
	}
	if repository.readPrincipal.ActorID != "usr_owner" || repository.executed.Principal.ActorID != "usr_owner" {
		t.Fatalf("resolved principals = %#v/%#v", repository.readPrincipal, repository.executed.Principal)
	}
	if repository.executed.Kind != command.ConfigureConnectionCredential || repository.executed.Mutation.Operation == "" || repository.executed.Mutation.IntentDigest == "" {
		t.Fatalf("executed command = %#v", repository.executed)
	}
}
