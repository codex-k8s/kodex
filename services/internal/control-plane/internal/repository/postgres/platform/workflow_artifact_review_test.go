package platform

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestArtifactReviewSubjectSelectsExactAcceptedOutput rejects ambiguity without reading file bodies.
// The opaque revision reference stays distinct from the numeric catalog revision checked by SQL.
// TestArtifactReviewSubjectSelectsExactAcceptedOutput отклоняет неоднозначность без чтения файлов.
// Opaque revision ref не подменяет numeric catalog revision, проверяемую SQL.
func TestArtifactReviewSubjectSelectsExactAcceptedOutput(t *testing.T) {
	requirement := entity.WorkflowResultRequirement{Key: "verdict", Kind: "ARTIFACT_REVIEW", SubjectStepKey: "author", SubjectResultKey: "spec"}
	fixture := func() []workflowResultDependency {
		return []workflowResultDependency{{StepKey: "author", SourceNodeID: "exact-owner-anchor", Receipt: entity.WorkflowAttemptResult{Attempt: 1, Accepted: true, Results: []entity.WorkflowAcceptedResult{{Key: "spec", Kind: "ARTIFACT", ArtifactRef: "art_fixture123", ArtifactRevision: "arv_fixture123", Digest: strings.Repeat("a", 64)}}}}}
	}
	if result, valid := workflowArtifactReviewSubject(fixture(), requirement); !valid || result.ArtifactRevision != "arv_fixture123" {
		t.Fatal("exact accepted subject missing")
	}
	for _, mutate := range []func([]workflowResultDependency) []workflowResultDependency{
		func(v []workflowResultDependency) []workflowResultDependency { v[0].Receipt.Accepted = false; return v },
		func(v []workflowResultDependency) []workflowResultDependency { v[0].SourceNodeID = ""; return v },
		func(v []workflowResultDependency) []workflowResultDependency { v[0].StepKey = "foreign"; return v },
		func(v []workflowResultDependency) []workflowResultDependency {
			v[0].Receipt.Results[0].Key = "other"
			return v
		},
		func(v []workflowResultDependency) []workflowResultDependency {
			v[0].Receipt.Results[0].Digest = "PASS"
			return v
		},
		func(v []workflowResultDependency) []workflowResultDependency { return append(v, v[0]) },
		func(v []workflowResultDependency) []workflowResultDependency {
			v[0].Receipt.Results = append(v[0].Receipt.Results, v[0].Receipt.Results[0])
			return v
		},
	} {
		if _, valid := workflowArtifactReviewSubject(mutate(fixture()), requirement); valid {
			t.Fatal("ambiguous or rejected subject accepted")
		}
	}
}
