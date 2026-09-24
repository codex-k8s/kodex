package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	egresspolicy "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

const openAPIImportComponentSource = `openapi: 3.1.0
info: {title: Заявки, version: 1.0.0}
servers:
  - url: https://api.example.test
paths:
  /health:
    get:
      operationId: getHealth
      summary: Проверить сервис
      responses: {'200': {description: OK}}
  /tickets/{id}:
    parameters:
      - {name: id, in: path, required: true, schema: {type: integer}}
    patch:
      operationId: updateTicket
      summary: Изменить заявку
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              additionalProperties: false
              properties:
                status: {type: string}
              required: [status]
      responses: {'200': {description: OK}}
`

type importedOriginResolver struct{}

func (importedOriginResolver) Resolve(_ context.Context, host string) (egresspolicy.Snapshot, error) {
	if host != "api.example.test" {
		return egresspolicy.Snapshot{}, errors.New("unexpected test origin")
	}
	return egresspolicy.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("93.184.215.14")}, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func testOpenAPIImportLifecycle(t *testing.T, ctx context.Context, repository *Repository) {
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	payload := integrationpackage.OpenAPIImportPayload{Source: openAPIImportComponentSource,
		Options: integrationpackage.OpenAPIImportOptions{Version: "1.0.0", HealthOperationID: "getHealth",
			Choices: []integrationpackage.OpenAPIImportChoice{
				{OperationID: "getHealth", Risk: "READ", ApprovalPolicy: "NONE"},
				{OperationID: "updateTicket", Risk: "WRITE", ApprovalPolicy: "HUMAN_SCOPED"},
			}}}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateIntegrationDefinition, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "openapi-import-draft-create"},
		Payload:  command.ManagedConfigurationInput{Name: "Импорт заявок", ContentFormat: "OPENAPI_IMPORT", Content: string(encoded)}})
	if err != nil || created.ManagedRevision == nil || created.ManagedRevision.ContentFormat != "JSON" {
		t.Fatalf("imported draft was not stored canonically: %v", err)
	}
	definition, err := integrationpackage.Parse([]byte(created.ManagedRevision.Content))
	if err != nil || definition.Metadata.Origin != integrationpackage.OriginUI ||
		definition.Spec.Adapter != string(integrationpackage.AdapterOpenAPIMCP) ||
		definition.Spec.Readiness != string(integrationpackage.ReadinessReady) ||
		len(definition.Spec.Capabilities) != 2 ||
		strings.Contains(created.ManagedRevision.Content, "openapi: 3.1.0") {
		t.Fatalf("imported draft lost pins or retained raw source: %v", err)
	}
	version := created.ManagedConfiguration.Version
	validated, err := service.Execute(ctx, command.Command{Kind: command.ValidateIntegrationDefinition, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "openapi-import-draft-validate", ExpectedVersion: &version},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref, RevisionRef: created.ManagedRevision.Ref}})
	if err != nil || validated.ManagedRevision == nil || validated.ManagedRevision.State != "VALID" {
		t.Fatalf("bounded OpenAPI draft did not validate: %v", err)
	}
	connection, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "openapi-import-connection-create"},
		Payload:  command.ConnectionInput{DefinitionKey: "openapi-mcp", Name: "Заявки", PublicConfiguration: map[string]any{"base_url": "https://api.example.test"}}})
	if err != nil || connection.Connection == nil {
		t.Fatalf("create unbound OpenAPI connection: %v", err)
	}
	connectionVersion := connection.Connection.Version
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "openapi-import-unbound-test", ExpectedVersion: &connectionVersion},
		Payload:  command.ConnectionInput{Ref: connection.Connection.Ref}}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("unbound template became executable: %v", err)
	}
	version = validated.ManagedConfiguration.Version
	published, err := service.Execute(ctx, command.Command{Kind: command.PublishIntegrationDefinition, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "openapi-import-publish", ExpectedVersion: &version},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref, RevisionRef: created.ManagedRevision.Ref}})
	if err != nil || published.ManagedRevision == nil || published.ManagedRevision.State != "PUBLISHED" {
		t.Fatalf("publish OpenAPI definition: %v", err)
	}
	impact, err := service.GetManagedConfigurationImpact(ctx, owner, created.ManagedConfiguration.Ref, created.ManagedRevision.Ref, query.Filter{})
	if err != nil || impact.Digest == "" {
		t.Fatalf("read OpenAPI binding impact: %v", err)
	}
	version = published.ManagedConfiguration.Version
	_, err = service.Execute(ctx, command.Command{Kind: command.RebindIntegrationDefinition, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "openapi-import-bind", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref, RevisionRef: created.ManagedRevision.Ref,
			ImpactDigest: impact.Digest, Consumers: []entity.ManagedConfigurationConsumer{{Kind: "INTEGRATION_CONNECTION", Ref: connection.Connection.Ref, ExpectedAbsent: true}}}})
	if err != nil {
		t.Fatalf("bind OpenAPI connection: %v", err)
	}
	hosts, err := repository.IntegrationEgressHostnames(ctx)
	if err != nil || len(hosts) != 1 || hosts[0] != "api.example.test" {
		t.Fatalf("bound OpenAPI origin was not projected: hosts=%v err=%v", hosts, err)
	}
	projection, err := repository.PrepareIntegrationEgressProjection(ctx, strings.Repeat("a", 64), importedOriginResolver{})
	if err != nil || projection.Validate() != nil || len(projection.Destinations) != 1 ||
		projection.Destinations[0].Hostname != "api.example.test" {
		t.Fatalf("bound OpenAPI origin did not produce exact network pin: %#v err=%v", projection, err)
	}
	bound, err := service.GetIntegrationConnection(ctx, owner, connection.Connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "openapi-import-bound-test", ExpectedVersion: &bound.Version},
		Payload:  command.ConnectionInput{Ref: bound.Ref}}); err != nil {
		t.Fatalf("bound OpenAPI health operation was not accepted: %v", err)
	}
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "integration-gateway", Operation: "platform.runtime.integrations.tests.claim",
	}, "integration-gateway")
	claimTest := func() map[string]any {
		t.Helper()
		claims, err := service.ClaimIntegrationConnectionTests(ctx, worker, "openapi-import-worker", 32)
		if err != nil {
			t.Fatal(err)
		}
		for _, claim := range claims {
			if stringMap(claim, "connectionRef") == bound.Ref {
				return claim
			}
		}
		return nil
	}
	first := claimTest()
	if first == nil {
		t.Fatal("bound OpenAPI READ-test was not claimed")
	}
	complete := func(claim map[string]any, key string, success bool, code string) (command.Result, error) {
		t.Helper()
		return service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.IntegrationConnectionTestInput{
				TestRef: stringMap(claim, "testRef"), LeaseRef: stringMap(claim, "leaseRef"),
				Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64),
				Success: success, SafeErrorCode: code,
			}})
	}
	retry, err := complete(first, "openapi-import-first-unavailable", false, "INTEGRATION_UNAVAILABLE")
	if err != nil || retry.Connection == nil || retry.Connection.State != "TESTING" {
		t.Fatalf("transient OpenAPI network failure was not requeued: %v", err)
	}
	if claimTest() != nil {
		t.Fatal("OpenAPI retry ignored bounded backoff")
	}
	if tag, err := repository.pool.Exec(ctx, `UPDATE control_plane.integration_connection_tests
SET updated_at=clock_timestamp()-INTERVAL '6 seconds' WHERE ref=$1 AND state='DUE'`, stringMap(first, "testRef")); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("advance disposable retry clock: %v", err)
	}
	second := claimTest()
	if second == nil || second["generation"].(int64) <= first["generation"].(int64) ||
		stringMap(second, "leaseRef") == stringMap(first, "leaseRef") {
		t.Fatal("OpenAPI retry reused an old claim or lease")
	}
	if _, err := complete(first, "openapi-import-stale-fence", true, ""); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("stale OpenAPI test fence completed new attempt: %v", err)
	}
	done, err := complete(second, "openapi-import-second-success", true, "")
	if err != nil || done.Connection == nil || done.Connection.State != "CONNECTED" {
		t.Fatalf("OpenAPI retry did not reach connected state: %v", err)
	}
	bound, err = service.GetIntegrationConnection(ctx, owner, connection.Connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "openapi-import-permanent-test", ExpectedVersion: &bound.Version},
		Payload: command.ConnectionInput{Ref: bound.Ref}}); err != nil {
		t.Fatalf("start permanent failure test: %v", err)
	}
	permanentClaim := claimTest()
	if permanentClaim == nil {
		t.Fatal("permanent failure test was not claimed")
	}
	permanent, err := complete(permanentClaim, "openapi-import-permanent-failure", false, "INTEGRATION_RESPONSE_INVALID")
	if err != nil || permanent.Connection == nil || permanent.Connection.State != "DEGRADED" || claimTest() != nil {
		t.Fatalf("permanent OpenAPI failure was retried: state=%v err=%v", permanent.Connection, err)
	}
	bound, err = service.GetIntegrationConnection(ctx, owner, connection.Connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "openapi-import-disable-due-test", ExpectedVersion: &bound.Version},
		Payload: command.ConnectionInput{Ref: bound.Ref}}); err != nil {
		t.Fatalf("start disable-due test: %v", err)
	}
	dueClaim := claimTest()
	if dueClaim == nil {
		t.Fatal("disable-due test was not claimed")
	}
	due, err := complete(dueClaim, "openapi-import-disable-due-failure", false, "INTEGRATION_UNAVAILABLE")
	if err != nil || due.Connection == nil || due.Connection.State != "TESTING" {
		t.Fatalf("disable-due test was not requeued: %v", err)
	}
	disabled, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "openapi-import-disable", ExpectedVersion: &due.Connection.Version},
		Payload:  command.ConnectionInput{Ref: bound.Ref, Enabled: false}})
	if err != nil || disabled.Connection == nil || disabled.Connection.Enabled {
		t.Fatalf("disable bound OpenAPI connection: %v", err)
	}
	if claimTest() != nil {
		t.Fatal("disabled OpenAPI connection reclaimed a queued test")
	}
	hosts, err = repository.IntegrationEgressHostnames(ctx)
	if err != nil || len(hosts) != 0 {
		t.Fatalf("disabled OpenAPI origin remained authorized: hosts=%v err=%v", hosts, err)
	}
	revoked, err := repository.PrepareIntegrationEgressProjection(ctx, strings.Repeat("a", 64), noOriginResolver{})
	if err != nil || revoked.Validate() != nil || revoked.Generation != projection.Generation+1 || len(revoked.Destinations) != 0 {
		t.Fatalf("disabled OpenAPI origin did not revoke network projection: %#v err=%v", revoked, err)
	}
	payload.Options.Choices[1].ApprovalPolicy = "NONE"
	encoded, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.CreateIntegrationDefinition, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "openapi-import-draft-unsafe"},
		Payload:  command.ManagedConfigurationInput{Name: "Небезопасный импорт", ContentFormat: "OPENAPI_IMPORT", Content: string(encoded)}})
	if !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("write without gate was stored: %v", err)
	}
}
