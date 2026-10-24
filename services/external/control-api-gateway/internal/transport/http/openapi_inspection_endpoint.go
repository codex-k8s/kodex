package httptransport

import (
	"net/http"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// InspectOpenAPIIntegration performs bounded local parsing of supplied OpenAPI source and checks
// public inspection field sizes before returning candidate operations. Inspection neither saves an
// executable definition nor grants admission, network access or integration permissions; those
// belong to CP.
// InspectOpenAPIIntegration выполняет ограниченный локальный разбор переданного OpenAPI и
// проверяет размеры публичных полей inspection до выдачи операций-кандидатов. Inspection не
// сохраняет исполняемое определение и не даёт admission, network access или прав интеграции; ими
// владеет CP.
func (server *Server) InspectOpenAPIIntegration(w http.ResponseWriter, r *http.Request, _ generated.InspectOpenAPIIntegrationParams) {
	body, ok := decodeJSON[generated.OpenAPIInspectionInput](w, r)
	if !ok {
		return
	}
	if len(body.Source) == 0 || len(body.Source) > 128<<10 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	inspection, err := integrationpackage.InspectOpenAPI(r.Context(), []byte(body.Source))
	if err != nil || len(inspection.Title) > 4096 || len(inspection.Version) > 128 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	result := generated.OpenAPIInspectionResult{
		Digest: inspection.Digest, Title: inspection.Title, Version: inspection.Version,
		Operations: make([]generated.OpenAPIInspectionOperation, 0, len(inspection.Operations)),
	}
	for _, operation := range inspection.Operations {
		if len(operation.ID) > 128 || len(operation.Method) > 16 || len(operation.Path) > 2048 ||
			len(operation.Summary) > 4096 || len(operation.ServerOrigin) > 2048 || len(operation.Reason) > 128 {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		result.Operations = append(result.Operations, generated.OpenAPIInspectionOperation{
			OperationId: operation.ID, Method: operation.Method, Path: operation.Path, Summary: operation.Summary,
			ServerOrigin: operation.ServerOrigin, Candidate: operation.Candidate, HealthCandidate: operation.HealthCandidate, Reason: operation.Reason,
		})
	}
	writeJSON(w, http.StatusOK, result)
}
