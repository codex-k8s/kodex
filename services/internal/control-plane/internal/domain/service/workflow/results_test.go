package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestDeclarationSharedWireCorpus checks the same exact serialized instances as the JSON Schema
// suite, including mathematically integral decimal/exponent forms and terminal newline rejection.
// TestDeclarationSharedWireCorpus проверяет те же сериализованные данные, что и JSON Schema:
// целые десятичные/экспоненциальные формы и запрет завершающего переноса строки.
func TestDeclarationSharedWireCorpus(t *testing.T) {
	raw, err := os.ReadFile("../../../../../../../contracts/runtime/workflow-result/v1/declaration-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name            string `json:"name"`
		RequirementKind string `json:"requirementKind"`
		Raw             string `json:"raw"`
		Valid           bool   `json:"valid"`
		Revision        int64  `json:"revision"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil || len(cases) == 0 {
		t.Fatal("shared declaration corpus is unavailable")
	}
	for _, item := range cases {
		t.Run(item.Name, func(t *testing.T) {
			results, err := DecodeDeclaration(item.Raw, []entity.WorkflowResultRequirement{{Key: "analysis", Kind: item.RequirementKind}})
			if (err == nil) != item.Valid {
				t.Fatalf("declaration outcome mismatch: %v", err)
			}
			if item.Valid && (results["analysis"].Source == nil || results["analysis"].Source.Revision != item.Revision) {
				t.Fatal("revision was rounded or lost")
			}
			if !item.Valid && results != nil {
				t.Fatal("invalid declaration produced usable results")
			}
		})
	}
}

// TestArtifactDeclarationSelectors verifies reachable current output selection and exact captured
// pins without claiming that decoding alone proves artifact ownership or business completion.
// TestArtifactDeclarationSelectors проверяет достижимый выбор нового файла и точные старые pins,
// не выдавая декодирование за доказательство владения артефактом или завершения бизнес-шага.
func TestArtifactDeclarationSelectors(t *testing.T) {
	requirements := []entity.WorkflowResultRequirement{{Key: "analysis", Kind: "ARTIFACT", FileName: "analysis.md", MediaType: "text/markdown"}}
	cases := map[string]string{
		"current":  `{"kind":"CURRENT_COMPLETION"}`,
		"captured": `{"kind":"CAPTURED_ARTIFACT","artifactRef":"art_12345678","revision":3,"digest":"sha256:` + strings.Repeat("a", 64) + `"}`,
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			raw := `{"schema":"kodex.workflow-result.v1","results":[{"key":"analysis","source":` + source + `}]}`
			results, err := DecodeDeclaration(raw, requirements)
			if err != nil || len(results) != 1 || results["analysis"].Source == nil {
				t.Fatalf("selector rejected: %v", err)
			}
			encoded, err := json.Marshal(results["analysis"].Source)
			if err != nil || string(encoded) != source {
				t.Fatal("immutable selector pins changed during roundtrip")
			}
		})
	}
}

// TestArtifactDeclarationRejectsAmbiguousSelectors prevents fallback between current and captured
// sources, schema aliases, missing pins and caller-assigned admission or provenance assertions.
// TestArtifactDeclarationRejectsAmbiguousSelectors запрещает fallback между источниками, подмену
// имён полей, неполные pins и назначаемые вызывающим утверждения о допуске либо происхождении.
func TestArtifactDeclarationRejectsAmbiguousSelectors(t *testing.T) {
	requirements := []entity.WorkflowResultRequirement{{Key: "analysis", Kind: "ARTIFACT"}}
	captured := `{"kind":"CAPTURED_ARTIFACT","artifactRef":"art_12345678","revision":3,"digest":"sha256:` + strings.Repeat("a", 64) + `"}`
	cases := map[string]string{
		"null":                          `null`,
		"array":                         `[]`,
		"missing kind":                  `{}`,
		"unknown kind":                  `{"kind":"LATEST"}`,
		"kind casing":                   `{"Kind":"CURRENT_COMPLETION"}`,
		"duplicate kind":                `{"kind":"CURRENT_COMPLETION","kind":"CURRENT_COMPLETION"}`,
		"escaped duplicate":             `{"kind":"CURRENT_COMPLETION","\u006bind":"CURRENT_COMPLETION"}`,
		"current with empty pin":        `{"kind":"CURRENT_COMPLETION","artifactRef":""}`,
		"current with null pin":         `{"kind":"CURRENT_COMPLETION","revision":null}`,
		"current with filename":         `{"kind":"CURRENT_COMPLETION","fileName":"analysis.md"}`,
		"current with admission":        `{"kind":"CURRENT_COMPLETION","scanState":"CLEAN"}`,
		"captured missing pins":         `{"kind":"CAPTURED_ARTIFACT"}`,
		"captured unknown lineage":      strings.Replace(captured, `"revision":3`, `"revision":3,"nodeRef":"node_foreign"`, 1),
		"captured alias":                strings.Replace(captured, `"artifactRef"`, `"ArtifactRef"`, 1),
		"captured duplicate pin":        strings.Replace(captured, `"revision":3`, `"revision":3,"revision":4`, 1),
		"captured null pin":             strings.Replace(captured, `"revision":3`, `"revision":null`, 1),
		"captured zero revision":        strings.Replace(captured, `"revision":3`, `"revision":0`, 1),
		"captured negative revision":    strings.Replace(captured, `"revision":3`, `"revision":-1`, 1),
		"captured fractional revision":  strings.Replace(captured, `"revision":3`, `"revision":1.5`, 1),
		"captured unsafe integer":       strings.Replace(captured, `"revision":3`, `"revision":9007199254740992`, 1),
		"captured overflow":             strings.Replace(captured, `"revision":3`, `"revision":9223372036854775808`, 1),
		"captured string revision":      strings.Replace(captured, `"revision":3`, `"revision":"3"`, 1),
		"captured foreign locator kind": strings.Replace(captured, "art_12345678", "run_12345678", 1),
		"captured URL":                  strings.Replace(captured, "art_12345678", "https://example.test/file", 1),
		"captured unprefixed digest":    strings.Replace(captured, "sha256:", "", 1),
		"captured uppercase digest":     strings.Replace(captured, strings.Repeat("a", 64), strings.Repeat("A", 64), 1),
		"captured short digest":         strings.Replace(captured, strings.Repeat("a", 64), "abcd", 1),
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			raw := `{"schema":"kodex.workflow-result.v1","results":[{"key":"analysis","source":` + source + `}]}`
			if results, err := DecodeDeclaration(raw, requirements); !errors.Is(err, ErrResultInvalid) || results != nil {
				t.Fatal("invalid selector produced a semantic result")
			}
		})
	}
}

// TestDeclarationRequiresExactShape closes encoding/json aliases and null/empty optional fields
// across every result kind; malformed input must not yield a partially decoded usable declaration.
// TestDeclarationRequiresExactShape закрывает алиасы encoding/json и пустые необязательные поля
// всех видов результата; повреждённый ввод не выдаёт частично пригодную декларацию.
func TestDeclarationRequiresExactShape(t *testing.T) {
	requirements := []entity.WorkflowResultRequirement{{Key: "analysis", Kind: "ARTIFACT"}}
	good := `{"schema":"kodex.workflow-result.v1","results":[{"key":"analysis","source":{"kind":"CURRENT_COMPLETION"}}]}`
	cases := []string{
		strings.Replace(good, `"schema"`, `"Schema"`, 1),
		strings.Replace(good, `"results"`, `"Results"`, 1),
		strings.Replace(good, `"key"`, `"Key"`, 1),
		strings.Replace(good, `"source"`, `"Source"`, 1),
		strings.Replace(good, `"key":"analysis"`, `"key":"analysis","receiptRef":""`, 1),
		strings.Replace(good, `"key":"analysis"`, `"key":"analysis","headSha":null`, 1),
		strings.Replace(good, `"source":{"kind":"CURRENT_COMPLETION"}`, `"source":null`, 1),
		`{"schema":"kodex.workflow-result.v1","results":[{"key":"analysis"}]}`,
		good + strings.Repeat(" ", MaximumResultBytes),
		good + string([]byte{0xff}),
	}
	for _, raw := range cases {
		if results, err := DecodeDeclaration(raw, requirements); !errors.Is(err, ErrResultInvalid) || results != nil {
			t.Fatal("invalid shape produced a semantic result")
		}
	}
	for _, kind := range []string{"PULL_REQUEST", "INTEGRATION_RECEIPT", "DEPLOYMENT", "REVIEW", "QA"} {
		if _, err := DecodeDeclaration(good, []entity.WorkflowResultRequirement{{Key: "analysis", Kind: kind}}); err == nil {
			t.Fatal("artifact selector accepted for an incompatible requirement")
		}
	}
	if _, err := DecodeDeclaration(`{"schema":"kodex.workflow-result.v1","results":[]}`, nil); err == nil {
		t.Fatal("empty result accepted without requirements")
	}
}

// TestDecodeDeclarationRejectsAmbiguousAndForeignResults checks malformed declarations before any
// candidate reference can select owner evidence.
// TestDecodeDeclarationRejectsAmbiguousAndForeignResults проверяет неоднозначные заявления до
// выбора доказательств владельца по ссылке.
func TestDecodeDeclarationRejectsAmbiguousAndForeignResults(t *testing.T) {
	requirements := []entity.WorkflowResultRequirement{{Key: "review", Kind: "REVIEW", SubjectStepKey: "developer"}}
	good := `{"schema":"kodex.workflow-result.v1","results":[{"key":"review","headSha":"` + strings.Repeat("a", 40) + `","verdict":"PASS"}]}`
	if _, err := DecodeDeclaration(good, requirements); err != nil {
		t.Fatal(err)
	}
	tests := []string{strings.Replace(good, `"verdict":"PASS"`, `"verdict":"PASS","verdict":"CHANGES_REQUIRED"`, 1), good + `{}`, strings.Replace(good, `"key":"review"`, `"key":"foreign"`, 1), strings.Replace(good, `"verdict":"PASS"`, `"verdict":"PASS","accepted":true`, 1), strings.Replace(good, strings.Repeat("a", 40), "main", 1), strings.Replace(good, `"results":[`, `"results":[],"results":[`, 1), `{"schema":"kodex.workflow-result.v1","results":[]}`}
	for _, raw := range tests {
		if _, err := DecodeDeclaration(raw, requirements); err == nil {
			t.Fatal("ambiguous declaration accepted")
		}
	}
}

// TestValidRequirementsClosesUnsupportedPredicates rejects incomplete contracts and foreign graph
// references while permitting the bounded PR-review profile.
// TestValidRequirementsClosesUnsupportedPredicates отклоняет неполные условия и чужие ссылки графа,
// разрешая ограниченный PR-review профиль.
func TestValidRequirementsClosesUnsupportedPredicates(t *testing.T) {
	if !ValidRequirements([]entity.WorkflowResultRequirement{{Key: "pr", Kind: "PULL_REQUEST", Repository: "codex-k8s/test-repo"}}, nil) {
		t.Fatal("valid PR rejected")
	}
	if !ValidRequirements([]entity.WorkflowResultRequirement{{Key: "review", Kind: "REVIEW", SubjectStepKey: "developer"}}, []string{"developer"}) {
		t.Fatal("valid review rejected")
	}
	for _, requirements := range [][]entity.WorkflowResultRequirement{nil, {{Key: "x", Kind: "URL"}}, {{Key: "x", Kind: "ARTIFACT", FileName: "../report.md", MediaType: "text/markdown"}}, {{Key: "x", Kind: "REVIEW", SubjectStepKey: "foreign"}}, {{Key: "x", Kind: "PULL_REQUEST", Repository: "https://example.test/repo"}}, {{Key: "x", Kind: "ARTIFACT", FileName: "report.md", MediaType: "text/plain"}, {Key: "x", Kind: "ARTIFACT", FileName: "report.md", MediaType: "text/plain"}}} {
		if ValidRequirements(requirements, nil) {
			t.Fatal("invalid contract accepted")
		}
	}
}

// TestArtifactRequirementsRejectUncapturableNames keeps the published requirement inside the
// ordinary outbox filename boundary instead of promising an impossible result to the employee.
// TestArtifactRequirementsRejectUncapturableNames ограничивает опубликованное требование
// именами штатного outbox, не обещая сотруднику недостижимый результат.
func TestArtifactRequirementsRejectUncapturableNames(t *testing.T) {
	for _, name := range []string{".", "..", "analysis\n.md", "analysis\r.md", "result.md", "workspace-write-result.json"} {
		if ValidRequirements([]entity.WorkflowResultRequirement{{Key: "analysis", Kind: "ARTIFACT", FileName: name, MediaType: "text/markdown"}}, nil) {
			t.Fatal("uncapturable artifact requirement accepted")
		}
	}
	if !ValidRequirements([]entity.WorkflowResultRequirement{{Key: "analysis", Kind: "ARTIFACT", FileName: "Спецификация.md", MediaType: "text/markdown"}}, nil) {
		t.Fatal("ordinary artifact requirement rejected")
	}
}
