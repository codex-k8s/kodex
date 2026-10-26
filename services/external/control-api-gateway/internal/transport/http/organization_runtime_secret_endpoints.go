package httptransport

import (
	"crypto/sha256"
	"net/http"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// ListSystemRuntimeSecrets reads organization secret metadata and rejects nil, project-scoped or
// malformed scope entries before using the common secret-page writer. Protective response headers
// apply, and secret values are never listed.
// ListSystemRuntimeSecrets читает metadata организационных секретов и отклоняет nil, проектные и
// неверные области до общего secret-page writer. Действуют защитные заголовки; значения секретов
// не перечисляются.
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

// CreateSystemRuntimeSecretDraft decodes a typed secret value into a transient buffer, prepares an
// organization-scoped owner operation pinned to its exact SHA-256 and saves through the existing
// prepared-operation path. The buffer is erased on return; creation is not publication or runtime
// secret activation.
// CreateSystemRuntimeSecretDraft декодирует typed значение секрета во временный буфер, готовит
// организационную операцию владельца с точным SHA-256 и сохраняет существующим prepared-operation
// путём. Буфер очищается при возврате; создание не является публикацией и runtime активацией
// секрета.
func (s *Server) CreateSystemRuntimeSecretDraft(w http.ResponseWriter, r *http.Request, p generated.CreateSystemRuntimeSecretDraftParams) {
	setRuntimeSecretHeaders(w)
	body, ok := decodeJSON[generated.RuntimeSecretCreateInput](w, r)
	if !ok {
		return
	}
	if purpose := runtimeSecretCreatePurpose(body); purpose != "" && purpose != "APPLICATION" {
		invalidSecretDraft(w)
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
