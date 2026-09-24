package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
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

func testOpenAPIImportDraftLifecycle(t *testing.T, ctx context.Context, repository *Repository) {
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
		definition.Spec.Readiness != string(integrationpackage.ReadinessNotReady) ||
		len(definition.Spec.Capabilities) != 2 ||
		strings.Contains(created.ManagedRevision.Content, "openapi: 3.1.0") {
		t.Fatalf("imported draft lost pins or retained raw source: %v", err)
	}
	version := created.ManagedConfiguration.Version
	validated, err := service.Execute(ctx, command.Command{Kind: command.ValidateIntegrationDefinition, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "openapi-import-draft-validate", ExpectedVersion: &version},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref, RevisionRef: created.ManagedRevision.Ref}})
	if err != nil || validated.ManagedRevision == nil || validated.ManagedRevision.State != "INVALID" {
		t.Fatalf("unready adapter draft became publishable: %v", err)
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
