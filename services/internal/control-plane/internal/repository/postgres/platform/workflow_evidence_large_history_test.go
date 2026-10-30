package platform

import (
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestWorkflowEvidenceLargeReadHistoryKeepsBoundedContinuation reproduces an
// ordinary documentation turn whose settled READ results exceed the task budget.
// Continuation must retain the original task and pin the complete immutable
// snapshot for protected paged reading, without increasing the task limit,
// truncating outcomes, or authorizing another external operation.
// TestWorkflowEvidenceLargeReadHistoryKeepsBoundedContinuation воспроизводит
// обычную работу с документами, где завершённые READ превышают бюджет задачи.
// Продолжение сохраняет исходное поручение и закрепляет полный immutable snapshot
// для защищённого постраничного чтения без увеличения лимита, усечения результатов
// либо разрешения нового внешнего действия.
func TestWorkflowEvidenceLargeReadHistoryKeepsBoundedContinuation(t *testing.T) {
	const originalTask = "Read the existing project documents, update the requirements, and return the mandatory captured files."
	const readBody = "EXACT_READ_BODY_NOT_AN_INLINE_TASK_FRAGMENT"
	outcomes := make([]map[string]any, 24)
	for index := range outcomes {
		outcomes[index] = map[string]any{
			"invocationRef": fmt.Sprintf("inv_%08d", index),
			"operation":     "github.repository.content.read",
			"state":         "SUCCEEDED",
			"resultSummary": strings.Repeat(readBody, 128),
			"receiptRef":    fmt.Sprintf("erc_%08d", index),
			"inputDigest":   strings.Repeat("a", 64),
			"safeErrorCode": "",
			"ownerDecision": nil,
		}
	}
	outcomes[23]["state"] = "REJECTED"
	outcomes[23]["resultSummary"] = ""
	outcomes[23]["receiptRef"] = ""
	outcomes[23]["safeErrorCode"] = "INTEGRATION_REQUEST_REJECTED"
	snapshot := asJSON(map[string]any{
		"schema": "kodex.workflow-evidence-outcomes.v1", "task": originalTask,
		"nativeOutcomes": outcomes,
	})
	if len(snapshot) <= 32768 {
		t.Fatal("fixture does not reproduce an oversized settled read history")
	}
	task, err := workflowEvidenceContinuationTask(snapshot)
	if err != nil {
		t.Fatal("ordinary settled read history prevents the next execution")
	}
	if !runtimecontract.ValidAssistantTurnContent(task) || !strings.Contains(task, originalTask) {
		t.Fatal("continuation changed the original task or its existing budget")
	}
	if !strings.Contains(task, "kodex.workflow-evidence-manifest.v1") ||
		!strings.Contains(task, workflowResultSHA(snapshot)) || !strings.Contains(task, "inv_00000000") {
		t.Fatal("bounded continuation does not pin the complete owner snapshot")
	}
	if strings.Contains(task, readBody) {
		t.Fatal("large result bodies remain embedded in the bounded task")
	}
}
