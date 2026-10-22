package callback

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// createSystemImageOperation names the SYSTEM-only image-recipe creation proposal exercised by local schema and draft tests; it is not a build command.
// createSystemImageOperation задаёт имя системного предложения создания рецепта образа для локальных тестов схемы и черновика; командой сборки не является.
const createSystemImageOperation = "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"

// updateSystemImageOperation names the SYSTEM-only image-recipe update proposal, keeping it distinct from creation and runtime configuration.
// updateSystemImageOperation задаёт имя системного предложения изменения рецепта образа отдельно от создания и настройки выполнения.
const updateSystemImageOperation = "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"

// prepareAssistantConfigurationOperation names the server-hydrated runtime-configuration proposal shared by system and eligible project-self tests.
// prepareAssistantConfigurationOperation задаёт имя предложения конфигурации выполнения с серверными привязками для тестов системного и допустимого собственного проектного помощника.
const prepareAssistantConfigurationOperation = "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION"

// TestSystemAssistantImageSchemaAllowsOnlyCatalogTemplateOmission checks image creation may omit Dockerfile for the owner catalog template but still requires the exact helper locator, name and environment key.
// An explicitly supplied Dockerfile remains a nonempty bounded string in the closed schema.
// TestSystemAssistantImageSchemaAllowsOnlyCatalogTemplateOmission проверяет допустимость отсутствия Dockerfile при создании образа по шаблону каталога владельца с обязательными точной ссылкой помощника, именем и ключом окружения.
// Явно переданный Dockerfile остаётся непустой ограниченной строкой в закрытой схеме.
func TestSystemAssistantImageSchemaAllowsOnlyCatalogTemplateOmission(t *testing.T) {
	for _, schema := range assistantPlanOperationSchemas(assistantConfigurationFixture(runtimecontract.AssistantScopeSystem)) {
		if assistantSchemaType(schema) != createSystemImageOperation {
			continue
		}
		parameters := schema["properties"].(map[string]any)["parameters"].(map[string]any)
		if !reflect.DeepEqual(parameters["required"], []string{"systemAssistantRef", "name", "environmentKey"}) || parameters["additionalProperties"] != false {
			t.Fatal("catalog template omission changed required owner locator or exposed extra fields")
		}
		dockerfile := parameters["properties"].(map[string]any)["dockerfile"].(map[string]any)
		if dockerfile["type"] != "string" || dockerfile["minLength"] != 1 || dockerfile["maxLength"] != 65536 {
			t.Fatal("explicit Dockerfile lost its bounded strict schema")
		}
		return
	}
	t.Fatal("system assistant image operation is missing")
}

// TestAssistantRuntimeConfigurationSearchClosedOwnBoundary checks hosted-search modes follow the runtime contract in both scopes, omission preserves the existing path and PROJECT cannot target another helper.
// Selecting a hosted-search mode does not test or enable arbitrary network access.
// TestAssistantRuntimeConfigurationSearchClosedOwnBoundary проверяет соответствие режимов серверного поиска контракту выполнения в обеих областях, сохранение прежнего пути при отсутствии поля и запрет чужого помощника в проектной области.
// Выбор режима серверного поиска не проверяет и не включает произвольный сетевой доступ.
func TestAssistantRuntimeConfigurationSearchClosedOwnBoundary(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		input := assistantConfigurationFixture(scope)
		for _, mode := range []any{nil, "", "disabled", "cached", "indexed", "live", "LIVE", "future", true, map[string]any{"private": "sentinel"}} {
			parameters := assistantConfigurationParameters(input.AgentRef)
			parameters["webSearchMode"] = mode
			text, ok := mode.(string)
			want := ok && runtimecontract.ValidWebSearchMode(text)
			if assistantConfigurationParametersAllowed(input, prepareAssistantConfigurationOperation, parameters) != want {
				t.Fatal("unknown mode crossed own configuration boundary")
			}
		}
		parameters := assistantConfigurationParameters(input.AgentRef)
		if !assistantConfigurationParametersAllowed(input, prepareAssistantConfigurationOperation, parameters) {
			t.Fatal("omission did not preserve existing supported path")
		}
		parameters["webSearchMode"], parameters["agentRef"] = "live", "agt_foreign123"
		if scope == runtimecontract.AssistantScopeProject && assistantConfigurationParametersAllowed(input, prepareAssistantConfigurationOperation, parameters) {
			t.Fatal("hosted search configuration granted foreign project authority")
		}
	}
	properties := assistantRuntimeConfigurationSchema(opaqueRefSchema())["properties"].(map[string]any)
	if !reflect.DeepEqual(properties["webSearchMode"].(map[string]any)["enum"], []string{"disabled", "cached", "indexed", "live"}) {
		t.Fatal("public typed modes differ from the pinned SDK")
	}
}

// assistantConfigurationFixture creates fresh synthetic self/profile/account/lease and project-screen metadata for local proposal tests, without files, credentials or a provider session.
// assistantConfigurationFixture создаёт новые синтетические метаданные помощника, профиля, аккаунта, аренды и проектного экрана для локальных тестов предложений без файлов, учётных данных или сессии провайдера.
func assistantConfigurationFixture(scope runtimecontract.AssistantScope) runtimecontract.RunnerInput {
	return runtimecontract.RunnerInput{AssistantScope: scope, AgentRef: "agt_own12345", ProjectRef: "prj_context123",
		RuntimeProfileRef: "builtin-safe-runtime", ProviderAccountRef: "pacc_current123", Model: "gpt-6.1-sol", EffectiveReasoningEffort: "medium",
		LeaseRef: "lease_current123", LeaseFence: "fence_current123", LeaseGeneration: 7,
		AssistantContext: &runtimecontract.RunnerAssistantContext{EntityKind: "PROJECT", EntityRef: "prj_context123"}}
}

// assistantConfigurationParameters builds editable runtime settings with one fixed-policy synthetic provider account of weight one; it contains no owner identity or credential values.
// assistantConfigurationParameters создаёт редактируемые настройки выполнения с одним синтетическим аккаунтом провайдера и весом один при фиксированной политике; идентичности владельца и значений учётных данных нет.
func assistantConfigurationParameters(agentRef string) map[string]any {
	return map[string]any{"agentRef": agentRef, "runtimeProfileRef": "builtin-safe-runtime", "model": "gpt-6.1-sol",
		"reasoningEffort": "medium", "providerPolicyMode": "FIXED",
		"providerAccounts": []any{map[string]any{"accountRef": "pacc_current123", "weight": float64(1)}}}
}

// TestAssistantRuntimeConfigurationReasoningUsesCanonicalBoundary checks reasoning schema, parameter validation and owner-draft routing agree on canonical syntax, empty omission and 64-character bounds.
// TestAssistantRuntimeConfigurationReasoningUsesCanonicalBoundary проверяет согласованность схемы рассуждений, проверки параметров и маршрута черновика владельца по каноническому синтаксису, пустому значению и пределу 64 символа.
func TestAssistantRuntimeConfigurationReasoningUsesCanonicalBoundary(t *testing.T) {
	schema := assistantRuntimeConfigurationSchema(opaqueRefSchema())["properties"].(map[string]any)["reasoningEffort"].(map[string]any)
	pattern := regexp.MustCompile(schema["pattern"].(string))
	if schema["maxLength"] != 64 {
		t.Fatal("model configuration narrowed canonical effort length")
	}
	for _, effort := range []string{"", "adaptive", strings.Repeat("a", 64), strings.Repeat("a", 65), "Adaptive", "adaptive/slower"} {
		parameters := assistantConfigurationParameters("agt_own12345")
		parameters["reasoningEffort"] = effort
		valid := effort == "" || runtimecontract.ValidateEffectiveReasoningEffort("", effort, runtimecontract.ReasoningSupported) == nil
		if pattern.MatchString(effort) != valid || assistantConfigurationParametersAllowed(assistantConfigurationFixture(runtimecontract.AssistantScopeSystem), prepareAssistantConfigurationOperation, parameters) != valid {
			t.Fatal("schema or dispatcher reasoning differs from canonical runtime contract")
		}
		client := &scopedAssistantPlanClient{}
		server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
		arguments := map[string]any{"summary": "Prepare eligible catalog reasoning", "operations": []any{map[string]any{"type": prepareAssistantConfigurationOperation, "parameters": parameters}}}
		_, err := server.proposeAssistantPlan(t.Context(), assistantConfigurationFixture(runtimecontract.AssistantScopeSystem), arguments, json.RawMessage(`16`))
		if (err == nil) != valid || valid && len(client.requests) != 1 || !valid && len(client.requests) != 0 {
			t.Fatal("model reasoning boundary did not protect owner draft RPC")
		}
	}
}

// TestAssistantConfigurationCatalogKeepsExactSelfAcrossScreenContexts checks six scope/screen combinations retain unique closed self-configuration schemas, immutable helper locators and credential-free current metadata.
// TestAssistantConfigurationCatalogKeepsExactSelfAcrossScreenContexts проверяет для шести сочетаний области и экрана уникальные закрытые схемы собственной настройки, неизменяемые ссылки помощника и текущие метаданные без учётных данных.
func TestAssistantConfigurationCatalogKeepsExactSelfAcrossScreenContexts(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		for _, screen := range []string{"PROJECT", "AGENT", "ENVIRONMENT"} {
			t.Run(string(scope)+"/"+screen, func(t *testing.T) {
				input := assistantConfigurationFixture(scope)
				input.AssistantContext.EntityKind, input.AssistantContext.EntityRef = screen, "res_foreign123"
				seen := make(map[string]map[string]any)
				for _, schema := range assistantPlanOperationSchemas(input) {
					kind := assistantSchemaType(schema)
					if kind == createSystemImageOperation || kind == updateSystemImageOperation || kind == prepareAssistantConfigurationOperation {
						if seen[kind] != nil {
							t.Fatal("duplicate specialized configuration schema")
						}
						seen[kind] = schema["properties"].(map[string]any)["parameters"].(map[string]any)
					}
				}
				configuration := seen[prepareAssistantConfigurationOperation]
				if configuration == nil || configuration["additionalProperties"] != false {
					t.Fatal("missing closed configuration schema")
				}
				properties := configuration["properties"].(map[string]any)
				if len(properties) != 7 || properties["model"].(map[string]any)["maxLength"] != 128 {
					t.Fatal("configuration schema exposed extra fields or differs from owner model bound")
				}
				account := properties["providerAccounts"].(map[string]any)["items"].(map[string]any)
				if account["additionalProperties"] != false || len(account["properties"].(map[string]any)) != 2 {
					t.Fatal("account schema exposed credentials or authority fields")
				}
				if scope == runtimecontract.AssistantScopeProject {
					if len(seen) != 1 || !reflect.DeepEqual(properties["agentRef"].(map[string]any)["enum"], []string{input.AgentRef}) {
						t.Fatal("project assistant gained organization image or foreign configuration")
					}
				} else {
					if len(seen) != 3 {
						t.Fatal("system assistant image setup is incomplete")
					}
					for _, kind := range []string{createSystemImageOperation, updateSystemImageOperation} {
						imageProperties := seen[kind]["properties"].(map[string]any)
						if seen[kind]["additionalProperties"] != false || !reflect.DeepEqual(imageProperties["systemAssistantRef"].(map[string]any)["enum"], []string{input.AgentRef}) {
							t.Fatal("system image detached from immutable assistant")
						}
						for _, forbidden := range []string{"projectRef", "organizationRef", "roleDefinitionRef", "agentVersion", "expectedVersion", "secretValue", "configToml"} {
							if imageProperties[forbidden] != nil {
								t.Fatalf("image schema exposed %s", forbidden)
							}
						}
					}
				}
				catalog, err := configurationCatalog(input, map[string]any{})
				if err != nil || !reflect.DeepEqual(catalog.(map[string]any)["current_runtime"], assistantCurrentRuntime(input)) {
					t.Fatal("catalog lost the safe immutable current configuration")
				}
				if len(assistantCurrentRuntime(input)) != 5 {
					t.Fatal("current configuration exposed extra runtime metadata")
				}
			})
		}
	}
}

// TestAssistantSpecializedConfigurationProposesOnlyOwnerDraft checks six image/runtime proposals reach one draft RPC with exact lease coordinates while dropping caller target, OCC and before/after authority.
// These local stubs neither build images nor apply configuration changes.
// TestAssistantSpecializedConfigurationProposesOnlyOwnerDraft проверяет отправку шести предложений образа и выполнения в один RPC черновика с точными координатами аренды и удалением переданных вызывающим цели, OCC и полномочий before/after.
// Локальные подстановки не собирают образы и не применяют изменения конфигурации.
func TestAssistantSpecializedConfigurationProposesOnlyOwnerDraft(t *testing.T) {
	for _, fixture := range []struct {
		name, kind, target string
		scope              runtimecontract.AssistantScope
		parameters         map[string]any
	}{
		{"system create image from catalog template", createSystemImageOperation, "ROLE_IMAGE_RECIPE", runtimecontract.AssistantScopeSystem, map[string]any{"systemAssistantRef": "agt_own12345", "name": "Assistant tools", "environmentKey": "standard"}},
		{"system create image", createSystemImageOperation, "ROLE_IMAGE_RECIPE", runtimecontract.AssistantScopeSystem, map[string]any{"systemAssistantRef": "agt_own12345", "name": "Assistant tools", "environmentKey": "standard", "dockerfile": "FROM scratch\n"}},
		{"system update image", updateSystemImageOperation, "ROLE_IMAGE_RECIPE", runtimecontract.AssistantScopeSystem, map[string]any{"systemAssistantRef": "agt_own12345", "recipeRef": "imgrec_current123", "dockerfile": "FROM scratch\n"}},
		{"system configure self", prepareAssistantConfigurationOperation, "AGENT", runtimecontract.AssistantScopeSystem, assistantConfigurationParameters("agt_own12345")},
		{"system configure project assistant", prepareAssistantConfigurationOperation, "AGENT", runtimecontract.AssistantScopeSystem, assistantConfigurationParameters("agt_project123")},
		{"project configure self", prepareAssistantConfigurationOperation, "AGENT", runtimecontract.AssistantScopeProject, assistantConfigurationParameters("agt_own12345")},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			input := assistantConfigurationFixture(fixture.scope)
			client := &scopedAssistantPlanClient{}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			arguments := map[string]any{"summary": "Prepare configuration for review", "operations": []any{map[string]any{
				"type": fixture.kind, "parameters": fixture.parameters, "before": map[string]any{"untrusted": true}, "after": map[string]any{"untrusted": true},
				"expectedVersion": float64(99), "target": map[string]any{"kind": "ORGANIZATION", "name": "untrusted", "version": float64(99)}}}}
			if _, err := server.proposeAssistantPlan(t.Context(), input, arguments, json.RawMessage(`12`)); err != nil {
				t.Fatal(err)
			}
			if len(client.requests) != 1 {
				t.Fatal("specialized operation did not reach exactly one draft RPC")
			}
			request := client.requests[0]
			operation := request.Operations[0]
			if request.LeaseRef != input.LeaseRef || request.Fence != input.LeaseFence || request.Generation != input.LeaseGeneration || operation.TargetKind != fixture.target || operation.ExpectedVersion != nil {
				t.Fatal("draft lost exact lease or retained caller authority/version")
			}
			if len(operation.Before.AsMap()) != 0 || !reflect.DeepEqual(operation.After.AsMap(), operation.Parameters.AsMap()) || !reflect.DeepEqual(operation.Parameters.AsMap(), fixture.parameters) {
				t.Fatal("draft retained caller projection or changed specialized parameters")
			}
			if fixture.kind != createSystemImageOperation && operation.Action.String() != "ACTION_UPDATE" {
				t.Fatal("configuration update mapped to create")
			}
		})
	}
}

// TestAssistantSpecializedConfigurationRejectsScopeAndAuthorityInjection checks foreign scopes/identities, extra owner or credential fields, invalid account weights and unknown policy modes cannot reach proposal RPC.
// TestAssistantSpecializedConfigurationRejectsScopeAndAuthorityInjection проверяет, что чужие области и идентичности, лишние поля владельца и учётных данных, неверные веса аккаунта и неизвестные режимы политики не достигают RPC предложения.
func TestAssistantSpecializedConfigurationRejectsScopeAndAuthorityInjection(t *testing.T) {
	for _, fixture := range []struct {
		name, kind string
		scope      runtimecontract.AssistantScope
		parameters map[string]any
	}{
		{"project organization image", createSystemImageOperation, runtimecontract.AssistantScopeProject, map[string]any{"systemAssistantRef": "agt_own12345", "name": "Tools", "environmentKey": "standard", "dockerfile": "FROM scratch"}},
		{"system foreign assistant image", updateSystemImageOperation, runtimecontract.AssistantScopeSystem, map[string]any{"systemAssistantRef": "agt_foreign123", "recipeRef": "imgrec_current123", "name": "Tools"}},
		{"system empty image update", updateSystemImageOperation, runtimecontract.AssistantScopeSystem, map[string]any{"systemAssistantRef": "agt_own12345", "recipeRef": "imgrec_current123"}},
		{"system image authority injection", createSystemImageOperation, runtimecontract.AssistantScopeSystem, map[string]any{"systemAssistantRef": "agt_own12345", "name": "Tools", "environmentKey": "standard", "dockerfile": "FROM scratch", "projectRef": "prj_foreign123"}},
		{"system image empty dockerfile", createSystemImageOperation, runtimecontract.AssistantScopeSystem, map[string]any{"systemAssistantRef": "agt_own12345", "name": "Tools", "environmentKey": "standard", "dockerfile": ""}},
		{"system image null dockerfile", createSystemImageOperation, runtimecontract.AssistantScopeSystem, map[string]any{"systemAssistantRef": "agt_own12345", "name": "Tools", "environmentKey": "standard", "dockerfile": nil}},
		{"project foreign assistant config", prepareAssistantConfigurationOperation, runtimecontract.AssistantScopeProject, assistantConfigurationParameters("agt_foreign123")},
		{"ordinary execution config", prepareAssistantConfigurationOperation, runtimecontract.AssistantScopeNone, assistantConfigurationParameters("agt_own12345")},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			assertAssistantConfigurationRejected(t, fixture.scope, fixture.kind, fixture.parameters)
		})
	}
	for _, key := range []string{"scopeKind", "organizationRef", "projectRef", "expectedVersion", "ownerRef", "secretValue", "configToml"} {
		t.Run(key, func(t *testing.T) {
			parameters := assistantConfigurationParameters("agt_own12345")
			parameters[key] = "untrusted"
			assertAssistantConfigurationRejected(t, runtimecontract.AssistantScopeSystem, prepareAssistantConfigurationOperation, parameters)
		})
	}
	parameters := assistantConfigurationParameters("agt_own12345")
	parameters["providerAccounts"].([]any)[0].(map[string]any)["apiKey"] = "synthetic-not-a-credential"
	assertAssistantConfigurationRejected(t, runtimecontract.AssistantScopeSystem, prepareAssistantConfigurationOperation, parameters)
	for _, weight := range []any{float64(0), float64(101), float64(1.5), "1", float64(2)} {
		parameters := assistantConfigurationParameters("agt_own12345")
		parameters["providerAccounts"].([]any)[0].(map[string]any)["weight"] = weight
		assertAssistantConfigurationRejected(t, runtimecontract.AssistantScopeSystem, prepareAssistantConfigurationOperation, parameters)
	}
	parameters = assistantConfigurationParameters("agt_own12345")
	parameters["providerPolicyMode"] = "UNKNOWN"
	assertAssistantConfigurationRejected(t, runtimecontract.AssistantScopeSystem, prepareAssistantConfigurationOperation, parameters)
}

// assertAssistantConfigurationRejected requires a local proposal error and zero captured owner requests for the supplied scope, operation and parameters.
// assertAssistantConfigurationRejected требует локальную ошибку предложения и отсутствие записанных запросов владельца для переданных области, операции и параметров.
func assertAssistantConfigurationRejected(t *testing.T, scope runtimecontract.AssistantScope, kind string, parameters map[string]any) {
	t.Helper()
	client := &scopedAssistantPlanClient{}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	arguments := map[string]any{"summary": "Prepare configuration", "operations": []any{map[string]any{"type": kind, "parameters": parameters}}}
	if _, err := server.proposeAssistantPlan(t.Context(), assistantConfigurationFixture(scope), arguments, json.RawMessage(`12`)); err == nil || len(client.requests) != 0 {
		t.Fatal("invalid configuration reached owner draft RPC")
	}
}
