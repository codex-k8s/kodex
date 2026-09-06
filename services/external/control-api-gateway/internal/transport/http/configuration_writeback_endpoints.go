package httptransport

import (
	"fmt"
	"net/http"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

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
func writePreparedWriteBack(w http.ResponseWriter, v *cp.ManagedConfigurationGitWriteBack, ref, kind string, version int64, body generated.PrepareConfigurationWriteBackInput) {
	result, ok := configurationWriteBackView(v)
	if !ok || result.ConfigurationRef != ref || string(result.Kind) != kind || result.ConfigurationVersion != version || result.SourceVersion != body.ExpectedSourceVersion || result.ProposedContentSha256 != writeBackContentDigest(body.Content) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeWriteBack(w, http.StatusCreated, result)
}
func writeWriteBack(w http.ResponseWriter, code int, value generated.ConfigurationWriteBack) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", value.Version))
	writeJSON(w, code, value)
}

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
func writeWriteBackDecision(w http.ResponseWriter, v *cp.ManagedConfigurationGitWriteBack, ref, digest string, version int64, action string) {
	result, ok := configurationWriteBackView(v)
	if !ok || result.Ref != ref || result.Version <= version || digest != "" && result.ApprovalDigest != digest || action == "APPROVE" && result.State != "QUEUED" || action == "REJECT" && result.State != "REJECTED" || action == "CANCEL" && result.State != "CANCELLED" && result.State != "UNKNOWN_OUTCOME" {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeWriteBack(w, http.StatusOK, result)
}

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
