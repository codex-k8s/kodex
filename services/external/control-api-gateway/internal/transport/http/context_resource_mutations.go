package httptransport

import (
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// requireContextBindingMutation decodes a binding body, checks its three exact
// references and safe binding-version range, then validates mutation headers.
// No RPC or authority decision occurs here; all four bind/unbind handlers retain
// their explicit typed commands and response-binding checks.
// requireContextBindingMutation разбирает body привязки, проверяет три точные
// ссылки и безопасный диапазон версии binding, затем заголовки mutation.
// Здесь нет RPC или решения о полномочиях; четыре bind/unbind handler сохраняют
// явные типизированные команды и проверки привязки ответа.
func requireContextBindingMutation(w http.ResponseWriter, r *http.Request, agent, resource, key, etag string) (generated.AgentContextBindingInput, *controlplanev1.MutationContext, bool) {
	body, ok := decodeJSON[generated.AgentContextBindingInput](w, r)
	if !ok {
		return body, nil, false
	}
	if !opaqueHTTPReference.MatchString(agent) || !opaqueHTTPReference.MatchString(resource) || !opaqueHTTPReference.MatchString(body.RevisionRef) || body.ExpectedBindingVersion < 0 || body.ExpectedBindingVersion > maximumSafeJSONInteger {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return body, nil, false
	}
	mutation, ok := requireVersionedMutation(w, key, etag)
	return body, mutation, ok
}

// requireSkillRevisionDigestMutation preserves the shared input sequence for
// validate/publish/discard: exact resource/revision shape, existing body decoder,
// digest format, then versioned headers. It never chooses the lifecycle command
// or treats a caller-supplied digest as proof of scan/review/admission.
// requireSkillRevisionDigestMutation сохраняет порядок validate/publish/discard:
// форма resource/revision, прежний body decoder, формат digest, затем versioned
// headers. Он не выбирает lifecycle-команду и не считает digest от клиента
// доказательством scan/review/admission.
func requireSkillRevisionDigestMutation(w http.ResponseWriter, r *http.Request, resource, revision, key, etag string) (generated.ContextRevisionDigestInput, *controlplanev1.MutationContext, bool) {
	if !opaqueHTTPReference.MatchString(resource) || !opaqueHTTPReference.MatchString(revision) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return generated.ContextRevisionDigestInput{}, nil, false
	}
	body, ok := decodeJSON[generated.ContextRevisionDigestInput](w, r)
	if !ok {
		return body, nil, false
	}
	if !validManagedDigest(body.ExpectedDigest) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return body, nil, false
	}
	mutation, ok := requireVersionedMutation(w, key, etag)
	return body, mutation, ok
}

// decodeContextSpecification runs the existing JSON decoder before the selected typed
// specification converter. Decode failure skips conversion; conversion failure keeps the
// returned value and writes the same local400 once. Callers retain reference/header order,
// request type, converter and RPC; a converted value does not establish resource authority.
// decodeContextSpecification вызывает прежний JSON decoder до выбранного типизированного
// преобразователя спецификации. Ошибка декодирования пропускает преобразование; его отказ
// сохраняет возвращённое значение и единожды пишет прежний local400. Порядок ссылок/headers,
// тип входа, преобразователь и RPC остаются у callers; значение полномочий не назначает.
func decodeContextSpecification[Input, Output any](w http.ResponseWriter, r *http.Request, convert func(Input) (Output, bool)) (Output, bool) {
	input, ok := decodeJSON[Input](w, r)
	if !ok {
		var zero Output
		return zero, false
	}
	result, ok := convert(input)
	if !ok {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
	}
	return result, ok
}
