package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

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

func emailReconciliationOutcome(value generated.EmailReconciliationOutcome) (controlplanev1.EmailEffectOutcome, bool) {
	switch value {
	case generated.EmailReconciliationOutcomeEFFECTCONFIRMED:
		return controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_EFFECT_CONFIRMED, true
	case generated.EmailReconciliationOutcomeNOEFFECTCONFIRMED:
		return controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED, true
	default:
		return controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNSPECIFIED, false
	}
}

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
