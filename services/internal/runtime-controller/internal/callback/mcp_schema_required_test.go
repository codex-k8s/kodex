package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// assertMCPRequiredArrays checks the serialized schema recursively because a Go
// schema decoder can accept JSON null into a nil slice while strict MCP clients reject it.
// assertMCPRequiredArrays рекурсивно проверяет сериализованную схему: Go decoder
// допускает JSON null для nil slice, хотя строгий MCP-клиент отвергает такой каталог.
func assertMCPRequiredArrays(t *testing.T, value any, location string) {
	t.Helper()
	switch node := value.(type) {
	case map[string]any:
		if required, exists := node["required"]; exists {
			if _, ok := required.([]any); !ok {
				t.Fatalf("%s.required must be a JSON array, got %T", location, required)
			}
		}
		for key, child := range node {
			if key == "properties" || key == "patternProperties" || key == "$defs" || key == "definitions" {
				if descriptors, ok := child.(map[string]any); ok {
					for name, descriptor := range descriptors {
						assertMCPRequiredArrays(t, descriptor, location+"."+key+"."+name)
					}
					continue
				}
			}
			assertMCPRequiredArrays(t, child, location+"."+key)
		}
	case []any:
		for _, child := range node {
			assertMCPRequiredArrays(t, child, location+"[]")
		}
	}
}

// TestMCPPublishedCatalogRequiredArrays exercises the real JSON-RPC tools/list
// handler for synthetic agent/system/project inputs, including nested input/output schemas.
// It checks wire interoperability only; direct handler invocation does not attest peer authority.
// TestMCPPublishedCatalogRequiredArrays проверяет настоящий JSON-RPC tools/list
// на синтетических agent/system/project inputs, включая вложенные input/output schemas.
// Это проверка wire-совместимости: прямой вызов handler не доказывает peer authority.
func TestMCPPublishedCatalogRequiredArrays(t *testing.T) {
	inputs := map[string]runtimecontract.RunnerInput{
		"agent":   {},
		"system":  assistantConfigurationFixture(runtimecontract.AssistantScopeSystem),
		"project": assistantConfigurationFixture(runtimecontract.AssistantScopeProject),
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://example.invalid/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"schema-fixture","method":"tools/list"}`))
			response := httptest.NewRecorder()
			(&Server{}).serveMCP(response, request, input)
			if response.Code != http.StatusOK {
				t.Fatalf("tools/list status = %d", response.Code)
			}
			var wire struct {
				Result struct {
					Tools []map[string]any `json:"tools"`
				} `json:"result"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
				t.Fatal(err)
			}
			if len(wire.Result.Tools) == 0 {
				t.Fatal("tools/list returned no descriptors")
			}
			for _, tool := range wire.Result.Tools {
				for _, key := range []string{"inputSchema", "outputSchema"} {
					if schema, exists := tool[key]; exists {
						assertMCPRequiredArrays(t, schema, tool["name"].(string)+"."+key)
						resolvedWorkflowSchema(t, schema.(map[string]any))
					}
				}
			}
		})
	}
}

// TestMCPCatalogOptionalArgumentsRemainClosed validates the shipped optional
// discovery input, accepting an empty object while refusing unknown and oversized fields.
// TestMCPCatalogOptionalArgumentsRemainClosed проверяет штатный optional
// discovery input: пустой объект допустим, неизвестные и слишком длинные поля отклоняются.
func TestMCPCatalogOptionalArgumentsRemainClosed(t *testing.T) {
	input := assistantConfigurationFixture(runtimecontract.AssistantScopeSystem)
	schema := configurationCatalogTool(input)["inputSchema"].(map[string]any)
	resolved := resolvedWorkflowSchema(t, schema)
	if err := resolved.Validate(map[string]any{}); err != nil {
		t.Fatalf("empty optional catalog input rejected: %v", err)
	}
	for _, invalid := range []map[string]any{{"unknown": true}, {"agent_query": strings.Repeat("x", 81)}} {
		if err := resolved.Validate(invalid); err == nil {
			t.Fatal("invalid optional catalog input accepted")
		}
	}
}

// TestMCPRequiredArgumentsRemainRequired preserves nonempty requirements and
// closed object validation while the nil case is normalized for serialization.
// TestMCPRequiredArgumentsRemainRequired сохраняет обязательные поля и
// закрытую проверку объекта при нормализации nil для сериализации.
func TestMCPRequiredArgumentsRemainRequired(t *testing.T) {
	schema := objectSchema([]string{"value"}, map[string]any{"value": stringSchema(1, 8)})
	resolved := resolvedWorkflowSchema(t, schema)
	if err := resolved.Validate(map[string]any{"value": "fixture"}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []map[string]any{{}, {"value": ""}, {"value": "fixture", "unknown": true}} {
		if err := resolved.Validate(invalid); err == nil {
			t.Fatal("required or closed object constraint was relaxed")
		}
	}
}

// TestMCPCatalogPinnedClientInterop connects the optional preinstalled pinned
// OpenCode CLI to the actual synthetic tools/list handler without model credentials.
// Absence or another CLI version is an explicit skip; no binary is downloaded.
// TestMCPCatalogPinnedClientInterop подключает заранее установленный закреплённый
// OpenCode CLI к настоящему synthetic tools/list handler без model credentials.
// Отсутствие или другая версия CLI явно пропускают тест; бинарники не загружаются.
func TestMCPCatalogPinnedClientInterop(t *testing.T) {
	executable, err := exec.LookPath("opencode")
	if err != nil {
		t.Skip("pinned OpenCode CLI is not installed")
	}
	probeContext, cancelProbe := context.WithTimeout(t.Context(), 5*time.Second)
	probe := exec.CommandContext(probeContext, executable, "--version")
	probe.Env = []string{"PATH=/usr/bin:/bin"}
	version, probeErr := probe.Output()
	cancelProbe()
	if probeErr != nil || strings.TrimSpace(string(version)) != runtimecontract.OpenCodeVersion {
		t.Skip("installed OpenCode CLI is not the pinned version")
	}
	input := assistantConfigurationFixture(runtimecontract.AssistantScopeSystem)
	var initializeCount, catalogCount, callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		raw, readErr := io.ReadAll(io.LimitReader(request.Body, 1<<20))
		if readErr != nil {
			http.Error(writer, "synthetic request unavailable", http.StatusBadRequest)
			return
		}
		var rpc mcpRequest
		if json.Unmarshal(raw, &rpc) == nil {
			switch rpc.Method {
			case "initialize":
				initializeCount.Add(1)
			case "tools/list":
				catalogCount.Add(1)
			case "tools/call":
				callCount.Add(1)
			}
		}
		request.Body = io.NopCloser(bytes.NewReader(raw))
		(&Server{}).serveMCP(writer, request, input)
	}))
	defer server.Close()
	home := t.TempDir()
	configuration, err := json.Marshal(map[string]any{
		"autoupdate": false, "share": "disabled",
		"permission": map[string]string{"*": "deny", "kodex_*": "allow"},
		"mcp":        map[string]any{"kodex": map[string]any{"type": "remote", "url": server.URL + "/mcp", "enabled": true, "oauth": false}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "mcp", "list")
	command.Dir = home
	command.Env = []string{
		"PATH=" + filepath.Dir(executable) + ":/usr/bin:/bin", "HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "config"), "XDG_DATA_HOME=" + filepath.Join(home, "data"),
		"XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "XDG_STATE_HOME=" + filepath.Join(home, "state"),
		"OPENCODE_CONFIG_CONTENT=" + string(configuration), "OPENCODE_DISABLE_PROJECT_CONFIG=true",
		"OPENCODE_DISABLE_AUTOUPDATE=true", "OPENCODE_DISABLE_MODELS_FETCH=true",
		"OPENCODE_DISABLE_AUTOCOMPACT=true", "OPENCODE_DISABLE_TERMINAL_TITLE=true",
	}
	output, runErr := command.CombinedOutput()
	if runErr != nil || !bytes.Contains(output, []byte("connected")) || bytes.Contains(output, []byte("failed")) {
		t.Fatalf("pinned MCP client did not connect: exit_error=%t initialize=%d catalogs=%d", runErr != nil, initializeCount.Load(), catalogCount.Load())
	}
	if initializeCount.Load() == 0 || catalogCount.Load() == 0 || callCount.Load() != 0 {
		t.Fatal("MCP handshake/catalog was not observed or an unexpected tool call occurred")
	}
}
