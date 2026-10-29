package controlplaneapi

import (
	"reflect"
	"testing"
)

// pdlcPublicationFixture pins one document and response to an existing PR and one local report.
// pdlcPublicationFixture закрепляет документ и ответ за существующим PR и локальный отчёт.
func pdlcPublicationFixture() WorkflowPublication {
	value := serverPublicationFixture()
	value.Version, value.ResultEvidenceProfile = 4, workflowPDLCProfile
	value.Steps = value.Steps[:1]
	value.Steps[0].ResultRequirements = []WorkflowPublicationRequirement{
		{Key: "document", Kind: "REPOSITORY_FILE", Repository: "codex-k8s/test-repo", TargetRef: "3", Branch: "feature/journal", FileName: "docs/requirements.md"},
		{Key: "response", Kind: "PR_COMMENT", Repository: "codex-k8s/test-repo", TargetRef: "3"},
		{Key: "report", Kind: "ARTIFACT", FileName: "requirements.md", MediaType: "text/markdown"},
	}
	return value
}

// TestWorkflowPublicationPDLCRoundtrip preserves the complete branch/PR/file commitment through
// the shared producer and consumer; changing a valid branch must change its canonical digest.
// TestWorkflowPublicationPDLCRoundtrip сохраняет branch/PR/file commitment через общий producer
// и consumer; изменение допустимой ветки обязано менять canonical digest.
func TestWorkflowPublicationPDLCRoundtrip(t *testing.T) {
	value := pdlcPublicationFixture()
	raw, digest, err := EncodeWorkflowPublication(value)
	if err != nil {
		t.Fatal("PDLC publication unavailable", err)
	}
	actual, err := DecodeWorkflowPublication(raw, digest)
	if err != nil || !reflect.DeepEqual(actual, value) {
		t.Fatal("PDLC publication lost exact branch or target pins", err)
	}
	value.Steps[0].ResultRequirements[0].Branch = "feature/updated"
	_, changed, err := EncodeWorkflowPublication(value)
	if err != nil || changed == digest {
		t.Fatal("PDLC branch mutation did not change commitment")
	}
}

// TestWorkflowPublicationPDLCRejectsAmbiguity closes version downgrade, target drift, traversal,
// duplicate file authority and leakage of executable branch fields into historical predicates.
// TestWorkflowPublicationPDLCRejectsAmbiguity закрывает downgrade версии, target drift, traversal,
// duplicate file authority и попадание исполняемых branch fields в прежние условия.
func TestWorkflowPublicationPDLCRejectsAmbiguity(t *testing.T) {
	for name, mutate := range map[string]func(*WorkflowPublication){
		"wire3":            func(v *WorkflowPublication) { v.Version = 3 },
		"unknown-profile":  func(v *WorkflowPublication) { v.ResultEvidenceProfile = "GITHUB_PDLC_V2" },
		"old-profile":      func(v *WorkflowPublication) { v.ResultEvidenceProfile = "GITHUB_REVIEW_QA_V1" },
		"missing-branch":   func(v *WorkflowPublication) { v.Steps[0].ResultRequirements[0].Branch = "" },
		"branch-alias":     func(v *WorkflowPublication) { v.Steps[0].ResultRequirements[0].Branch = "refs/heads/feature/journal" },
		"branch-traversal": func(v *WorkflowPublication) { v.Steps[0].ResultRequirements[0].Branch = "feature/../main" },
		"file-traversal":   func(v *WorkflowPublication) { v.Steps[0].ResultRequirements[0].FileName = "../requirements.md" },
		"leading-zero":     func(v *WorkflowPublication) { v.Steps[0].ResultRequirements[0].TargetRef = "03" },
		"overflow":         func(v *WorkflowPublication) { v.Steps[0].ResultRequirements[0].TargetRef = "2147483648" },
		"other-pr":         func(v *WorkflowPublication) { v.Steps[0].ResultRequirements[1].TargetRef = "4" },
		"other-repository": func(v *WorkflowPublication) { v.Steps[0].ResultRequirements[1].Repository = "codex-k8s/other" },
		"comment-branch":   func(v *WorkflowPublication) { v.Steps[0].ResultRequirements[1].Branch = "feature/journal" },
		"artifact-branch":  func(v *WorkflowPublication) { v.Steps[0].ResultRequirements[2].Branch = "feature/journal" },
		"duplicate-file": func(v *WorkflowPublication) {
			v.Steps[0].ResultRequirements[1] = v.Steps[0].ResultRequirements[0]
			v.Steps[0].ResultRequirements[1].Key = "second"
		},
		"ready-without-file": func(v *WorkflowPublication) {
			v.Steps[0].ResultRequirements[0] = WorkflowPublicationRequirement{Key: "pr", Kind: "PULL_REQUEST", Repository: "codex-k8s/test-repo", TargetRef: "3"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := pdlcPublicationFixture()
			mutate(&value)
			if _, _, err := EncodeWorkflowPublication(value); err == nil {
				t.Fatal("ambiguous PDLC publication accepted")
			}
		})
	}
	for _, version := range []int{1, 2, 3} {
		value := serverPublicationFixture()
		value.Version = version
		if version == 1 {
			value = publicationFixture()
			value.Steps[0].ResultRequirements = []WorkflowPublicationRequirement{{Key: "report", Kind: "ARTIFACT", FileName: "report.md", MediaType: "text/markdown"}}
		} else if version == 3 {
			value.ResultEvidenceProfile = "GITHUB_REVIEW_QA_V1"
		}
		value.Steps[0].ResultRequirements[0].Branch = "feature/journal"
		if _, _, err := EncodeWorkflowPublication(value); err == nil {
			t.Fatal("historical profile accepted an executable branch selector")
		}
	}
}
