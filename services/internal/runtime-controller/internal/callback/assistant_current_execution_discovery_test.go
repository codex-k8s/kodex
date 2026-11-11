package callback

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestAssistantCurrentExecutionDiscoverySeparatesPolicyAndSDKDefault checks both scopes separate immutable execution workspace policy from fresh configuration and report SDK cached-search defaults as unverified metadata.
// Private workspace/auth paths and execution secrets must not enter the projected JSON.
// TestAssistantCurrentExecutionDiscoverySeparatesPolicyAndSDKDefault проверяет в обеих областях отделение неизменяемой политики рабочей области выполнения от свежей конфигурации и выдачу кешированного поиска SDK как непроверенных метаданных по умолчанию.
// Частные пути рабочей области и авторизации, секреты выполнения не должны попадать в проецируемый JSON.
func TestAssistantCurrentExecutionDiscoverySeparatesPolicyAndSDKDefault(t *testing.T) {
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
			search := map[string]any{"configuration_source": "OPENCODE_MANAGED_TOOLS", "requested_mode": "NONE", "effective_status": "UNSUPPORTED", "owner_editable": false,
				"sandbox_domain_allowlist_applies": false, "actual_call_verified": false}
			if !reflect.DeepEqual(execution["workspace_policy"], workspace) || !reflect.DeepEqual(execution["hosted_native_search"], search) {
				t.Fatal("safe workspace policy or bounded SDK default metadata is missing")
			}
			current := result["current_configuration"].(map[string]any)
			if current["workspace_policy"] != nil || !reflect.DeepEqual(current["hosted_native_search"], search) {
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

// TestAssistantCurrentSearchSeparatesFreshOwnerFromTurn checks fresh published live-search settings remain distinct from the turn's cached overlay and invalid settings never fall back silently.
// TestAssistantCurrentSearchSeparatesFreshOwnerFromTurn проверяет отделение свежих опубликованных настроек живого поиска от кешированного поиска настроек хода и запрет молчаливой замены некорректных настроек значением по умолчанию.
func TestAssistantCurrentSearchSeparatesFreshOwnerFromTurn(t *testing.T) {
	input, arguments, response := assistantOwnCurrentFixture(runtimecontract.AssistantScopeSystem)
	input.ConfigOverlay = "web_search = \"cached\"\n"
	response.AssistantConfigurationCatalog.CurrentConfiguration.PublishedOverlay.Content = "web_search = \"live\"\n"
	request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
	if err != nil {
		t.Fatal(err)
	}
	result, err := castAssistantConfigurationCatalog(input, request, response.AssistantConfigurationCatalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ field, mode string }{{"current_configuration", "live"}, {"execution_snapshot", "cached"}} {
		search := result[test.field].(map[string]any)["hosted_native_search"].(map[string]any)
		if search["requested_mode"] != test.mode || search["configuration_source"] != "PUBLISHED_CONFIG_OVERLAY" || search["actual_call_verified"] != false || search["sandbox_domain_allowlist_applies"] != false {
			t.Fatal("fresh current/default/actual effect was confused with the immutable turn")
		}
	}
	for _, raw := range []string{`web_search = "unknown"`, `web_search = true`, `web_search = ""`} {
		if _, err := assistantHostedSearchDiscovery(raw); err == nil {
			t.Fatal("invalid search configuration was replaced by a default")
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

// TestAssistantCurrentExecutionDiscoveryDoesNotInventHostedSearch proves empty configuration never claims an upstream default or grants unavailable tools.
// TestAssistantCurrentExecutionDiscoveryDoesNotInventHostedSearch проверяет, что empty configuration не выдумывает upstream default и не выдаёт unavailable tools.
func TestAssistantCurrentExecutionDiscoveryDoesNotInventHostedSearch(t *testing.T) {
	result, err := assistantHostedSearchDiscovery("")
	if err != nil || result["effective_status"] != "UNSUPPORTED" || result["owner_editable"] != false || result["requested_mode"] != "NONE" {
		t.Fatal("unavailable native tool was advertised as usable")
	}
}
