package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// integrationCredentialRepository maps synthetic external principal to owner identity and records
// owner reads and commands for integration credential boundary tests.
// integrationCredentialRepository переводит синтетический внешний principal в identity владельца и
// записывает чтения и команды для тестов границы реквизитов интеграции.
type integrationCredentialRepository struct {
	platformrepo.Repository
	resolveCalls  int
	readPrincipal value.Principal
	executed      command.Command
	connection    *entity.IntegrationConnection
}

// ResolvePrincipal accepts only the synthetic external identity, counts resolution and replaces
// actor and tenant with fixture owner values.
// ResolvePrincipal принимает только синтетическую внешнюю identity, подсчитывает разрешение и
// заменяет actor и tenant тестовыми значениями владельца.
func (repository *integrationCredentialRepository) ResolvePrincipal(_ context.Context, principal value.Principal) (value.Principal, error) {
	repository.resolveCalls++
	if principal.ActorID != "external-actor" || principal.AuthorityTenant != "external-tenant" {
		return value.Principal{}, errs.ErrForbidden
	}
	principal.ActorID = "usr_owner"
	principal.AuthorityTenant = "org_owner"
	return principal, nil
}

// GetIntegrationConnection records the resolved read principal and returns an override connection
// or a synthetic configure-credential-capable GitHub connection.
// GetIntegrationConnection записывает разрешённый principal чтения и возвращает заданное
// подключение либо синтетическое GitHub-подключение с действием configure credential.
func (repository *integrationCredentialRepository) GetIntegrationConnection(_ context.Context, principal value.Principal, ref string) (entity.IntegrationConnection, error) {
	repository.readPrincipal = principal
	if repository.connection != nil {
		return *repository.connection, nil
	}
	return entity.IntegrationConnection{
		Ref: ref, DefinitionKey: "github", Version: 3, CredentialSecretKey: "token", NextActions: []string{"CONFIGURE_CREDENTIAL"},
	}, nil
}

// TestConfigureIntegrationCredentialRequiresPublishedOpenAPIPin checks an unmodified shipped
// OpenAPI template cannot configure credentials while an owner-read published-definition pin
// enables the typed command.
// TestConfigureIntegrationCredentialRequiresPublishedOpenAPIPin проверяет запрет настройки
// реквизитов для неизменённого shipped OpenAPI-шаблона и допуск типизированной команды по
// прочитанному владельцем pin опубликованного определения.
func TestConfigureIntegrationCredentialRequiresPublishedOpenAPIPin(t *testing.T) {
	t.Parallel()
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	base := definitions["openapi-mcp"]
	repository := &integrationCredentialRepository{connection: &entity.IntegrationConnection{
		Ref: "intconn_openapi", DefinitionKey: "openapi-mcp", DefinitionDigest: base.Digest,
		Version: 3, CredentialSecretKey: "token", NextActions: []string{"CONFIGURE_CREDENTIAL"},
		Capabilities: []entity.IntegrationCapability{{Key: "openapi.read"}},
	}}
	service, err := New(repository, WithCredentialMaterializer(integrationCredentialMaterializer{}))
	if err != nil {
		t.Fatal(err)
	}
	version := int64(3)
	principal := value.Principal{ActorID: "external-actor", AuthorityTenant: "external-tenant", Permission: "platform.command.integrations.configure-credential", CorrelationRef: "correlation-openapi", CallerWorkload: "control-api-gateway", CredentialRevision: 1}
	mutation := value.Mutation{IdempotencyKey: "openapi-credential-fixture", ExpectedVersion: &version}
	_, err = service.ConfigureIntegrationCredential(context.Background(), principal, mutation, repository.connection.Ref, []byte("fixture-value"))
	if !errors.Is(err, errs.ErrForbidden) || repository.executed.Kind != "" {
		t.Fatalf("unpublished OpenAPI template admitted: %v", err)
	}
	repository.connection.DefinitionDigest = "sha256:published-owner-revision"
	_, err = service.ConfigureIntegrationCredential(context.Background(), principal, mutation, repository.connection.Ref, []byte("fixture-value"))
	if err != nil || repository.executed.Kind != command.ConfigureConnectionCredential {
		t.Fatalf("published OpenAPI credential rejected: %v", err)
	}
}

// Execute records the integration command and returns a synthetic version-four connection matching
// its payload reference.
// Execute записывает команду интеграции и возвращает синтетическое подключение четвёртой версии со
// ссылкой из payload.
func (repository *integrationCredentialRepository) Execute(_ context.Context, input command.Command) (command.Result, error) {
	repository.executed = input
	connection := entity.IntegrationConnection{Ref: input.Payload.(command.ConnectionInput).Ref, Version: 4}
	return command.Result{Connection: &connection}, nil
}

// integrationCredentialMaterializer supplies fixed synthetic storage metadata for local
// integration credential tests without storing plaintext.
// integrationCredentialMaterializer предоставляет фиксированные синтетические метаданные хранения
// для локальных тестов реквизитов интеграции без сохранения plaintext.
type integrationCredentialMaterializer struct{}

// Materialize returns a fixed synthetic immutable descriptor without using the supplied identity
// or credential bytes.
// Materialize возвращает фиксированный синтетический immutable-дескриптор без использования
// переданных identity либо байтов реквизитов.
func (integrationCredentialMaterializer) Materialize(_ context.Context, _ string, _ []byte) (MaterializedCredential, error) {
	return MaterializedCredential{
		SecretRef: "integration-credentials", SecretUID: "secret-uid", SecretResourceVersion: "12",
		ContentSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, nil
}

// TestConfigureIntegrationCredentialDoesNotResolveOpaquePrincipalAgain checks credential reads and
// commands use the once-resolved owner identity with operation/intent pins instead of resolving
// opaque identity a second time.
// TestConfigureIntegrationCredentialDoesNotResolveOpaquePrincipalAgain проверяет использование
// единожды разрешённой identity владельца с pins operation и intent в чтении и командах реквизитов
// без второго разрешения opaque identity.
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
