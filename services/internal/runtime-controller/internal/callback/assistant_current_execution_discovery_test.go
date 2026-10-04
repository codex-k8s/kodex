package callback

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

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
			search := map[string]any{"configuration_source": "SDK_DEFAULT_CACHED", "owner_editable": false,
				"sandbox_domain_allowlist_applies": false, "actual_call_verified": false}
			if !reflect.DeepEqual(execution["workspace_policy"], workspace) || !reflect.DeepEqual(execution["hosted_native_search"], search) {
				t.Fatal("safe workspace policy or bounded SDK default metadata is missing")
			}
			current := result["current_configuration"].(map[string]any)
			if current["workspace_policy"] != nil || current["hosted_native_search"] != nil {
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

// Metadata относится к default закреплённого SDK, а не к доказанному native call.
// Изменение pin или writer требует повторной проверки первичного source SDK.
func TestAssistantCurrentExecutionDiscoveryPinsSDKDefaultWriter(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path is unavailable")
	}
	job := filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "jobs", "agent-runner")
	dockerfile, err := os.ReadFile(filepath.Join(job, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(dockerfile), "ARG KODEX_CODEX_PACKAGE=@openai/codex@0.160.0\n") != 2 {
		t.Fatal("SDK default metadata requires revalidation for changed production/local pins")
	}
	writer, err := os.ReadFile(filepath.Join(job, "internal", "codex", "config.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(writer), "web_search") {
		t.Fatal("SDK default metadata requires revalidation for an explicit writer selection")
	}
}
