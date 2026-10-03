package httptransport

import (
	"crypto/sha256"
	"net/http"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (s *Server) ListSystemRuntimeSecrets(w http.ResponseWriter, r *http.Request, p generated.ListSystemRuntimeSecretsParams) {
	setRuntimeSecretHeaders(w)
	response, err := s.control.Query.ListOrganizationRuntimeSecrets(r.Context(), &cp.ListOrganizationRuntimeSecretsRequest{Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil {
		invalidSecretDraft(w)
		return
	}
	for _, secret := range response.GetSecrets() {
		if secret == nil || secret.GetScopeKind() != cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION || !validRuntimeResourceScope(runtimeResourceScopeKind(secret.GetScopeKind().String()), secret.GetOrganizationRef(), secret.GetProjectRef()) {
			invalidSecretDraft(w)
			return
		}
	}
	writeRuntimeSecretPage(w, &cp.ListRuntimeSecretsResponse{Secrets: response.GetSecrets(), Page: response.GetPage()})
}

func (s *Server) CreateSystemRuntimeSecretDraft(w http.ResponseWriter, r *http.Request, p generated.CreateSystemRuntimeSecretDraftParams) {
	setRuntimeSecretHeaders(w)
	body, ok := decodeJSON[generated.RuntimeSecretCreateInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, "")
	if !ok {
		return
	}
	valueType := runtimeSecretValueType(string(body.ValueType))
	value, ok := decodeRuntimeSecretValue(w, valueType, body.Value)
	if !ok {
		return
	}
	defer erase(value)
	prepared, err := s.control.Command.PrepareOrganizationRuntimeSecretDraft(r.Context(), &cp.PrepareOrganizationRuntimeSecretDraftRequest{Mutation: mutation, Name: body.Name, Description: body.Description, ValueType: valueType, ExpectedContentSha256: runtimeSecretSHA256(sha256.Sum256(value))})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	s.savePreparedSecretDraft(w, r, prepared.GetOperation(), &cp.PrepareSaveRuntimeSecretDraftRequest{Name: body.Name, Description: body.Description, ValueType: valueType}, value, generated.RuntimeResourceScopeKindORGANIZATION)
}
