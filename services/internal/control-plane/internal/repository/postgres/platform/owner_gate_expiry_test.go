package platform

import (
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestOwnerGateExpiryPolicy preserves legacy serialized steps and rejects invalid explicit waits.
// TestOwnerGateExpiryPolicy сохраняет прежние байты этапа и отклоняет недопустимый явный срок.
func TestOwnerGateExpiryPolicy(t *testing.T) {
	legacy, err := json.Marshal(entity.WorkflowStep{})
	if err != nil || string(legacy) != `{"Key":"","Name":"","AgentRef":"","Instructions":"","ExpectedResult":"","Position":0,"ParallelGroup":0,"TimeoutSeconds":0,"Parallel":false,"HumanGateAfter":false,"DependsOn":null,"GateDecisions":null,"RequiredCapabilityKeys":null}` {
		t.Fatal("legacy workflow step bytes changed")
	}
	for _, seconds := range []int32{-1, 0, 1, 604800, 604801} {
		for _, gated := range []bool{false, true} {
			version := validWorkflowFixture()
			version.Steps[1].ApprovalTimeoutSeconds = &seconds
			version.Steps[1].HumanGateAfter = gated
			if validWorkflowVersion(version) != (gated && seconds >= 1 && seconds <= 604800) {
				t.Fatal("explicit gate timeout bounds changed")
			}
		}
	}
}
