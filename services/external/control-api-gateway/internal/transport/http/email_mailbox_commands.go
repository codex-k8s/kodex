package httptransport

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"net/http"
)

// CreateEmailMailboxDraft requires configuration-ref and If-Match presence to agree, decodes exactly one supported content representation, then creates an owner draft with idempotency. The checked no-store response pins the connection and optional configuration; it neither connects to mail nor publishes settings.
// CreateEmailMailboxDraft требует согласованное наличие configuration-ref и If-Match, декодирует ровно одно поддерживаемое представление content, затем создаёт owner draft с идемпотентностью. Проверенный no-store ответ закрепляет connection и необязательный configuration; почту не подключает и настройки не публикует.
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

// SaveEmailMailboxDraft requires a versioned mutation, converts draft content and invokes the dedicated save command with configuration/revision refs. The response pins configuration but does not require the old revision ref, since owner save may return a new draft revision.
// SaveEmailMailboxDraft требует versioned mutation, преобразует content и вызывает отдельный save с configuration/revision refs. Ответ закрепляет configuration, но не требует старый revision ref, поскольку owner save может вернуть новую ревизию черновика.
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

// ValidateEmailMailboxDraft forwards exact configuration/revision and mutation pins to owner validation, then requires the same configuration/revision in the checked no-store response. Validation does not publish mailbox delivery state or authorize an email send.
// ValidateEmailMailboxDraft передаёт точные configuration/revision и mutation pins owner validation, затем требует те же configuration/revision в проверенном no-store ответе. Проверка не публикует mailbox delivery state и не разрешает отправку письма.
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

// PublishEmailMailboxDraft uses the dedicated owner publication command with exact revision and versioned mutation, validating the returned configuration/revision before output. Publication acceptance is metadata, not proof that delivery is READY or an email was sent.
// PublishEmailMailboxDraft использует отдельную owner publication command с точной ревизией и versioned mutation, сверяет configuration/revision ответа до выдачи. Publication acceptance — metadata, не доказательство delivery READY и не отправка письма.
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

// DiscardEmailMailboxDraft invokes the dedicated discard command with exact revision and mutation context and validates the returned configuration/revision. It does not emulate a terminal state, delete mailbox content or unbind a connection locally.
// DiscardEmailMailboxDraft вызывает отдельную discard command с точной ревизией и mutation context и сверяет configuration/revision ответа. Terminal state не подделывает, mailbox content не удаляет и connection локально не отвязывает.
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

// BindEmailMailboxConfiguration requires a syntactic connection ref and safe expected connection version, then binds the exact owner configuration/revision through a versioned command. The response must match all selected refs; binding metadata does not bypass owner eligibility or send confirmation.
// BindEmailMailboxConfiguration требует формат connection ref и safe expected connection version, затем привязывает точные owner configuration/revision через versioned command. Ответ обязан совпасть со всеми выбранными refs; binding metadata не обходит owner eligibility и подтверждение отправки.
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

// UnbindEmailMailboxConfiguration uses the specialized versioned owner unbind, then requires a valid publication with no configuration revision and a safe returned connection version. It writes that version ETag and no-store metadata, not a fabricated empty publication or deleted mailbox.
// UnbindEmailMailboxConfiguration использует отдельный versioned owner unbind, затем требует допустимую publication без configuration revision и safe connection version ответа. Выдаёт её ETag и no-store metadata, не выдуманную пустую publication и не удалённый ящик.
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
