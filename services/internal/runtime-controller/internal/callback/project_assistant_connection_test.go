package callback

import (
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"testing"
)

// TestProjectAssistantConnectionClosedPlanShape checks PROJECT-only, server-hydrated connection preparation is advertised only with its snapshot operation.
// Foreign helper identities, caller owner/credential fields and other scopes are rejected without creating a real connection.
// TestProjectAssistantConnectionClosedPlanShape проверяет объявление проектной подготовки подключения с серверными привязками только при наличии операции в снимке.
// Чужой помощник, переданные вызывающей стороной поля владельца или учётных данных и другие области отклоняются без создания настоящего подключения.
func TestProjectAssistantConnectionClosedPlanShape(t *testing.T) {
	const kind = "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION"
	input := assistantConfigurationFixture(runtimecontract.AssistantScopeProject)
	input.AssistantContext.AllowedOperations = []string{kind}
	parameters := map[string]any{"projectAssistantRef": input.AgentRef, "definitionKey": "github", "name": "Own repository", "publicConfiguration": map[string]any{"owner": "fixture", "repository": "repository"}}
	if !assistantConfigurationParametersAllowed(input, kind, parameters) || !assistantServerHydratedOperation(kind) || assistantServerAction(kind) != "CREATE" {
		t.Fatal("closed owner-confirmed preparation unreachable")
	}
	target := assistantServerTarget(kind, parameters, nil)
	if target["kind"] != "PROJECT_ASSISTANT" || target["name"] != input.AgentRef {
		t.Fatal("target is not exact source helper")
	}
	advertised := func(input runtimecontract.RunnerInput) bool {
		for _, schema := range assistantPlanOperationSchemas(input) {
			if assistantSchemaType(schema) == kind {
				return true
			}
		}
		return false
	}
	if !advertised(input) {
		t.Fatal("approved PROJECT capability absent from schema")
	}
	input.AssistantContext.AllowedOperations = nil
	if advertised(input) {
		t.Fatal("missing canonical capability advertised")
	}
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeNone} {
		other := assistantConfigurationFixture(scope)
		other.AssistantContext.AllowedOperations = []string{kind}
		if advertised(other) || assistantConfigurationParametersAllowed(other, kind, parameters) {
			t.Fatal("non-PROJECT source admitted specialty")
		}
	}
	for _, field := range []string{"projectRef", "organizationRef", "assistantProfileRef", "scopeKind", "agentVersion", "credentials", "agentRef"} {
		bad := map[string]any{}
		for key, value := range parameters {
			bad[key] = value
		}
		bad[field] = ""
		if assistantConfigurationParametersAllowed(input, kind, bad) {
			t.Fatalf("caller owner/secret field %s accepted", field)
		}
	}
	for _, value := range []any{"agt_foreign123", nil, ""} {
		bad := map[string]any{}
		for key, item := range parameters {
			bad[key] = item
		}
		bad["projectAssistantRef"] = value
		if assistantConfigurationParametersAllowed(input, kind, bad) {
			t.Fatal("foreign or malformed helper accepted")
		}
	}
}
