package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// personalCredentialComponentMaterializer stores only synthetic test bytes under deterministic
// keys, reporting whether a lost owner response caused a second materializer invocation.
// personalCredentialComponentMaterializer хранит только synthetic test bytes по детерминированным
// ключам и позволяет проверить второй вызов materializer после потерянного ответа владельца.
type personalCredentialComponentMaterializer struct {
	values map[string][]byte
	calls  int
}

// Materialize returns exact synthetic metadata and rejects different content under one key.
// Materialize возвращает точные synthetic metadata и отклоняет другой content по прежнему key.
func (materializer *personalCredentialComponentMaterializer) Materialize(_ context.Context, key string, raw []byte) (platformservice.MaterializedCredential, error) {
	materializer.calls++
	if materializer.values == nil {
		materializer.values = make(map[string][]byte)
	}
	if previous, exists := materializer.values[key]; exists && string(previous) != string(raw) {
		return platformservice.MaterializedCredential{}, platformservice.ErrCredentialMaterializationConflict
	}
	materializer.values[key] = append([]byte(nil), raw...)
	hash := sha256.Sum256(raw)
	return platformservice.MaterializedCredential{SecretRef: "kodex-system/kodex-integration-credentials#" + key,
		SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: strconv.Itoa(len(materializer.values)),
		ContentSHA256: hex.EncodeToString(hash[:])}, nil
}

// lostPersonalCredentialResponse wraps a real owner transaction and loses one successful response.
// It does not simulate a database rollback or claim to test network/JWT transport.
// lostPersonalCredentialResponse оборачивает реальную owner transaction и теряет один успешный ответ.
// Откат БД не имитируется; проверка network/JWT transport не заявляется.
type lostPersonalCredentialResponse struct {
	port.Repository
	owner *Repository
	lose  bool
}

// Execute commits through the real owner once before returning the injected lost-response error.
// Execute один раз выполняет реальный owner commit до внесённой ошибки потерянного ответа.
func (repository *lostPersonalCredentialResponse) Execute(ctx context.Context, input command.Command) (command.Result, error) {
	result, err := repository.owner.Execute(ctx, input)
	if err == nil && repository.lose && input.Kind == command.ConfigurePersonalIntegrationCredential {
		repository.lose = false
		return command.Result{}, errs.ErrUnavailable
	}
	return result, err
}

// GetPersonalIntegrationCredential preserves the real owner's self-scoped addressed reader.
// GetPersonalIntegrationCredential сохраняет реальный self-scoped адресный reader владельца.
func (repository *lostPersonalCredentialResponse) GetPersonalIntegrationCredential(ctx context.Context, principal value.Principal, ref string) (entity.PersonalIntegrationCredential, error) {
	return repository.owner.GetPersonalIntegrationCredential(ctx, principal, ref)
}

// ReadPersonalIntegrationCredentialReceipt preserves the real durable receipt reconciliation.
// ReadPersonalIntegrationCredentialReceipt сохраняет реальную сверку durable receipt.
func (repository *lostPersonalCredentialResponse) ReadPersonalIntegrationCredentialReceipt(ctx context.Context, principal value.Principal, mutation value.Mutation, payload command.PersonalIntegrationCredentialInput) (entity.PersonalIntegrationCredential, bool, error) {
	return repository.owner.ReadPersonalIntegrationCredentialReceipt(ctx, principal, mutation, payload)
}

// ListPersonalIntegrationConnections preserves the real owner's bounded, freshly authorized catalog.
// ListPersonalIntegrationConnections сохраняет реальный bounded каталог владельца со свежим допуском.
func (repository *lostPersonalCredentialResponse) ListPersonalIntegrationConnections(ctx context.Context, principal value.Principal, filter query.PersonalIntegrationConnectionFilter) ([]entity.PersonalIntegrationConnection, string, error) {
	return repository.owner.ListPersonalIntegrationConnections(ctx, principal, filter)
}

// TestPersonalIntegrationCredentialsComponent proves durable self ownership, same-key isolation,
// lost-response reconciliation, OCC/revoke and immutable history. Principals are verified fixture
// inputs; real RPC authentication, provider tests, launch and gateway paths are separate tests.
// TestPersonalIntegrationCredentialsComponent проверяет durable self ownership, изоляцию одинаковых
// keys, lost-response reconciliation, OCC/revoke и immutable history. Principals — проверенные
// fixture inputs; реальные RPC authentication, provider tests, launch и gateway проверяются отдельно.
func TestPersonalIntegrationCredentialsComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated personal credential database")
	}
	defer pool.Close()
	r := newSyntheticRuntimeComponentRepository(t, ctx, pool)
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create"}, "control-api-gateway")
	materializer := &personalCredentialComponentMaterializer{}
	lost := &lostPersonalCredentialResponse{Repository: r, owner: r, lose: true}
	service, err := platformservice.New(lost, platformservice.WithCredentialMaterializer(materializer))
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-connection-create"}, Payload: command.ConnectionInput{
			DefinitionKey: "gitlab", Name: "Personal catalog fixture one", PublicConfiguration: map[string]any{"base_url": "https://git.example.invalid", "project_path": "fixture/project"}}})
	if err != nil || created.Connection == nil {
		t.Fatal("create synthetic personal Connection", err)
	}
	connection := *created.Connection
	owner.Permission = "platform.integrations.credential-policy.set"
	policyInput := command.IntegrationCredentialPolicyInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialInitiatorRequired}
	if _, err := service.Execute(ctx, command.Command{Kind: command.SetIntegrationCredentialPolicy, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-policy-enabled", ExpectedVersion: &connection.Version}, Payload: policyInput}); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("policy switch on an enabled Connection was admitted", err)
	}
	owner.Permission = "platform.command.integrations.enable"
	disabled, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-policy-disable", ExpectedVersion: &connection.Version},
		Payload:  command.ConnectionInput{Ref: connection.Ref, Enabled: false}})
	if err != nil || disabled.Connection == nil {
		t.Fatal("disable Connection before changing policy", err)
	}
	owner.Permission = "platform.integrations.credential-policy.set"
	policyMutation := value.Mutation{IdempotencyKey: "personal-policy-select", ExpectedVersion: &disabled.Connection.Version}
	selected, err := service.Execute(ctx, command.Command{Kind: command.SetIntegrationCredentialPolicy, Principal: owner,
		Mutation: policyMutation, Payload: policyInput})
	if err != nil || selected.Connection == nil || selected.Connection.CredentialPolicy != entity.IntegrationCredentialInitiatorRequired {
		t.Fatal("select personal policy through the owner command", err)
	}
	connection = *selected.Connection
	sharedPrincipal := owner
	sharedPrincipal.Permission = "platform.command.integrations.configure-credential"
	if _, err := service.ConfigureIntegrationCredential(ctx, sharedPrincipal,
		value.Mutation{IdempotencyKey: "personal-policy-no-shared-write", ExpectedVersion: &connection.Version},
		connection.Ref, []byte("synthetic-shared-fallback")); !errors.Is(err, errs.ErrConflict) || materializer.calls != 0 {
		t.Fatal("personal policy materialized a common credential", err)
	}
	replayedPolicy, err := service.Execute(ctx, command.Command{Kind: command.SetIntegrationCredentialPolicy, Principal: owner,
		Mutation: policyMutation, Payload: policyInput})
	if err != nil || replayedPolicy.Connection == nil || replayedPolicy.Connection.Version != connection.Version {
		t.Fatal("policy lost-response replay did not preserve one owner write", err)
	}
	var secondID string
	if err := pool.QueryRow(ctx, `INSERT INTO control_plane.subjects(organization_id,ref,issuer,external_subject_digest,display_name)
        VALUES($1::uuid,'usr_personal_second','https://identity.example.invalid',repeat('d',64),'Second fixture user') RETURNING id::text`, owner.AuthorityTenant).Scan(&secondID); err != nil {
		t.Fatal("prepare second personal subject")
	}
	role, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-view-role"}, Payload: command.AccessRoleInput{
			Name: "Personal viewer", PermissionKeys: []string{"integration.view"}, AllowedScopes: []string{"RESOURCE_INSTANCE"}, ChangeComment: "Synthetic credential fixture"}})
	if err != nil || role.AccessRole == nil {
		t.Fatal("create exact view role", err)
	}
	viewBinding, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-view-binding"}, Payload: command.AccessBindingInput{
			SubjectKind: "USER", SubjectRef: "usr_personal_second", RoleVersionRef: role.AccessRole.CurrentVersion.Ref,
			Scope: entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connection.Ref}}})
	if err != nil || viewBinding.AccessBinding == nil {
		t.Fatal("bind exact personal Connection view", err)
	}
	second := owner
	second.ActorID = secondID
	if _, err := service.Execute(ctx, command.Command{Kind: command.SetIntegrationCredentialPolicy, Principal: second,
		Mutation: value.Mutation{IdempotencyKey: "personal-policy-viewer-denied", ExpectedVersion: &connection.Version},
		Payload:  command.IntegrationCredentialPolicyInput{ConnectionRef: connection.Ref, Policy: entity.IntegrationCredentialProjectShared}}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("self credential viewer changed the common selection policy", err)
	}
	create := func(principal value.Principal) entity.PersonalIntegrationCredential {
		t.Helper()
		principal.Permission = "platform.personal-integrations.create"
		result, err := service.Execute(ctx, command.Command{Kind: command.CreatePersonalIntegrationCredential, Principal: principal,
			Mutation: value.Mutation{IdempotencyKey: "same-personal-create-key"}, Payload: command.PersonalIntegrationCredentialInput{
				ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version}})
		if err != nil || result.PersonalIntegrationCredential == nil {
			t.Fatal("create self binding", err)
		}
		return *result.PersonalIntegrationCredential
	}
	firstBinding, secondBinding := create(owner), create(second)
	if firstBinding.Ref == secondBinding.Ref || firstBinding.SubjectID == secondBinding.SubjectID {
		t.Fatal("two users shared a personal binding")
	}
	owner.Permission = "platform.personal-integrations.configure"
	mutation := value.Mutation{IdempotencyKey: "same-personal-configure-key", ExpectedVersion: &firstBinding.Version}
	if _, err := service.ConfigurePersonalIntegrationCredential(ctx, owner, mutation, connection.Ref, connection.Version, []byte("synthetic-first-value")); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("lost owner response was not surfaced")
	}
	configured, err := service.ConfigurePersonalIntegrationCredential(ctx, owner, mutation, connection.Ref, connection.Version, []byte("synthetic-first-value"))
	if err != nil || configured.Version != 2 || configured.Credential == nil || materializer.calls != 1 {
		t.Fatal("lost response did not reconcile before second materialization", err)
	}
	if _, err := service.ConfigurePersonalIntegrationCredential(ctx, owner, mutation, connection.Ref, connection.Version, []byte("different-synthetic-value")); !errors.Is(err, errs.ErrIdempotencyReuse) || materializer.calls != 1 {
		t.Fatal("changed intent reused the committed receipt")
	}
	second.Permission = "platform.personal-integrations.configure"
	secondConfigured, err := service.ConfigurePersonalIntegrationCredential(ctx, second,
		value.Mutation{IdempotencyKey: mutation.IdempotencyKey, ExpectedVersion: &secondBinding.Version}, connection.Ref, connection.Version, []byte("synthetic-second-value"))
	if err != nil || secondConfigured.Credential == nil || secondConfigured.Credential.SecretRef == configured.Credential.SecretRef || materializer.calls != 2 {
		t.Fatal("same operation key collided across users", err)
	}
	owner.Permission = "platform.personal-integrations.revoke"
	revoked, err := service.Execute(ctx, command.Command{Kind: command.RevokePersonalIntegrationCredential, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-revoke-first", ExpectedVersion: &configured.Version},
		Payload:  command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref}})
	if err != nil || revoked.PersonalIntegrationCredential == nil || revoked.PersonalIntegrationCredential.State != "REVOKED" {
		t.Fatal("self revoke failed", err)
	}
	remaining, err := service.GetPersonalIntegrationCredential(ctx, second, connection.Ref)
	if err != nil || remaining.Ref != secondConfigured.Ref || remaining.Version != secondConfigured.Version || remaining.Credential.Ref != secondConfigured.Credential.Ref {
		t.Fatal("another user's revoke changed the personal binding", err)
	}
	owner.Permission = "platform.personal-integrations.configure"
	if _, err := service.ConfigurePersonalIntegrationCredential(ctx, owner, mutation, connection.Ref, connection.Version, []byte("synthetic-first-value")); err != nil || materializer.calls != 2 {
		t.Fatal("historical configure receipt repeated materialization after revoke", err)
	}
	current, err := service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
	if err != nil || current.State != "REVOKED" || current.Version != revoked.PersonalIntegrationCredential.Version {
		t.Fatal("historical configure receipt revived a revoked binding", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connections SET credential_revision_id=(
        SELECT revision.id FROM control_plane.integration_personal_credential_revisions revision WHERE revision.ref=$1)
        WHERE ref=$2`, secondConfigured.Credential.Ref, connection.Ref); err == nil {
		t.Fatal("personal revision was assignable to a shared Connection credential")
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_personal_credentials binding
        SET credential_revision_id=(SELECT id FROM control_plane.integration_personal_credential_revisions WHERE ref=$1),
            state='CHECK_REQUIRED',version=version+1,generation=generation+1
        WHERE binding.ref=$2`, secondConfigured.Credential.Ref, configured.Ref); err == nil {
		t.Fatal("foreign personal revision crossed the binding owner boundary")
	}
	wrongOperation := second
	wrongOperation.Permission = "platform.command.integrations.configure-credential"
	if _, err := service.Execute(ctx, command.Command{Kind: command.RevokePersonalIntegrationCredential, Principal: wrongOperation,
		Mutation: value.Mutation{IdempotencyKey: "personal-wrong-operation", ExpectedVersion: &secondConfigured.Version},
		Payload:  command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref}}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("shared credential operation authorized a personal revoke")
	}
	rotated, err := service.ConfigurePersonalIntegrationCredential(ctx, second,
		value.Mutation{IdempotencyKey: "personal-rotate-second", ExpectedVersion: &secondConfigured.Version},
		connection.Ref, connection.Version, []byte("synthetic-second-replacement"))
	if err != nil || rotated.Credential == nil || rotated.Credential.Revision != secondConfigured.Credential.Revision+1 ||
		rotated.Credential.Ref == secondConfigured.Credential.Ref || rotated.Generation != secondConfigured.Generation+1 || materializer.calls != 3 {
		t.Fatal("personal rotation did not append a fresh immutable revision", err)
	}
	current, err = service.GetPersonalIntegrationCredential(ctx, owner, connection.Ref)
	if err != nil || current.State != "REVOKED" || current.Version != revoked.PersonalIntegrationCredential.Version {
		t.Fatal("another user's rotation changed the revoked owner binding", err)
	}
	var revisions, shared int
	if pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.integration_personal_credential_revisions").Scan(&revisions) != nil || revisions != 3 ||
		pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.integration_connections WHERE ref=$1 AND credential_revision_id IS NOT NULL", connection.Ref).Scan(&shared) != nil || shared != 0 {
		t.Fatal("personal revisions were duplicated or published as a shared credential")
	}
	if _, err := pool.Exec(ctx, "UPDATE control_plane.integration_personal_credential_revisions SET content_sha256=$1", strings.Repeat("e", 64)); err == nil {
		t.Fatal("runtime rewrote immutable credential history")
	}
	assertPersonalVerificationRetryTransition(t, ctx, pool, rotated)
	connection, rotated = assertPersonalCredentialWork(t, ctx, r, service, owner, second, connection, rotated)
	assertPersonalCredentialCatalog(t, ctx, pool, service, owner, second, connection, current, rotated, *viewBinding.AccessBinding)
	assertPersonalGrantConfiguration(t, ctx, r, service, owner, second, connection)
	assertPersonalIntegrationRuntime(t, ctx, r, service, owner, second, connection)
	assertPersonalSystemAssistantRuntime(t, ctx, r, service, owner, connection.Ref)
	assertPersonalProjectAssistantRuntime(t, ctx, r, service, owner)
	assertPersonalWorkflowGateInitiator(t, ctx, r, service, owner, connection.Ref)
	assertPersonalDelegatedCredentialContinuation(t, ctx, r, service, owner, second, connection.Ref)
	assertPersonalConfluenceResourcePermission(t, ctx, r, service, owner)
	assertPersonalJiraResourcePermissions(t, ctx, r, service, owner)
	assertPersonalGitLabWritePermissions(t, ctx, r, service, owner)
	assertPersonalObjectReadPermissions(t, ctx, r, service, owner)
}

// assertPersonalVerificationRetryTransition qualifies the draft migration's guarded retry
// transition using isolated fixture SQL only. It rolls back both updates and does not claim
// to execute a worker, validate provider permissions or persist a production test receipt.
// assertPersonalVerificationRetryTransition квалифицирует ограждённый retry-переход draft migration
// только изолированным fixture SQL. Оба обновления откатываются; worker, provider permissions
// и сохранение production test receipt здесь не проверяются.
func assertPersonalVerificationRetryTransition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, binding entity.PersonalIntegrationCredential) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE control_plane.integration_personal_credentials
        SET state='CHECKING',version=version+1 WHERE ref=$1 AND version=$2 AND state='CHECK_REQUIRED'`, binding.Ref, binding.Version)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatal("migration rejected current verification start", err)
	}
	tag, err = tx.Exec(ctx, `UPDATE control_plane.integration_personal_credentials
        SET state='CHECK_REQUIRED',version=version+1 WHERE ref=$1 AND version=$2 AND state='CHECKING'`, binding.Ref, binding.Version+1)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatal("verification unavailability could not restore check-required", err)
	}
}

// assertPersonalCredentialCatalog checks real eligible pagination, absent-binding preservation,
// cross-actor/filter cursor rejection and fresh ACL revocation with two independently owned keys.
// This fixture has two Connections, not two projects or a real provider/JWT gateway flow.
// assertPersonalCredentialCatalog проверяет реальную eligible pagination, сохранение отсутствующей
// привязки, отказ cursor другого actor/filter и текущий отзыв ACL с двумя независимыми ключами.
// Fixture содержит два Connection, не два проекта либо реальный provider/JWT gateway flow.
func assertPersonalCredentialCatalog(t *testing.T, ctx context.Context, pool *pgxpool.Pool, service *platformservice.Service, owner, second value.Principal, connection entity.IntegrationConnection, firstBinding, secondBinding entity.PersonalIntegrationCredential, viewBinding entity.AccessBinding) {
	t.Helper()
	owner.Permission = "platform.command.integrations.create"
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-catalog-second-connection"}, Payload: command.ConnectionInput{
			DefinitionKey: "gitlab", Name: "Personal catalog fixture two", PublicConfiguration: map[string]any{"base_url": "https://git.example.invalid", "project_path": "fixture/second"}}})
	if err != nil || created.Connection == nil {
		t.Fatal("create second catalog Connection", err)
	}
	owner.Permission = "platform.personal-integrations.list"
	filter := query.PersonalIntegrationConnectionFilter{Query: "Personal catalog fixture", Page: query.Page{Size: 1}}
	first, token, err := service.ListPersonalIntegrationConnections(ctx, owner, filter)
	if err != nil || len(first) != 1 || token == "" {
		t.Fatal("bounded personal catalog first page failed", err)
	}
	filter.Page.Token = token
	next, finalToken, err := service.ListPersonalIntegrationConnections(ctx, owner, filter)
	if err != nil || len(next) != 1 || finalToken != "" || next[0].Ref == first[0].Ref {
		t.Fatal("eligible personal catalog continuation failed", err)
	}
	for _, item := range append(first, next...) {
		if !item.Available {
			t.Fatal("enabled fixture adapter was reported unavailable")
		}
		if item.Ref == connection.Ref {
			if item.Credential == nil || item.Credential.Ref != firstBinding.Ref || item.Credential.SubjectID != firstBinding.SubjectID ||
				item.Credential.State != "REVOKED" || item.Credential.Version != firstBinding.Version || item.Policy != entity.IntegrationCredentialInitiatorRequired {
				t.Fatal("owner catalog selected another user's current binding")
			}
		} else if item.Ref != created.Connection.Ref || item.Credential != nil || item.Policy != entity.IntegrationCredentialProjectShared {
			t.Fatal("catalog fabricated a binding or shared fallback")
		}
	}
	second.Permission = "platform.personal-integrations.list"
	if _, _, err := service.ListPersonalIntegrationConnections(ctx, second, filter); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("catalog accepted another actor's continuation", err)
	}
	filter.Query = "different filter"
	if _, _, err := service.ListPersonalIntegrationConnections(ctx, owner, filter); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("catalog accepted a cursor with a changed query", err)
	}
	filter.Query, filter.Page.Token = "Personal catalog fixture", ""
	items, nextToken, err := service.ListPersonalIntegrationConnections(ctx, second, filter)
	if err != nil || len(items) != 1 || nextToken != "" || items[0].Ref != connection.Ref || items[0].Credential == nil ||
		items[0].Credential.Ref != secondBinding.Ref || items[0].Credential.Credential == nil || items[0].Credential.Credential.Ref != secondBinding.Credential.Ref {
		t.Fatal("catalog crossed visibility or personal credential ownership", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE control_plane.integration_definitions SET enabled=false WHERE stable_key='gitlab'"); err != nil {
		t.Fatal("disable isolated catalog adapter fixture", err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, "UPDATE control_plane.integration_definitions SET enabled=true WHERE stable_key='gitlab'"); err != nil {
			t.Error("restore isolated catalog adapter fixture", err)
		}
	}()
	items, _, err = service.ListPersonalIntegrationConnections(ctx, second, filter)
	if err != nil || len(items) != 1 || items[0].Available || items[0].Credential == nil || items[0].Credential.Ref != secondBinding.Ref {
		t.Fatal("disabled adapter hid a self-owned credential needed for revoke", err)
	}
	second.Permission = "platform.personal-integrations.revoke"
	if result, err := service.Execute(ctx, command.Command{Kind: command.RevokePersonalIntegrationCredential, Principal: second,
		Mutation: value.Mutation{IdempotencyKey: "personal-catalog-disabled-self-revoke", ExpectedVersion: &secondBinding.Version},
		Payload:  command.PersonalIntegrationCredentialInput{ConnectionRef: connection.Ref}}); err != nil || result.PersonalIntegrationCredential == nil || result.PersonalIntegrationCredential.State != "REVOKED" {
		t.Fatal("disabled adapter prevented self revocation", err)
	}
	second.Permission = "platform.personal-integrations.list"
	owner.Permission = "platform.access.bindings.revoke"
	if _, err := service.Execute(ctx, command.Command{Kind: command.RevokeAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-catalog-revoke-view", ExpectedVersion: &viewBinding.Version},
		Payload:  command.AccessBindingInput{BindingRef: viewBinding.Ref}}); err != nil {
		t.Fatal("revoke personal catalog visibility", err)
	}
	items, nextToken, err = service.ListPersonalIntegrationConnections(ctx, second, filter)
	if err != nil || len(items) != 0 || nextToken != "" {
		t.Fatal("catalog retained a row after current ACL revocation", err)
	}
	if _, err := service.GetPersonalIntegrationCredential(ctx, second, connection.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("addressed self read retained revoked Connection access", err)
	}
}
