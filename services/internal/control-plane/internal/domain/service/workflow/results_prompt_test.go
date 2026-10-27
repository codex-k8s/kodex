package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// finalPromptExample extracts the single final JSON example consumed by the model, rather than
// rebuilding a test-only declaration which might silently diverge from the actual instructions.
// finalPromptExample извлекает единственный финальный JSON-пример, доступный модели, вместо
// создания отдельной тестовой декларации, которая могла бы расходиться с инструкциями.
func finalPromptExample(t *testing.T, prompt string) string {
	t.Helper()
	var examples []string
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, `{"schema":"kodex.workflow-result.v1"`) {
			examples = append(examples, line)
		}
	}
	if len(examples) != 1 {
		t.Fatal("one final declaration example required")
	}
	return examples[0]
}

// TestFinalPromptArtifactExamplesUseClosedParser proves both published artifact selector forms
// pass the production parser while the observed extra requirement fields still fail closed.
// TestFinalPromptArtifactExamplesUseClosedParser проверяет обе формы файлового селектора тем же
// parser, сохраняя отказ для лишних полей требований из наблюдавшегося неуспешного ответа.
func TestFinalPromptArtifactExamplesUseClosedParser(t *testing.T) {
	requirements := []entity.WorkflowResultRequirement{{Key: "analysis_file", Kind: "ARTIFACT", FileName: "release-scope.md", MediaType: "text/markdown"}}
	prompt, err := ResultDeclarationPrompt(requirements)
	if err != nil {
		t.Fatal(err)
	}
	example := finalPromptExample(t, prompt)
	if _, err := DecodeDeclaration(example, requirements); err != nil {
		t.Fatal("published current-completion example rejected")
	}
	captured := strings.Replace(example, `{"kind":"CURRENT_COMPLETION"}`, `{"kind":"CAPTURED_ARTIFACT","artifactRef":"art_fixture12345","revision":7,"digest":"sha256:`+strings.Repeat("a", 64)+`"}`, 1)
	if _, err := DecodeDeclaration(captured, requirements); err != nil {
		t.Fatal("catalog-substituted captured-artifact example rejected")
	}
	observed := strings.Replace(captured, `"key":"analysis_file"`, `"key":"analysis_file","kind":"ARTIFACT","fileName":"release-scope.md","mediaType":"text/markdown"`, 1)
	if _, err := DecodeDeclaration(observed, requirements); err == nil {
		t.Fatal("observed extra requirement fields became accepted")
	}
	for _, required := range []string{"только key и source", "не являются полями элемента results", "RUN_RESULT", "целое число", "предыдущего технического хода"} {
		if !strings.Contains(prompt, required) {
			t.Fatal("closed artifact instructions missing")
		}
	}
}

// TestFinalPromptExternalSelectorsAreNotEvidence keeps placeholders structurally distinct from
// usable receipts or commits; only explicit fixture substitutions satisfy the closed parser.
// TestFinalPromptExternalSelectorsAreNotEvidence отличает обозначения от допустимых receipts
// и коммитов; только явная подстановка тестовых данных удовлетворяет закрытому parser.
func TestFinalPromptExternalSelectorsAreNotEvidence(t *testing.T) {
	for _, kind := range []string{"PULL_REQUEST", "INTEGRATION_RECEIPT", "DEPLOYMENT", "REVIEW", "QA"} {
		t.Run(kind, func(t *testing.T) {
			requirements := []entity.WorkflowResultRequirement{{Key: "proof", Kind: kind}}
			prompt, err := ResultDeclarationPrompt(requirements)
			if err != nil {
				t.Fatal(err)
			}
			example := finalPromptExample(t, prompt)
			if _, err := DecodeDeclaration(example, requirements); err == nil {
				t.Fatal("placeholder accepted as evidence selector")
			}
			var declaration entity.WorkflowResultDeclaration
			if json.Unmarshal([]byte(example), &declaration) != nil {
				t.Fatal("invalid example JSON")
			}
			result := &declaration.Results[0]
			if kind == "REVIEW" || kind == "QA" {
				result.HeadSHA, result.Verdict = strings.Repeat("a", 40), "CHANGES_REQUIRED"
			} else {
				result.ReceiptRef = "erc_fixture12345"
			}
			encoded, err := json.Marshal(declaration)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeDeclaration(string(encoded), requirements); err != nil {
				t.Fatal("fixture-substituted external selector rejected")
			}
		})
	}
}

// TestFinalPromptRejectsAmbiguousRequirements prevents malformed published selector sets from
// producing instructions that suggest duplicate, unknown or caller-injected result fields.
// TestFinalPromptRejectsAmbiguousRequirements запрещает выдавать инструкции по повреждённому
// набору ключей с повторениями, неизвестными типами либо внедрёнными полями.
func TestFinalPromptRejectsAmbiguousRequirements(t *testing.T) {
	for _, requirements := range [][]entity.WorkflowResultRequirement{
		nil,
		{{Key: "proof", Kind: "UNKNOWN"}},
		{{Key: `proof"}`, Kind: "ARTIFACT"}},
		{{Key: "proof", Kind: "ARTIFACT"}, {Key: "proof", Kind: "QA"}},
		make([]entity.WorkflowResultRequirement, 5),
	} {
		if prompt, err := ResultDeclarationPrompt(requirements); err == nil || prompt != "" {
			t.Fatal("malformed requirements produced final instructions")
		}
	}
}
