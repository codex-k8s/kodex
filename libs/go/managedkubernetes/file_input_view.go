package managedkubernetes

import (
	"strings"
	"time"
)

// FileInputView exposes original source provenance and confirmed INPUT progress only.
// It contains no payload, private object locator, credential, raw fence or native execution credit.
// FileInputView раскрывает original source provenance и confirmed INPUT progress.
// Payload, private object locator, credential, raw fence либо native execution credit отсутствуют.
type FileInputView struct {
	SourceRef, InvocationRef, PlanRef, ChildRef, StreamRef string
	State                                                  string
	Version                                                int64
	ArtifactRef, ArtifactRevisionRef, ArtifactSHA256       string
	ArtifactSizeBytes                                      uint64
	TransferFormat, NormalizedSHA256                       string
	NormalizedSizeBytes                                    uint64
	NormalizationRevision                                  uint32
	StagedBytes, AcknowledgedBytes                         uint64
	EOFAcknowledged                                        bool
	Reason                                                 string
	CreatedAt, HardDeadline                                time.Time
}

// Valid closes the metadata representation without granting artifact visibility or execution rights.
// Historical expiry is retained; READY means private input material, never remote copy success.
// Valid закрывает metadata representation без выдачи artifact visibility либо execution rights.
// Historical expiry сохраняется; READY означает private input material, не remote copy success.
func (input FileInputView) Valid() bool {
	if !validFileInputViewRef(input.SourceRef, "kfs_") || !validFileInputViewRef(input.InvocationRef, "inv_") || !validFileInputViewRef(input.PlanRef, "kpl_") || !validFileInputViewRef(input.ChildRef, "kch_") || input.StreamRef != "" && !validFileInputViewRef(input.StreamRef, "kst_") || input.Version < 1 || input.ArtifactRef == "" || input.ArtifactRevisionRef == "" || !ValidLocator(input.ArtifactRef) || !ValidLocator(input.ArtifactRevisionRef) || !ValidDigest(input.ArtifactSHA256) || input.ArtifactSizeBytes > 16<<20 || input.TransferFormat != "FILE" && input.TransferFormat != "TAR" || input.CreatedAt.IsZero() || !input.HardDeadline.After(input.CreatedAt) {
		return false
	}
	switch input.State {
	case "PREPARING", "READY", "UNKNOWN", "REJECTED", "CANCELLED", "TRANSFERRED", "EXPIRED":
	default:
		return false
	}
	switch input.Reason {
	case "NONE", "SOURCE_UNAVAILABLE", "SOURCE_CHANGED", "PERMISSION_LOST", "CONTENT_REJECTED", "INPUT_UNKNOWN", "CANCELLED", "EXPIRED":
	default:
		return false
	}
	if input.NormalizedSHA256 == "" {
		return input.NormalizedSizeBytes == 0 && input.NormalizationRevision == 0 && input.StagedBytes == 0 && input.AcknowledgedBytes == 0 && !input.EOFAcknowledged && input.State != "READY" && input.State != "TRANSFERRED"
	}
	if !ValidDigest(input.NormalizedSHA256) || input.NormalizedSizeBytes < 1 || input.NormalizedSizeBytes > 16<<20 || input.NormalizationRevision != CopyArchiveNormalizationRevision || input.AcknowledgedBytes > input.StagedBytes || input.StagedBytes > input.NormalizedSizeBytes || input.EOFAcknowledged && input.AcknowledgedBytes != input.NormalizedSizeBytes {
		return false
	}
	return input.State != "TRANSFERRED" || input.EOFAcknowledged
}

// validFileInputViewRef validates only an opaque metadata shape, not ownership.
// validFileInputViewRef проверяет лишь opaque metadata shape, не ownership.
func validFileInputViewRef(ref, prefix string) bool {
	return len(ref) >= 12 && len(ref) <= 96 && strings.HasPrefix(ref, prefix) && ValidLocator(ref)
}
