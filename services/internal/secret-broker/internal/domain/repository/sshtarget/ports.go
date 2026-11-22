package sshtarget

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// ErrIdentityRejected is the fixed private material diagnostic; it never includes Kubernetes object metadata, key bytes or SDK diagnostics.
// ErrIdentityRejected задаёт fixed private material diagnostic без Kubernetes object metadata, key bytes либо SDK diagnostics.
var ErrIdentityRejected = errors.New("SSH identity material rejected")

// ErrIdentityUnknown preserves an ambiguous create/delete acknowledgement; callers may only reconcile exact owner intent and must never infer permission to resend a write.
// ErrIdentityUnknown сохраняет ambiguous create/delete acknowledgement; caller может только сверять exact owner intent и не получает permission на повтор write.
var ErrIdentityUnknown = errors.New("SSH identity materialization outcome unknown")

// ErrIdentityAcknowledgementRejected distinguishes a completed successful HTTP response with invalid material evidence from a lost transport acknowledgement; the setup owner must persist a terminal rejection before admitting any observation.
// ErrIdentityAcknowledgementRejected отличает завершённый успешный HTTP response с invalid material evidence от lost transport acknowledgement; setup owner обязан сохранить terminal rejection до допуска любого observation.
var ErrIdentityAcknowledgementRejected = errors.New("SSH identity acknowledgement rejected")

// IdentityAcknowledgementRejection carries bounded private evidence for a terminal setup receipt, never a usable material locator; an observed UID is retained even when the remaining acknowledged object fails validation.
// IdentityAcknowledgementRejection содержит bounded private evidence для terminal setup receipt, а не usable material locator; observed UID сохраняется даже при отказе validation остального acknowledged object.
type IdentityAcknowledgementRejection struct {
	ResponseSHA256 string
	Truncated      bool
	ObservedUID    string
}

// Error emits only the fixed diagnostic so ordinary error logging cannot disclose the private acknowledgement witness.
// Error возвращает только fixed diagnostic, чтобы ordinary error logging не раскрывало private acknowledgement witness.
func (rejection *IdentityAcknowledgementRejection) Error() string {
	return ErrIdentityAcknowledgementRejected.Error()
}

// String conceals private response hashes and observed object pins during ordinary formatting.
// String скрывает private response hashes и observed object pins при ordinary formatting.
func (rejection *IdentityAcknowledgementRejection) String() string { return rejection.Error() }

// GoString applies the same redaction to detailed Go formatting instead of exposing the witness fields.
// GoString применяет ту же redaction к detailed Go formatting вместо выдачи witness fields.
func (rejection *IdentityAcknowledgementRejection) GoString() string { return rejection.Error() }

// Unwrap exposes only terminal acknowledgement classification, deliberately excluding UNKNOWN and pre-dispatch rejection.
// Unwrap предоставляет только terminal acknowledgement classification, намеренно исключая UNKNOWN и pre-dispatch rejection.
func (rejection *IdentityAcknowledgementRejection) Unwrap() error {
	return ErrIdentityAcknowledgementRejected
}

// ErrIdentityAbsent reports one authoritative read absence; it does not prove an earlier create could not still complete or authorize another create.
// ErrIdentityAbsent сообщает authoritative read absence; оно не доказывает невозможность позднего completion прежнего create и не разрешает новый create.
var ErrIdentityAbsent = errors.New("SSH identity materialization absent")

// ErrIdentityUnavailable closes a read-only reconciliation attempt without leaking dependency diagnostics or changing the immutable setup intent.
// ErrIdentityUnavailable закрывает read-only reconciliation attempt без dependency diagnostics либо изменения immutable setup intent.
var ErrIdentityUnavailable = errors.New("SSH identity materialization unavailable")

// IdentityMaterialReader reads exactly one SSH-purpose immutable material revision selected by fresh owner admission, returning a private owned copy for bounded use and clearing.
// IdentityMaterialReader читает ровно одну SSH-purpose immutable material revision по fresh owner admission, возвращая private owned copy для bounded use и очистки.
type IdentityMaterialReader interface {
	ReadSSHIdentity(context.Context, value.SSHIdentityLocator) ([]byte, error)
}

// IdentityMaterialStore separates the sole owner-admitted write, initial read-only reconciliation and exact cleanup; a fresh lease never becomes another create permission.
// IdentityMaterialStore разделяет единственный owner-admitted write, initial read-only reconciliation и exact cleanup; fresh lease не становится новым create permission.
type IdentityMaterialStore interface {
	IdentityMaterialReader
	// CreateSSHIdentity issues one API request with an owner-prepared key; lost acknowledgement stays UNKNOWN, while completed invalid success must be durably recorded as terminal ACK_REJECTED with no future Create or Observe admission.
	// CreateSSHIdentity отправляет один API request с owner-prepared key; lost acknowledgement остаётся UNKNOWN, а completed invalid success необходимо durable сохранить как terminal ACK_REJECTED без будущего допуска Create либо Observe.
	CreateSSHIdentity(context.Context, value.SSHIdentityProvision, []byte) (value.SSHIdentityLocator, error)
	// ObserveSSHIdentity may be admitted only before the first owner binding commit; the owner rejects later replacement adoption and never treats absence as write permission.
	// ObserveSSHIdentity допускается только до первого owner binding commit; owner запрещает позднее принятие replacement и не превращает absence в write permission.
	ObserveSSHIdentity(context.Context, value.SSHIdentityProvision) (value.SSHIdentityLocator, error)
	// DeleteSSHIdentity requires current owner cleanup admission, no active references and exact object UID/resourceVersion; the locator alone grants none of those permissions.
	// DeleteSSHIdentity требует current owner cleanup admission, отсутствие active references и exact UID/resourceVersion; locator сам не выдаёт этих permissions.
	DeleteSSHIdentity(context.Context, value.SSHIdentityLocator) error
}
