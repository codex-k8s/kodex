package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// captureProgressFixture declares only published artifact selectors alongside an external result.
// It supplies no lease, ownership, capture or acceptance proof.
// captureProgressFixture задаёт только опубликованные файловые селекторы и внешний результат,
// не предоставляя доказательств lease, владения, capture или приёмки.
func captureProgressFixture() []entity.WorkflowResultRequirement {
	return []entity.WorkflowResultRequirement{
		{Key: "bundle", Kind: "ARTIFACT", FileName: "source.bundle.json", MediaType: "application/json"},
		{Key: "manifest", Kind: "ARTIFACT", FileName: "manifest.json", MediaType: "application/json"},
		{Key: "handoff", Kind: "ARTIFACT", FileName: "handoff.md", MediaType: "text/markdown"},
		{Key: "pr", Kind: "PULL_REQUEST", Repository: "example/project"},
	}
}

// TestCaptureProgressSharedWireCorpus binds JSON Schema cases to the actual strict domain decoder,
// including duplicate members, escaped aliases, exclusive phases and exact whitespace handling.
// TestCaptureProgressSharedWireCorpus связывает JSON Schema cases с настоящим строгим decoder:
// повторными полями, экранированными aliases, разделением фаз и допустимыми пробелами JSON.
func TestCaptureProgressSharedWireCorpus(t *testing.T) {
	raw, err := os.ReadFile("../../../../../../../contracts/runtime/workflow-result/v1/capture-progress-cases.json")
	if err != nil {
		t.Fatal("capture progress corpus unavailable")
	}
	var cases []struct {
		Name  string `json:"name"`
		Raw   string `json:"raw"`
		Valid bool   `json:"valid"`
	}
	if json.Unmarshal(raw, &cases) != nil || len(cases) == 0 {
		t.Fatal("capture progress corpus invalid")
	}
	requirements := captureProgressFixture()
	requirements[3] = entity.WorkflowResultRequirement{Key: "analysis", Kind: "ARTIFACT", FileName: "analysis.md", MediaType: "text/markdown"}
	for _, item := range cases {
		t.Run(item.Name, func(t *testing.T) {
			declaration, err := DecodeCaptureProgress(item.Raw, ServerDAGV1, requirements)
			if (err == nil) != item.Valid {
				t.Fatal("capture progress corpus outcome mismatch")
			}
			if !item.Valid && (!errors.Is(err, ErrResultInvalid) || declaration.ArtifactKeys != nil || declaration.Schema != "") {
				t.Fatal("invalid progress returned usable declaration")
			}
		})
	}
}

// TestCaptureProgressPublishedSubset checks semantic constraints unavailable to the static schema:
// exact server mode, published keys, artifact kind and unchanged final-declaration semantics.
// TestCaptureProgressPublishedSubset проверяет условия сверх static schema: точный серверный режим,
// опубликованные ключи, файловый вид и неизменённую семантику финальной декларации.
func TestCaptureProgressPublishedSubset(t *testing.T) {
	valid := `{"schema":"kodex.workflow-capture-progress.v1","phase":"CAPTURE_PROGRESS","cause":"ARTIFACTS_FOR_CONTINUATION","artifactKeys":["handoff","bundle"]}`
	requirements := captureProgressFixture()
	declaration, err := DecodeCaptureProgress(valid, ServerDAGV1, requirements)
	if err != nil || strings.Join(declaration.ArtifactKeys, ",") != "handoff,bundle" {
		t.Fatal("published subset or its ordering changed")
	}
	encoded, err := json.Marshal(declaration)
	if err != nil || string(encoded) != valid {
		t.Fatal("progress declaration roundtrip changed")
	}
	for _, mode := range []string{"", "MANAGER_DRIVEN", "server_dag_v1", "UNKNOWN"} {
		if _, err := DecodeCaptureProgress(valid, mode, requirements); !errors.Is(err, ErrResultInvalid) {
			t.Fatal("progress widened legacy execution mode")
		}
	}
	for _, key := range []string{"unknown", "pr", "handoff.md", "bundle\n"} {
		raw, _ := json.Marshal(entity.WorkflowCaptureProgressDeclaration{Schema: CaptureProgressSchema, Phase: CaptureProgressPhase, Cause: CaptureProgressCause, ArtifactKeys: []string{key}})
		if _, err := DecodeCaptureProgress(string(raw), ServerDAGV1, requirements); !errors.Is(err, ErrResultInvalid) {
			t.Fatal("progress accepted unpublished or non-artifact selector")
		}
	}
	for _, invalid := range []string{valid + strings.Repeat(" ", MaximumResultBytes), string([]byte{0xff}), "null", "[]"} {
		if _, err := DecodeCaptureProgress(invalid, ServerDAGV1, requirements); !errors.Is(err, ErrResultInvalid) {
			t.Fatal("progress accepted invalid JSON or exceeded byte budget")
		}
	}
	if result, err := DecodeDeclaration(valid, requirements); !errors.Is(err, ErrResultInvalid) || result != nil {
		t.Fatal("capture progress became a final business result")
	}
	final := `{"schema":"kodex.workflow-result.v1","results":[{"key":"bundle","source":{"kind":"CURRENT_COMPLETION"}}]}`
	if _, err := DecodeDeclaration(final, requirements[:1]); err != nil {
		t.Fatal("legacy final declaration changed")
	}
	if _, err := DecodeCaptureProgress(final, ServerDAGV1, requirements); !errors.Is(err, ErrResultInvalid) {
		t.Fatal("final declaration became a capture request")
	}
}

// TestCaptureProgressRejectsInvalidRegistry prevents a caller from using ambiguous or malformed
// publication data to authorize a capture key; actual owner authority is still required separately.
// TestCaptureProgressRejectsInvalidRegistry запрещает использовать повреждённый реестр публикации
// для capture key; настоящие полномочия владельца всё равно проверяются отдельно.
func TestCaptureProgressRejectsInvalidRegistry(t *testing.T) {
	raw := `{"schema":"kodex.workflow-capture-progress.v1","phase":"CAPTURE_PROGRESS","cause":"ARTIFACTS_FOR_CONTINUATION","artifactKeys":["bundle"]}`
	for _, requirements := range [][]entity.WorkflowResultRequirement{
		nil, {{Key: "bundle", Kind: "PULL_REQUEST"}},
		{{Key: "bundle", Kind: "ARTIFACT", FileName: "../source.json", MediaType: "application/json"}},
		{captureProgressFixture()[0], captureProgressFixture()[0]},
		append(captureProgressFixture(), entity.WorkflowResultRequirement{Key: "fifth", Kind: "ARTIFACT"}),
	} {
		if _, err := DecodeCaptureProgress(raw, ServerDAGV1, requirements); !errors.Is(err, ErrResultInvalid) {
			t.Fatal("progress accepted invalid published registry")
		}
	}
}
