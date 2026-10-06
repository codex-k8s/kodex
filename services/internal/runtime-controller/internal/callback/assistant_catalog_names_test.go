package callback

import (
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

func assistantCatalogNameServer(client *assistantDefinitionCatalogClient) *Server {
	return &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
}

func TestAssistantCatalogOwnSystemContextNameIsReadable(t *testing.T) {
	for _, test := range []struct {
		name, kind, ref, label, expected string
		scope                            runtimecontract.AssistantScope
	}{
		{"own system", "AGENT", "agt_own12345", "i18n:SYSTEM_ASSISTANT_NAME", "Системный помощник", runtimecontract.AssistantScopeSystem},
		{"project marker", "AGENT", "agt_own12345", "i18n:SYSTEM_ASSISTANT_NAME", "i18n:SYSTEM_ASSISTANT_NAME", runtimecontract.AssistantScopeProject},
		{"foreign agent", "AGENT", "agt_foreign123", "i18n:SYSTEM_ASSISTANT_NAME", "i18n:SYSTEM_ASSISTANT_NAME", runtimecontract.AssistantScopeSystem},
		{"other kind", "PROJECT", "agt_own12345", "i18n:SYSTEM_ASSISTANT_NAME", "i18n:SYSTEM_ASSISTANT_NAME", runtimecontract.AssistantScopeSystem},
		{"other marker", "AGENT", "agt_own12345", "i18n:OTHER_NAME", "i18n:OTHER_NAME", runtimecontract.AssistantScopeSystem},
		{"owner name", "AGENT", "agt_own12345", "Помощник владельца", "Помощник владельца", runtimecontract.AssistantScopeSystem},
	} {
		t.Run(test.name, func(t *testing.T) {
			input, arguments, response := assistantOwnCurrentFixture(test.scope)
			version := int64(17)
			input.AssistantContext = &runtimecontract.RunnerAssistantContext{Route: "/agents/own", EntityKind: test.kind,
				EntityRef: test.ref, EntityName: test.label, EntityVersion: &version, AllowedOperations: []string{"UPDATE_AGENT"}}
			originalContext := *input.AssistantContext
			client := &assistantDefinitionCatalogClient{response: response}
			server := assistantCatalogNameServer(client)
			result, err := server.configurationCatalog(t.Context(), input, arguments)
			if err != nil {
				t.Fatal(err)
			}
			expectedInput := input
			expectedContext := originalContext
			expectedContext.EntityName = test.expected
			expectedInput.AssistantContext = &expectedContext
			expected, err := server.configurationCatalog(t.Context(), expectedInput, arguments)
			if err != nil || !reflect.DeepEqual(result, expected) {
				t.Fatal("context display changed fields beyond the own system assistant name")
			}
			context := result.(map[string]any)["context"].(map[string]any)
			if context["entity_name"] != test.expected || !reflect.DeepEqual(*input.AssistantContext, originalContext) {
				t.Fatal("model context name is unreadable or immutable input was modified")
			}
			request := client.request
			if request.GetLeaseRef() != input.LeaseRef || request.GetFence() != input.LeaseFence || request.GetGeneration() != input.LeaseGeneration ||
				request.GetAssistantConfigurationCatalog().GetAssistantRef() != input.AgentRef {
				t.Fatal("display name changed exact fenced current configuration read")
			}
		})
	}
}

func TestAssistantCatalogOwnSystemEntryNameIsReadable(t *testing.T) {
	for _, test := range []struct {
		name, ref, label, expected string
		scope                      runtimecontract.AssistantScope
		foreign                    bool
	}{
		{"own system", "agt_own12345", "i18n:SYSTEM_ASSISTANT_NAME", "Системный помощник", runtimecontract.AssistantScopeSystem, false},
		{"project marker", "agt_own12345", "i18n:SYSTEM_ASSISTANT_NAME", "i18n:SYSTEM_ASSISTANT_NAME", runtimecontract.AssistantScopeProject, false},
		{"foreign agent", "agt_foreign123", "i18n:SYSTEM_ASSISTANT_NAME", "i18n:SYSTEM_ASSISTANT_NAME", runtimecontract.AssistantScopeSystem, true},
		{"self project entry", "agt_own12345", "i18n:SYSTEM_ASSISTANT_NAME", "i18n:SYSTEM_ASSISTANT_NAME", runtimecontract.AssistantScopeSystem, true},
		{"other marker", "agt_own12345", "i18n:OTHER_NAME", "i18n:OTHER_NAME", runtimecontract.AssistantScopeSystem, false},
		{"owner name", "agt_own12345", "Помощник владельца", "Помощник владельца", runtimecontract.AssistantScopeSystem, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			input, arguments, response := assistantFreshCatalogFixture(test.scope, "ASSISTANTS")
			entry := response.AssistantConfigurationCatalog.Entries[0]
			entry.Ref, entry.Name, entry.Version = test.ref, test.label, 19
			if test.foreign {
				entry.ScopeKind, entry.ProjectRef, entry.AssistantProfileRef = "PROJECT", "prj_foreign123", "asstprof_foreign123"
			}
			original := proto.Clone(response)
			client := &assistantDefinitionCatalogClient{response: response}
			server := assistantCatalogNameServer(client)
			result, err := server.configurationCatalog(t.Context(), input, arguments)
			if err != nil {
				t.Fatal(err)
			}
			entry.Name = test.expected
			expected, err := server.configurationCatalog(t.Context(), input, arguments)
			entry.Name = test.label
			if err != nil || !reflect.DeepEqual(result, expected) {
				t.Fatal("catalog display changed resource refs, scope, versions or other fields")
			}
			catalog := result.(map[string]any)["assistant_configuration_catalog"].(map[string]any)
			if catalog["entries"].([]map[string]any)[0]["name"] != test.expected || !proto.Equal(response, original) {
				t.Fatal("model catalog name is unreadable or authoritative response was modified")
			}
		})
	}
}
