package archive

import (
	"errors"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

var (
	errSourceIdentity         = errors.New("session source file identity is invalid")
	errSourceDigest           = errors.New("session source file digest mismatch")
	errObjectWrite            = errors.New("put session archive object")
	errObjectReadback         = errors.New("read back session archive object")
	errObjectReadbackMismatch = errors.New("session archive object readback mismatch")
)

// Причина определяется идентичностью ошибки, а не её текстом или ответом SDK.
func ClassifyFailureStage(err error) model.FailureStage {
	switch {
	case errors.Is(err, errSourceIdentity):
		return model.FailureStageSourceIdentity
	case errors.Is(err, errSourceDigest):
		return model.FailureStageSourceDigest
	case errors.Is(err, errObjectWrite):
		return model.FailureStageObjectWrite
	case errors.Is(err, errObjectReadback), errors.Is(err, errObjectReadbackMismatch):
		return model.FailureStageObjectReadback
	default:
		return model.FailureStageUnknown
	}
}
