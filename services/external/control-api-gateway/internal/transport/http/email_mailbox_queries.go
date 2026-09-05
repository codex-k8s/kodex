package httptransport

import (
	"net/http"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

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

func (s *Server) GetEmailMailboxConfiguration(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.GetEmailMailboxConfigurationParams) {
	setRuntimeSecretHeaders(w)
	response, err := s.control.Query.GetEmailMailboxConfiguration(r.Context(), &cp.GetEmailMailboxConfigurationRequest{ConnectionRef: ref, ConfigurationRef: stringValue(p.ConfigurationRef), RevisionRef: stringValue(p.RevisionRef)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMailboxConfiguration(w, 200, response.GetConfiguration(), ref, stringValue(p.ConfigurationRef), stringValue(p.RevisionRef))
}

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
