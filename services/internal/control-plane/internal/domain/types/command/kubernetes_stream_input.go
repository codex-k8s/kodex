package command

import "github.com/codex-k8s/kodex/libs/go/managedkubernetes"

// KubernetesStreamInputSubmit supplies one bounded private payload; the owner assigns its sequence after resolving the original root, stream and current grant.
// KubernetesStreamInputSubmit задаёт один bounded private payload; sequence назначает owner после разрешения original root, stream и current grant.
type KubernetesStreamInputSubmit struct {
	InvocationRef, StreamRef, Channel, PayloadDigest string
	Payload                                          []byte `json:"-"`
	EndOfInput                                       bool
}

// ValidPayload validates public source syntax only; FILE_CONTENT belongs exclusively to the pinned-artifact owner reader, and CONTROL is not a raw command channel.
// ValidPayload проверяет лишь public source syntax; FILE_CONTENT принадлежит только pinned-artifact owner reader, CONTROL не является raw command channel.
func (input KubernetesStreamInputSubmit) ValidPayload() bool {
	return managedkubernetes.ValidatePublicStreamInput(input.Channel, input.Payload, input.PayloadDigest, input.EndOfInput) == nil
}

// String prevents private input bytes from appearing in ordinary diagnostics.
// String не допускает private input bytes в обычную диагностику.
func (KubernetesStreamInputSubmit) String() string {
	return "[managed Kubernetes private stream input]"
}

// GoString preserves the private input boundary under detailed diagnostic formatting.
// GoString сохраняет private input boundary при detailed diagnostic formatting.
func (input KubernetesStreamInputSubmit) GoString() string { return input.String() }

// KubernetesStreamInputStop selects one original tool stream for owner closure without accepting actor, fence, source data or a whole-Run cancellation selector.
// KubernetesStreamInputStop выбирает один original tool stream для owner closure без actor, fence, source data либо selector отмены всего Run.
type KubernetesStreamInputStop struct{ InvocationRef, StreamRef string }
