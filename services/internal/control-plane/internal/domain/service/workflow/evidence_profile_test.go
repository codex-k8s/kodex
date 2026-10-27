package workflow

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
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

// TestEvidenceSharedWireCorpus binds the exact public JSON instances to the owner decoder;
// contextual REVIEW/QA requirement checks remain stronger than the shape-only schema.
// TestEvidenceSharedWireCorpus связывает public JSON с owner decoder; contextual проверки
// REVIEW/QA published requirements остаются строже shape-only schema и не выдают authority.
func TestEvidenceSharedWireCorpus(t *testing.T) {
	for _, corpus := range []struct {
		path   string
		report bool
	}{
		{"workflow-qa-report/v1/report-cases.json", true},
		{"workflow-review-qa-result/v1/declaration-cases.json", false},
	} {
		raw, err := os.ReadFile("../../../../../../../contracts/runtime/" + corpus.path)
		if err != nil {
			t.Fatal("read shared evidence wire corpus")
		}
		var cases []struct {
			Name  string `json:"name"`
			Kind  string `json:"kind"`
			Raw   string `json:"raw"`
			Valid bool   `json:"valid"`
		}
		if json.Unmarshal(raw, &cases) != nil || len(cases) == 0 {
			t.Fatal("invalid shared evidence corpus")
		}
		for _, item := range cases {
			t.Run(item.Name, func(t *testing.T) {
				var err error
				if corpus.report {
					_, err = DecodeQAReport([]byte(item.Raw))
				} else {
					_, err = DecodeDeclarationForProfile(item.Raw, []entity.WorkflowResultRequirement{{Key: "checked", Kind: item.Kind}}, GitHubReviewQAV1)
				}
				if (err == nil) != item.Valid {
					t.Fatal("shared evidence wire outcome mismatch")
				}
			})
		}
	}
}

// TestEvidenceSubjectGraphContract rejects a REVIEW-only or ambiguous PR predecessor only for
// the new profile, before publication can create a graph rejected by the owner SQL guards.
// TestEvidenceSubjectGraphContract отклоняет REVIEW-only либо неоднозначный PR predecessor
// только нового профиля до публикации графа, который owner SQL guards не смогут исполнить.
func TestEvidenceSubjectGraphContract(t *testing.T) {
	for _, kind := range []string{"REVIEW", "QA"} {
		version := serverVersionFixture()
		step := entity.WorkflowStep{Key: "checker", AgentRef: "agt_checker", DependsOn: []string{"reviewer"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "check", Kind: kind, SubjectStepKey: "reviewer"}}}
		if kind == "QA" {
			step.ResultRequirements = append(step.ResultRequirements, entity.WorkflowResultRequirement{Key: "report", Kind: "ARTIFACT", FileName: "qa.json", MediaType: "application/json"})
		}
		version.Steps = append(version.Steps, step)
		if !ValidServerVersion(version) {
			t.Fatal("legacy reviewer subject changed")
		}
		version.ResultEvidenceProfile = GitHubReviewQAV1
		if kind == "QA" {
			version.Steps[2].ResultRequirements[0].EvidenceKey = "report"
		}
		if ValidServerVersion(version) {
			t.Fatal("new evidence profile accepted REVIEW-only subject")
		}
		version.Steps[2].DependsOn = []string{"developer"}
		version.Steps[2].ResultRequirements[0].SubjectStepKey = "developer"
		if !ValidServerVersion(version) {
			t.Fatal("exact direct PR subject rejected")
		}
		version.Steps[0].ResultRequirements = append(version.Steps[0].ResultRequirements, entity.WorkflowResultRequirement{Key: "other_pr", Kind: "PULL_REQUEST", Repository: "codex-k8s/test-repo"})
		if ValidServerVersion(version) {
			t.Fatal("ambiguous published PR subject accepted")
		}
	}
}
