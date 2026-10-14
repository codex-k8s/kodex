package callback

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestAssistantFileRevisionClosedSourceMatrix(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeProject, runtimecontract.AssistantScopeSystem} {
		for _, screen := range []string{"PROJECT", "FILE", "AGENT", "ENVIRONMENT"} {
			t.Run(string(scope)+"/"+screen, func(t *testing.T) {
				input := assistantConfigurationFixture(scope)
				input.Capabilities = []string{runtimecontract.ArtifactCapability}
				input.AssistantContext.EntityKind = screen
				input.AssistantContext.AllowedOperations = []string{"CREATE_PROJECT_FILE_REVISION"}
				want := scope == runtimecontract.AssistantScopeProject || screen == "PROJECT" || screen == "FILE"
				catalog, err := configurationCatalog(input, map[string]any{"operation_types": []any{"CREATE_PROJECT_FILE_REVISION"}})
				if (err == nil) != want {
					t.Fatal("dynamic discovery crossed approved source context matrix")
				}
				if want && len(catalog.(map[string]any)["operation_schemas"].([]map[string]any)) != 1 {
					t.Fatal("exact revision schema missing")
				}
				client := &scopedAssistantPlanClient{}
				server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
				_, err = server.proposeAssistantPlan(t.Context(), input, map[string]any{"summary": "Новая версия заметки", "operations": []any{map[string]any{
					"type": "CREATE_PROJECT_FILE_REVISION", "title": "Обновить заметку", "parameters": map[string]any{"artifactRef": "art_exact123", "mediaType": "text/markdown", "content": "новый текст"}}}}, json.RawMessage(`1`))
				if (err == nil) != want || (len(client.requests) == 1) != want {
					t.Fatal("native callback crossed approved source context matrix")
				}
				if want {
					operation := client.requests[0].Operations[0]
					if operation.GetAction().String() != "ACTION_UPDATE" || operation.TargetKind != "ARTIFACT" || operation.TargetRef != "" || operation.ExpectedVersion != nil ||
						client.requests[0].GetLeaseRef() != input.LeaseRef || client.requests[0].GetFence() != input.LeaseFence {
						t.Fatal("callback invented authority/OCC or lost immutable source execution")
					}
				}
			})
		}
	}
}

func TestAssistantFileRevisionParametersRejectAuthorityAndBodyMismatch(t *testing.T) {
	input := assistantConfigurationFixture(runtimecontract.AssistantScopeProject)
	input.Capabilities = []string{runtimecontract.ArtifactCapability}
	input.AssistantContext.AllowedOperations = []string{"CREATE_PROJECT_FILE_REVISION"}
	for _, scenario := range []struct {
		name   string
		change func(*runtimecontract.RunnerInput, map[string]any)
	}{
		{"capability missing", func(i *runtimecontract.RunnerInput, _ map[string]any) { i.Capabilities = nil }},
		{"context missing", func(i *runtimecontract.RunnerInput, _ map[string]any) { i.AssistantContext = nil }},
		{"project missing", func(i *runtimecontract.RunnerInput, _ map[string]any) { i.ProjectRef = "" }},
		{"actor", func(_ *runtimecontract.RunnerInput, p map[string]any) { p["actorRef"] = "usr_foreign123" }},
		{"project payload", func(_ *runtimecontract.RunnerInput, p map[string]any) { p["projectRef"] = "prj_foreign123" }},
		{"OCC", func(_ *runtimecontract.RunnerInput, p map[string]any) { p["expectedVersion"] = 7 }},
		{"prepared grant", func(_ *runtimecontract.RunnerInput, p map[string]any) { p["contentRef"] = "pfcnt_foreign123" }},
		{"BASE64", func(_ *runtimecontract.RunnerInput, p map[string]any) { p["contentEncoding"] = "BASE64" }},
		{"oversize", func(_ *runtimecontract.RunnerInput, p map[string]any) { p["content"] = strings.Repeat("я", 1<<20) }},
		{"NUL", func(_ *runtimecontract.RunnerInput, p map[string]any) { p["content"] = "a\x00b" }},
		{"invalid UTF8", func(_ *runtimecontract.RunnerInput, p map[string]any) { p["content"] = string([]byte{255}) }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			i := input
			p := map[string]any{"artifactRef": "art_exact123", "mediaType": "text/plain", "content": "text"}
			scenario.change(&i, p)
			if assistantConfigurationParametersAllowed(i, "CREATE_PROJECT_FILE_REVISION", p) {
				t.Fatal("revision parameter boundary accepted authority or malformed body")
			}
		})
	}
}
