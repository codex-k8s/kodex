package httptransport

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// GetEmailEffectReceipt reads the exact invocation receipt and optional reconciliation decision,
// requiring matching receipt version/digest, invocation and coherent decision time before
// returning the receipt ETag. It does not retry an uncertain external mail effect.
// GetEmailEffectReceipt читает receipt точного invocation и необязательное решение reconciliation,
// требуя совпадения версии/digest receipt, invocation и согласованного времени решения до выдачи
// ETag. Неопределённый внешний почтовый эффект не повторяется.
func (server *Server) GetEmailEffectReceipt(w http.ResponseWriter, r *http.Request, ref generated.IntegrationInvocationRef) {
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.GetEmailEffectReceipt(r.Context(), &controlplanev1.GetEmailEffectReceiptRequest{InvocationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	receipt, ok := emailEffectReceiptView(response.GetReceipt())
	if !ok || receipt.InvocationRef != ref {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.EmailEffectReceiptView{Receipt: receipt}
	if response.GetDecision() != nil {
		decision, valid := emailReconciliationDecisionView(response.GetDecision())
		if !valid || decision.ReceiptRef != receipt.Ref || decision.ReceiptVersion != receipt.Version ||
			decision.ReceiptDigest != receipt.ExternalReceiptDigest || decision.InvocationRef != ref || decision.CreatedAt.Before(receipt.CreatedAt) {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		result.Decision = &decision
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(receipt.Version, 10)+"\"")
	writeJSON(w, http.StatusOK, result)
}

// ReconcileEmailEffect validates receipt/digest/outcome/note under versioned mutation headers,
// consumes a fresh exact reconciliation authorization at the boundary and delegates the decision
// to the owner. Returned receipt pins and outcome must match; this path records a decision rather
// than resending mail.
// ReconcileEmailEffect проверяет receipt/digest/outcome/note с versioned mutation заголовками,
// потребляет свежее точное разрешение reconciliation в boundary и поручает решение владельцу. Pins
// receipt и outcome ответа обязаны совпасть; путь записывает решение без повторной отправки почты.
func (server *Server) ReconcileEmailEffect(w http.ResponseWriter, r *http.Request, ref generated.EmailEffectReceiptRef, p generated.ReconcileEmailEffectParams) {
	body, ok := decodeJSON[generated.EmailReconciliationInput](w, r)
	if !ok {
		return
	}
	note := stringValue(body.Note)
	outcome, valid := emailReconciliationOutcome(body.Outcome)
	if !opaqueHTTPReference.MatchString(ref) || !valid || !validManagedDigest(body.ExpectedReceiptDigest) ||
		utf8.RuneCountInString(note) > 2000 || strings.ContainsRune(note, '\x00') {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	if server.boundary == nil {
		writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", false)
		return
	}
	if err := server.boundary.ConsumeEmailReconciliation(r.Context(), w, ref, mutation.GetExpectedVersion(), body.ExpectedReceiptDigest); err != nil {
		if errors.Is(err, boundary.ErrElevationUnavailable) {
			writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", false)
		} else {
			writeLocalProblem(w, http.StatusForbidden, "FRESH_AUTHENTICATION_REQUIRED", false)
		}
		return
	}
	response, err := server.control.Command.ReconcileEmailEffect(r.Context(), &controlplanev1.ReconcileEmailEffectRequest{
		Mutation: mutation, ReceiptRef: ref, ExpectedReceiptDigest: body.ExpectedReceiptDigest, Outcome: outcome, Note: note,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	decision, ok := emailReconciliationDecisionView(response.GetDecision())
	if !ok || decision.ReceiptRef != ref || decision.ReceiptVersion != mutation.GetExpectedVersion() ||
		decision.ReceiptDigest != body.ExpectedReceiptDigest || decision.Outcome != body.Outcome {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", "\""+strconv.FormatInt(decision.Version, 10)+"\"")
	writeJSON(w, http.StatusOK, decision)
}

// emailReconciliationOutcome maps only explicit EFFECT_CONFIRMED or NO_EFFECT_CONFIRMED owner
// decisions from the public API; UNKNOWN_OUTCOME cannot be submitted as a reconciliation decision.
// emailReconciliationOutcome преобразует из публичного API только явные решения EFFECT_CONFIRMED
// либо NO_EFFECT_CONFIRMED; UNKNOWN_OUTCOME нельзя передать как решение reconciliation.
func emailReconciliationOutcome(value generated.EmailReconciliationOutcome) (controlplanev1.EmailEffectOutcome, bool) {
	switch value {
	case generated.EFFECTCONFIRMED:
		return controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_EFFECT_CONFIRMED, true
	case generated.NOEFFECTCONFIRMED:
		return controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED, true
	default:
		return controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNSPECIFIED, false
	}
}

// emailEffectOutcome projects known external receipt outcomes, preserving UNKNOWN_OUTCOME as
// distinct from confirmed effect or confirmed absence. Unknown protobuf values are rejected.
// emailEffectOutcome проецирует известные исходы внешнего receipt, сохраняя UNKNOWN_OUTCOME
// отдельно от подтверждённого эффекта или его отсутствия. Неизвестные protobuf значения
// отклоняются.
func emailEffectOutcome(value controlplanev1.EmailEffectOutcome) (generated.EmailEffectOutcome, bool) {
	switch value {
	case controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNKNOWN_OUTCOME:
		return generated.EmailEffectOutcomeUNKNOWNOUTCOME, true
	case controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_EFFECT_CONFIRMED:
		return generated.EmailEffectOutcomeEFFECTCONFIRMED, true
	case controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED:
		return generated.EmailEffectOutcomeNOEFFECTCONFIRMED, true
	default:
		return "", false
	}
}

// emailEffectReceiptView checks receipt identity/version, invocation and semantic/external
// digests, mailbox/configuration/connection/project coordinates and ordered timestamps. The public
// DTO excludes execution keys and worker grants that are unnecessary for an owner decision.
// emailEffectReceiptView проверяет identity/version receipt, invocation и semantic/external
// digests, координаты mailbox/configuration/connection/project и порядок времени. Публичный DTO
// исключает execution keys и worker grants, не нужные для решения владельца.
func emailEffectReceiptView(value *controlplanev1.EmailEffectReceipt) (generated.EmailEffectReceipt, bool) {
	created, createdOK := contextTimestamp(value.GetCreatedAt())
	updated, updatedOK := contextTimestamp(value.GetUpdatedAt())
	outcome, outcomeOK := emailEffectOutcome(value.GetOutcome())
	if !createdOK || !updatedOK || updated.Before(created) || !outcomeOK || !opaqueHTTPReference.MatchString(value.GetRef()) ||
		!validManagedVersion(value.GetVersion()) || !opaqueHTTPReference.MatchString(value.GetInvocationRef()) ||
		!validManagedDigest(value.GetExternalReceiptDigest()) || !validManagedDigest(value.GetSemanticInputDigest()) ||
		!validInteractionExternalRef(value.GetMailboxRef()) || !validManagedVersion(value.GetConfigurationRevision()) ||
		!opaqueHTTPReference.MatchString(value.GetConnectionRef()) || !opaqueHTTPReference.MatchString(value.GetProjectRef()) {
		return generated.EmailEffectReceipt{}, false
	}
	// Внешние ключи исполнения и worker grant не нужны для решения в интерфейсе.
	return generated.EmailEffectReceipt{
		Ref: value.GetRef(), Version: value.GetVersion(), InvocationRef: value.GetInvocationRef(),
		ExternalReceiptDigest: value.GetExternalReceiptDigest(), SemanticInputDigest: value.GetSemanticInputDigest(), Outcome: outcome,
		MailboxRef: value.GetMailboxRef(), ConfigurationRevision: value.GetConfigurationRevision(), ConnectionRef: value.GetConnectionRef(),
		ProjectRef: value.GetProjectRef(), CreatedAt: created, UpdatedAt: updated,
	}, true
}

// emailReconciliationDecisionView projects a confirmed non-unknown decision only with valid
// receipt/version/digest and actor/invocation refs plus a strictly later expiry. The result
// describes the recorded owner decision without proving a new external effect.
// emailReconciliationDecisionView проецирует подтверждённое решение без UNKNOWN только с
// корректными receipt/version/digest и actor/invocation refs и строго более поздним expiry.
// Описывается записанное решение владельца без доказательства нового внешнего эффекта.
func emailReconciliationDecisionView(value *controlplanev1.EmailReconciliationDecision) (generated.EmailReconciliationDecision, bool) {
	created, createdOK := contextTimestamp(value.GetCreatedAt())
	expires, expiresOK := contextTimestamp(value.GetExpiresAt())
	outcome, outcomeOK := emailEffectOutcome(value.GetOutcome())
	if !createdOK || !expiresOK || !expires.After(created) || !outcomeOK || outcome == generated.EmailEffectOutcomeUNKNOWNOUTCOME ||
		!opaqueHTTPReference.MatchString(value.GetRef()) || !validManagedVersion(value.GetVersion()) ||
		!opaqueHTTPReference.MatchString(value.GetReceiptRef()) || !validManagedVersion(value.GetReceiptVersion()) ||
		!validManagedDigest(value.GetReceiptDigest()) || !opaqueHTTPReference.MatchString(value.GetInvocationRef()) ||
		!opaqueHTTPReference.MatchString(value.GetActorRef()) {
		return generated.EmailReconciliationDecision{}, false
	}
	return generated.EmailReconciliationDecision{
		Ref: value.GetRef(), Version: value.GetVersion(), ReceiptRef: value.GetReceiptRef(), ReceiptVersion: value.GetReceiptVersion(),
		ReceiptDigest: value.GetReceiptDigest(), InvocationRef: value.GetInvocationRef(), Outcome: generated.EmailReconciliationOutcome(outcome),
		ActorRef: value.GetActorRef(), CreatedAt: created, ExpiresAt: expires,
	}, true
}
