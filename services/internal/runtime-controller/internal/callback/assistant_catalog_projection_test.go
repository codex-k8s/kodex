package callback

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestAssistantCatalogSafeProjectionPublishesOnlyClosedKind(t *testing.T) {
	input := assistantConfigurationFixture(runtimecontract.AssistantScopeSystem)
	for _, kind := range []string{"ASSISTANTS", "RUNTIME_PROFILES", "PROVIDER_ACCOUNTS", "MODELS", "ROLE_IMAGE_RECIPES", "IMAGE_ARTIFACTS", "ROLE_ENVIRONMENTS", "CURRENT_CONFIGURATION"} {
		t.Run(kind, func(t *testing.T) {
			arguments := map[string]any{
				"assistant_configuration_catalog": map[string]any{
					"kind": kind, "assistant_ref": "PRIVATE_ASSISTANT_SENTINEL", "account_ref": "PRIVATE_ACCOUNT_SENTINEL",
					"runtime_profile_ref": "PRIVATE_PROFILE_SENTINEL", "query": "PRIVATE_QUERY_SENTINEL", "offset": 20,
					"input": "PRIVATE_INPUT_SENTINEL", "workspace": "PRIVATE_WORKSPACE_SENTINEL", "token": "PRIVATE_TOKEN_SENTINEL",
				},
				"operation_types": []any{"PRIVATE_OPERATION_SENTINEL"}, "headers": map[string]any{"Authorization": "PRIVATE_HEADER_SENTINEL"},
			}
			parameters, capability, grant, ok := safeToolCallParameters(input, "get_configuration_catalog", arguments)
			if !ok || capability != "platform.configuration.read" || grant != "" || !reflect.DeepEqual(parameters, map[string]any{"catalogKind": kind}) {
				t.Fatal("catalog activity must publish only the closed catalog kind")
			}
		})
	}
}

func TestAssistantCatalogSafeProjectionKeepsUnknownAndInvalidSelectorsEmpty(t *testing.T) {
	input := assistantConfigurationFixture(runtimecontract.AssistantScopeSystem)
	for _, selector := range []any{
		nil, "ROLE_ENVIRONMENTS", []any{"ROLE_ENVIRONMENTS"},
		map[string]any{}, map[string]any{"kind": nil}, map[string]any{"kind": 1},
		map[string]any{"kind": []any{"ROLE_ENVIRONMENTS"}}, map[string]any{"kind": "UNKNOWN_PRIVATE_SENTINEL"},
		map[string]any{"kind": "role_environments"}, map[string]any{"kind": "ROLE_ENVIRONMENTS\nPRIVATE_SENTINEL"},
	} {
		parameters, capability, grant, ok := safeToolCallParameters(input, "get_configuration_catalog", map[string]any{
			"assistant_configuration_catalog": selector, "catalogKind": "ROLE_ENVIRONMENTS", "query": "PRIVATE_QUERY_SENTINEL",
		})
		if !ok || capability != "platform.configuration.read" || grant != "" || len(parameters) != 0 {
			t.Fatal("unknown or invalid selector must not escape the empty safe projection")
		}
	}
}
