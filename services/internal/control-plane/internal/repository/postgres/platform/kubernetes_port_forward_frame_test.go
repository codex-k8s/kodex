package platform

import "testing"

// TestPortForwardInputLifecycleRequiresTheOriginalStagedAction prevents staged intent, closure or a different stage from becoming native input admission.
// TestPortForwardInputLifecycleRequiresTheOriginalStagedAction запрещает превращать staged intent, closure либо другой stage в native input admission.
func TestPortForwardInputLifecycleRequiresTheOriginalStagedAction(t *testing.T) {
	for _, item := range []struct {
		action, state, stage, open, close string
		admitted, writeClosed, allowed    bool
	}{
		{"OPEN", "PREPARED", "open", "open", "", false, false, true},
		{"OPEN", "OPENING", "open", "open", "", true, false, false},
		{"OPEN", "CLOSED", "open", "open", "", true, false, false},
		{"OPEN", "PREPARED", "other", "open", "", false, false, false},
		{"DATA", "OPEN", "data", "open", "", true, false, true},
		{"DATA", "OPENING", "data", "open", "", true, false, false},
		{"DATA", "OPEN", "data", "open", "", false, false, false},
		{"DATA", "OPEN", "data", "open", "", true, true, false},
		{"HALF_CLOSE", "OPEN", "half", "open", "", true, false, true},
		{"HALF_CLOSE", "OPEN", "half", "open", "", true, true, false},
		{"CLOSE", "CLOSED", "close", "open", "close", true, false, true},
		{"CLOSE", "CLOSED", "other", "open", "close", true, false, false},
		{"CLOSE", "CLOSED", "close", "open", "close", false, false, false},
		{"CLOSE", "OPEN", "close", "open", "close", true, false, false},
		{"UNKNOWN", "OPEN", "data", "open", "", true, false, false},
	} {
		record := kubernetesPortForwardFrameRecord{action: item.action, state: item.state, stage: item.stage, openStage: item.open, closeStage: item.close, openAdmitted: item.admitted, writeClosed: item.writeClosed}
		if kubernetesPortForwardInputAllowed(record) != item.allowed {
			t.Fatalf("unexpected admission for action %s state %s", item.action, item.state)
		}
	}
}
