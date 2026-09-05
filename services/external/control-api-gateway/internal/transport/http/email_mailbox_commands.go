package httptransport

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"net/http"
)

func (s *Server) CreateEmailMailboxDraft(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.CreateEmailMailboxDraftParams) {
	setRuntimeSecretHeaders(w)
	body, ok := decodeJSON[generated.EmailMailboxDraftInput](w, r)
	if !ok {
		return
	}
	if (body.ConfigurationRef == nil) != (p.IfMatch == nil) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, stringValue(p.IfMatch))
	if !ok {
		return
	}
	content, ok := mailboxContentInput(body.Content)
	if !ok {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Command.CreateEmailMailboxDraft(r.Context(), &cp.CreateEmailMailboxDraftRequest{Mutation: mutation, ConnectionRef: ref, ConfigurationRef: stringValue(body.ConfigurationRef), Name: body.Name, Content: content})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMailboxConfiguration(w, 201, response.GetConfiguration(), ref, stringValue(body.ConfigurationRef), "")
}

func (s *Server) SaveEmailMailboxDraft(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, revision generated.ConfigurationRevisionRef, p generated.SaveEmailMailboxDraftParams) {
	setRuntimeSecretHeaders(w)
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.EmailMailboxDraftContent](w, r)
	if !ok {
		return
	}
	content, ok := mailboxContentInput(body)
	if !ok {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Command.SaveEmailMailboxDraft(r.Context(), &cp.SaveEmailMailboxDraftRequest{Mutation: mutation, ConfigurationRef: ref, RevisionRef: revision, Content: content})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMailboxConfiguration(w, 200, response.GetConfiguration(), "", ref, "")
}

func (s *Server) ValidateEmailMailboxDraft(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, revision generated.ConfigurationRevisionRef, p generated.ValidateEmailMailboxDraftParams) {
	setRuntimeSecretHeaders(w)
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := s.control.Command.ValidateEmailMailboxDraft(r.Context(), &cp.ValidateEmailMailboxDraftRequest{Mutation: mutation, ConfigurationRef: ref, RevisionRef: revision})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMailboxConfiguration(w, 200, response.GetConfiguration(), "", ref, revision)
}

func (s *Server) PublishEmailMailboxDraft(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, revision generated.ConfigurationRevisionRef, p generated.PublishEmailMailboxDraftParams) {
	setRuntimeSecretHeaders(w)
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := s.control.Command.PublishEmailMailboxDraft(r.Context(), &cp.PublishEmailMailboxDraftRequest{Mutation: mutation, ConfigurationRef: ref, RevisionRef: revision})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMailboxConfiguration(w, 200, response.GetConfiguration(), "", ref, revision)
}

func (s *Server) DiscardEmailMailboxDraft(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, revision generated.ConfigurationRevisionRef, p generated.DiscardEmailMailboxDraftParams) {
	setRuntimeSecretHeaders(w)
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := s.control.Command.DiscardEmailMailboxDraft(r.Context(), &cp.DiscardEmailMailboxDraftRequest{Mutation: mutation, ConfigurationRef: ref, RevisionRef: revision})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMailboxConfiguration(w, 200, response.GetConfiguration(), "", ref, revision)
}

func (s *Server) BindEmailMailboxConfiguration(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, revision generated.ConfigurationRevisionRef, p generated.BindEmailMailboxConfigurationParams) {
	setRuntimeSecretHeaders(w)
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.EmailMailboxBindingInput](w, r)
	if !ok {
		return
	}
	if !opaqueHTTPReference.MatchString(body.ConnectionRef) || !validManagedVersion(body.ExpectedConnectionVersion) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Command.BindEmailMailboxConfiguration(r.Context(), &cp.BindEmailMailboxConfigurationRequest{Mutation: mutation, ConfigurationRef: ref, RevisionRef: revision, ConnectionRef: body.ConnectionRef, ExpectedConnectionVersion: body.ExpectedConnectionVersion})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMailboxConfiguration(w, 200, response.GetConfiguration(), body.ConnectionRef, ref, revision)
}
func (s *Server) UnbindEmailMailboxConfiguration(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.UnbindEmailMailboxConfigurationParams) {
	setRuntimeSecretHeaders(w)
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := s.control.Command.UnbindEmailMailboxConfiguration(r.Context(), &cp.UnbindEmailMailboxConfigurationRequest{Mutation: mutation, ConnectionRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	publication, ok := mailboxPublicationView(response.GetPublication())
	if !ok || publication == nil || publication.ConfigurationRevisionRef != "" || !validManagedVersion(response.GetConnectionVersion()) {
		invalidSecretDraft(w)
		return
	}
	setVersionETag(w, uint64(response.GetConnectionVersion()))
	writeJSON(w, 200, generated.EmailMailboxUnbinding{Publication: *publication, ConnectionVersion: response.GetConnectionVersion()})
}
