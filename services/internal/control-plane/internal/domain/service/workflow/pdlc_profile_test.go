package workflow

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// pdlcAuthorRequirements supplies one mandatory repository write, a genuine file output and a
// PR comment; references describe published intent rather than authority for executing effects.
// pdlcAuthorRequirements задаёт обязательную запись репозитория, настоящий файловый результат и
// комментарий PR; ссылки описывают опубликованное намерение, а не полномочия внешнего исполнения.
func pdlcAuthorRequirements() []entity.WorkflowResultRequirement {
	return []entity.WorkflowResultRequirement{
		{Key: "document", Kind: "REPOSITORY_FILE", Repository: "acme/repo", TargetRef: "3", Branch: "feature/journal", FileName: "docs/requirements.md"},
		{Key: "report", Kind: "ARTIFACT", FileName: "requirements.md", MediaType: "text/markdown"},
		{Key: "comment", Kind: "PR_COMMENT", Repository: "acme/repo", TargetRef: "3"},
	}
}

// TestPDLCFileDeclaration requires three distinct native receipt selectors while rejecting
// model-supplied content, authority, verdicts, nulls and historical-profile reinterpretation.
// TestPDLCFileDeclaration требует три разных native receipt selectors и отклоняет присланные
// моделью содержимое, authority, verdict, null и переинтерпретацию прежнего профиля.
func TestPDLCFileDeclaration(t *testing.T) {
	r := pdlcAuthorRequirements()[:1]
	const raw = `{"schema":"kodex.workflow-result.v1","results":[{"key":"document","receiptRef":"erc_write0000","headReceiptRef":"erc_head00000","contentReceiptRef":"erc_content00"}]}`
	result, err := DecodeDeclarationForProfile(raw, r, GitHubPDLCV1)
	if err != nil || result["document"].HeadReceiptRef != "erc_head00000" || result["document"].ContentReceiptRef != "erc_content00" {
		t.Fatal("native repository-file selectors were lost", err)
	}
	for _, profile := range []string{"", GitHubReviewQAV1, "GITHUB_PDLC_V2"} {
		if _, err := DecodeDeclarationForProfile(raw, r, profile); err == nil {
			t.Fatal("unpublished profile accepted repository-file selectors")
		}
	}
	for _, invalid := range []string{
		strings.Replace(raw, `,"contentReceiptRef":"erc_content00"`, "", 1),
		strings.Replace(raw, `"erc_content00"`, "null", 1),
		strings.Replace(raw, `"erc_content00"`, `"erc_write0000"`, 1),
		strings.Replace(raw, `"headReceiptRef"`, `"HeadReceiptRef"`, 1),
		strings.Replace(raw, `"key":"document"`, `"key":"document","verdict":"PASS"`, 1),
		strings.Replace(raw, `"key":"document"`, `"key":"document","branch":"main"`, 1),
		strings.Replace(raw, `"key":"document"`, `"key":"document","contentReceiptRef":"erc_forged000"`, 1),
	} {
		if _, err := DecodeDeclarationForProfile(invalid, r, GitHubPDLCV1); err == nil {
			t.Fatal("ambiguous repository-file declaration accepted")
		}
	}
	comment := pdlcAuthorRequirements()[2:]
	const commentRaw = `{"schema":"kodex.workflow-result.v1","results":[{"key":"comment","receiptRef":"erc_comment00"}]}`
	if _, err := DecodeDeclarationForProfile(commentRaw, comment, GitHubPDLCV1); err != nil {
		t.Fatal("native PR comment selector rejected", err)
	}
	if _, err := DecodeDeclarationForProfile(commentRaw, comment, GitHubReviewQAV1); err == nil {
		t.Fatal("historical profile accepted a new PR comment predicate")
	}
}

// TestPDLCAuthorRequirements checks the opt-in author contract and rejects silent broadening of
// historical profiles, ambiguous file authority, foreign PR selectors and malformed branch/path.
// TestPDLCAuthorRequirements проверяет явный контракт автора и запрещает неявное расширение
// прежних профилей, неоднозначные файловые полномочия, чужой PR и неверные branch/path.
func TestPDLCAuthorRequirements(t *testing.T) {
	const profile = "GITHUB_PDLC_V1"
	if !ValidRequirementsForProfile(profile, pdlcAuthorRequirements(), nil) {
		t.Fatal("explicit PDLC author requirements are unavailable")
	}
	for _, old := range []string{"", GitHubReviewQAV1} {
		if ValidRequirementsForProfile(old, pdlcAuthorRequirements(), nil) {
			t.Fatal("historical profile gained repository-write predicates")
		}
		ordinary := []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: "report.md", MediaType: "text/markdown", Branch: "feature/journal"}}
		if ValidRequirementsForProfile(old, ordinary, nil) {
			t.Fatal("historical predicate silently ignored an executable branch selector")
		}
	}
	for _, mutate := range []func([]entity.WorkflowResultRequirement){
		func(r []entity.WorkflowResultRequirement) { r[0].Branch = "" },
		func(r []entity.WorkflowResultRequirement) { r[0].Branch = "refs/heads/feature/journal" },
		func(r []entity.WorkflowResultRequirement) { r[0].Branch = "feature/../main" },
		func(r []entity.WorkflowResultRequirement) { r[0].FileName = "../requirements.md" },
		func(r []entity.WorkflowResultRequirement) { r[0].FileName = "/docs/requirements.md" },
		func(r []entity.WorkflowResultRequirement) { r[0].TargetRef = "03" },
		func(r []entity.WorkflowResultRequirement) { r[2].TargetRef = "4" },
		func(r []entity.WorkflowResultRequirement) { r[2].Repository = "acme/other" },
		func(r []entity.WorkflowResultRequirement) { r[2].Branch = "feature/journal" },
		func(r []entity.WorkflowResultRequirement) { r[2] = r[0]; r[2].Key = "other_file" },
	} {
		r := pdlcAuthorRequirements()
		mutate(r)
		if ValidRequirementsForProfile(profile, r, nil) {
			t.Fatal("ambiguous PDLC intent was accepted")
		}
	}
}
