package workload

import (
	"testing"

	"github.com/google/cel-go/cel"
	admissionv1 "k8s.io/api/admissionregistration/v1"
)

// runtimeAdmissionActivation evaluates the real ordered policy variables against the generated Pod, preventing hand-built fixtures from omitting new admission dependencies.
// It supplies test values only and neither installs a policy nor proves cluster admission or execution authority.
// runtimeAdmissionActivation вычисляет actual ordered policy variables по generated Pod, исключая пропуск новых admission dependencies в ручной fixture.
// Функция задаёт только test values; она не устанавливает policy и не доказывает cluster admission либо execution authority.
func runtimeAdmissionActivation(t *testing.T, environment *cel.Env, declarations []admissionv1.Variable, object map[string]any) map[string]any {
	t.Helper()
	if len(declarations) == 0 {
		t.Fatal("runtime admission variables are absent")
	}
	variables := make(map[string]any, len(declarations))
	activation := map[string]any{"object": object, "variables": variables}
	for _, declaration := range declarations {
		if _, duplicate := variables[declaration.Name]; duplicate || declaration.Name == "" {
			t.Fatal("runtime admission variable name is invalid")
		}
		ast, issues := environment.Compile(declaration.Expression)
		if issues != nil && issues.Err() != nil {
			t.Fatal(issues.Err())
		}
		program, err := environment.Program(ast, cel.CostLimit(100000))
		if err != nil {
			t.Fatal(err)
		}
		result, _, err := program.Eval(activation)
		if err != nil || result == nil {
			t.Fatalf("runtime admission variable %s could not be evaluated: %v", declaration.Name, err)
		}
		variables[declaration.Name] = result
	}
	return activation
}
