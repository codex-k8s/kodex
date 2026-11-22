package managedkubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/opaqueref"
)

// ErrInvalidCaptureTaskBinding rejects a malformed artifact task without exposing its fence.
// ErrInvalidCaptureTaskBinding отклоняет неверную artifact task без раскрытия fence.
var ErrInvalidCaptureTaskBinding = errors.New("managed Kubernetes capture task binding is invalid")

// CaptureContractRevision identifies the exact background capture task contract, independently
// from mixed invocation delivery, live output credit and input staging revisions.
// CaptureContractRevision определяет exact background capture task contract отдельно от mixed
// invocation delivery, live output credit и input staging revisions.
const CaptureContractRevision uint32 = 1

// CaptureTaskBinding belongs only to artifact finalization. It cannot be converted into a native
// invocation, stream or frame grant; the owner independently validates the current task attempt.
// CaptureTaskBinding относится только к artifact finalization. Не преобразуется в native invocation,
// stream либо frame grant; owner независимо проверяет текущий task attempt.
type CaptureTaskBinding struct {
	CaptureRef       string
	LeaseRef         string
	Fence            string `json:"-"`
	Generation       int64
	WorkloadInstance string
	SnapshotDigest   string
}

// Validate checks representable owner-issued coordinates, not task ownership or authority.
// Validate проверяет представимые owner-issued coordinates, не task ownership либо authority.
func (binding CaptureTaskBinding) Validate() error {
	if !opaqueref.Valid(binding.CaptureRef, "koc") || !opaqueref.Valid(binding.LeaseRef, "kca") || len(binding.Fence) < 32 || len(binding.Fence) > 256 || !ValidLocator(binding.Fence) || binding.Generation < 1 || len(binding.WorkloadInstance) > 128 || !ValidLocator(binding.WorkloadInstance) || !ValidDigest(binding.SnapshotDigest) {
		return ErrInvalidCaptureTaskBinding
	}
	return nil
}

// FenceDigest is the one-way commitment stored by the task owner, never an execution credential.
// FenceDigest — односторонний commitment владельца task, не execution credential.
func (binding CaptureTaskBinding) FenceDigest() string {
	digest := sha256.Sum256([]byte(binding.Fence))
	return hex.EncodeToString(digest[:])
}

// Digest commits the complete validated task incarnation with a one-way fence digest.
// Digest фиксирует complete validated task incarnation с односторонним fence digest.
func (binding CaptureTaskBinding) Digest() (string, error) {
	if binding.Validate() != nil {
		return "", ErrInvalidCaptureTaskBinding
	}
	raw, err := json.Marshal(struct {
		CaptureRef, LeaseRef, FenceDigest, WorkloadInstance, SnapshotDigest string
		Generation                                                          int64
	}{binding.CaptureRef, binding.LeaseRef, binding.FenceDigest(), binding.WorkloadInstance, binding.SnapshotDigest, binding.Generation})
	if err != nil {
		return "", ErrInvalidCaptureTaskBinding
	}
	sum := sha256.Sum256(append([]byte("kodex.kubernetes.capture-task-binding/v1\n"), raw...))
	return hex.EncodeToString(sum[:]), nil
}

// String suppresses the artifact task fence in ordinary formatting.
// String скрывает artifact task fence при обычном форматировании.
func (binding CaptureTaskBinding) String() string { return "[managed Kubernetes capture task binding]" }

// GoString preserves fence redaction in diagnostic Go formatting.
// GoString сохраняет скрытие fence при диагностическом Go formatting.
func (binding CaptureTaskBinding) GoString() string { return binding.String() }

// ValidCaptureReason admits only bounded artifact-task reason codes, never raw provider diagnostics.
// ValidCaptureReason принимает только closed artifact-task reason codes, не raw provider diagnostics.
func ValidCaptureReason(reason string) bool {
	switch reason {
	case "", "CAPTURE_ELIGIBILITY_CHANGED", "CAPTURE_SOURCE_INVALID", "CAPTURE_STORAGE_UNKNOWN", "CAPTURE_RETRY_EXHAUSTED", "CAPTURE_RETENTION_EXPIRED", "CAPTURE_CONTENT_REJECTED", "CAPTURE_SUPERSEDED":
		return true
	default:
		return false
	}
}
