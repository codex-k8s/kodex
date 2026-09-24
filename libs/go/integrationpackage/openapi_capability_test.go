package integrationpackage

import (
	"encoding/json"
	"testing"
)

func openAPIPackageFixture(t *testing.T) Package {
	t.Helper()
	shipped, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	base := shipped["https-json"]
	base.Metadata.Key = "ticket-service"
	base.Metadata.Origin = OriginUI
	base.Spec.Adapter = string(AdapterOpenAPIMCP)
	base.Spec.Readiness = string(ReadinessNotReady)
	base.Spec.ConfigurationFields = base.Spec.ConfigurationFields[:1]
	base.Spec.HealthCheck.Operation = "openapi.health"
	base.Spec.Capabilities = []Capability{
		{
			Key: "health.read", Name: "Проверить сервис", Description: "Проверяет доступность сервиса.",
			Operation: "openapi.health", Risk: string(RiskRead), ApprovalPolicy: string(ApprovalNone),
			ResourceScope: ResourceScope{Kind: "HTTPS_RESOURCE", ConnectionFields: []string{"base_url"}},
			OutputFields:  base.Spec.Capabilities[0].OutputFields,
			Execution:     Execution{Idempotency: string(IdempotencyReadOnly), TimeoutSeconds: 20, MaxAttempts: 2, RetryBackoffMilliseconds: 250},
			OpenAPI: &OpenAPIHTTP{OperationID: "getHealth", Method: "GET", Path: "/health", AuthScheme: "BEARER",
				InputSchema: map[string]any{"type": "object", "additionalProperties": false,
					"properties": map[string]any{}}},
		},
		{
			Key: "ticket.update", Name: "Изменить заявку", Description: "Изменяет заявку по её номеру.",
			Operation: "openapi.ticket.update", Risk: string(RiskWrite), ApprovalPolicy: string(ApprovalHumanScoped),
			ResourceScope: ResourceScope{Kind: "HTTPS_RESOURCE", ConnectionFields: []string{"base_url"}},
			OutputFields:  base.Spec.Capabilities[0].OutputFields,
			Execution:     Execution{Idempotency: string(IdempotencyOneAttempt), TimeoutSeconds: 20, MaxAttempts: 1, RetryBackoffMilliseconds: 250},
			OpenAPI: &OpenAPIHTTP{OperationID: "updateTicket", Method: "PATCH", Path: "/tickets/{id}", AuthScheme: "BEARER",
				InputSchema: map[string]any{"type": "object", "additionalProperties": false,
					"properties": map[string]any{
						"path": map[string]any{"type": "object", "additionalProperties": false,
							"properties": map[string]any{"id": map[string]any{"type": "integer"}}, "required": []any{"id"}},
						"body": map[string]any{"type": "object", "additionalProperties": false,
							"properties": map[string]any{"title": map[string]any{"type": "string"}}},
					}, "required": []any{"path", "body"}}},
		},
	}
	return base
}

func TestOpenAPIPackageScopedApprovalIsNotExecutableBeforeGateway(t *testing.T) {
	candidate := openAPIPackageFixture(t)
	raw, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.ExecutableBy(OwnerIntegrationGateway, RouteManagedMCP) {
		t.Fatal("unimplemented OpenAPI adapter became executable")
	}
	write, ok := parsed.Capability("ticket.update")
	if !ok || write.ValidateApprovalScopePaths([]string{"/path/id"}) != nil {
		t.Fatal("scoped write binding is invalid")
	}
	shipped, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	legacy := shipped["github"]
	for index := range legacy.Spec.Capabilities {
		if legacy.Spec.Capabilities[index].Risk != string(RiskRead) {
			legacy.Spec.Capabilities[index].ApprovalPolicy = string(ApprovalHumanScoped)
			if validate(&legacy) == nil {
				t.Fatal("shipped adapter accepted scoped approval without executable owner path")
			}
			break
		}
	}
}

func TestOpenAPIPackageRejectsUnsafeWriteBinding(t *testing.T) {
	for name, mutate := range map[string]func(*Package){
		"write without approval":     func(value *Package) { value.Spec.Capabilities[1].ApprovalPolicy = string(ApprovalNone) },
		"retry without provider key": func(value *Package) { value.Spec.Capabilities[1].Execution.MaxAttempts = 2 },
		"unknown method":             func(value *Package) { value.Spec.Capabilities[1].OpenAPI.Method = "TRACE" },
		"dynamic host":               func(value *Package) { value.Spec.Capabilities[1].OpenAPI.Path = "https://other.example/tickets" },
	} {
		t.Run(name, func(t *testing.T) {
			value := openAPIPackageFixture(t)
			mutate(&value)
			if validate(&value) == nil {
				t.Fatal("unsafe OpenAPI binding accepted")
			}
		})
	}
}

func TestOpenAPIManagedRevisionMayAddOnlyTypedBoundOperations(t *testing.T) {
	candidate := openAPIPackageFixture(t)
	baseline := candidate
	baseline.Metadata.Origin = Origin
	baseline.Spec.Capabilities = append([]Capability(nil), candidate.Spec.Capabilities[0])
	readBinding := *baseline.Spec.Capabilities[0].OpenAPI
	readBinding.AuthScheme = "NONE"
	baseline.Spec.Capabilities[0].OpenAPI = &readBinding
	baseline.Spec.Credential = nil
	baseline = parseOpenAPITestPackage(t, baseline)
	candidate = parseOpenAPITestPackage(t, candidate)
	if err := ValidateExecutableRevision(candidate, baseline); err != nil {
		t.Fatalf("bounded OpenAPI revision rejected: %v", err)
	}
	for name, mutate := range map[string]func(*Package){
		"foreign adapter": func(value *Package) { value.Spec.Adapter = string(AdapterHTTPSJSONRead) },
		"new destination": func(value *Package) {
			value.Spec.NetworkDestinations[0].ConfigurationField = "other_url"
		},
		"new configuration": func(value *Package) {
			value.Spec.ConfigurationFields = append(value.Spec.ConfigurationFields, Field{Key: "other_url", Type: "STRING", Required: false, MaximumLength: 100})
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := openAPIPackageFixture(t)
			mutate(&changed)
			raw, err := json.Marshal(changed)
			if err == nil {
				changed, err = Parse(raw)
			}
			if err == nil && ValidateExecutableRevision(changed, baseline) == nil {
				t.Fatal("managed revision expanded destination or adapter")
			}
		})
	}
}

func parseOpenAPITestPackage(t *testing.T, value Package) Package {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
