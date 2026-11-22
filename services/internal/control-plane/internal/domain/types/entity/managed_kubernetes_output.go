package entity

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"time"
)

// ManagedKubernetesOutputFrameReceipt returns only private-stage facts and the original accepted credit.
// It exposes no bytes, object locator or artifact publication, and Get never extends AuthorizedUntil.
// ManagedKubernetesOutputFrameReceipt возвращает лишь private-stage facts и original accepted credit.
// Не раскрывает bytes/object locator/artifact publication; Get не продлевает AuthorizedUntil.
type ManagedKubernetesOutputFrameReceipt struct {
	Ref, InvocationRef, StreamRef, Channel, PayloadDigest, BindingDigest string
	Generation                                                           int64
	Sequence                                                             uint64
	ByteCount, DeliveryOrdinal                                           uint32
	State                                                                enum.ManagedKubernetesOutputFrameState
	AuthorizedUntil                                                      time.Time
}
