package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestWorkflowEffectSlots checks published operation/target admission independently of incidental model-selected tool names.
// TestWorkflowEffectSlots проверяет published operation/target независимо от выбранных моделью имён инструментов.
func TestWorkflowEffectSlots(t *testing.T) {
	scope := []byte(`{"owner":"owner","repository":"repo"}`)
	step := entity.WorkflowStep{ResultRequirements: []entity.WorkflowResultRequirement{{Key: "pr", Kind: "PULL_REQUEST", Repository: "owner/repo"}}}
	for _, operation := range []string{repositoryBundleOperation, "github.pull_request.create"} {
		if key, err := workflowEffectSlot(step, nil, operation, scope, []byte(`{}`)); err != nil || key != "pr" {
			t.Fatal("published write slot unavailable")
		}
	}
	for _, operation := range []string{"github.issue.comment.create", "github.pull_request.review.create", "github.pull_request.update", "ssh.compose.deploy"} {
		if _, err := workflowEffectSlot(step, nil, operation, scope, []byte(`{}`)); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("unbound operation admitted")
		}
	}
	step.ResultRequirements = append(step.ResultRequirements, entity.WorkflowResultRequirement{Key: "other-pr", Kind: "PULL_REQUEST", Repository: "owner/repo"})
	if _, err := workflowEffectSlot(step, nil, "github.pull_request.create", scope, []byte(`{}`)); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("ambiguous result key admitted")
	}
	step.ResultRequirements = step.ResultRequirements[:1]
	if _, err := workflowEffectSlot(step, nil, "github.pull_request.create", []byte(`{"owner":"foreign","repository":"repo"}`), []byte(`{}`)); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("foreign repository admitted")
	}
	if _, err := workflowEffectSlot(step, nil, "github.pull_request.create", []byte(`{"owner":"OWNER","repository":"repo"}`), []byte(`{}`)); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("write admission disagrees with exact result repository predicate")
	}
}

// TestWorkflowEffectMergeTarget requires the exact owner-accepted predecessor PR/head rather than an arbitrary same-repository mutation.
// TestWorkflowEffectMergeTarget требует exact accepted predecessor PR/head вместо произвольного WRITE в том же repository.
func TestWorkflowEffectMergeTarget(t *testing.T) {
	sha := strings.Repeat("a", 40)
	step := entity.WorkflowStep{DependsOn: []string{"author"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "merge", Kind: "INTEGRATION_RECEIPT", Repository: "owner/repo", SubjectStepKey: "author", Operation: "github.pull_request.merge"}}}
	inputs := []workflowResultDependency{{StepKey: "author", SourceNodeID: "owner-source-node", Receipt: entity.WorkflowAttemptResult{Attempt: 1, Accepted: true, Results: []entity.WorkflowAcceptedResult{{Key: "pr", Kind: "PULL_REQUEST", Repository: "owner/repo", Number: 42, HeadSHA: sha}}}}}
	for _, test := range []struct {
		input   string
		allowed bool
	}{{`{"pull_request_number":42,"sha":"` + sha + `"}`, true}, {`{"pull_request_number":43,"sha":"` + sha + `"}`, false}, {`{"pull_request_number":42,"sha":"` + strings.Repeat("b", 40) + `"}`, false}} {
		_, err := workflowEffectSlot(step, inputs, "github.pull_request.merge", []byte(`{"owner":"owner","repository":"repo"}`), []byte(test.input))
		if (err == nil) != test.allowed {
			t.Fatal("merge target did not follow accepted predecessor pins")
		}
	}
}

// TestWorkflowEffectSemanticDigest preserves complete values while normalizing only JSON object ordering, including large exact integers.
// TestWorkflowEffectSemanticDigest сохраняет полные значения, нормализуя только порядок JSON keys, включая точные большие integers.
func TestWorkflowEffectSemanticDigest(t *testing.T) {
	a, err := workflowEffectSemanticDigest("pr", "github.pull_request.create", "scope", []byte(`{"head":"branch","number":9007199254740991,"body":"exact\ntext"}`))
	b, secondErr := workflowEffectSemanticDigest("pr", "github.pull_request.create", "scope", []byte(`{ "body":"exact\ntext", "number":9007199254740991, "head":"branch" }`))
	if err != nil || secondErr != nil || a != b {
		t.Fatal("equivalent canonical inputs differ")
	}
	for _, raw := range []string{`{"head":"other","number":9007199254740991,"body":"exact\ntext"}`, `{"head":"branch","number":9007199254740990,"body":"exact\ntext"}`, `{"head":"branch","number":9007199254740991,"body":"changed"}`} {
		changed, err := workflowEffectSemanticDigest("pr", "github.pull_request.create", "scope", []byte(raw))
		if err != nil || changed == a {
			t.Fatal("changed semantic input collapsed")
		}
	}
	if _, err := workflowEffectSemanticDigest("pr", "operation", "scope", []byte(`{} []`)); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("trailing JSON admitted")
	}
}
