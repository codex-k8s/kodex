package httptransport

import (
	"crypto/sha256"
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (s *Server) GetRuntimeSecretDraft(w http.ResponseWriter, r *http.Request, ref generated.RuntimeSecretDraftRef) {
	setRuntimeSecretHeaders(w)
	response, err := s.control.Query.GetRuntimeSecretDraft(r.Context(), &cp.GetRuntimeSecretDraftRequest{DraftRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	draft, ok := runtimeSecretDraftView(response.GetDraft())
	if !ok || draft.Ref != ref {
		invalidSecretDraft(w)
		return
	}
	writeSecretDraft(w, http.StatusOK, draft, nil)
}

func (s *Server) CreateRuntimeSecretDraft(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.CreateRuntimeSecretDraftParams) {
	setRuntimeSecretHeaders(w)
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.RuntimeSecretCreateInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, "")
	if !ok {
		return
	}
	s.saveSecretDraft(w, r, &cp.PrepareSaveRuntimeSecretDraftRequest{Mutation: mutation, ProjectRef: ref, Name: body.Name, Description: body.Description, ValueType: runtimeSecretValueType(string(body.ValueType))}, body.Value)
}

func (s *Server) SaveRuntimeSecretDraft(w http.ResponseWriter, r *http.Request, ref generated.SecretRef, p generated.SaveRuntimeSecretDraftParams) {
	setRuntimeSecretHeaders(w)
	body, ok := decodeJSON[generated.RuntimeSecretRotateInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	s.saveSecretDraft(w, r, &cp.PrepareSaveRuntimeSecretDraftRequest{Mutation: mutation, SecretRef: ref, ValueType: runtimeSecretValueType(string(body.ValueType))}, body.Value)
}

func (s *Server) saveSecretDraft(w http.ResponseWriter, r *http.Request, input *cp.PrepareSaveRuntimeSecretDraftRequest, encoded string) {
	value, ok := decodeRuntimeSecretValue(w, input.ValueType, encoded)
	if !ok {
		return
	}
	defer erase(value)
	input.ExpectedContentSha256 = runtimeSecretSHA256(sha256.Sum256(value))
	prepared, err := s.control.Command.PrepareSaveRuntimeSecretDraft(r.Context(), input)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	op := prepared.GetOperation()
	draft, ok := runtimeSecretDraftView(op.GetDraft())
	if !ok || draft.ValueType != generated.RuntimeSecretValueType(runtimeSecretValueTypeName(input.ValueType)) || input.SecretRef != "" && draft.SecretRef != input.SecretRef || input.ProjectRef != "" && (draft.ProjectRef != input.ProjectRef || draft.Name != input.Name || draft.Description != input.Description) {
		invalidSecretDraft(w)
		return
	}
	if terminalSecretDraft(w, http.StatusCreated, op, "DRAFT") {
		return
	}
	if !s.secretDraftReady(w, r) {
		return
	}
	response, err := s.secretDrafts.SaveSecretDraft(r.Context(), &sb.SaveSecretDraftRequest{OperationGrant: op.GetOperationGrant(), Value: value})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	s.finishSecretDraft(w, http.StatusCreated, draft, response.GetDraft(), nil, "DRAFT")
}

func (s *Server) ValidateRuntimeSecretDraft(w http.ResponseWriter, r *http.Request, ref generated.RuntimeSecretDraftRef, p generated.ValidateRuntimeSecretDraftParams) {
	s.changeSecretDraft(w, r, ref, p.IdempotencyKey, p.IfMatch, "VALID", nil)
}
func (s *Server) DiscardRuntimeSecretDraft(w http.ResponseWriter, r *http.Request, ref generated.RuntimeSecretDraftRef, p generated.DiscardRuntimeSecretDraftParams) {
	s.changeSecretDraft(w, r, ref, p.IdempotencyKey, p.IfMatch, "DISCARDED", nil)
}
func (s *Server) PublishRuntimeSecretDraft(w http.ResponseWriter, r *http.Request, ref generated.RuntimeSecretDraftRef, p generated.PublishRuntimeSecretDraftParams) {
	setRuntimeSecretHeaders(w)
	body, ok := decodeJSON[generated.RuntimeSecretDraftPublishInput](w, r)
	if !ok {
		return
	}
	if !validManagedVersion(body.ExpectedSecretVersion) || !opaqueHTTPReference.MatchString(body.ImpactPlanRef) || len(body.ImpactPlanRef) > 96 || body.SelectedItemRefs == nil || len(body.SelectedItemRefs) > 1000 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	seen := map[string]bool{}
	for _, item := range body.SelectedItemRefs {
		if !opaqueHTTPReference.MatchString(item) || len(item) > 96 || seen[item] {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		seen[item] = true
	}
	s.changeSecretDraft(w, r, ref, p.IdempotencyKey, p.IfMatch, "PUBLISHED", &body)
}

func (s *Server) changeSecretDraft(w http.ResponseWriter, r *http.Request, ref, key, match, target string, publication *generated.RuntimeSecretDraftPublishInput) {
	setRuntimeSecretHeaders(w)
	mutation, ok := requireMutation(w, key, match)
	if !ok {
		return
	}
	var op *cp.RuntimeSecretDraftOperationReceipt
	var err error
	switch target {
	case "VALID":
		var response *cp.PrepareValidateRuntimeSecretDraftResponse
		response, err = s.control.Command.PrepareValidateRuntimeSecretDraft(r.Context(), &cp.PrepareValidateRuntimeSecretDraftRequest{Mutation: mutation, DraftRef: ref})
		op = response.GetOperation()
	case "PUBLISHED":
		var response *cp.PreparePublishRuntimeSecretDraftResponse
		response, err = s.control.Command.PreparePublishRuntimeSecretDraft(r.Context(), &cp.PreparePublishRuntimeSecretDraftRequest{Mutation: mutation, DraftRef: ref, ExpectedSecretVersion: publication.ExpectedSecretVersion, ImpactPlanRef: publication.ImpactPlanRef, SelectedItemRefs: append([]string{}, publication.SelectedItemRefs...)})
		op = response.GetOperation()
	case "DISCARDED":
		var response *cp.PrepareDiscardRuntimeSecretDraftResponse
		response, err = s.control.Command.PrepareDiscardRuntimeSecretDraft(r.Context(), &cp.PrepareDiscardRuntimeSecretDraftRequest{Mutation: mutation, DraftRef: ref})
		op = response.GetOperation()
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	draft, ok := runtimeSecretDraftView(op.GetDraft())
	if !ok || draft.Ref != ref {
		invalidSecretDraft(w)
		return
	}
	if terminalSecretDraft(w, http.StatusOK, op, target) {
		return
	}
	if target == "PUBLISHED" && draft.SecretVersion != publication.ExpectedSecretVersion {
		invalidSecretDraft(w)
		return
	}
	if !s.secretDraftReady(w, r) {
		return
	}
	var result *sb.RuntimeSecretDraftMetadata
	var secret *generated.RuntimeSecret
	switch target {
	case "VALID":
		var response *sb.ValidateSecretDraftResponse
		response, err = s.secretDrafts.ValidateSecretDraft(r.Context(), &sb.ValidateSecretDraftRequest{OperationGrant: op.GetOperationGrant()})
		result = response.GetDraft()
	case "PUBLISHED":
		var response *sb.PublishSecretDraftResponse
		response, err = s.secretDrafts.PublishSecretDraft(r.Context(), &sb.PublishSecretDraftRequest{OperationGrant: op.GetOperationGrant()})
		result = response.GetDraft()
		if response.GetSecret() != nil {
			v := castRuntimeSecretMetadata(response.GetSecret())
			v.DisplayHint = nil
			secret = &v
		}
	case "DISCARDED":
		var response *sb.DiscardSecretDraftResponse
		response, err = s.secretDrafts.DiscardSecretDraft(r.Context(), &sb.DiscardSecretDraftRequest{OperationGrant: op.GetOperationGrant()})
		result = response.GetDraft()
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	s.finishSecretDraft(w, http.StatusOK, draft, result, secret, target)
}

func (s *Server) secretDraftReady(w http.ResponseWriter, r *http.Request) bool {
	if s.secretDrafts == nil {
		writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", true)
		return false
	}
	response, err := s.secretDrafts.CheckSecretDraftReadiness(r.Context(), &sb.CheckSecretDraftReadinessRequest{})
	if err != nil {
		writeRPCProblem(w, err)
		return false
	}
	if !response.GetReady() {
		writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", true)
		return false
	}
	return true
}

func terminalSecretDraft(w http.ResponseWriter, status int, op *cp.RuntimeSecretDraftOperationReceipt, target string) bool {
	draft, ok := runtimeSecretDraftView(op.GetDraft())
	if !ok || !opaqueHTTPReference.MatchString(op.GetOperationRef()) {
		invalidSecretDraft(w)
		return true
	}
	switch op.GetState() {
	case cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_PREPARED:
		validState := target == "DRAFT" && draft.State == "PREPARING" || target == "VALID" && (draft.State == "DRAFT" || draft.State == "VALID") || target == "PUBLISHED" && draft.State == "PUBLISHING" || target == "DISCARDED" && draft.State == "DISCARDED"
		if !validState || op.GetOperationGrant() == "" || len(op.GetOperationGrant()) > 8192 || op.GetExpiresAt() == nil || op.GetExpiresAt().CheckValid() != nil || op.GetTerminalSecret() != nil || op.GetFailureCode() != cp.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_UNSPECIFIED {
			invalidSecretDraft(w)
			return true
		}
		return false
	case cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_COMPLETED:
		var secret *generated.RuntimeSecret
		if op.GetTerminalSecret() != nil {
			v := castControlPlaneRuntimeSecret(op.GetTerminalSecret())
			v.DisplayHint = nil
			secret = &v
		}
		if op.GetOperationGrant() != "" || op.GetFailureCode() != cp.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_UNSPECIFIED || string(draft.State) != target || !validDraftPublication(draft, secret, target) {
			invalidSecretDraft(w)
			return true
		}
		writeSecretDraft(w, status, draft, secret)
	case cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_FAILED:
		if op.GetOperationGrant() != "" || op.GetTerminalSecret() != nil || op.GetFailureCode() == cp.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_UNSPECIFIED || cp.RuntimeSecretFailureCode_name[int32(op.GetFailureCode())] == "" {
			invalidSecretDraft(w)
			return true
		}
		writeLocalProblem(w, http.StatusConflict, "RUNTIME_SECRET_OPERATION_FAILED", false)
	default:
		invalidSecretDraft(w)
	}
	return true
}

func (s *Server) finishSecretDraft(w http.ResponseWriter, status int, before generated.RuntimeSecretDraft, response *sb.RuntimeSecretDraftMetadata, secret *generated.RuntimeSecret, target string) {
	draft, ok := runtimeSecretDraftView(&cp.RuntimeSecretDraft{Ref: response.GetRef(), Version: response.GetVersion(), Generation: response.GetGeneration(), ProjectRef: response.GetProjectRef(), SecretRef: response.GetSecretRef(), SecretVersion: response.GetSecretVersion(), Name: response.GetName(), Description: response.GetDescription(), ValueType: cp.RuntimeSecretValueType(response.GetValueType()), State: cp.RuntimeSecretDraftState(response.GetState()), PublishedRevision: response.GetPublishedRevision(), CreatedAt: response.GetCreatedAt(), UpdatedAt: response.GetUpdatedAt(), ExpiresAt: response.GetExpiresAt()})
	if !ok || draft.Ref != before.Ref || draft.Generation != before.Generation || draft.ProjectRef != before.ProjectRef || draft.SecretRef != before.SecretRef || draft.ValueType != before.ValueType || draft.Name != before.Name || draft.Description != before.Description || draft.Version < before.Version || target != "DISCARDED" && draft.Version == before.Version || !draft.CreatedAt.Equal(before.CreatedAt) || !draft.ExpiresAt.Equal(before.ExpiresAt) || draft.UpdatedAt.Before(before.UpdatedAt) || string(draft.State) != target || !validDraftPublication(draft, secret, target) {
		invalidSecretDraft(w)
		return
	}
	writeSecretDraft(w, status, draft, secret)
}

func runtimeSecretDraftView(v *cp.RuntimeSecretDraft) (generated.RuntimeSecretDraft, bool) {
	result := generated.RuntimeSecretDraft{}
	if v == nil || !validManagedVersion(v.GetVersion()) || !validManagedVersion(v.GetGeneration()) || !validManagedVersion(v.GetSecretVersion()) || v.GetPublishedRevision() < 0 || v.GetPublishedRevision() > maximumSafeJSONInteger {
		return result, false
	}
	for _, ref := range []string{v.GetRef(), v.GetProjectRef(), v.GetSecretRef()} {
		if !opaqueHTTPReference.MatchString(ref) || len(ref) > 96 {
			return result, false
		}
	}
	state := generated.RuntimeSecretDraftState(strings.TrimPrefix(v.GetState().String(), "RUNTIME_SECRET_DRAFT_STATE_"))
	valueType := generated.RuntimeSecretValueType(runtimeSecretValueTypeName(v.GetValueType()))
	if !state.Valid() || !valueType.Valid() || (state == "PUBLISHED") != (v.GetPublishedRevision() > 0) || !utf8.ValidString(v.GetName()) || utf8.RuneCountInString(v.GetName()) < 1 || utf8.RuneCountInString(v.GetName()) > 120 || !utf8.ValidString(v.GetDescription()) || len(v.GetDescription()) > 1000 {
		return result, false
	}
	if v.GetCreatedAt() == nil || v.GetCreatedAt().CheckValid() != nil || v.GetUpdatedAt() == nil || v.GetUpdatedAt().CheckValid() != nil || v.GetExpiresAt() == nil || v.GetExpiresAt().CheckValid() != nil {
		return result, false
	}
	if v.GetUpdatedAt().AsTime().Before(v.GetCreatedAt().AsTime()) || !v.GetExpiresAt().AsTime().After(v.GetCreatedAt().AsTime()) {
		return result, false
	}
	return generated.RuntimeSecretDraft{Ref: v.GetRef(), Version: v.GetVersion(), Generation: v.GetGeneration(), ProjectRef: v.GetProjectRef(), SecretRef: v.GetSecretRef(), SecretVersion: v.GetSecretVersion(), Name: v.GetName(), Description: v.GetDescription(), State: state, ValueType: valueType, PublishedRevision: v.GetPublishedRevision(), CreatedAt: v.GetCreatedAt().AsTime(), UpdatedAt: v.GetUpdatedAt().AsTime(), ExpiresAt: v.GetExpiresAt().AsTime()}, true
}

func validDraftPublication(draft generated.RuntimeSecretDraft, secret *generated.RuntimeSecret, target string) bool {
	if target != "PUBLISHED" {
		return secret == nil
	}
	return secret != nil && secret.Ref == draft.SecretRef && secret.ProjectRef == draft.ProjectRef && secret.State == "ACTIVE" && validManagedVersion(secret.Version) && secret.Version == draft.SecretVersion && secret.CurrentRevision == draft.PublishedRevision && secret.Name == draft.Name && secret.Description == draft.Description && secret.ValueType == draft.ValueType && !secret.CreatedAt.IsZero() && !secret.UpdatedAt.Before(secret.CreatedAt)
}

func invalidSecretDraft(w http.ResponseWriter) {
	writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
}
func writeSecretDraft(w http.ResponseWriter, status int, draft generated.RuntimeSecretDraft, secret *generated.RuntimeSecret) {
	setRuntimeSecretHeaders(w)
	setVersionETag(w, uint64(draft.Version))
	if secret != nil {
		writeJSON(w, status, generated.RuntimeSecretDraftPublication{Draft: draft, Secret: *secret})
		return
	}
	writeJSON(w, status, draft)
}
