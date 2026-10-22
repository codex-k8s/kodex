package testfixture

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestTurnInputPreservesBindingsAndIndependentWorkspace checks repeatable fixture values,
// recomputed input/environment/execution/MCP digests and independent mutable workspace rules.
// It exercises local construction only, without credentials, files or runtime workloads.
// TestTurnInputPreservesBindingsAndIndependentWorkspace проверяет повторяемость тестовых значений,
// пересчитанные дайджесты входа, окружения, выполнения и MCP и независимость изменяемых правил рабочей области.
// Проверяется только локальное создание данных без учётных данных, файлов и запуска нагрузок.
func TestTurnInputPreservesBindingsAndIndependentWorkspace(t *testing.T) {
	t.Parallel()
	first, second := TurnInput(), TurnInput()
	if !reflect.DeepEqual(first, second) {
		t.Fatal("runtime input fixtures differ between calls")
	}
	inputDigest, err := runtimecontract.RuntimeBoundedInputDigest(first.BoundedInput)
	if err != nil || inputDigest != first.InputDigest {
		t.Fatal("fixture bounded input digest differs")
	}
	environmentDigest, err := runtimecontract.RuntimeEnvironmentDigest(nil, nil, first.EnvironmentImage, nil, first.EnvironmentPolicy)
	if err != nil || environmentDigest != first.RuntimeEnvironmentDigest {
		t.Fatal("fixture environment digest differs")
	}
	executionDigest, mcpDigest, err := runtimecontract.RuntimeExecutionBindingDigests(first)
	if err != nil || executionDigest != first.ExecutionBindingDigest || mcpDigest != first.MCPBindingDigest {
		t.Fatal("fixture execution binding digests differ")
	}
	if err := first.WorkspacePolicy.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(first.WorkspacePolicy.Rules) == 0 || len(first.WorkspacePolicy.DenialReasons) == 0 {
		t.Fatal("fixture workspace policy is empty")
	}
	first.WorkspacePolicy.Rules[0].Path = "/changed"
	first.WorkspacePolicy.DenialReasons[0] = "changed"
	if !reflect.DeepEqual(second, TurnInput()) {
		t.Fatal("fixture workspace policy aliases another call")
	}
}
