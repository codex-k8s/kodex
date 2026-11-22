package managedkubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// MaximumInputDeliveryLifetime caps source handoff metadata independently from a stream hard deadline.
// The owner must additionally narrow it to the current lease, staged source and operation limits.
// MaximumInputDeliveryLifetime ограничивает source handoff metadata независимо от stream hard deadline.
// Owner дополнительно сужает срок current lease, staged source и operation limits.
const MaximumInputDeliveryLifetime = 30 * time.Second

// InputFrameDelivery transfers one exclusive private frame from the original owner claim.
// It is not public input authority; a broker must still BeginINPUT before its single native write.
// InputFrameDelivery передаёт один exclusive private frame исходного owner claim.
// Не является public input authority; broker обязан BeginINPUT до единственного native write.
type InputFrameDelivery struct {
	Ref, InvocationRef, StreamRef, Channel, PayloadDigest, BindingDigest string
	Generation                                                           int64
	Sequence                                                             uint64
	ByteCount                                                            uint32
	EndOfInput                                                           bool
	Payload                                                              []byte `json:"-"`
	DeliveryExpiresAt                                                    time.Time
}

// Validate checks exact original lineage, bounded bytes/hash/deadline and the single shared payload syntax.
// FILE_CONTENT is only structurally checked here; exact artifact/version/archive authority belongs to the owner.
// Validate проверяет exact original lineage, bounded bytes/hash/deadline и единую shared payload syntax.
// FILE_CONTENT проверяется лишь структурно; exact artifact/version/archive authority принадлежит owner.
func (frame InputFrameDelivery) Validate(binding Binding, streamRef string, now time.Time) error {
	digest, err := BindingDigest(binding)
	if err != nil || binding.InvocationRef == "" || binding.ConnectionTestRef != "" || frame.InvocationRef != binding.InvocationRef || frame.Generation != binding.Generation || frame.BindingDigest != digest || frame.StreamRef != streamRef || !ValidLocator(streamRef) || len(streamRef) > 96 || !strings.HasPrefix(frame.Ref, "kif_") || !ValidLocator(frame.Ref) || len(frame.Ref) < 12 || len(frame.Ref) > 96 || !ValidFrameSequence(frame.Sequence) || frame.ByteCount > MaximumFrameBytes || int(frame.ByteCount) != len(frame.Payload) || frame.DeliveryExpiresAt.IsZero() || !frame.DeliveryExpiresAt.After(now) || frame.DeliveryExpiresAt.After(now.Add(MaximumInputDeliveryLifetime)) {
		return ErrInvalidOperationInput
	}
	if frame.Channel == "FILE_CONTENT" {
		sum := sha256.Sum256(frame.Payload)
		if !ValidDigest(frame.PayloadDigest) || hex.EncodeToString(sum[:]) != frame.PayloadDigest || !(frame.ByteCount > 0 || frame.EndOfInput) {
			return ErrInvalidOperationInput
		}
		return nil
	}
	return ValidatePublicStreamInput(frame.Channel, frame.Payload, frame.PayloadDigest, frame.EndOfInput)
}

// String suppresses private payload under ordinary diagnostic formatting.
// String скрывает private payload при обычном diagnostic formatting.
func (InputFrameDelivery) String() string { return "[managed Kubernetes private input frame]" }

// GoString suppresses private payload under detailed diagnostic formatting as well.
// GoString скрывает private payload и при подробном diagnostic formatting.
func (frame InputFrameDelivery) GoString() string { return frame.String() }
