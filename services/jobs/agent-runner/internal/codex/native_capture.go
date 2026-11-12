package codex

import (
	"os"
	"path/filepath"
	"syscall"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// nativeCaptureSchema identifies the current native export proof; no legacy archive decoder is used.
// nativeCaptureSchema обозначает доказательство текущего native export; legacy archive decoder не используется.
const nativeCaptureSchema = "kodex.native-session-capture.v1"

// nativeCaptureProof relates an observed export to one exact execution; its seal is never JSON authority.
// nativeCaptureProof связывает наблюдаемый export с exact execution; seal не является JSON authority.
type nativeCaptureProof struct {
	Schema                 string                                  `json:"schema"`
	ExecutionBindingDigest string                                  `json:"execution_binding_digest"`
	RuntimeRevisionDigest  string                                  `json:"runtime_revision_digest"`
	InputDigest            string                                  `json:"input_digest"`
	LeaseRef               string                                  `json:"lease_ref"`
	LeaseGeneration        int64                                   `json:"lease_generation"`
	OrganizationRef        string                                  `json:"organization_ref"`
	ProjectRef             string                                  `json:"project_ref"`
	AgentRef               string                                  `json:"agent_ref"`
	SessionRef             string                                  `json:"session_ref"`
	TurnRef                string                                  `json:"turn_ref"`
	Attempt                int32                                   `json:"attempt"`
	Checkpoint             runtimecontract.NativeSessionCheckpoint `json:"checkpoint"`
	sealed                 bool
}

// bound checks exact current metadata without trusting a caller-assigned seal or observing file bytes.
// bound проверяет exact current metadata без доверия к caller-assigned seal или наблюдения file bytes.
func (proof *nativeCaptureProof) bound(input model.Input) bool {
	return proof != nil && proof.Schema == nativeCaptureSchema && proof.Checkpoint.ValidateCompletion(input) == nil &&
		validCaptureDigest(input.ExecutionBindingDigest) && validCaptureDigest(input.RuntimeRevisionDigest) && validCaptureDigest(input.InputDigest) &&
		proof.ExecutionBindingDigest == input.ExecutionBindingDigest && proof.RuntimeRevisionDigest == input.RuntimeRevisionDigest && proof.InputDigest == input.InputDigest &&
		input.LeaseRef != "" && proof.LeaseRef == input.LeaseRef && input.LeaseGeneration > 0 && proof.LeaseGeneration == input.LeaseGeneration &&
		input.OrganizationRef != "" && proof.OrganizationRef == input.OrganizationRef && proof.ProjectRef == input.ProjectRef && input.AgentRef != "" && proof.AgentRef == input.AgentRef &&
		input.Attempt > 0 && proof.Attempt == input.Attempt && input.SessionRef != "" && proof.SessionRef == input.SessionRef && input.TurnRef != "" && proof.TurnRef == input.TurnRef
}

// HasVerifiedNativeCheckpoint requires a private seal and exact input/result bindings, not exported fields.
// HasVerifiedNativeCheckpoint требует private seal и exact input/result bindings, а не exported fields.
func (result Result) HasVerifiedNativeCheckpoint(input model.Input) bool {
	return result.nativeCapture != nil && result.nativeCapture.sealed && result.nativeCapture.bound(input) && result.NativeSession != nil && *result.NativeSession == result.nativeCapture.Checkpoint
}

// verifyNativeCapture reopens and hashes a single-link published export under the trusted workspace root.
// It verifies the provider UID and runtime-only group; caller-supplied paths never select another source.
// verifyNativeCapture повторно открывает и хеширует single-link export под trusted workspace root.
// Проверяет provider UID и runtime-only group; caller paths не выбирают другой source.
func verifyNativeCapture(input model.Input, result Result, proof *nativeCaptureProof, writerUID uint32) error {
	if writerUID != providerWriterUID || !proof.bound(input) || result.NativeSession == nil || *result.NativeSession != proof.Checkpoint {
		return errProviderBrokerResponseInvalid
	}
	path := filepath.Join(input.WorkspaceRoot, filepath.FromSlash(proof.Checkpoint.RelativePath))
	file, info, err := openProtectedFile(input.WorkspaceRoot, path)
	if err != nil {
		return errProviderBrokerResponseInvalid
	}
	defer file.Close()
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != writerUID || stat.Gid != 29000 || info.Mode().Perm() != 0o640 || info.Size() != proof.Checkpoint.SizeBytes {
		return errProviderBrokerResponseInvalid
	}
	digest, err := digestArchive(file, info)
	if err != nil || digest != proof.Checkpoint.SHA256 {
		return errProviderBrokerResponseInvalid
	}
	current, currentInfo, err := openProtectedFile(input.WorkspaceRoot, path)
	if err != nil {
		return errProviderBrokerResponseInvalid
	}
	defer current.Close()
	currentStat, currentOK := currentInfo.Sys().(*syscall.Stat_t)
	if !currentOK || !os.SameFile(info, currentInfo) || currentInfo.Size() != info.Size() || currentInfo.Mode() != info.Mode() || currentStat.Uid != stat.Uid || currentStat.Gid != stat.Gid || currentStat.Nlink != stat.Nlink || currentStat.Mtim != stat.Mtim || currentStat.Ctim != stat.Ctim {
		return errProviderBrokerResponseInvalid
	}
	return nil
}
