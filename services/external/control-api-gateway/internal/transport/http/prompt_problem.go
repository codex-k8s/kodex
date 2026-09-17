package httptransport

import (
	"encoding/json"
	"net/http"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const promptTemplateInvalid = "PROMPT_TEMPLATE_INVALID"

func writePromptProblem(writer http.ResponseWriter, err error) {
	upstream := status.Convert(err)
	if upstream.Code() != codes.InvalidArgument {
		writeRPCProblem(writer, err)
		return
	}
	details := upstream.Details()
	if len(details) == 0 {
		writeRPCProblem(writer, err)
		return
	}
	if len(details) != 1 {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	detail, ok := details[0].(*cp.PromptTemplateErrorDetail)
	if !ok {
		writeRPCProblem(writer, err)
		return
	}
	diagnostics, valid := promptDiagnosticViews(detail.GetDiagnostics())
	if !valid || len(diagnostics) == 0 || !promptDiagnosticsContainError(detail.GetDiagnostics()) {
		writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	for _, diagnostic := range diagnostics {
		item := diagnostic.(map[string]any)
		item["message"] = "i18n:" + item["code"].(string)
	}
	title := http.StatusText(http.StatusBadRequest)
	value := map[string]any{
		"type":          "urn:kodex:problem:prompt_template_invalid",
		"title":         title,
		"status":        http.StatusBadRequest,
		"code":          promptTemplateInvalid,
		"correlationId": uuid.NewString(),
		"retryable":     false,
		"diagnostics":   diagnostics,
	}
	if localizer, ok := writer.(interface{ Localize(string) string }); ok {
		value["title"] = localizer.Localize(promptTemplateInvalid)
		LocalizeSafeErrors(value, localizer.Localize)
	}
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(writer).Encode(value)
}

func promptDiagnosticsContainError(values []*cp.PromptTemplateDiagnostic) bool {
	for _, value := range values {
		if value.GetSeverity() == "ERROR" {
			return true
		}
	}
	return false
}
