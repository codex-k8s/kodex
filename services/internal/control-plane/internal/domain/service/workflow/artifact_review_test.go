package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// artifactReviewVersionFixture supplies an explicit profile and an independent exact output subject.
// artifactReviewVersionFixture задаёт explicit profile и независимый exact output subject.
func artifactReviewVersionFixture() entity.WorkflowVersion {
	return entity.WorkflowVersion{ExecutionMode: ServerDAGV1, ReworkPolicy: OwnerApprovedUpstreamV1, Concurrency: 2, Steps: []entity.WorkflowStep{
		{Key: "author", AgentRef: "agt_author123", ResultRequirements: []entity.WorkflowResultRequirement{{Key: "spec", Kind: "ARTIFACT", FileName: "spec.md", MediaType: "text/markdown"}}},
		{Key: "review", AgentRef: "agt_review123", DependsOn: []string{"author"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "verdict", Kind: "ARTIFACT_REVIEW", SubjectStepKey: "author", SubjectResultKey: "spec"}}},
	}}
}

// TestArtifactReviewRequiresPublishedProfileAndExactSubject preserves independent assignments.
// TestArtifactReviewRequiresPublishedProfileAndExactSubject сохраняет независимые назначения.
func TestArtifactReviewRequiresPublishedProfileAndExactSubject(t *testing.T) {
	if !ValidServerVersion(artifactReviewVersionFixture()) {
		t.Fatal("valid artifact review profile rejected")
	}
	for _, mutation := range []func(*entity.WorkflowVersion){
		func(v *entity.WorkflowVersion) { v.ReworkPolicy = "" },
		func(v *entity.WorkflowVersion) { v.ReworkPolicy = "UNKNOWN" },
		func(v *entity.WorkflowVersion) { v.Steps[1].AgentRef = v.Steps[0].AgentRef },
		func(v *entity.WorkflowVersion) { v.Steps[1].ResultRequirements[0].SubjectResultKey = "foreign" },
		func(v *entity.WorkflowVersion) { v.Steps[1].ResultRequirements[0].SubjectStepKey = "foreign" },
		func(v *entity.WorkflowVersion) { v.Steps[0].ResultRequirements[0].Kind = "REVIEW" },
		func(v *entity.WorkflowVersion) { v.Steps[1].DependsOn = nil },
	} {
		value := artifactReviewVersionFixture()
		mutation(&value)
		if ValidServerVersion(value) {
			t.Fatal("invalid artifact review route accepted")
		}
	}
	legacy := serverVersionFixture()
	raw, err := json.Marshal(legacy)
	if err != nil || strings.Contains(string(raw), "ReworkPolicy") || strings.Contains(string(raw), "SubjectResultKey") {
		t.Fatal("legacy published fields changed")
	}
}

// TestArtifactReviewDeclarationRejectsNarrativeAndMixedProof checks the actual strict decoder.
// TestArtifactReviewDeclarationRejectsNarrativeAndMixedProof проверяет настоящий strict decoder.
func TestArtifactReviewDeclarationRejectsNarrativeAndMixedProof(t *testing.T) {
	requirements := artifactReviewVersionFixture().Steps[1].ResultRequirements
	declaration := entity.WorkflowResultDeclaration{Schema: "kodex.workflow-result.v1", Results: []entity.WorkflowDeclaredResult{{Key: "verdict", Source: &entity.WorkflowArtifactSource{Kind: "CAPTURED_ARTIFACT", ArtifactRef: "art_fixture123", Revision: 2, Digest: "sha256:" + strings.Repeat("a", 64)}, Verdict: "CHANGES_REQUIRED"}}}
	raw, _ := json.Marshal(declaration)
	if _, err := DecodeDeclaration(string(raw), requirements); err != nil {
		t.Fatal("valid closed artifact verdict rejected")
	}
	for _, mutation := range []func(*entity.WorkflowDeclaredResult){
		func(v *entity.WorkflowDeclaredResult) { v.Source.Kind = "CURRENT_COMPLETION" },
		func(v *entity.WorkflowDeclaredResult) { v.Source.Revision = 0 },
		func(v *entity.WorkflowDeclaredResult) { v.Source.Digest = "PASS in report" },
		func(v *entity.WorkflowDeclaredResult) { v.HeadSHA = strings.Repeat("a", 40) },
		func(v *entity.WorkflowDeclaredResult) { v.Verdict = "Everything PASS" },
	} {
		var value entity.WorkflowResultDeclaration
		if json.Unmarshal(raw, &value) != nil {
			t.Fatal("copy synthetic declaration")
		}
		mutation(&value.Results[0])
		bad, _ := json.Marshal(value)
		if _, err := DecodeDeclaration(string(bad), requirements); err == nil {
			t.Fatal("narrative or mixed proof accepted")
		}
	}
}
