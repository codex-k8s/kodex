package integrationpackage

import (
	"strings"
	"testing"
)

const openAPIFixture = `openapi: 3.1.0
info:
  title: Заявки
  version: 1.0.0
servers:
  - url: https://api.example.test
paths:
  /v1/applications/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema: {type: string}
    get:
      operationId: getApplication
      summary: Прочитать заявку
      responses:
        '200': {description: OK}
    patch:
      operationId: updateApplication
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
      responses:
        '200': {description: OK}
`

func TestInspectOpenAPI(t *testing.T) {
	result, err := InspectOpenAPI(t.Context(), []byte(openAPIFixture))
	if err != nil {
		t.Fatalf("valid fixture rejected: %v", err)
	}
	if result.Title != "Заявки" || result.Version != "1.0.0" || len(result.Digest) != 64 || len(result.Operations) != 2 {
		t.Fatalf("inspection lost document metadata: %#v", result)
	}
	for _, operation := range result.Operations {
		if !operation.Candidate || operation.HealthCandidate || operation.Path != "/v1/applications/{id}" || operation.ServerOrigin != "https://api.example.test" {
			t.Fatalf("bounded operation rejected: %#v", operation)
		}
	}
}

func TestInspectOpenAPIIdentifiesParameterFreeHealthRead(t *testing.T) {
	result, err := InspectOpenAPI(t.Context(), []byte(openAPIImportFixture))
	if err != nil || len(result.Operations) != 2 || !result.Operations[0].HealthCandidate || result.Operations[1].HealthCandidate {
		t.Fatalf("health candidate mismatch: %v %#v", err, result.Operations)
	}
}

func TestInspectOpenAPIRejectsUnpinnedOrUnsafeServer(t *testing.T) {
	for name, raw := range map[string]string{
		"missing":  strings.Replace(openAPIFixture, "servers:\n  - url: https://api.example.test\n", "", 1),
		"http":     strings.Replace(openAPIFixture, "https://api.example.test", "http://api.example.test", 1),
		"variable": strings.Replace(openAPIFixture, "https://api.example.test", "https://{host}.example.test", 1),
		"path":     strings.Replace(openAPIFixture, "https://api.example.test", "https://api.example.test/v1", 1),
	} {
		t.Run(name, func(t *testing.T) {
			result, err := InspectOpenAPI(t.Context(), []byte(raw))
			if err != nil {
				return
			}
			for _, operation := range result.Operations {
				if operation.Candidate || operation.Reason != "SERVER_ORIGIN_UNSUPPORTED" {
					t.Fatalf("unsafe server is importable: %#v", operation)
				}
			}
		})
	}
}

func TestInspectOpenAPIResolvesOnlyLocalReferences(t *testing.T) {
	raw := strings.Replace(openAPIFixture, "schema: {type: string}",
		"schema: {$ref: '#/components/schemas/ApplicationId'}", 1)
	raw += `components:
  schemas:
    ApplicationId:
      type: string
      minLength: 1
`
	result, err := InspectOpenAPI(t.Context(), []byte(raw))
	if err != nil || len(result.Operations) != 2 {
		t.Fatalf("local reference rejected: %v", err)
	}
	for _, operation := range result.Operations {
		if !operation.Candidate {
			t.Fatalf("resolved local reference became unavailable: %#v", operation)
		}
	}
}

func TestInspectOpenAPIRejectsUnsafeDocument(t *testing.T) {
	for name, raw := range map[string]string{
		"duplicate key":      strings.Replace(openAPIFixture, "  version: 1.0.0", "  version: 1.0.0\n  version: 2.0.0", 1),
		"external reference": strings.Replace(openAPIFixture, "schema: {type: string}", "schema: {$ref: 'https://example.invalid/schema.json'}", 1),
		"YAML alias":         strings.Replace(openAPIFixture, "  version: 1.0.0", "  version: &version 1.0.0", 1),
		"second document":    openAPIFixture + "---\nopenapi: 3.1.0\n",
		"no operations":      "openapi: 3.1.0\ninfo: {title: Empty, version: 1.0.0}\npaths: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := InspectOpenAPI(t.Context(), []byte(raw)); err == nil {
				t.Fatal("unsafe OpenAPI document accepted")
			}
		})
	}
}

func TestInspectOpenAPIMarksUnsupportedMethod(t *testing.T) {
	raw := strings.Replace(openAPIFixture, "    patch:\n", "    head:\n", 1)
	result, err := InspectOpenAPI(t.Context(), []byte(raw))
	if err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}
	for _, operation := range result.Operations {
		if operation.ID == "updateApplication" && (operation.Candidate || operation.Reason != "HTTP_METHOD_UNSUPPORTED") {
			t.Fatalf("unsupported operation hidden or enabled: %#v", operation)
		}
	}
}

func TestInspectOpenAPIMarksNonJSONSuccessResponseUnsupported(t *testing.T) {
	raw := strings.Replace(openAPIFixture, "'200': {description: OK}",
		"'200': {description: OK, content: {text/plain: {schema: {type: string}}}}", 1)
	result, err := InspectOpenAPI(t.Context(), []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range result.Operations {
		if operation.ID == "getApplication" && (operation.Candidate || operation.Reason != "RESPONSE_MEDIA_UNSUPPORTED") {
			t.Fatalf("non-JSON response became callable: %#v", operation)
		}
	}
}

func TestInspectOpenAPIMarksOperationsRejectedByImporter(t *testing.T) {
	for name, fixture := range map[string]struct {
		raw, reason string
	}{
		"unsupported basic authentication": {
			raw: strings.Replace(openAPIImportFixture, "paths:\n", `components:
  securitySchemes:
    password:
      type: http
      scheme: basic
security:
  - password: []
paths:
`, 1), reason: "SECURITY_SCHEME_UNSUPPORTED",
		},
		"open request body": {
			raw: strings.Replace(openAPIImportFixture, "              additionalProperties: false\n", "", 1), reason: "INPUT_SCHEMA_UNSUPPORTED",
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := InspectOpenAPI(t.Context(), []byte(fixture.raw))
			if err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, operation := range result.Operations {
				if operation.ID != "updateTicket" {
					continue
				}
				found = true
				if operation.Candidate || operation.HealthCandidate || operation.Reason != fixture.reason {
					t.Fatalf("non-importable operation appears selectable: %#v", operation)
				}
			}
			if !found {
				t.Fatal("operation disappeared from inspection")
			}
		})
	}
}
