package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// nativeWireCase carries an actual owner materialization and an independently expected rendered fragment to the native consumer qualification.
// nativeWireCase передаёт настоящую материализацию владельца и независимо ожидаемый фрагмент проверке native consumer.
type nativeWireCase struct {
	Name     string                      `json:"name"`
	Input    runtimecontract.RunnerInput `json:"input"`
	Expected string                      `json:"expected"`
	SHA256   string                      `json:"sha256"`
}

// TestNativeWireMaterializations resolves fresh branch, range, nested-template and function inputs with the production renderer, validates its consumer contract and optionally exports synthetic fixtures to a new private file. Equal live values and pinned retry must preserve exact bytes; no rendered-prompt cache is substituted.
// TestNativeWireMaterializations разрешает свежие условия, range, вложенный шаблон и функцию production renderer, проверяет consumer contract и опционально экспортирует synthetic fixtures в новый приватный файл. Равные live-значения и pinned retry сохраняют точные байты; кеш готового prompt не подставляется.
func TestNativeWireMaterializations(t *testing.T) {
	text := `{{define "project"}}{{with .}}{{printf "%s" .name}}{{end}}{{end}}{{if .task}}{{template "project" .project}}{{else}}SYNTHETIC_BRANCH_OFF{{end}}:{{range .input.files}}{{.name}};{{end}}`
	var cases []nativeWireCase
	for index, settings := range []struct{ name, project, file, task, expected string }{
		{"initial", "SYNTHETIC_PROJECT_A", "SYNTHETIC_FILE_A", "present", "SYNTHETIC_PROJECT_A:SYNTHETIC_FILE_A;"},
		{"changed", "SYNTHETIC_PROJECT_B", "SYNTHETIC_FILE_B", "present", "SYNTHETIC_PROJECT_B:SYNTHETIC_FILE_B;"},
		{"fresh_equal", "SYNTHETIC_PROJECT_B", "SYNTHETIC_FILE_B", "present", "SYNTHETIC_PROJECT_B:SYNTHETIC_FILE_B;"},
		{"branch_off", "SYNTHETIC_PROJECT_C", "SYNTHETIC_FILE_C", "", "SYNTHETIC_BRANCH_OFF:SYNTHETIC_FILE_C;"},
	} {
		snapshot := semanticFixture()
		snapshot.StructuredVariables = validationTemplateData()
		snapshot.StructuredVariables["project"].(map[string]any)["name"] = settings.project
		snapshot.StructuredVariables["input"].(map[string]any)["files"] = []any{map[string]any{"name": settings.file}}
		snapshot.Variables["task"] = settings.task
		snapshot.StructuredVariables["task"] = settings.task
		result, err := Materialize(text, snapshot)
		if err != nil || !result.Complete || !strings.Contains(result.Prompt, settings.expected) {
			t.Fatalf("native wire fixture materialization failed: case=%s err=%v", settings.name, err)
		}
		retry, err := Materialize(text, snapshot)
		if err != nil || retry.Prompt != result.Prompt || retry.Digest != result.Digest {
			t.Fatal("pinned materialization changed")
		}
		input := runtimecontract.RunnerInput{Instructions: result.Prompt, Capabilities: result.EffectiveCapabilities,
			PromptServiceTemplateRevision: result.ServiceTemplateRevision, PromptServiceTemplateDigest: result.ServiceTemplateDigest, PromptTargetKind: snapshot.TargetKind}
		if _, err := runtimecontract.DecodePromptService(input); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(input.Instructions))
		cases = append(cases, nativeWireCase{Name: settings.name, Input: input, Expected: settings.expected, SHA256: hex.EncodeToString(digest[:])})
		if index == 2 && input.Instructions != cases[1].Input.Instructions {
			t.Fatal("freshly resolved equal values changed materialized bytes")
		}
	}
	if cases[0].SHA256 == cases[1].SHA256 || cases[2].SHA256 == cases[3].SHA256 {
		t.Fatal("live dependency change did not change materialized bytes")
	}
	path := os.Getenv("KODEX_TEST_TEMPLATE_WIRE_FIXTURE")
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal("create synthetic native wire fixture")
	}
	err = json.NewEncoder(file).Encode(cases)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatal("write synthetic native wire fixture")
	}
}
