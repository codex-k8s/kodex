package httptransport

import (
	"bytes"
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// personalCredentialInvalidRequestCode is the existing public validation problem code.
// personalCredentialInvalidRequestCode — существующий публичный код ошибки проверки запроса.
const personalCredentialInvalidRequestCode = "INVALID_REQUEST"

// TestPersonalIntegrationCredential queues only an owner-selected READ health operation, using
// independent binding and Connection OCC. The browser cannot supply probes, URLs or another user.
// TestPersonalIntegrationCredential ставит только выбранную владельцем READ health operation в очередь
// с независимыми binding и Connection OCC. Browser не задаёт probes, URLs либо другого пользователя.
func (server *Server) TestPersonalIntegrationCredential(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, params generated.TestPersonalIntegrationCredentialParams) {
	body, mutation, ok := decodeMutationBody[generated.PersonalIntegrationCredentialCreateInput](w, r, params.IdempotencyKey, params.IfMatch)
	if !ok {
		return
	}
	if body.ExpectedConnectionVersion < 1 {
		writeLocalProblem(w, http.StatusBadRequest, personalCredentialInvalidRequestCode, false)
		return
	}
	response, err := server.control.PersonalCredentials.TestPersonalIntegrationCredential(r.Context(), &controlplanev1.TestPersonalIntegrationCredentialRequest{
		ConnectionRef: ref, Mutation: mutation, ExpectedConnectionVersion: body.ExpectedConnectionVersion})
	writeRPCResult(w, http.StatusOK, response, err, "credential", "")
}

// ListPersonalIntegrationConnections accepts only bounded search and continuation parameters;
// the owner derives actor eligibility and never reads another user's binding or shared key.
// ListPersonalIntegrationConnections принимает только bounded search и continuation parameters;
// владелец определяет допуск actor и не читает чужую привязку либо общий ключ.
func (server *Server) ListPersonalIntegrationConnections(w http.ResponseWriter, r *http.Request, params generated.ListPersonalIntegrationConnectionsParams) {
	for key, values := range r.URL.Query() {
		if (key != "query" && key != "pageSize" && key != "pageToken") || len(values) != 1 {
			writeLocalProblem(w, http.StatusBadRequest, personalCredentialInvalidRequestCode, false)
			return
		}
	}
	if params.Query != nil && !validSearchText(*params.Query, 0, 200) || params.PageSize != nil && (*params.PageSize < 1 || *params.PageSize > 100) || params.PageToken != nil && len(*params.PageToken) > 512 {
		writeLocalProblem(w, http.StatusBadRequest, personalCredentialInvalidRequestCode, false)
		return
	}
	response, err := server.control.PersonalCredentials.ListPersonalIntegrationConnections(r.Context(), &controlplanev1.ListPersonalIntegrationConnectionsRequest{
		Page: page(params.PageSize, params.PageToken), Query: stringValue(params.Query)})
	writeReadRPCResult(w, response, err, "", "connections")
}

// SetIntegrationCredentialPolicy maps one closed configuration enum to its dedicated owner RPC;
// self-service credential rights do not replace Connection management authorization.
// SetIntegrationCredentialPolicy преобразует закрытый enum настройки в выделенный owner RPC;
// права на собственный ключ не заменяют полномочий управления Connection.
func (server *Server) SetIntegrationCredentialPolicy(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, params generated.SetIntegrationCredentialPolicyParams) {
	body, mutation, ok := decodeMutationBody[generated.IntegrationCredentialPolicyInput](w, r, params.IdempotencyKey, params.IfMatch)
	if !ok {
		return
	}
	policy := controlplanev1.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_UNSPECIFIED
	switch string(body.Policy) {
	case "PROJECT_SHARED":
		policy = controlplanev1.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_PROJECT_SHARED
	case "INITIATOR_REQUIRED":
		policy = controlplanev1.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_INITIATOR_REQUIRED
	default:
		writeLocalProblem(w, http.StatusBadRequest, personalCredentialInvalidRequestCode, false)
		return
	}
	response, err := server.control.PersonalCredentials.SetIntegrationCredentialPolicy(r.Context(), &controlplanev1.SetIntegrationCredentialPolicyRequest{
		ConnectionRef: ref, Mutation: mutation, Policy: policy})
	writeRPCResult(w, http.StatusOK, response, err, "connection", "")
}

// GetPersonalIntegrationCredential reads only the current session's binding through the dedicated
// owner RPC; no subject, credential descriptor or Authorization override is supplied by the browser.
// GetPersonalIntegrationCredential читает только привязку текущей сессии через выделенный owner RPC;
// browser не передаёт subject, credential descriptor либо Authorization override.
func (server *Server) GetPersonalIntegrationCredential(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef) {
	response, err := server.control.PersonalCredentials.GetPersonalIntegrationCredential(r.Context(), &controlplanev1.GetPersonalIntegrationCredentialRequest{ConnectionRef: ref})
	writeReadRPCResult(w, response, err, "credential", "")
}

// CreatePersonalIntegrationCredential creates metadata only, with exact Connection OCC and an
// idempotent self-scoped command. It neither copies a shared key nor submits credential material.
// CreatePersonalIntegrationCredential создаёт только metadata с exact Connection OCC и
// идемпотентной собственной командой. Общий ключ не копируется, credential material не передаётся.
func (server *Server) CreatePersonalIntegrationCredential(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, params generated.CreatePersonalIntegrationCredentialParams) {
	body, mutation, ok := decodeMutationBody[generated.PersonalIntegrationCredentialCreateInput](w, r, params.IdempotencyKey, "")
	if !ok {
		return
	}
	if body.ExpectedConnectionVersion < 1 {
		writeLocalProblem(w, http.StatusBadRequest, personalCredentialInvalidRequestCode, false)
		return
	}
	response, err := server.control.PersonalCredentials.CreatePersonalIntegrationCredential(r.Context(), &controlplanev1.CreatePersonalIntegrationCredentialRequest{
		ConnectionRef: ref, Mutation: mutation, ExpectedConnectionVersion: body.ExpectedConnectionVersion})
	writeRPCResult(w, http.StatusCreated, response, err, "credential", "")
}

// ConfigurePersonalIntegrationCredential sends the write-only value only to the dedicated owner
// materializer, clearing mutable copies and the decoded string reference after use. This is best
// effort memory hygiene and does not promise erasure of all immutable decoder/transport copies.
// ConfigurePersonalIntegrationCredential передаёт write-only value только выделенному owner
// materializer, очищая mutable copies и decoded string reference после использования. Это best
// effort гигиена памяти без обещания стереть все immutable decoder/transport copies.
func (server *Server) ConfigurePersonalIntegrationCredential(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, params generated.ConfigurePersonalIntegrationCredentialParams) {
	body, mutation, ok := decodeMutationBody[generated.PersonalIntegrationCredentialConfigureInput](w, r, params.IdempotencyKey, params.IfMatch)
	if !ok {
		return
	}
	credential := []byte(stringValue(body.Value))
	body.Value = nil
	defer clear(credential)
	if body.ExpectedConnectionVersion < 1 || len(credential) > 16<<10 || len(bytes.TrimSpace(credential)) == 0 {
		writeLocalProblem(w, http.StatusBadRequest, personalCredentialInvalidRequestCode, false)
		return
	}
	response, err := server.control.PersonalCredentials.ConfigurePersonalIntegrationCredential(r.Context(), &controlplanev1.ConfigurePersonalIntegrationCredentialRequest{
		ConnectionRef: ref, Mutation: mutation, ExpectedConnectionVersion: body.ExpectedConnectionVersion, CredentialValue: credential})
	writeRPCResult(w, http.StatusOK, response, err, "credential", "")
}

// RevokePersonalIntegrationCredential accepts an empty typed body and binding OCC only; selecting
// another user, a Secret or a generic action through extra fields is rejected before any RPC.
// RevokePersonalIntegrationCredential принимает только пустое typed body и binding OCC; выбор
// другого пользователя, Secret либо generic action дополнительными полями отвергается до RPC.
func (server *Server) RevokePersonalIntegrationCredential(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, params generated.RevokePersonalIntegrationCredentialParams) {
	body, mutation, ok := decodeMutationBody[generated.PersonalIntegrationCredentialRevokeInput](w, r, params.IdempotencyKey, params.IfMatch)
	if !ok {
		return
	}
	if len(body) != 0 {
		writeLocalProblem(w, http.StatusBadRequest, personalCredentialInvalidRequestCode, false)
		return
	}
	response, err := server.control.PersonalCredentials.RevokePersonalIntegrationCredential(r.Context(), &controlplanev1.RevokePersonalIntegrationCredentialRequest{ConnectionRef: ref, Mutation: mutation})
	writeRPCResult(w, http.StatusOK, response, err, "credential", "")
}
