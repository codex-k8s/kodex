package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// KubernetesStreamOutputFrame contains only bounded untrusted data and immutable public provenance.
// KubernetesStreamOutputFrame содержит только ограниченные недоверенные данные и immutable public provenance.
type KubernetesStreamOutputFrame struct {
	StageRef, Channel, Digest string
	Sequence                  uint64
	Payload                   []byte
}

// KubernetesStreamOutput binds one disclosure to its exact cursor and independent deadline, not native execution credit.
// KubernetesStreamOutput связывает выдачу с точным cursor и отдельным сроком, не native execution credit.
type KubernetesStreamOutput struct {
	InvocationRef, StreamRef, Status             string
	Generation                                   int64
	AfterSequence, OutputWatermark, DroppedBytes uint64
	AuthorizedUntil                              time.Time
	NativeTerminalKnown, TransportJoined         bool
	Frame                                        *KubernetesStreamOutputFrame
}

// Valid rejects mismatched cursors, unknown status, oversized bodies and fabricated completion before transport conversion.
// Valid отклоняет неправильный cursor, неизвестный status, чрезмерный body и выдуманное завершение до transport conversion.
func (v KubernetesStreamOutput) Valid() bool {
	if v.InvocationRef == "" || v.StreamRef == "" || v.Generation < 1 || v.AfterSequence > v.OutputWatermark || v.OutputWatermark > managedkubernetes.MaximumFrameSequence || v.AuthorizedUntil.IsZero() || v.TransportJoined && !v.NativeTerminalKnown {
		return false
	}
	switch v.Status {
	case "FRAME":
		f := v.Frame
		if f == nil || f.StageRef == "" || f.Sequence != v.AfterSequence+1 || f.Sequence > v.OutputWatermark || len(f.Payload) == 0 || len(f.Payload) > managedkubernetes.MaximumFrameBytes || !managedkubernetes.ValidDigest(f.Digest) {
			return false
		}
		switch f.Channel {
		case "STDOUT", "STDERR", "CONTROL", "PORT_FORWARD", "FILE_CONTENT":
		default:
			return false
		}
		sum := sha256.Sum256(f.Payload)
		return hex.EncodeToString(sum[:]) == f.Digest
	case "WAITING", "INCOMPLETE":
		return v.Frame == nil
	case "ENDED":
		return v.Frame == nil && v.AfterSequence == v.OutputWatermark && v.DroppedBytes == 0 && v.NativeTerminalKnown && v.TransportJoined
	default:
		return false
	}
}
