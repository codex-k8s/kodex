package platform

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestRepositoryBundleCallbackTask preserves exact owner feedback in the joint immutable input and rejects incomplete or contradictory terminal projections before scheduling another turn.
// TestRepositoryBundleCallbackTask сохраняет exact owner feedback в joint immutable input и отклоняет неполные или противоречивые terminal projections до постановки следующего turn.
func TestRepositoryBundleCallbackTask(t *testing.T) {
	comment := "Preserve the exact input.\nDo not repeat WRITE. <owner>"
	valid := map[string]any{
		"completedChildren":                []any{map[string]any{"runRef": "run_child123", "state": "SUCCEEDED", "artifactRefs": []string{"art_child123"}}},
		"remainingStepKeys":                []string{},
		"repositoryPublicationOutcome":     map[string]any{"total": 1, "pending": 0, "unsuccessful": 1},
		"repositoryPublicationIntegration": integrationContinuationSnapshot{InvocationRef: "inv_exact12345", State: "REJECTED", OwnerDecision: integrationContinuationDecision{GateRef: "gat_exact12345", GateVersion: 2, Decision: "REJECT", Comment: comment}},
	}
	encode := func() []byte {
		t.Helper()
		raw, err := json.Marshal(valid)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw := encode()
	task, err := callbackContinuationTask(raw)
	if err != nil || !strings.HasSuffix(task, string(raw)) || !strings.Contains(task, "The owner rejected this invocation") || !strings.Contains(task, "SUCCEEDED child execution and model resultSummary do not prove") {
		t.Fatal("joint task lost exact owner authority", err)
	}
	var restored struct {
		Integration integrationContinuationSnapshot `json:"repositoryPublicationIntegration"`
	}
	if json.Unmarshal([]byte(strings.SplitN(task, "\n\n", 2)[1]), &restored) != nil || restored.Integration.OwnerDecision.Comment != comment {
		t.Fatal("joint task modified persisted owner comment")
	}
	children := make([]any, 9)
	for i := range children {
		children[i] = map[string]any{"runRef": "run_child12345", "state": "SUCCEEDED", "resultSummary": strings.Repeat("x", 4000), "artifactRefs": []string{}}
	}
	valid["completedChildren"] = children
	if got, err := callbackContinuationTask(encode()); got != "" || !errors.Is(err, errRepositoryBundleCallbackInputTooLarge) || errors.Is(err, errs.ErrConflict) {
		t.Fatal("typed oversized presentation was confused with authority/SQL failure", err)
	}
	valid["completedChildren"] = []any{map[string]any{"runRef": "run_child12345", "state": "SUCCEEDED", "artifactRefs": []string{}}}
	delete(valid, "repositoryPublicationIntegration")
	if got, err := callbackContinuationTask(encode()); got != "" || !errors.Is(err, errs.ErrConflict) {
		t.Fatal("outcome without exact owner decision accepted")
	}
	valid["repositoryPublicationIntegration"] = integrationContinuationSnapshot{InvocationRef: "inv_exact12345", State: "SUCCEEDED", OwnerDecision: integrationContinuationDecision{GateRef: "gat_exact12345", GateVersion: 2, Decision: "REJECT", Comment: comment}}
	if got, err := callbackContinuationTask(encode()); got != "" || !errors.Is(err, errs.ErrConflict) {
		t.Fatal("contradictory terminal decision accepted")
	}
	delete(valid, "repositoryPublicationOutcome")
	if got, err := callbackContinuationTask(encode()); got != "" || !errors.Is(err, errs.ErrConflict) {
		t.Fatal("decision without complete publication outcome accepted")
	}
}
