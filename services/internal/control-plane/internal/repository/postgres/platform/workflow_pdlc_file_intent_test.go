package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
)

// workflowPDLCIntentFixture builds a private journal record for selection tests only. Native
// authority, actual receipt custody and SQL chronology are not established by these locators.
// workflowPDLCIntentFixture создаёт private journal record только для selector tests. Locators
// не доказывают native authority, фактическую сохранность receipt либо SQL chronology.
func workflowPDLCIntentFixture(key, call, ref, predecessor string) workflowEffectIntent {
	return workflowEffectIntent{Schema: "kodex.workflow-effect-intent.v1", Operation: "github.repository.content.update", ResultKey: key,
		OriginNodeID: "node-author", FileCallKey: call, InvocationRef: ref, FilePredecessorRef: predecessor}
}

// TestWorkflowPDLCFileIntentSelection keeps file revisions ordered, allows independent paths
// and prefers exact historical native replay without treating a reused key as a new write.
// TestWorkflowPDLCFileIntentSelection сохраняет порядок file revisions, допускает независимые
// paths и предпочитает exact historical native replay, не считая повтор ключа новой записью.
func TestWorkflowPDLCFileIntentSelection(t *testing.T) {
	first := workflowPDLCIntentFixture("file-a", "call-0001", "inv_first000", "")
	other := workflowPDLCIntentFixture("file-b", "call-0002", "inv_other000", "")
	second := workflowPDLCIntentFixture("file-a", "call-0003", "inv_second00", first.InvocationRef)
	current := workflowEffectIntent{ResultKey: "file-a", OriginNodeID: "node-author"}
	for _, mode := range []string{"empty", "latest", "original-call", "other-path", "fork", "duplicate-call", "duplicate-invocation", "missing-call", "cross-path-call", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			intents := []workflowEffectIntent{first, other, second}
			request := current
			call, want, exact := "call-0004", 2, false
			switch mode {
			case "empty":
				intents, want = nil, -1
			case "original-call":
				call, want, exact = first.FileCallKey, 0, true
			case "other-path":
				request.ResultKey, want = "file-b", 1
			case "fork":
				intents[2].FilePredecessorRef = ""
			case "duplicate-call":
				intents[2].FileCallKey = first.FileCallKey
			case "duplicate-invocation":
				intents[2].InvocationRef = other.InvocationRef
			case "missing-call":
				intents[0].FileCallKey = ""
			case "cross-path-call":
				call = other.FileCallKey
			case "overflow":
				intents = make([]workflowEffectIntent, workflowPDLCEffectLimit+1)
			}
			index, actualExact, err := selectWorkflowPDLCFileIntent(intents, request, call)
			if contains([]string{"empty", "latest", "original-call", "other-path"}, mode) {
				if err != nil || index != want || actualExact != exact {
					t.Fatal("native file intent selection lost ordering or original call", mode)
				}
			} else if !errors.Is(err, errs.ErrConflict) {
				t.Fatal("corrupt native intent history accepted", mode)
			}
		})
	}
}

// TestWorkflowPDLCFileEpochBindsPredecessor distinguishes a deliberate A-to-B-to-A revision
// chain from retry of the original A intent while keeping every historical profile's limit.
// TestWorkflowPDLCFileEpochBindsPredecessor отличает намеренную цепочку ревизий A-to-B-to-A
// от retry исходного A intent, сохраняя лимит каждого прежнего профиля.
func TestWorkflowPDLCFileEpochBindsPredecessor(t *testing.T) {
	desired := workflowResultSHA([]byte("same-file-bytes"))
	first := workflowPDLCFileEpochDigest(desired, "")
	revert := workflowPDLCFileEpochDigest(desired, "inv_previous0")
	if first == revert || first != workflowPDLCFileEpochDigest(desired, "") || revert == workflowPDLCFileEpochDigest(workflowResultSHA([]byte("different")), "inv_previous0") {
		t.Fatal("file intent epoch lost its desired bytes or predecessor")
	}
	for _, profile := range []string{"", workflowservice.GitHubReviewQAV1, "UNKNOWN"} {
		if workflowEffectLimitForProfile(profile) != 16 {
			t.Fatal("historical or unknown profile acquired a larger journal")
		}
	}
	if workflowEffectLimitForProfile(workflowservice.GitHubPDLCV1) != 64 {
		t.Fatal("explicit PDLC journal budget unavailable")
	}
}
