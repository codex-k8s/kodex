package archive

import (
	"errors"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
)

var (
	// errSourceIdentity marks a source file or size identity mismatch before archiving.
	// RU: errSourceIdentity отмечает несовпадение идентичности файла либо размера до архивирования.
	errSourceIdentity = errors.New("session source file identity is invalid")
	// errSourceDigest marks source bytes that fail their declared digest or read boundary.
	// RU: errSourceDigest отмечает байты источника с неверным дайджестом либо нарушенной границей чтения.
	errSourceDigest = errors.New("session source file digest mismatch")
	// errObjectWrite marks a failed archive storage attempt without exposing an SDK error.
	// RU: errObjectWrite отмечает неудачную попытку сохранения архива без раскрытия ошибки SDK.
	errObjectWrite = errors.New("put session archive object")
	// errObjectReadback marks failure to retrieve the just-written archive object.
	// RU: errObjectReadback отмечает отказ получения только что записанного объекта архива.
	errObjectReadback = errors.New("read back session archive object")
	// errObjectReadbackMismatch marks a retrieved object that differs from its committed receipt.
	// RU: errObjectReadbackMismatch отмечает полученный объект, отличающийся от зафиксированной квитанции.
	errObjectReadbackMismatch = errors.New("session archive object readback mismatch")
)

// ClassifyFailureStage uses internal sentinel identity, including wrapped errors, to choose a closed diagnostic stage.
// RU: ClassifyFailureStage выбирает закрытый этап диагностики по идентичности внутренней ошибки, включая обёрнутые ошибки.
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
