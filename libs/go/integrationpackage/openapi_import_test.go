package integrationpackage

import (
	"strings"
	"testing"
)

const openAPIImportFixture = `openapi: 3.1.0
info: {title: Заявки, version: 1.0.0}
servers:
  - url: https://api.example.test
paths:
  /health:
    get:
      operationId: getHealth
      summary: Проверить сервис
      responses:
        '200': {description: OK}
  /tickets/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema: {type: integer}
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
                status: {type: string, maxLength: 32}
              required: [status]
      responses:
        '200': {description: OK}
`

func openAPIImportOptions() OpenAPIImportOptions {
	return OpenAPIImportOptions{Version: "1.0.0", HealthOperationID: "getHealth", Choices: []OpenAPIImportChoice{
		{OperationID: "getHealth", Risk: "READ", ApprovalPolicy: "NONE"},
		{OperationID: "updateTicket", Risk: "WRITE", ApprovalPolicy: "HUMAN_SCOPED"},
	}}
}

func TestDraftOpenAPIPackagePinsSelectedOperationsAndSource(t *testing.T) {
	definition, err := DraftOpenAPIPackage(t.Context(), []byte(openAPIImportFixture), openAPIImportOptions())
	if err != nil {
		t.Fatal(err)
	}
	if definition.Metadata.Key != "openapi-mcp" || definition.Metadata.Origin != OriginUI ||
		definition.Spec.Readiness != string(ReadinessNotReady) || len(definition.Spec.Capabilities) != 2 ||
		definition.Spec.Credential != nil || definition.Digest == "" {
		t.Fatalf("unsafe or incomplete import: %#v", definition.Metadata)
	}
	if definition.ValidateConfiguration(map[string]string{"base_url": "https://api.example.test"}) != nil {
		t.Fatal("selected HTTPS origin rejected")
	}
	if definition.Spec.Capabilities[0].OpenAPI.ServerOrigin != "https://api.example.test" ||
		len(definition.Spec.Capabilities[0].OpenAPI.SourceDigest) != 64 ||
		definition.Spec.HealthCheck.Operation != definition.Spec.Capabilities[0].Operation {
		t.Fatal("source, server or health operation not pinned")
	}
	write := definition.Spec.Capabilities[1]
	if write.OpenAPI.Path != "/tickets/{id}" || write.OpenAPI.Method != "PATCH" ||
		write.Execution.Idempotency != string(IdempotencyOneAttempt) ||
		write.ValidateApprovalScopePaths([]string{"/path/id"}) != nil {
		t.Fatal("selected write binding is incomplete")
	}
	if _, err := write.ValidateInput([]byte(`{"path":{"id":3},"body":{"status":"DONE"}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := write.ValidateInput([]byte(`{"path":{"id":3},"body":{"unknown":"DONE"}}`)); err == nil {
		t.Fatal("unselected body field accepted")
	}
}

func TestDraftOpenAPIPackageRejectsUnsafeSelections(t *testing.T) {
	for name, mutate := range map[string]func(*OpenAPIImportOptions){
		"missing health":  func(value *OpenAPIImportOptions) { value.HealthOperationID = "updateTicket" },
		"read write risk": func(value *OpenAPIImportOptions) { value.Choices[1].Risk = "READ" },
		"write without gate": func(value *OpenAPIImportOptions) {
			value.Choices[1].ApprovalPolicy = "NONE"
		},
		"duplicate": func(value *OpenAPIImportOptions) {
			value.Choices = append(value.Choices, value.Choices[1])
		},
		"unknown": func(value *OpenAPIImportOptions) { value.Choices[1].OperationID = "foreign" },
	} {
		t.Run(name, func(t *testing.T) {
			options := openAPIImportOptions()
			mutate(&options)
			if _, err := DraftOpenAPIPackage(t.Context(), []byte(openAPIImportFixture), options); err == nil {
				t.Fatal("unsafe operation selection accepted")
			}
		})
	}
	for name, raw := range map[string]string{
		"open body":      strings.Replace(openAPIImportFixture, "              additionalProperties: false\n", "", 1),
		"foreign origin": strings.Replace(openAPIImportFixture, "https://api.example.test", "http://api.example.test", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DraftOpenAPIPackage(t.Context(), []byte(raw), openAPIImportOptions()); err == nil {
				t.Fatal("unsafe OpenAPI document imported")
			}
		})
	}
}

func TestDraftOpenAPIPackageDerivesOnlySupportedAuthentication(t *testing.T) {
	bearer := strings.Replace(openAPIImportFixture, "paths:\n", `components:
  securitySchemes:
    token:
      type: http
      scheme: bearer
security:
  - token: []
paths:
`, 1)
	definition, err := DraftOpenAPIPackage(t.Context(), []byte(bearer), openAPIImportOptions())
	if err != nil || definition.Spec.Credential == nil || definition.Spec.Credential.SecretKey != "token" ||
		definition.Spec.Capabilities[1].OpenAPI.AuthScheme != "BEARER" {
		t.Fatalf("bearer contract was not derived: %v", err)
	}
	apiKey := strings.Replace(bearer, "type: http\n      scheme: bearer", "type: apiKey\n      in: header\n      name: X-Service-Key", 1)
	definition, err = DraftOpenAPIPackage(t.Context(), []byte(apiKey), openAPIImportOptions())
	if err != nil || definition.Spec.Capabilities[1].OpenAPI.AuthScheme != "API_KEY_HEADER" ||
		definition.Spec.Capabilities[1].OpenAPI.AuthHeader != "X-Service-Key" {
		t.Fatalf("API key contract was not derived: %v", err)
	}
	for name, raw := range map[string]string{
		"basic":  strings.Replace(bearer, "scheme: bearer", "scheme: basic", 1),
		"cookie": strings.Replace(apiKey, "in: header", "in: cookie", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DraftOpenAPIPackage(t.Context(), []byte(raw), openAPIImportOptions()); err == nil {
				t.Fatal("unsupported authentication accepted")
			}
		})
	}
}
