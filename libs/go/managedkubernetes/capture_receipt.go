package managedkubernetes

import (
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/opaqueref"
	"strings"
)

// CaptureState is the closed retained-source task lifecycle, separate from native outcomes.
// CaptureState — closed lifecycle retained-source task, отдельно от native outcomes.
type CaptureState string

// CaptureKind describes transcript coverage without asserting remote success or stop.
// CaptureKind описывает transcript coverage, не утверждая remote success либо stop.
type CaptureKind string

const (
	// CaptureReady awaits an independently fenced background attempt.
	// CaptureReady ожидает independently fenced background attempt.
	CaptureReady CaptureState = "READY"
	// CaptureClaimed owns one current background attempt.
	// CaptureClaimed имеет одну current background attempt.
	CaptureClaimed CaptureState = "CLAIMED"
	// CaptureComplete has an immutable final capture receipt.
	// CaptureComplete имеет immutable final capture receipt.
	CaptureComplete CaptureState = "COMPLETE"
	// CaptureUnknown preserves unresolved preparation or publication evidence.
	// CaptureUnknown сохраняет unresolved preparation либо publication evidence.
	CaptureUnknown CaptureState = "UNKNOWN"
	// CaptureRejected records a proven closed content/source refusal.
	// CaptureRejected фиксирует proven closed content/source refusal.
	CaptureRejected CaptureState = "REJECTED"
	// CaptureWaitingOwner preserves exhausted or administratively blocked work.
	// CaptureWaitingOwner сохраняет exhausted либо administratively blocked work.
	CaptureWaitingOwner CaptureState = "WAITING_OWNER"
	// CaptureSuperseded points to a newer immutable owner-ACK generation.
	// CaptureSuperseded означает newer immutable owner-ACK generation.
	CaptureSuperseded CaptureState = "SUPERSEDED"
	// CaptureEmpty proves a complete zero-byte stream without inventing an artifact.
	// CaptureEmpty подтверждает complete zero-byte stream без выдуманного artifact.
	CaptureEmpty CaptureKind = "EMPTY"
	// CaptureFull covers every admitted output plus positive native join.
	// CaptureFull охватывает every admitted output и positive native join.
	CaptureFull CaptureKind = "FULL"
	// CapturePartial contains only the immutable confirmed prefix.
	// CapturePartial содержит только immutable confirmed prefix.
	CapturePartial CaptureKind = "PARTIAL"
)

// CapturedArtifactReceipt names one canonical immutable revision, never a provider object locator.
// CapturedArtifactReceipt указывает canonical immutable revision, не provider object locator.
type CapturedArtifactReceipt struct {
	Channel           string `json:"channel"`
	ArtifactRef       string `json:"artifactRef"`
	RevisionRef       string `json:"revisionRef"`
	Digest            string `json:"digest"`
	SizeBytes         int64  `json:"sizeBytes"`
	CaptureGeneration int64  `json:"captureGeneration"`
	Partial           bool   `json:"partial"`
}

// CaptureTaskReceipt is metadata-only: it conveys neither output credit nor artifact read access.
// CaptureTaskReceipt содержит только metadata, без output credit либо artifact read access.
type CaptureTaskReceipt struct {
	CaptureRef, StreamRef, SnapshotDigest, TaskBindingDigest string
	TaskGeneration, CaptureGeneration                        int64
	State                                                    CaptureState
	Kind                                                     CaptureKind
	Artifacts                                                []CapturedArtifactReceipt
	ReasonCode                                               string
}

// ValidateFor checks the exact issued task binding and a closed, complete response variant.
// It cannot establish current artifact eligibility, storage durability or native execution authority.
// ValidateFor проверяет exact issued task binding и closed complete response variant.
// Current artifact eligibility, storage durability либо native execution authority не создаёт.
func (receipt CaptureTaskReceipt) ValidateFor(binding CaptureTaskBinding) error {
	digest, err := binding.Digest()
	if err != nil || receipt.CaptureRef != binding.CaptureRef || receipt.SnapshotDigest != binding.SnapshotDigest || receipt.TaskGeneration != binding.Generation || receipt.TaskBindingDigest != digest || !opaqueref.Valid(receipt.StreamRef, "kst") || receipt.CaptureGeneration < 1 || !ValidCaptureReason(receipt.ReasonCode) {
		return ErrInvalidCaptureTaskBinding
	}
	switch receipt.State {
	case CaptureReady, CaptureClaimed, CaptureComplete, CaptureUnknown, CaptureRejected, CaptureWaitingOwner, CaptureSuperseded:
	default:
		return ErrInvalidCaptureTaskBinding
	}
	if receipt.State != CaptureComplete {
		if receipt.Kind != "" || len(receipt.Artifacts) != 0 {
			return ErrInvalidCaptureTaskBinding
		}
		return nil
	}
	switch receipt.Kind {
	case CaptureEmpty:
		if len(receipt.Artifacts) != 0 {
			return ErrInvalidCaptureTaskBinding
		}
	case CaptureFull:
		if len(receipt.Artifacts) == 0 {
			return ErrInvalidCaptureTaskBinding
		}
	case CapturePartial:
	default:
		return ErrInvalidCaptureTaskBinding
	}
	if receipt.ReasonCode != "" || len(receipt.Artifacts) > 5 {
		return ErrInvalidCaptureTaskBinding
	}
	seen := map[string]bool{}
	for _, artifact := range receipt.Artifacts {
		channel := artifact.Channel
		if !(channel == "STDOUT" || channel == "STDERR" || channel == "CONTROL" || channel == "PORT_FORWARD" || channel == "FILE_CONTENT") || seen[channel] || !opaqueref.Valid(artifact.ArtifactRef, "art") || !opaqueref.Valid(artifact.RevisionRef, "arv") || !strings.HasPrefix(artifact.Digest, "sha256:") || !ValidDigest(strings.TrimPrefix(artifact.Digest, "sha256:")) || artifact.SizeBytes < 0 || artifact.SizeBytes > 536870912 || artifact.CaptureGeneration != receipt.CaptureGeneration || artifact.Partial != (receipt.Kind == CapturePartial) {
			return ErrInvalidCaptureTaskBinding
		}
		seen[channel] = true
	}
	return nil
}
