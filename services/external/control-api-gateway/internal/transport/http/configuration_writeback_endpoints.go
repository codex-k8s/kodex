package httptransport

import (
	"fmt"
	"net/http"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// writeBackPrepareInput decodes proposed content, validates the configuration locator and expected
// source version/content bounds, then parses mutation headers. It prepares transport input without
// authorizing a repository write.
// writeBackPrepareInput разбирает предложенное содержимое, проверяет locator конфигурации,
// ожидаемую версию источника и ограничения содержимого, затем mutation заголовки. Готовится
// transport вход без авторизации записи в репозиторий.
func writeBackPrepareInput(w http.ResponseWriter, r *http.Request, ref, key, etag string) (generated.PrepareConfigurationWriteBackInput, *cp.MutationContext, bool) {
	body, ok := decodeJSON[generated.PrepareConfigurationWriteBackInput](w, r)
	if !ok {
		return body, nil, false
	}
	if !fileTargetRef(ref) || !validManagedVersion(body.ExpectedSourceVersion) || !validWriteBackContent(body.Content, false) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return body, nil, false
	}
	mutation, ok := requireMutation(w, key, etag)
	return body, mutation, ok
}

// PrepareRoleImageGitWriteBack forwards a validated role-image configuration proposal with its
// exact expected source version and content, then checks the returned proposal binding before
// emitting 201. No Git push or approval is performed here.
// PrepareRoleImageGitWriteBack передаёт проверенное предложение конфигурации role image с точной
// ожидаемой версией источника и содержимым, затем сверяет привязку proposal до 201. Git push и
// approve здесь не выполняются.
func (s *Server) PrepareRoleImageGitWriteBack(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.PrepareRoleImageGitWriteBackParams) {
	body, mutation, ok := writeBackPrepareInput(w, r, ref, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := s.control.Command.PrepareRoleImageGitWriteBack(r.Context(), &cp.PrepareRoleImageGitWriteBackRequest{Mutation: mutation, ConfigurationRef: ref, ExpectedSourceVersion: body.ExpectedSourceVersion, Content: body.Content})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writePreparedWriteBack(w, response.GetProposal(), ref, "ROLE_IMAGE", mutation.GetExpectedVersion(), body)
}

// PrepareIntegrationDefinitionGitWriteBack uses the same bounded proposal input but calls the
// dedicated integration-definition owner command, preserving distinct configuration kind and
// source/version pins in the returned proposal.
// PrepareIntegrationDefinitionGitWriteBack использует общий ограниченный вход proposal, но
// вызывает отдельную команду владельца integration definition, сохраняя отдельный вид конфигурации
// и source/version pins возвращённого предложения.
func (s *Server) PrepareIntegrationDefinitionGitWriteBack(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.PrepareIntegrationDefinitionGitWriteBackParams) {
	body, mutation, ok := writeBackPrepareInput(w, r, ref, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := s.control.Command.PrepareIntegrationDefinitionGitWriteBack(r.Context(), &cp.PrepareIntegrationDefinitionGitWriteBackRequest{Mutation: mutation, ConfigurationRef: ref, ExpectedSourceVersion: body.ExpectedSourceVersion, Content: body.Content})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writePreparedWriteBack(w, response.GetProposal(), ref, "INTEGRATION_DEFINITION", mutation.GetExpectedVersion(), body)
}

// writePreparedWriteBack requires proposal configuration/kind/version/source-version to match the
// request and proposed content SHA-256 to match exact bytes. Only this verified readback is
// emitted as a created proposal.
// writePreparedWriteBack требует совпадения configuration/kind/version/source-version proposal с
// запросом и SHA-256 предложенного содержимого с точными байтами. Только проверенный readback
// выдаётся как созданное предложение.
func writePreparedWriteBack(w http.ResponseWriter, v *cp.ManagedConfigurationGitWriteBack, ref, kind string, version int64, body generated.PrepareConfigurationWriteBackInput) {
	result, ok := configurationWriteBackView(v)
	if !ok || result.ConfigurationRef != ref || string(result.Kind) != kind || result.ConfigurationVersion != version || result.SourceVersion != body.ExpectedSourceVersion || result.ProposedContentSha256 != writeBackContentDigest(body.Content) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeWriteBack(w, http.StatusCreated, result)
}

// writeWriteBack writes an already validated public proposal under no-store with its authoritative
// version ETag and the caller-selected HTTP status. It does not mutate proposal state.
// writeWriteBack выдаёт уже проверенное публичное предложение под no-store с ETag авторитетной
// версии и HTTP статусом caller. Состояние предложения не меняется.
func writeWriteBack(w http.ResponseWriter, code int, value generated.ConfigurationWriteBack) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", value.Version))
	writeJSON(w, code, value)
}

// writeBackDecisionInput decodes the decision approval digest, validates its shape and proposal
// locator, then parses mutation headers. A digest identifies proposed content but does not grant
// the right to approve it.
// writeBackDecisionInput разбирает approval digest решения, проверяет его форму и locator
// proposal, затем mutation заголовки. Digest указывает предложенное содержимое, но не
// предоставляет право его одобрить.
func writeBackDecisionInput(w http.ResponseWriter, r *http.Request, ref, key, etag string) (string, *cp.MutationContext, bool) {
	body, ok := decodeJSON[generated.ConfigurationWriteBackDecisionInput](w, r)
	if !ok {
		return "", nil, false
	}
	if !fileTargetRef(ref) || !validManagedDigest(body.ApprovalDigest) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return "", nil, false
	}
	mutation, ok := requireMutation(w, key, etag)
	return body.ApprovalDigest, mutation, ok
}

// ApproveManagedConfigurationGitWriteBack submits the exact proposal and approval digest to the
// owner approval command, then requires a newer QUEUED readback with the same digest. Approval
// permission and scheduling belong to the owner.
// ApproveManagedConfigurationGitWriteBack передаёт точные proposal и approval digest команде
// одобрения владельца, затем требует более новый QUEUED readback с тем же digest. Право одобрения
// и планирование принадлежат владельцу.
func (s *Server) ApproveManagedConfigurationGitWriteBack(w http.ResponseWriter, r *http.Request, ref generated.WriteBackProposalRef, p generated.ApproveManagedConfigurationGitWriteBackParams) {
	digest, mutation, ok := writeBackDecisionInput(w, r, ref, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := s.control.Command.ApproveManagedConfigurationGitWriteBack(r.Context(), &cp.ApproveManagedConfigurationGitWriteBackRequest{Mutation: mutation, ProposalRef: ref, ApprovalDigest: digest})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeWriteBackDecision(w, response.GetProposal(), ref, digest, mutation.GetExpectedVersion(), "APPROVE")
}

// RejectManagedConfigurationGitWriteBack delegates rejection with the exact proposal/digest and
// mutation context, accepting only a newer REJECTED owner result before public output. It does not
// perform external repository effects.
// RejectManagedConfigurationGitWriteBack поручает отказ по точным proposal/digest и mutation
// context, принимая только более новый REJECTED результат владельца до выдачи. Внешние эффекты
// репозитория не выполняются.
func (s *Server) RejectManagedConfigurationGitWriteBack(w http.ResponseWriter, r *http.Request, ref generated.WriteBackProposalRef, p generated.RejectManagedConfigurationGitWriteBackParams) {
	digest, mutation, ok := writeBackDecisionInput(w, r, ref, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := s.control.Command.RejectManagedConfigurationGitWriteBack(r.Context(), &cp.RejectManagedConfigurationGitWriteBackRequest{Mutation: mutation, ProposalRef: ref, ApprovalDigest: digest})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeWriteBackDecision(w, response.GetProposal(), ref, digest, mutation.GetExpectedVersion(), "REJECT")
}

// CancelManagedConfigurationGitWriteBack delegates cancellation for a valid proposal under
// mutation headers. The owner may return CANCELLED or UNKNOWN_OUTCOME after an uncertain external
// effect; the gateway preserves that distinction rather than declaring rollback.
// CancelManagedConfigurationGitWriteBack поручает отмену корректного proposal с mutation
// заголовками. После неопределённого внешнего эффекта владелец может вернуть CANCELLED либо
// UNKNOWN_OUTCOME; gateway сохраняет различие и не объявляет rollback.
func (s *Server) CancelManagedConfigurationGitWriteBack(w http.ResponseWriter, r *http.Request, ref generated.WriteBackProposalRef, p generated.CancelManagedConfigurationGitWriteBackParams) {
	if !fileTargetRef(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := s.control.Command.CancelManagedConfigurationGitWriteBack(r.Context(), &cp.CancelManagedConfigurationGitWriteBackRequest{Mutation: mutation, ProposalRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeWriteBackDecision(w, response.GetProposal(), ref, "", mutation.GetExpectedVersion(), "CANCEL")
}

// writeWriteBackDecision checks proposal identity, strictly advanced version, optional matching
// digest and action-specific resulting state before writing the result. Cancel explicitly permits
// UNKNOWN_OUTCOME without retrying an effect.
// writeWriteBackDecision проверяет identity proposal, строго возросшую версию, необязательное
// совпадение digest и итоговое состояние по действию до выдачи. Cancel явно допускает
// UNKNOWN_OUTCOME без повтора эффекта.
func writeWriteBackDecision(w http.ResponseWriter, v *cp.ManagedConfigurationGitWriteBack, ref, digest string, version int64, action string) {
	result, ok := configurationWriteBackView(v)
	if !ok || result.Ref != ref || result.Version <= version || digest != "" && result.ApprovalDigest != digest || action == "APPROVE" && result.State != "QUEUED" || action == "REJECT" && result.State != "REJECTED" || action == "CANCEL" && result.State != "CANCELLED" && result.State != "UNKNOWN_OUTCOME" {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeWriteBack(w, http.StatusOK, result)
}

// GetManagedConfigurationGitWriteBack reads one owner proposal, validates exact identity plus
// bounded base/proposed contents and both byte digests, then returns content with the proposal
// version ETag under no-store.
// GetManagedConfigurationGitWriteBack читает одно предложение владельца, проверяет точную
// identity, ограничения base/proposed содержимого и оба байтовых digest, затем возвращает
// содержимое с ETag версии proposal под no-store.
func (s *Server) GetManagedConfigurationGitWriteBack(w http.ResponseWriter, r *http.Request, ref generated.WriteBackProposalRef) {
	if !fileTargetRef(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Query.GetManagedConfigurationGitWriteBack(r.Context(), &cp.GetManagedConfigurationGitWriteBackRequest{ProposalRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	proposal, ok := configurationWriteBackView(response.GetProposal())
	if !ok || proposal.Ref != ref || !validWriteBackContent(response.GetBaseContent(), true) || !validWriteBackContent(response.GetProposedContent(), false) || writeBackContentDigest(response.GetBaseContent()) != proposal.BaseContentSha256 || writeBackContentDigest(response.GetProposedContent()) != proposal.ProposedContentSha256 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", proposal.Version))
	writeJSON(w, http.StatusOK, generated.ConfigurationWriteBackView{Proposal: proposal, BaseContent: response.BaseContent, ProposedContent: response.ProposedContent})
}

// ListManagedConfigurationGitWriteBacks queries proposal history for one configuration with
// validated page controls and rejects malformed totals/cursors, foreign configuration refs or
// duplicate proposals before writing no-store JSON.
// ListManagedConfigurationGitWriteBacks запрашивает историю предложений одной конфигурации с
// проверенными параметрами страницы и отклоняет неверные total/cursor, чужие configuration refs и
// повторы proposal до no-store JSON.
func (s *Server) ListManagedConfigurationGitWriteBacks(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.ListManagedConfigurationGitWriteBacksParams) {
	if !fileTargetRef(ref) || !validHTTPPage(p.PageSize, p.PageToken) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := s.control.Query.ListManagedConfigurationGitWriteBacks(r.Context(), &cp.ListManagedConfigurationGitWriteBacksRequest{ConfigurationRef: ref, Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	next := response.GetPage().GetNextPageToken()
	if response == nil || response.Total < int64(len(response.Proposals)) || response.Total > maximumSafeJSONInteger || len(response.Proposals) > int(page(p.PageSize, p.PageToken).PageSize) || len(next) > 512 || !utf8.ValidString(next) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.ConfigurationWriteBackPage{Items: []generated.ConfigurationWriteBack{}, Total: response.Total, NextPageToken: optionalManagedString(next)}
	seen := map[string]bool{}
	for _, value := range response.Proposals {
		proposal, ok := configurationWriteBackView(value)
		if !ok || proposal.ConfigurationRef != ref || seen[proposal.Ref] {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[proposal.Ref] = true
		result.Items = append(result.Items, proposal)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}
