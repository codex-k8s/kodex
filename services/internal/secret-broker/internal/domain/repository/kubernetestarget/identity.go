// Package kubernetestarget defines the private Kubernetes material port without transport DTOs.
// Пакет kubernetestarget задаёт private Kubernetes material port без transport DTOs.
package kubernetestarget

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// ErrIdentityAcknowledgementRejected distinguishes a completed invalid ACK from transport ambiguity.
// Its owner must retain terminal rejection before admitting any continuation of this intent.
// ErrIdentityAcknowledgementRejected отличает completed invalid ACK от transport ambiguity.
// Owner сохраняет terminal rejection до допуска любого continuation этого intent.
var ErrIdentityAcknowledgementRejected = errors.New("managed Kubernetes identity acknowledgement rejected")

// ErrIdentityCreateUnknown records a joined write with no complete successful acknowledgement.
// It grants no retry; only the owner can admit a separate observation of the original intent.
// ErrIdentityCreateUnknown обозначает joined write без complete successful acknowledgement.
// Не даёт retry; лишь owner допускает отдельный observation исходного intent.
var ErrIdentityCreateUnknown = errors.New("managed Kubernetes identity materialization outcome is unknown")

// IdentityAcknowledgementRejection carries only a bounded private receipt, never response body bytes.
// IdentityAcknowledgementRejection содержит только bounded private receipt, не response body bytes.
type IdentityAcknowledgementRejection struct {
	Receipt value.KubernetesIdentityAcknowledgementRejection `json:"-"`
}

// Error exposes a fixed diagnostic without witness, upstream details or credential material.
// Error выдаёт fixed diagnostic без witness, upstream details либо credential material.
func (*IdentityAcknowledgementRejection) Error() string {
	return ErrIdentityAcknowledgementRejected.Error()
}

// Unwrap exposes only terminal classification and deliberately excludes the UNKNOWN sentinel.
// Unwrap предоставляет только terminal classification, намеренно исключая UNKNOWN sentinel.
func (*IdentityAcknowledgementRejection) Unwrap() error { return ErrIdentityAcknowledgementRejected }

// GoString prevents detailed error formatting from exposing the private receipt.
// GoString не допускает раскрытие private receipt при подробном error formatting.
func (rejection *IdentityAcknowledgementRejection) GoString() string { return rejection.Error() }

// IdentityMaterialReader reads only one owner-resolved purpose object inside the broker.
// It must reject generic runtime material, foreign identities and changed UID/RV/content pins.
// IdentityMaterialReader читает один owner-resolved purpose object только внутри broker.
// Обязан отвергать generic runtime material, foreign identities и изменённые UID/RV/content pins.
type IdentityMaterialReader interface {
	// ReadKubernetesIdentity returns a private byte copy for immediate bounded parsing; the consumer
	// clears every returned slice, including partial bytes returned together with an error.
	// ReadKubernetesIdentity возвращает private byte copy для немедленного bounded parsing; consumer
	// очищает каждый returned slice, включая partial bytes вместе с ошибкой.
	ReadKubernetesIdentity(context.Context, value.KubernetesIdentityLocator) ([]byte, error)
}

// IdentityMaterialWriter creates a separate immutable purpose object after one-shot owner admission.
// It never updates or deletes a name, and a failed create does not authorize an automatic retry.
// IdentityMaterialWriter создаёт отдельный неизменяемый purpose object после одноразового owner admission.
// Не изменяет и не удаляет имя; ошибка create не разрешает автоматический повтор.
type IdentityMaterialWriter interface {
	// CreateKubernetesIdentity attempts exactly one immutable create; ambiguous responses stay unknown.
	// CreateKubernetesIdentity делает одну попытку immutable create; неоднозначный ответ остаётся unknown.
	CreateKubernetesIdentity(context.Context, value.KubernetesIdentityProvision, []byte) (value.KubernetesIdentityLocator, error)
	// ObserveKubernetesIdentity reads only the exact admitted purpose name for metadata reconciliation.
	// A missing object is not permission to create another one; bytes never leave this broker port.
	// ObserveKubernetesIdentity читает exact admitted purpose name только для сверки metadata.
	// Отсутствие не разрешает новый create; байты не выходят из этого broker port.
	ObserveKubernetesIdentity(context.Context, value.KubernetesIdentityProvision) (value.KubernetesIdentityLocator, error)
}
