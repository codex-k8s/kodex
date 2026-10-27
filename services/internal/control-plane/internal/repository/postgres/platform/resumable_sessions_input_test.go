package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestContinuationWorkflowInputUsesExactInheritedValues accepts exact inherited required text
// input and rejects missing, null, wrong-typed, unknown-field or corrupt workflow input.
// TestContinuationWorkflowInputUsesExactInheritedValues принимает точное унаследованное
// обязательное текстовое поле и отклоняет отсутствующий, null, неверно типизированный, содержащий
// неизвестное поле либо повреждённый вход workflow.
func TestContinuationWorkflowInputUsesExactInheritedValues(t *testing.T) {
	fields := []entity.WorkflowInputField{{Key: "facts", Type: "TEXT", Required: true}}
	for _, sample := range []struct {
		raw   string
		valid bool
	}{
		{`{"facts":"Original facts"}`, true},
		{`{}`, false},
		{`null`, false},
		{`{"facts":1}`, false},
		{`{"facts":"Original facts","unknown":"extra"}`, false},
		{`{`, false},
	} {
		if validContinuationWorkflowInput(fields, []byte(sample.raw)) != sample.valid {
			t.Fatal("inherited input validation changed")
		}
	}
}
