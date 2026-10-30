package platform

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// workflowPDLCFileFixture provides three independently receipt-shaped native results; it does
// not fabricate database lineage, current grants, chronology or real external GitHub delivery.
// workflowPDLCFileFixture предоставляет три native результата формы receipt; не имитирует
// database lineage, текущие grants, хронологию либо фактическую внешнюю доставку GitHub.
func workflowPDLCFileFixture(data []byte) (entity.WorkflowResultRequirement, entity.WorkflowDeclaredResult, []workflowIntegrationResult) {
	requirement := entity.WorkflowResultRequirement{Key: "document", Kind: "REPOSITORY_FILE", Repository: "acme/repo", Branch: "feature/journal", TargetRef: "3", FileName: "docs/requirements.md"}
	declaration := entity.WorkflowDeclaredResult{Key: "document", ReceiptRef: "erc_write0000", HeadReceiptRef: "erc_head00000", ContentReceiptRef: "erc_content00"}
	headSHA, writeSHA, blobSHA := strings.Repeat("b", 40), strings.Repeat("a", 40), strings.Repeat("c", 40)
	text := string(data)
	if len(text) > 100 {
		text = text[:100]
	}
	proofs := []workflowIntegrationResult{
		{Operation: "github.repository.content.update", Input: asJSON(map[string]any{"path": requirement.FileName, "branch": requirement.Branch, "message": "Уточнить требования", "content_base64": base64.StdEncoding.EncodeToString(data), "sha": strings.Repeat("d", 40)}), Summary: string(asJSON(map[string]any{"sha": writeSHA, "path": requirement.FileName})), ProviderRef: "github-commit:" + writeSHA},
		{Operation: "github.pull_request.read", Input: []byte(`{"pull_request_number":3}`), Summary: string(asJSON(map[string]any{"number": 3, "state": "open", "head": requirement.Branch, "base": "main", "sha": headSHA, "draft": true})), ProviderRef: "github-pull:3"},
		{Operation: "github.repository.content.read", Input: asJSON(map[string]any{"path": requirement.FileName, "ref": headSHA}), Summary: string(asJSON(map[string]any{
			"path": requirement.FileName, "type": "file", "sha": blobSHA, "size": len(data), "text": text, "commit_sha": headSHA,
			"offset_bytes": 0, "next_offset_bytes": len(text), "eof": len(text) == len(data), "source_digest": "sha256:" + workflowResultSHA(data), "chunk_digest": "sha256:" + workflowResultSHA([]byte(text)),
		})), ProviderRef: "github-content:" + blobSHA},
	}
	for index := range proofs {
		proofs[index].Scope = []byte(`{"owner":"acme","repository":"repo"}`)
		proofs[index].Digest = workflowResultSHA([]byte(proofs[index].Summary))
	}
	return requirement, declaration, proofs
}

// TestWorkflowPDLCFileEvidence verifies exact published bytes at the PR head while permitting
// later commits for other files, draft documentation and native full-source digests on a text page.
// TestWorkflowPDLCFileEvidence проверяет exact опубликованные bytes на head PR, допуская более
// поздние commits других файлов, draft документации и native full-source digest ограниченной страницы.
func TestWorkflowPDLCFileEvidence(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("Требования к журналу обращений\n"), []byte(strings.Repeat("x", 200<<10))} {
		requirement, declaration, p := workflowPDLCFileFixture(data)
		result, reason := acceptWorkflowRepositoryFileResult(requirement, declaration, p[0], p[1], p[2])
		if reason != "" || result.HeadSHA != strings.Repeat("b", 40) || result.FileName != requirement.FileName || result.Branch != requirement.Branch || result.Number != 3 || result.Digest == "" || result.ContentReceiptRef != declaration.ContentReceiptRef {
			t.Fatalf("native published-file proof rejected: bytes=%d reason=%s", len(data), reason)
		}
		if result.Digest == p[0].Digest || result.Digest == p[1].Digest || result.Digest == p[2].Digest {
			t.Fatal("file result lost its composite evidence commitment")
		}
	}
}

// TestWorkflowPDLCFileEvidenceRejectsMismatches isolates provider/input/published-intent faults
// while recomputing the envelope digest so a matching hash cannot conceal wrong business evidence.
// TestWorkflowPDLCFileEvidenceRejectsMismatches изолирует ошибки provider/input/published intent
// с пересчётом envelope digest: совпавший хеш не скрывает неверное бизнес-доказательство.
func TestWorkflowPDLCFileEvidenceRejectsMismatches(t *testing.T) {
	for _, fault := range []string{"repository", "write-operation", "path", "branch", "content", "head-number", "head-branch", "closed", "head-provider", "content-ref", "content-path", "source-digest", "chunk-digest", "chunk-text", "size", "offset", "eof", "empty-page", "content-provider", "null-write", "duplicate-head", "unknown-content", "native-lineage", "digest"} {
		t.Run(fault, func(t *testing.T) {
			r, d, p := workflowPDLCFileFixture([]byte("Точные требования\n"))
			changeSummary := func(index int, key string, value any) {
				t.Helper()
				var output map[string]any
				if json.Unmarshal([]byte(p[index].Summary), &output) != nil {
					t.Fatal("decode independent result fixture")
				}
				output[key] = value
				p[index].Summary = string(asJSON(output))
			}
			switch fault {
			case "repository":
				p[2].Scope = []byte(`{"owner":"acme","repository":"other"}`)
			case "write-operation":
				p[0].Operation = "github.issue.update"
			case "path":
				r.FileName = "docs/other.md"
			case "branch":
				r.Branch = "feature/other"
			case "content":
				p[0].Input = []byte(strings.Replace(string(p[0].Input), base64.StdEncoding.EncodeToString([]byte("Точные требования\n")), base64.StdEncoding.EncodeToString([]byte("Другой текст\n")), 1))
			case "head-number":
				changeSummary(1, "number", 4)
			case "head-branch":
				changeSummary(1, "head", "feature/other")
			case "closed":
				changeSummary(1, "state", "closed")
			case "head-provider":
				p[1].ProviderRef = "github-pull:4"
			case "content-ref":
				p[2].Input = asJSON(map[string]any{"path": r.FileName, "ref": strings.Repeat("a", 40)})
			case "content-path":
				changeSummary(2, "path", "docs/other.md")
			case "source-digest":
				changeSummary(2, "source_digest", "sha256:"+strings.Repeat("f", 64))
			case "chunk-digest":
				changeSummary(2, "chunk_digest", "sha256:"+strings.Repeat("f", 64))
			case "chunk-text":
				changeSummary(2, "text", "Другой текст\n")
				changeSummary(2, "next_offset_bytes", len("Другой текст\n"))
				changeSummary(2, "chunk_digest", "sha256:"+workflowResultSHA([]byte("Другой текст\n")))
			case "size":
				changeSummary(2, "size", 999)
			case "offset":
				changeSummary(2, "offset_bytes", 1)
			case "eof":
				changeSummary(2, "eof", false)
			case "empty-page":
				changeSummary(2, "text", "")
				changeSummary(2, "next_offset_bytes", 0)
				changeSummary(2, "eof", false)
				changeSummary(2, "chunk_digest", "sha256:"+workflowResultSHA(nil))
			case "content-provider":
				p[2].ProviderRef = "github-content:" + strings.Repeat("d", 40)
			case "null-write":
				p[0].Input = []byte(strings.Replace(string(p[0].Input), `"content_base64":"`+base64.StdEncoding.EncodeToString([]byte("Точные требования\n"))+`"`, `"content_base64":null`, 1))
			case "duplicate-head":
				p[1].Input = []byte(`{"pull_request_number":4,"pull_request_number":3}`)
			case "unknown-content":
				p[2].Input = asJSON(map[string]any{"path": r.FileName, "ref": strings.Repeat("b", 40), "actor": "owner"})
			case "native-lineage":
				p[2].LineageFailure = workflowReceiptLineageInvalid
			}
			for index := range p {
				p[index].Digest = workflowResultSHA([]byte(p[index].Summary))
			}
			if fault == "digest" {
				p[2].Digest = strings.Repeat("f", 64)
			}
			if _, reason := acceptWorkflowRepositoryFileResult(r, d, p[0], p[1], p[2]); reason == "" {
				t.Fatal("mismatched native published-file evidence accepted")
			}
		})
	}
}

// TestWorkflowPDLCContentPageParameter checks the shipped adapter's canonical optional page
// selector, including the live 16501-byte source and rejection of undocumented aliases/types.
// TestWorkflowPDLCContentPageParameter проверяет канонический optional selector поставляемого
// adapter, включая живой размер источника16501байт и отказ неизвестным alias/типам.
func TestWorkflowPDLCContentPageParameter(t *testing.T) {
	for _, test := range []struct {
		name   string
		fields map[string]any
		limit  int
		valid  bool
	}{
		{"omitted", nil, 100, true},
		{"minimum", map[string]any{"maximum_bytes": 4}, 4, true},
		{"live", map[string]any{"maximum_bytes": 4096, "offset_bytes": 0}, 4096, true},
		{"maximum", map[string]any{"maximum_bytes": 16384}, 16384, true},
		{"zero", map[string]any{"maximum_bytes": 0}, 100, false},
		{"below_minimum", map[string]any{"maximum_bytes": 3}, 3, false},
		{"negative", map[string]any{"maximum_bytes": -1}, 100, false},
		{"too_large", map[string]any{"maximum_bytes": 16385}, 100, false},
		{"null", map[string]any{"maximum_bytes": nil}, 100, false},
		{"string", map[string]any{"maximum_bytes": "4096"}, 100, false},
		{"boolean", map[string]any{"maximum_bytes": true}, 100, false},
		{"alias", map[string]any{"max_bytes": 4096}, 100, false},
		{"both", map[string]any{"maximum_bytes": 4096, "max_bytes": 4096}, 100, false},
		{"page_exceeds_requested_limit", map[string]any{"maximum_bytes": 4}, 5, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := []byte(strings.Repeat("x", 16501))
			requirement, declaration, proofs := workflowPDLCFileFixture(data)
			var input, output map[string]any
			if json.Unmarshal(proofs[2].Input, &input) != nil || json.Unmarshal([]byte(proofs[2].Summary), &output) != nil {
				t.Fatal("decode native content-page fixture")
			}
			for key, value := range test.fields {
				input[key] = value
			}
			text := data[:test.limit]
			output["text"], output["next_offset_bytes"], output["eof"] = string(text), len(text), false
			output["chunk_digest"] = "sha256:" + workflowResultSHA(text)
			proofs[2].Input, proofs[2].Summary = asJSON(input), string(asJSON(output))
			proofs[2].Digest = workflowResultSHA([]byte(proofs[2].Summary))
			_, reason := acceptWorkflowRepositoryFileResult(requirement, declaration, proofs[0], proofs[1], proofs[2])
			if (reason == "") != test.valid {
				t.Fatalf("native optional content-page parameter: accepted=%t reason=%s", reason == "", reason)
			}
		})
	}
}
