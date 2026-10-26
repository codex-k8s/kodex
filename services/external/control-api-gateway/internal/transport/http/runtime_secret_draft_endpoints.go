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

// GetRuntimeSecretDraft reads a value-free owner draft by reference, validates its public shape and exact ref, then writes no-store metadata with a version ETag. It neither reveals the draft bytes nor resumes its operation.
// GetRuntimeSecretDraft читает черновик владельца без значения по ссылке, проверяет публичную форму и точный ref, выдаёт no-store metadata с ETag версии. Байты черновика не раскрывает и операцию не возобновляет.
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

// CreateRuntimeSecretDraft binds the project, decodes creation metadata and requires idempotency before handing value bytes to the shared save preparation. The owner assigns draft/secret identity; client metadata is not authority.
// CreateRuntimeSecretDraft привязывает проект, декодирует creation metadata и требует идемпотентность до общей подготовки сохранения байтов. Draft/secret identity назначает владелец; клиентские metadata не являются полномочиями.
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
	s.saveSecretDraft(w, r, &cp.PrepareSaveRuntimeSecretDraftRequest{Purpose: runtimeSecretCreatePurpose(body), Mutation: mutation, ProjectRef: ref, Name: body.Name, Description: body.Description, ValueType: runtimeSecretValueType(string(body.ValueType))}, body.Value)
}

// SaveRuntimeSecretDraft decodes replacement value input and requires a versioned mutation for the referenced secret before preparing a new draft. It does not publish that replacement or bypass owner conflict checks.
// SaveRuntimeSecretDraft декодирует замену и требует versioned mutation указанного секрета до подготовки нового черновика. Замену не публикует и owner conflict checks не обходит.
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

// saveSecretDraft decodes and hashes the exact input bytes into the prepare request, invokes the dedicated owner command and forwards its receipt to broker continuation. The mutable input gains a content pin; decoded bytes are erased on return.
// saveSecretDraft декодирует и хеширует точные байты в prepare request, вызывает отдельную команду владельца и передаёт receipt продолжению через брокер. Изменяемый input получает content pin; декодированные байты затираются при возврате.
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
	s.savePreparedSecretDraft(w, r, prepared.GetOperation(), input, value, "")
}

// savePreparedSecretDraft checks prepared draft scope/type and requested metadata pins, handles terminal replay, then requires broker readiness before SaveSecretDraft. The broker result is checked against the prepared snapshot; the grant, not caller-selected metadata, carries execution authority.
// savePreparedSecretDraft сверяет scope/type подготовленного черновика и запрошенные metadata pins, обрабатывает terminal replay, затем требует readiness брокера до SaveSecretDraft. Результат сверяется с prepared snapshot; исполнение разрешает grant, а не выбранные клиентом metadata.
func (s *Server) savePreparedSecretDraft(w http.ResponseWriter, r *http.Request, op *cp.RuntimeSecretDraftOperationReceipt, input *cp.PrepareSaveRuntimeSecretDraftRequest, value []byte, scopeKind generated.RuntimeResourceScopeKind) {
	draft, ok := runtimeSecretDraftView(op.GetDraft())
	if !ok || scopeKind != "" && draft.ScopeKind != scopeKind || draft.ValueType != generated.RuntimeSecretValueType(runtimeSecretValueTypeName(input.ValueType)) || input.SecretRef != "" && draft.SecretRef != input.SecretRef || input.SecretRef == "" && (draft.ProjectRef != input.ProjectRef || draft.Name != input.Name || draft.Description != input.Description) {
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

// ValidateRuntimeSecretDraft selects the dedicated VALID draft transition through the shared versioned prepare/broker path; validation does not publish the secret value.
// ValidateRuntimeSecretDraft выбирает отдельный переход VALID через общий versioned prepare/broker path; проверка значение секрета не публикует.
func (s *Server) ValidateRuntimeSecretDraft(w http.ResponseWriter, r *http.Request, ref generated.RuntimeSecretDraftRef, p generated.ValidateRuntimeSecretDraftParams) {
	s.changeSecretDraft(w, r, ref, p.IdempotencyKey, p.IfMatch, "VALID", nil)
}

// DiscardRuntimeSecretDraft selects the dedicated DISCARDED transition with idempotency and version handling; it does not directly delete draft storage or manufacture a successful terminal receipt.
// DiscardRuntimeSecretDraft выбирает отдельный переход DISCARDED с идемпотентностью и версией; хранилище черновика напрямую не удаляет и успешный terminal receipt не создаёт.
func (s *Server) DiscardRuntimeSecretDraft(w http.ResponseWriter, r *http.Request, ref generated.RuntimeSecretDraftRef, p generated.DiscardRuntimeSecretDraftParams) {
	s.changeSecretDraft(w, r, ref, p.IdempotencyKey, p.IfMatch, "DISCARDED", nil)
}

// PublishRuntimeSecretDraft validates the expected secret version, impact-plan ref and bounded unique selected-item refs, then requests the dedicated publish transition. The supplied plan identifies owner state; it is not itself proof of approval or a direct broker grant.
// PublishRuntimeSecretDraft проверяет ожидаемую версию секрета, impact-plan ref и ограниченные уникальные selected-item refs, затем запрашивает отдельный publish transition. План указывает owner state, но сам не является proof согласия или broker grant.
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

// changeSecretDraft dispatches only the three internal VALID/PUBLISHED/DISCARDED targets to specialized owner prepare and broker methods. It validates exact draft identity, terminal replay, publication version and readiness before the broker call, then checks the returned snapshot; no generic client-selected transition is exposed.
// changeSecretDraft направляет только три внутренних VALID/PUBLISHED/DISCARDED к специализированным owner prepare и broker methods. До брокера проверяет точный draft, terminal replay, publication version и readiness, затем сверяет snapshot ответа; generic client-selected transition наружу не открывает.
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

// secretDraftReady requires a configured draft broker and a successful ready reply using the request context; missing or not-ready service yields retryable 503, while RPC errors retain their mapped problem. No draft mutation occurs here.
// secretDraftReady требует настроенный draft broker и успешный ready reply в request context; отсутствие и неготовность дают retryable 503, RPC errors сохраняют mapping. Черновик здесь не изменяется.
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

// terminalSecretDraft validates operation identity and draft shape before branching on PREPARED, COMPLETED or FAILED. Only a consistent prepared grant returns false; completed replay must match the target/publication tuple, failed receipts yield conflict, and unknown or inconsistent envelopes fail closed.
// terminalSecretDraft проверяет identity операции и форму draft до ветвления PREPARED, COMPLETED, FAILED. Только согласованный prepared grant возвращает false; completed replay должен совпасть с target/publication tuple, failed даёт conflict, неизвестный или противоречивый envelope отклоняется.
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

// finishSecretDraft rebuilds a public draft from broker metadata and compares identity, generation, scope, metadata and timestamps with the prepared snapshot. Version must advance except for discard; target/publication bindings are validated before response. It returns no raw value bytes; display-hint removal belongs to callers.
// finishSecretDraft восстанавливает публичный draft из metadata брокера и сравнивает identity, generation, scope, metadata и время с prepared snapshot. Версия обязана продвинуться кроме discard; target/publication bindings проверяются до ответа. Сырые байты не выдаются; удаление display hints принадлежит вызывающему коду.
func (s *Server) finishSecretDraft(w http.ResponseWriter, status int, before generated.RuntimeSecretDraft, response *sb.RuntimeSecretDraftMetadata, secret *generated.RuntimeSecret, target string) {
	draft, ok := runtimeSecretDraftView(&cp.RuntimeSecretDraft{Ref: response.GetRef(), ScopeKind: cp.RuntimeResourceScopeKind(response.GetScopeKind()), OrganizationRef: response.GetOrganizationRef(), Version: response.GetVersion(), Generation: response.GetGeneration(), ProjectRef: response.GetProjectRef(), SecretRef: response.GetSecretRef(), SecretVersion: response.GetSecretVersion(), Name: response.GetName(), Description: response.GetDescription(), ValueType: cp.RuntimeSecretValueType(response.GetValueType()), State: cp.RuntimeSecretDraftState(response.GetState()), PublishedRevision: response.GetPublishedRevision(), CreatedAt: response.GetCreatedAt(), UpdatedAt: response.GetUpdatedAt(), ExpiresAt: response.GetExpiresAt()})
	if !ok || draft.Ref != before.Ref || draft.Generation != before.Generation || draft.ScopeKind != before.ScopeKind || draft.OrganizationRef != before.OrganizationRef || draft.ProjectRef != before.ProjectRef || draft.SecretRef != before.SecretRef || draft.ValueType != before.ValueType || draft.Name != before.Name || draft.Description != before.Description || draft.Version < before.Version || target != "DISCARDED" && draft.Version == before.Version || !draft.CreatedAt.Equal(before.CreatedAt) || !draft.ExpiresAt.Equal(before.ExpiresAt) || draft.UpdatedAt.Before(before.UpdatedAt) || string(draft.State) != target || !validDraftPublication(draft, secret, target) {
		invalidSecretDraft(w)
		return
	}
	writeSecretDraft(w, status, draft, secret)
}

// runtimeSecretDraftView rejects absent, unsafe-version, malformed-scope/ref/enum/text and inconsistent revision/time metadata before constructing a value-free draft. Timestamps must be valid and ordered relative to creation; this checks metadata shape, not current expiry against a clock or actor eligibility.
// runtimeSecretDraftView отклоняет отсутствие, unsafe versions, неверные scope/ref/enum/text и несогласованные revision/time metadata до создания draft без значения. Время должно быть допустимым и согласованным с creation; это форма metadata, не текущий expiry по часам и не actor eligibility.
func runtimeSecretDraftView(v *cp.RuntimeSecretDraft) (generated.RuntimeSecretDraft, bool) {
	result := generated.RuntimeSecretDraft{}
	if v == nil || !validManagedVersion(v.GetVersion()) || !validManagedVersion(v.GetGeneration()) || !validManagedVersion(v.GetSecretVersion()) || v.GetPublishedRevision() < 0 || v.GetPublishedRevision() > maximumSafeJSONInteger {
		return result, false
	}
	scopeKind := runtimeResourceScopeKind(v.GetScopeKind().String())
	if !validRuntimeResourceScope(scopeKind, v.GetOrganizationRef(), v.GetProjectRef()) {
		return result, false
	}
	for _, ref := range []string{v.GetRef(), v.GetSecretRef()} {
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
	return generated.RuntimeSecretDraft{Ref: v.GetRef(), ScopeKind: scopeKind, OrganizationRef: v.GetOrganizationRef(), Version: v.GetVersion(), Generation: v.GetGeneration(), ProjectRef: v.GetProjectRef(), SecretRef: v.GetSecretRef(), SecretVersion: v.GetSecretVersion(), Name: v.GetName(), Description: v.GetDescription(), State: state, ValueType: valueType, PublishedRevision: v.GetPublishedRevision(), CreatedAt: v.GetCreatedAt().AsTime(), UpdatedAt: v.GetUpdatedAt().AsTime(), ExpiresAt: v.GetExpiresAt().AsTime()}, true
}

// validDraftPublication requires no secret for non-publication targets; PUBLISHED requires the exact active secret scope/ref/version/revision/metadata tuple and consistent timestamps. It compares metadata only and does not approve an impact plan or inspect the value.
// validDraftPublication требует отсутствие secret для непубликующих targets; PUBLISHED требует точный active secret scope/ref/version/revision/metadata tuple и согласованное время. Сравнивает только metadata, не подтверждает impact plan и не читает значение.
func validDraftPublication(draft generated.RuntimeSecretDraft, secret *generated.RuntimeSecret, target string) bool {
	if target != "PUBLISHED" {
		return secret == nil
	}
	return secret != nil && secret.Ref == draft.SecretRef && secret.ScopeKind == draft.ScopeKind && secret.OrganizationRef == draft.OrganizationRef && secret.ProjectRef == draft.ProjectRef && secret.State == "ACTIVE" && validManagedVersion(secret.Version) && secret.Version == draft.SecretVersion && secret.CurrentRevision == draft.PublishedRevision && secret.Name == draft.Name && secret.Description == draft.Description && secret.ValueType == draft.ValueType && !secret.CreatedAt.IsZero() && !secret.UpdatedAt.Before(secret.CreatedAt)
}

// invalidSecretDraft writes the shared non-retryable 502 invalid-upstream problem without draft contents, operation grants or provider diagnostics.
// invalidSecretDraft выдаёт общий non-retryable 502 invalid-upstream problem без содержимого draft, operation grants и provider diagnostics.
func invalidSecretDraft(w http.ResponseWriter) {
	writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
}

// writeSecretDraft sets no-store headers and the draft version ETag, then selects draft-only or publication metadata shape based on the optional secret. It assumes the caller already checked publication bindings and never serializes raw value bytes.
// writeSecretDraft задаёт no-store headers и ETag версии draft, выбирает форму draft-only либо publication metadata по необязательному secret. Предполагает проверенные вызывающим кодом publication bindings и сырые байты значения не сериализует.
func writeSecretDraft(w http.ResponseWriter, status int, draft generated.RuntimeSecretDraft, secret *generated.RuntimeSecret) {
	setRuntimeSecretHeaders(w)
	setVersionETag(w, uint64(draft.Version))
	if secret != nil {
		writeJSON(w, status, generated.RuntimeSecretDraftPublication{Draft: draft, Secret: *secret})
		return
	}
	writeJSON(w, status, draft)
}
