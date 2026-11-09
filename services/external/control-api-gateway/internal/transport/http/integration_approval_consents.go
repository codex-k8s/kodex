package httptransport

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"net/http"
)

// ListIntegrationApprovalConsents accepts only bounded pagination; actor and tenant are derived by the owner from authenticated transport, never from query fields.
// ListIntegrationApprovalConsents принимает лишь bounded pagination; actor и tenant выводятся owner из authenticated transport, не из query fields.
func (server *Server) ListIntegrationApprovalConsents(w http.ResponseWriter, r *http.Request, params generated.ListIntegrationApprovalConsentsParams) {
	for key, values := range r.URL.Query() {
		if (key != "pageSize" && key != "pageToken") || len(values) != 1 {
			writeLocalProblem(w, http.StatusBadRequest, personalCredentialInvalidRequestCode, false)
			return
		}
	}
	if params.PageSize != nil && (*params.PageSize < 1 || *params.PageSize > 100) || params.PageToken != nil && len(*params.PageToken) > 512 {
		writeLocalProblem(w, http.StatusBadRequest, personalCredentialInvalidRequestCode, false)
		return
	}
	response, err := server.control.Query.ListIntegrationApprovalConsents(r.Context(), &cp.ListIntegrationApprovalConsentsRequest{Page: page(params.PageSize, params.PageToken)})
	writeReadRPCResult(w, response, err, "", "consents")
}

// RevokeIntegrationApprovalConsent forwards an opaque self-resource ref with OCC/idempotency only; a guessed ref cannot select a different user.
// RevokeIntegrationApprovalConsent передаёт только opaque self-resource ref с OCC/idempotency; guessed ref не выбирает другого пользователя.
func (server *Server) RevokeIntegrationApprovalConsent(w http.ResponseWriter, r *http.Request, ref string, params generated.RevokeIntegrationApprovalConsentParams) {
	body, mutation, ok := decodeMutationBody[generated.PersonalIntegrationCredentialRevokeInput](w, r, params.IdempotencyKey, params.IfMatch)
	if !ok {
		return
	}
	if len(body) != 0 {
		writeLocalProblem(w, http.StatusBadRequest, personalCredentialInvalidRequestCode, false)
		return
	}
	response, err := server.control.Command.RevokeIntegrationApprovalConsent(r.Context(), &cp.RevokeIntegrationApprovalConsentRequest{ConsentRef: ref, Mutation: mutation})
	writeRPCResult(w, http.StatusOK, response, err, "consent", "")
}
