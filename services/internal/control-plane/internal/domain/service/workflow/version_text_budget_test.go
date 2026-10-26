package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestServerTextBudgetUTF8Boundary fixes the existing aggregate byte limit across all counted fields; input labels and descriptions do not consume this particular budget.
// TestServerTextBudgetUTF8Boundary закрепляет прежний суммарный байтовый предел по всем учитываемым полям; подписи и описания входов не расходуют именно этот бюджет.
func TestServerTextBudgetUTF8Boundary(t *testing.T) {
	for _, size := range []int{8192, 8193} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			version := serverVersionFixture()
			version.Name, version.Purpose, version.CompletionCriteria = "План", "Цель", "Итог"
			used := len(version.Name) + len(version.Purpose) + len(version.CompletionCriteria)
			for i := range version.Steps {
				step := &version.Steps[i]
				step.Name, step.Instructions, step.ExpectedResult = "Шаг", "Работа", "Результат"
				used += len(step.Name) + len(step.Instructions) + len(step.ExpectedResult)
			}
			remaining := size - used
			version.Instructions = strings.Repeat("я", remaining/2) + strings.Repeat("x", remaining%2)
			version.Inputs = []entity.WorkflowInputField{{Key: "input", Label: "Поле", Help: strings.Repeat("я", 500), Type: "TEXT"}}
			if ValidServerVersion(version) != (size == 8192) {
				t.Fatal("server aggregate UTF-8 byte boundary changed")
			}
		})
	}
}
