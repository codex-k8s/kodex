package httptransport

import (
	"net/http"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// ListEmailMailboxConfigurations queries owner-visible mailbox configuration history with bounded
// filters under secret-response headers, validates page totals/actions and rejects duplicate or
// foreign-connection entries before output.
// ListEmailMailboxConfigurations запрашивает видимую у владельца историю mailbox configuration с
// ограниченными фильтрами и защитными заголовками, проверяет total/actions и отклоняет дубликаты и
// чужие connection записи до выдачи.
func (s *Server) ListEmailMailboxConfigurations(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.ListEmailMailboxConfigurationsParams) {
	setRuntimeSecretHeaders(w)
	if !validHTTPPage(p.PageSize, p.PageToken) || !validSearchText(stringValue(p.Query), 0, 200) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Query.ListEmailMailboxConfigurations(r.Context(), &cp.ListEmailMailboxConfigurationsRequest{ConnectionRef: ref, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.GetItems()) > 100 || response.GetTotal() < int64(len(response.GetItems())) || response.GetTotal() > maximumSafeJSONInteger || len(response.GetPage().GetNextPageToken()) > 512 {
		invalidSecretDraft(w)
		return
	}
	actions, ok := mailboxActionViews(response.GetNextActions(), true)
	if !ok {
		invalidSecretDraft(w)
		return
	}
	result := generated.EmailMailboxConfigurationPage{Items: []generated.EmailMailboxConfigurationView{}, Total: response.GetTotal(), NextPageToken: response.GetPage().GetNextPageToken(), NextActions: actions}
	seen := map[string]bool{}
	for _, v := range response.GetItems() {
		item, ok := mailboxConfigurationView(v)
		if !ok || item.ConnectionRef != ref || seen[item.Configuration.Ref] {
			invalidSecretDraft(w)
			return
		}
		seen[item.Configuration.Ref] = true
		result.Items = append(result.Items, item)
	}
	writeJSON(w, 200, result)
}

// GetEmailMailboxConfiguration reads the requested connection/configuration/revision coordinates
// and uses the shared mailbox writer to bind the returned projection to them. Secret-response
// headers apply even on failure.
// GetEmailMailboxConfiguration читает запрошенные connection/configuration/revision координаты и
// общим mailbox writer связывает с ними возвращённую проекцию. Защитные заголовки ответа действуют
// и при ошибке.
func (s *Server) GetEmailMailboxConfiguration(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.GetEmailMailboxConfigurationParams) {
	setRuntimeSecretHeaders(w)
	response, err := s.control.Query.GetEmailMailboxConfiguration(r.Context(), &cp.GetEmailMailboxConfigurationRequest{ConnectionRef: ref, ConfigurationRef: stringValue(p.ConfigurationRef), RevisionRef: stringValue(p.RevisionRef)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMailboxConfiguration(w, 200, response.GetConfiguration(), ref, stringValue(p.ConfigurationRef), stringValue(p.RevisionRef))
}

// ListEmailMailboxCredentials validates optional credential kind and pagination, then lists
// metadata for the exact connection. Every item must match the connection/kind, and repeated
// name/generation identities reject the whole page; credential values are never returned.
// ListEmailMailboxCredentials проверяет необязательный вид credential и пагинацию, затем читает
// metadata точной connection. Каждый элемент соответствует connection/kind, а повтор
// name/generation отклоняет всю страницу; значения credentials не возвращаются.
func (s *Server) ListEmailMailboxCredentials(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.ListEmailMailboxCredentialsParams) {
	setRuntimeSecretHeaders(w)
	kind, ok := mailboxEnumInput(p.Kind, cp.EmailMailboxCredentialKind_value, "EMAIL_MAILBOX_CREDENTIAL_KIND_")
	if !ok || !validHTTPPage(p.PageSize, p.PageToken) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Query.ListEmailMailboxCredentials(r.Context(), &cp.ListEmailMailboxCredentialsRequest{ConnectionRef: ref, Kind: cp.EmailMailboxCredentialKind(kind), Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.GetItems()) > 100 || response.GetTotal() < int64(len(response.GetItems())) || response.GetTotal() > maximumSafeJSONInteger || len(response.GetPage().GetNextPageToken()) > 512 {
		invalidSecretDraft(w)
		return
	}
	result := generated.EmailMailboxCredentialPage{Items: []generated.EmailMailboxCredential{}, Total: response.GetTotal(), NextPageToken: response.GetPage().GetNextPageToken()}
	// credentialIdentity identifies one credential metadata generation by name plus generation, so a
	// reused name with a newer generation remains distinct while exact page duplicates are rejected.
	// credentialIdentity определяет одно поколение metadata credential по имени и generation, различая
	// повторное имя с новым поколением и отклоняя точные повторы страницы.
	type credentialIdentity struct {
		name       string
		generation int64
	}
	seen := map[credentialIdentity]bool{}
	for _, v := range response.GetItems() {
		item, ok := mailboxCredentialView(v, ref, cp.EmailMailboxCredentialKind(kind))
		identity := credentialIdentity{item.Name, item.Generation}
		if !ok || seen[identity] {
			invalidSecretDraft(w)
			return
		}
		seen[identity] = true
		result.Items = append(result.Items, item)
	}
	writeJSON(w, 200, result)
}

// GetEmailMailboxCredentialReceipt reads an authoritative credential-operation receipt by bounded
// idempotency key for the exact connection, validates metadata and returns its connection-version
// ETag without resending the credential write.
// GetEmailMailboxCredentialReceipt читает авторитетный receipt операции credential по
// ограниченному idempotency key точной connection, проверяет metadata и возвращает ETag версии
// connection без повторной записи credential.
func (s *Server) GetEmailMailboxCredentialReceipt(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.GetEmailMailboxCredentialReceiptParams) {
	setRuntimeSecretHeaders(w)
	if len(p.IdempotencyKey) < 1 || len(p.IdempotencyKey) > 128 {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Query.GetEmailMailboxCredentialReceipt(r.Context(), &cp.GetEmailMailboxCredentialReceiptRequest{ConnectionRef: ref, IdempotencyKey: p.IdempotencyKey})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	result, ok := mailboxCredentialView(response.GetCredential(), ref, 0)
	if !ok {
		invalidSecretDraft(w)
		return
	}
	setVersionETag(w, uint64(result.ConnectionVersion))
	writeJSON(w, 200, result)
}

// PreviewEmailMailboxConfiguration maps draft content into the owner read-only preview, then
// checks specification, canonical YAML size and validity/diagnostic consistency. Preview does not
// create a revision or configure live mailbox credentials.
// PreviewEmailMailboxConfiguration преобразует draft content в preview чтения владельца, затем
// проверяет specification, размер canonical YAML и согласованность valid/diagnostics. Preview не
// создаёт revision и не настраивает живые mailbox credentials.
func (s *Server) PreviewEmailMailboxConfiguration(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.PreviewEmailMailboxConfigurationParams) {
	setRuntimeSecretHeaders(w)
	body, ok := decodeJSON[generated.EmailMailboxDraftContent](w, r)
	if !ok {
		return
	}
	content, ok := mailboxContentInput(body)
	if !ok {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Query.PreviewEmailMailboxConfiguration(r.Context(), &cp.PreviewEmailMailboxConfigurationRequest{ConnectionRef: ref, Content: content})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	spec, ok := mailboxSpecificationView(response.GetSpecification())
	if !ok {
		invalidSecretDraft(w)
		return
	}
	diagnostics, ok := mailboxDiagnostics(response.GetDiagnostics())
	if !ok || response == nil || len(response.GetCanonicalYaml()) > 262144 || spec == nil && response.GetCanonicalYaml() != "" || response.GetValid() && (spec == nil || response.GetCanonicalYaml() == "" || len(diagnostics) != 0) || !response.GetValid() && len(diagnostics) == 0 {
		invalidSecretDraft(w)
		return
	}
	writeJSON(w, 200, generated.EmailMailboxPreview{Specification: spec, CanonicalYaml: response.GetCanonicalYaml(), Diagnostics: diagnostics, Valid: response.GetValid()})
}
