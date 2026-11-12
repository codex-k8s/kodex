package callback

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestAssistantCurrentExecutionDiscoverySeparatesWorkspaceAndNativePreferences checks both scopes separate immutable execution workspace policy from fresh configuration and report requested preferences without invented enforcement.
// Private workspace/auth paths and execution secrets must not enter the projected JSON.
// TestAssistantCurrentExecutionDiscoverySeparatesWorkspaceAndNativePreferences проверяет в обеих областях отделение неизменяемой политики рабочей области выполнения от свежей конфигурации и выдачу намерений настроек без выдуманной гарантии исполнения.
// Частные пути рабочей области и авторизации, секреты выполнения не должны попадать в проецируемый JSON.
func TestAssistantCurrentExecutionDiscoverySeparatesWorkspaceAndNativePreferences(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			input, arguments, response := assistantOwnCurrentFixture(scope)
			request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
			if err != nil {
				t.Fatal(err)
			}
			result, err := castAssistantConfigurationCatalog(input, request, response.AssistantConfigurationCatalog)
			if err != nil {
				t.Fatal(err)
			}
			execution := result["execution_snapshot"].(map[string]any)
			workspace := map[string]any{"revision": input.WorkspacePolicy.Revision, "root": "/workspace",
				"maximum_writable_bytes": input.WorkspacePolicy.MaximumWritableBytes, "maximum_file_count": input.WorkspacePolicy.MaximumFileCount,
				"readonly_logical_roots": []string{"input", "knowledge", "context"}}
			search := map[string]any{"requested_context": (*runtimecontract.OverlayContext)(nil), "requested_cache": (*runtimecontract.OverlayCache)(nil), "effective_status": "UNKNOWN"}
			if !reflect.DeepEqual(execution["workspace_policy"], workspace) || !reflect.DeepEqual(execution["native_preferences"], search) {
				t.Fatal("safe workspace policy or bounded SDK default metadata is missing")
			}
			current := result["current_configuration"].(map[string]any)
			if current["workspace_policy"] != nil || !reflect.DeepEqual(current["native_preferences"], search) {
				t.Fatal("immutable execution/default metadata was substituted into fresh owner configuration")
			}
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{"auth.json", ".kodex", input.ProviderCredentialSHA256, input.Instructions, input.Task} {
				if strings.Contains(string(raw), private) {
					t.Fatal("discovery exposed private workspace or execution metadata")
				}
			}
		})
	}
}

// TestAssistantNativePreferencesSeparateFreshOwnerFromTurn preserves immutable turn intent when current published preferences change.
// TestAssistantNativePreferencesSeparateFreshOwnerFromTurn сохраняет намерение immutable turn при изменении опубликованных настроек.
func TestAssistantNativePreferencesSeparateFreshOwnerFromTurn(t *testing.T) {
	input, arguments, response := assistantOwnCurrentFixture(runtimecontract.AssistantScopeSystem)
	input.ConfigOverlay = "[context]\nprune = false\n"
	response.AssistantConfigurationCatalog.CurrentConfiguration.PublishedOverlay.Content = "[context]\nprune = true\n"
	request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
	if err != nil {
		t.Fatal(err)
	}
	result, err := castAssistantConfigurationCatalog(input, request, response.AssistantConfigurationCatalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		field string
		prune bool
	}{{"current_configuration", true}, {"execution_snapshot", false}} {
		prefs := result[test.field].(map[string]any)["native_preferences"].(map[string]any)
		context := prefs["requested_context"].(*runtimecontract.OverlayContext)
		if context == nil || context.Prune == nil || *context.Prune != test.prune || prefs["effective_status"] != "UNKNOWN" {
			t.Fatal("fresh preference replaced immutable turn or invented enforcement")
		}
	}
	for _, raw := range []string{`web_search = "live"`, `context.prune = "false"`, `cache.qualification = "SUPPORTED"`} {
		if _, err := assistantNativePreferencesDiscovery(raw); err == nil {
			t.Fatal("invalid preferences accepted")
		}
	}
}

// TestAssistantCurrentExecutionDiscoveryRejectsMissingOrInvalidPolicy checks ten workspace-policy mutations reject the entire read projection rather than defaulting, exposing private paths or claiming a valid policy.
// TestAssistantCurrentExecutionDiscoveryRejectsMissingOrInvalidPolicy проверяет отклонение всей проекции чтения для десяти изменений политики рабочей области без подстановки умолчания, раскрытия частных путей или заявления корректности политики.
func TestAssistantCurrentExecutionDiscoveryRejectsMissingOrInvalidPolicy(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*runtimecontract.RuntimeWorkspacePolicy)
	}{
		{"missing", func(p *runtimecontract.RuntimeWorkspacePolicy) { *p = runtimecontract.RuntimeWorkspacePolicy{} }},
		{"revision", func(p *runtimecontract.RuntimeWorkspacePolicy) { p.Revision++ }},
		{"root", func(p *runtimecontract.RuntimeWorkspacePolicy) { p.Root = "/private-sentinel" }},
		{"capacity", func(p *runtimecontract.RuntimeWorkspacePolicy) { p.MaximumWritableBytes++ }},
		{"file count", func(p *runtimecontract.RuntimeWorkspacePolicy) { p.MaximumFileCount++ }},
		{"readonly access", func(p *runtimecontract.RuntimeWorkspacePolicy) {
			p.Rules[0].Access = runtimecontract.RuntimeWorkspaceWritable
		}},
		{"duplicate rule", func(p *runtimecontract.RuntimeWorkspacePolicy) { p.Rules[1] = p.Rules[0] }},
		{"private path", func(p *runtimecontract.RuntimeWorkspacePolicy) { p.Rules[3].Path = "/private-sentinel/auth.json" }},
		{"digest", func(p *runtimecontract.RuntimeWorkspacePolicy) { p.Digest = strings.Repeat("0", 64) }},
		{"denial reasons", func(p *runtimecontract.RuntimeWorkspacePolicy) { p.DenialReasons = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input, arguments, response := assistantOwnCurrentFixture(runtimecontract.AssistantScopeSystem)
			test.mutate(&input.WorkspacePolicy)
			request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
			if err != nil {
				t.Fatal(err)
			}
			result, err := castAssistantConfigurationCatalog(input, request, response.AssistantConfigurationCatalog)
			if err == nil || result != nil || strings.Contains(err.Error(), "private-sentinel") {
				t.Fatal("invalid policy was projected, defaulted or disclosed")
			}
		})
	}
}
