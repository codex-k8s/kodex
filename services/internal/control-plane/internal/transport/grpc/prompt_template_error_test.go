package grpc

import (
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestPromptTemplateErrorCarriesOnlyTypedDiagnostics checks invalid templates retain the fixed
// InvalidArgument message and one typed detail with exact diagnostic code, variable and source
// position.
// TestPromptTemplateErrorCarriesOnlyTypedDiagnostics проверяет фиксированные InvalidArgument
// message и единственный typed detail неверного шаблона с точными кодом, переменной и позицией
// диагностики.
func TestPromptTemplateErrorCarriesOnlyTypedDiagnostics(t *testing.T) {
	err := promptTemplateError([]promptservice.Diagnostic{{
		Severity: "ERROR", Code: "PROMPT_TEMPLATE_VARIABLE_UNKNOWN",
		Message: "Prompt template contains an unknown variable",
		Line:    2, Column: 4, VariableName: "unknown.value",
	}})
	mapped := status.Convert(err)
	if mapped.Code() != codes.InvalidArgument || mapped.Message() != "prompt template is invalid" || len(mapped.Details()) != 1 {
		t.Fatalf("status = code=%s message=%q details=%d", mapped.Code(), mapped.Message(), len(mapped.Details()))
	}
	detail, ok := mapped.Details()[0].(*controlplanev1.PromptTemplateErrorDetail)
	if !ok || len(detail.GetDiagnostics()) != 1 {
		t.Fatalf("detail = %#v", mapped.Details())
	}
	diagnostic := detail.GetDiagnostics()[0]
	if diagnostic.GetVariableName() != "unknown.value" || diagnostic.GetLine() != 2 || diagnostic.GetColumn() != 4 || diagnostic.GetCode() != "PROMPT_TEMPLATE_VARIABLE_UNKNOWN" {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
}

// TestPromptDiagnosticsHaveError checks absent or warning-only diagnostics are not failures while
// a collection containing an ERROR is recognized.
// TestPromptDiagnosticsHaveError проверяет отсутствие отказа у отсутствующей либо только
// warning-диагностики и распознавание набора с ERROR.
func TestPromptDiagnosticsHaveError(t *testing.T) {
	if promptDiagnosticsHaveError(nil) || promptDiagnosticsHaveError([]promptservice.Diagnostic{{Severity: "WARNING"}}) {
		t.Fatal("non-error diagnostics were accepted as a prompt failure")
	}
	if !promptDiagnosticsHaveError([]promptservice.Diagnostic{{Severity: "WARNING"}, {Severity: "ERROR", Code: "CAPABILITY_REQUIRED"}}) {
		t.Fatal("error diagnostic was not detected")
	}
}
