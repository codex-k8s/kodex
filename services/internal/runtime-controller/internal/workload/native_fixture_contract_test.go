package workload

import (
	"errors"
	"testing"
)

// TestNativeControllerFixtureHasCompleteOwnerProjection exercises the real producer entry point for the shared synthetic OpenCode fixture.
// TestNativeControllerFixtureHasCompleteOwnerProjection проверяет настоящий producer entry point для общего synthetic OpenCode fixture.
func TestNativeControllerFixtureHasCompleteOwnerProjection(t *testing.T) {
	manager := &Manager{config: testManagerConfig()}
	execution := testExecution(false)
	_, _, err := manager.BuildTurnInput(execution)
	if err != nil {
		var failure *TurnInputBuildError
		if errors.As(err, &failure) {
			t.Fatalf("native fixture rejected: stage=%s reason=%s cause=%v", failure.Stage, failure.ValidationReason(), failure.Cause)
		}
		t.Fatal(err)
	}
}
