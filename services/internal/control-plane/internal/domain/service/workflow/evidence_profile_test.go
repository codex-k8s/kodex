package workflow

import (
	"encoding/json"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"strings"
	"testing"
)

// TestEvidenceProfileRequirements separates the opt-in report selector from legacy contracts
// and rejects unknown, foreign or non-JSON report keys before publication.
// TestEvidenceProfileRequirements отделяет opt-in report selector от legacy contracts и
// отвергает unknown, foreign и non-JSON report keys до публикации.
func TestEvidenceProfileRequirements(t *testing.T) {
	good := []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: "qa.json", MediaType: "application/json"}, {Key: "qa", Kind: "QA", SubjectStepKey: "author", EvidenceKey: "report"}}
	if !ValidRequirementsForProfile(GitHubReviewQAV1, good, []string{"author"}) || ValidRequirements(good, []string{"author"}) {
		t.Fatal("profile boundary changed")
	}
	for _, change := range []func([]entity.WorkflowResultRequirement){
		func(v []entity.WorkflowResultRequirement) { v[1].EvidenceKey = "missing" },
		func(v []entity.WorkflowResultRequirement) { v[0].MediaType = "text/plain" },
		func(v []entity.WorkflowResultRequirement) { v[0].EvidenceKey = "report" },
		func(v []entity.WorkflowResultRequirement) { v[1].SubjectStepKey = "foreign" },
	} {
		v := append([]entity.WorkflowResultRequirement{}, good...)
		change(v)
		if ValidRequirementsForProfile(GitHubReviewQAV1, v, []string{"author"}) {
			t.Fatal("invalid report binding accepted")
		}
	}
	if ValidRequirementsForProfile("UNKNOWN", good, []string{"author"}) {
		t.Fatal("unknown profile accepted")
	}
}

// TestEvidenceProfileDeclarations requires native COMMENT selectors only in the opted-in profile;
// QA cannot attach an Issue to PASS or copy provider authority into the declaration.
// TestEvidenceProfileDeclarations требует native COMMENT selector только в opt-in profile;
// QA не связывает Issue с PASS и не копирует provider authority в declaration.
func TestEvidenceProfileDeclarations(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for _, kind := range []string{"REVIEW", "QA"} {
		req := []entity.WorkflowResultRequirement{{Key: "checked", Kind: kind}}
		legacy := `{"schema":"kodex.workflow-result.v1","results":[{"key":"checked","headSha":"` + sha + `","verdict":"PASS"}]}`
		receipt := strings.Replace(legacy, `"verdict":"PASS"`, `"verdict":"PASS","receiptRef":"erc_12345678"`, 1)
		if _, err := DecodeDeclaration(legacy, req); err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeDeclaration(receipt, req); err == nil {
			t.Fatal("legacy shape widened")
		}
		_, err := DecodeDeclarationForProfile(receipt, req, GitHubReviewQAV1)
		if (err == nil) != (kind == "REVIEW") {
			t.Fatal("PASS receipt policy mismatch")
		}
		negative := strings.Replace(receipt, `"verdict":"PASS"`, `"verdict":"CHANGES_REQUIRED"`, 1)
		if _, err := DecodeDeclarationForProfile(negative, req, GitHubReviewQAV1); err != nil {
			t.Fatal(err)
		}
		for _, raw := range []string{strings.Replace(negative, `"receiptRef"`, `"ReceiptRef"`, 1), strings.Replace(negative, `"receiptRef":"erc_12345678"`, `"receiptRef":null`, 1), strings.Replace(negative, `"verdict":"CHANGES_REQUIRED"`, `"verdict":"APPROVED"`, 1)} {
			if _, err := DecodeDeclarationForProfile(raw, req, GitHubReviewQAV1); err == nil {
				t.Fatal("malformed native declaration accepted")
			}
		}
	}
}

// qaReportFixture provides employee-reported data and invalid-as-authority immutable selectors;
// only owner tests can establish that the referenced files or commands actually exist.
// qaReportFixture задаёт employee-reported data и selectors без authority; только owner tests
// доказывают существование файлов и фактических команд.
func qaReportFixture() string {
	return `{"schema":"kodex.workflow-qa-report.v1","repository":"owner/repo","pullRequestNumber":42,"headSha":"` + strings.Repeat("a", 40) + `","verdict":"PASS","tests":[{"name":"Go","command":"go test ./...","exitCode":0,"log":{"kind":"CAPTURED_ARTIFACT","artifactRef":"art_12345678","revision":1,"digest":"sha256:` + strings.Repeat("b", 64) + `"}}],"findings":[]}`
}

// TestQAReportClosedShape rejects lossy numeric conversion, missing captured logs, authority
// assertions and fabricated PASS findings while retaining exact UTF-8 command/report bytes.
// TestQAReportClosedShape отвергает округление чисел, отсутствующие captured logs, authority
// assertions и findings при PASS, сохраняя exact UTF-8 command/report bytes.
func TestQAReportClosedShape(t *testing.T) {
	good := qaReportFixture()
	for _, raw := range []string{good, strings.Replace(good, `"pullRequestNumber":42`, `"pullRequestNumber":4.2e1`, 1), strings.Replace(good, `"exitCode":0`, `"exitCode":0.0e+0`, 1), strings.Replace(good, `"exitCode":0`, `"exitCode":-0e999999999`, 1)} {
		report, err := DecodeQAReport([]byte(raw))
		if err != nil || report.Number != 42 || report.Tests[0].ExitCode != 0 {
			t.Fatal("integral report rejected", err)
		}
	}
	negatives := []string{
		strings.Replace(good, `"exitCode":0`, `"exitCode":1`, 1),
		strings.Replace(good, `"exitCode":0`, `"exitCode":0.0000000000000000001`, 1),
		strings.Replace(good, `"pullRequestNumber":42`, `"pullRequestNumber":42.000000000000000001`, 1),
		strings.Replace(good, `"pullRequestNumber":42`, `"pullRequestNumber":9007199254740992`, 1),
		strings.Replace(good, `"pullRequestNumber":42`, `"pullRequestNumber":1e100000000`, 1),
		strings.Replace(good, `"revision":1`, `"revision":1.00000000000000001`, 1),
		strings.Replace(good, `"schema":`, `"Schema":`, 1),
		strings.Replace(good, `"findings":[]`, `"findings":null`, 1),
		strings.Replace(good, `"findings":[]`, `"findings":[{"key":"bug","summary":"Ошибка"}]`, 1),
		strings.Replace(good, `"tests":[`, `"executed":true,"tests":[`, 1),
		strings.Replace(good, `"exitCode":0`, `"exitCode":0,"exitCode":0`, 1),
		strings.Replace(good, `"kind":"CAPTURED_ARTIFACT","artifactRef":"art_12345678","revision":1,"digest":"sha256:`+strings.Repeat("b", 64)+`"`, `"kind":"CURRENT_COMPLETION"`, 1),
		good + `{}`,
	}
	for _, raw := range negatives {
		if _, err := DecodeQAReport([]byte(raw)); err == nil {
			t.Fatal("invalid report accepted")
		}
	}
	var value map[string]any
	if json.Unmarshal([]byte(good), &value) != nil {
		t.Fatal("fixture malformed")
	}
	value["verdict"] = "CHANGES_REQUIRED"
	value["findings"] = []map[string]any{{"key": "bug", "summary": "Фактический дефект"}}
	value["issue"] = QAIssue{Title: "Дефект", Body: "Шаги воспроизведения"}
	raw, _ := json.Marshal(value)
	if report, err := DecodeQAReport(raw); err != nil || report.Issue == nil {
		t.Fatal("negative report unavailable", err)
	}
}
