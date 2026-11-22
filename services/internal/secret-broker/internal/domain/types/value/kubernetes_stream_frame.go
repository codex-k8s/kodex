package value

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// KubernetesStreamGrant is immutable owner credit, not authority assigned by a transport caller.
// KubernetesStreamGrant содержит immutable owner credit, а не полномочия transport caller.
type KubernetesStreamGrant struct {
	Ref                           string
	Generation                    int64
	ExpiresAt                     time.Time
	MaximumFrameBytes             uint32
	MaximumTotalBytes             uint64
	InputChannels, OutputChannels []string
}

// Valid checks finite local bounds without replacing current owner admission for each frame.
// Valid проверяет конечные local bounds, не заменяя current owner admission каждого frame.
func (grant KubernetesStreamGrant) Valid(now time.Time) bool {
	if !managedkubernetes.ValidLocator(grant.Ref) || grant.Generation < 1 || !grant.ExpiresAt.After(now) ||
		grant.MaximumFrameBytes == 0 || grant.MaximumFrameBytes > managedkubernetes.MaximumFrameBytes ||
		grant.MaximumTotalBytes < uint64(grant.MaximumFrameBytes) || grant.MaximumTotalBytes > managedkubernetes.MaximumStreamBytes {
		return false
	}
	for _, channels := range [][]string{grant.InputChannels, grant.OutputChannels} {
		if len(channels) > 7 {
			return false
		}
		for index, channel := range channels {
			if !managedkubernetes.ValidChannel(channel) || slices.Contains(channels[:index], channel) {
				return false
			}
		}
	}
	return len(grant.InputChannels)+len(grant.OutputChannels) > 0
}

// KubernetesStreamFrame owns only transport payload; owner RPCs receive its separate metadata.
// KubernetesStreamFrame хранит только transport payload; owner RPC получает отдельные metadata.
type KubernetesStreamFrame struct {
	Sequence      uint64
	Channel       string
	Payload       []byte `json:"-"`
	PayloadDigest string
	EndOfInput    bool
	// AuthorizedUntil bounds one freshly admitted OUTPUT delivery; INPUT never consumes it.
	// AuthorizedUntil ограничивает одну fresh OUTPUT delivery; INPUT его не использует.
	AuthorizedUntil time.Time
	// DeliveryOrdinal identifies this owner-issued OUTPUT delivery; INPUT must leave it zero.
	// DeliveryOrdinal обозначает эту owner-issued OUTPUT delivery; INPUT оставляет ноль.
	DeliveryOrdinal uint32
}

// ValidInput verifies exact bytes and the immutable input channel set before private owner I/O.
// ValidInput сверяет exact bytes и immutable input channels до private owner I/O.
func (frame KubernetesStreamFrame) ValidInput(grant KubernetesStreamGrant, now time.Time) bool {
	if !grant.Valid(now) || !managedkubernetes.ValidFrameSequence(frame.Sequence) || !slices.Contains(grant.InputChannels, frame.Channel) ||
		len(frame.Payload) > int(grant.MaximumFrameBytes) || !managedkubernetes.ValidDigest(frame.PayloadDigest) || !frame.AuthorizedUntil.IsZero() || frame.DeliveryOrdinal != 0 {
		return false
	}
	digest := sha256.Sum256(frame.Payload)
	return hex.EncodeToString(digest[:]) == frame.PayloadDigest
}

// String keeps payload and private pins out of ordinary diagnostics.
// String скрывает payload и private pins из обычной диагностики.
func (KubernetesStreamFrame) String() string { return "[managed Kubernetes private frame]" }

// GoString preserves the same diagnostic boundary under detailed formatting.
// GoString сохраняет ту же diagnostic boundary при подробном форматировании.
func (frame KubernetesStreamFrame) GoString() string { return frame.String() }

// KubernetesFrameMetadata is the payload-free immutable owner frame intent.
// KubernetesFrameMetadata содержит immutable owner frame intent без payload.
type KubernetesFrameMetadata struct {
	StreamRef, Direction, Channel, PayloadDigest string
	Sequence                                     uint64
	ByteCount                                    uint32
	EndOfInput                                   bool
	// NativeAuthorizedUntil is fresh broker evidence, excluded from immutable frame identity.
	// NativeAuthorizedUntil является свежим broker evidence вне immutable frame identity.
	NativeAuthorizedUntil time.Time
	// PortForward binds native OUTPUT to one original owner connection/event; INPUT resolves its source at the owner.
	// PortForward связывает native OUTPUT с original owner connection/event; INPUT разрешает source у owner.
	PortForward *managedkubernetes.PortForwardConnectionWitness
}

// KubernetesFrameAdmission distinguishes first-send credit from receipt replay and uncertainty.
// KubernetesFrameAdmission различает first-send credit, receipt replay и uncertainty.
type KubernetesFrameAdmission struct {
	Disposition          string
	ExpiresAt            time.Time
	DeliveryOrdinal      uint32
	PortForwardRetention *managedkubernetes.PortForwardRetentionSet
}
