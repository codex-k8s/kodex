package model

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	// FormatVersion identifies the supported archive manifest and payload layout.
	// RU: FormatVersion определяет поддерживаемый формат манифеста и содержимого архива.
	FormatVersion = 1
	// MaximumTaskBytes limits accepted task files and embedded archive manifest bytes.
	// RU: MaximumTaskBytes ограничивает принимаемые файлы заданий и байты встроенного манифеста архива.
	MaximumTaskBytes = 64 << 10
	// MaximumSourceBytes bounds the source session file accepted by a task.
	// RU: MaximumSourceBytes ограничивает размер исходного файла сессии, принимаемого заданием.
	MaximumSourceBytes int64 = 64 << 20
	// MaximumObjectBytes reserves bounded TAR overhead above the source-file budget.
	// RU: MaximumObjectBytes оставляет ограниченный запас TAR сверх бюджета исходного файла.
	MaximumObjectBytes int64 = 68 << 20
	// SafeErrorPVCMissing names the closed failure for an absent session volume.
	// RU: SafeErrorPVCMissing задаёт закрытый код отказа при отсутствии тома сессии.
	SafeErrorPVCMissing = "SESSION_ARCHIVE_PVC_MISSING"
	// SafeErrorPVCReplaced names the closed failure when a volume no longer matches its binding.
	// RU: SafeErrorPVCReplaced задаёт закрытый код отказа при несовпадении тома с его привязкой.
	SafeErrorPVCReplaced = "SESSION_ARCHIVE_PVC_REPLACED"
)

// ArchiveBinding carries the archive object receipt and original source metadata pinned by its owner.
// RU: ArchiveBinding переносит квитанцию объекта архива и метаданные исходного файла, закреплённые владельцем.
type ArchiveBinding struct {
	ArchiveRef         string `json:"archive_ref"`
	FormatVersion      uint32 `json:"format_version"`
	ObjectKey          string `json:"object_key"`
	ObjectVersion      string `json:"object_version"`
	ObjectETag         string `json:"object_etag"`
	ObjectDigest       string `json:"object_digest"`
	ObjectSizeBytes    int64  `json:"object_size_bytes"`
	SourceRelativePath string `json:"source_relative_path"`
	SourceSHA256       string `json:"source_sha256"`
	SourceSizeBytes    int64  `json:"source_size_bytes"`
}

// Task binds one archive action to its session, runtime revision, content generation, input and attempt.
// RU: Task связывает действие архива с сессией, runtime-ревизией, поколением содержимого, входом и попыткой.
type Task struct {
	TaskRef, Kind, OrganizationRef, ProjectRef, SessionRef string
	ProviderAccountRef, RuntimeRevisionRef                 string
	RuntimeRevisionVersion, ContentGeneration              int64
	RuntimeRevisionDigest, CodexSessionID                  string
	PVCName, InputDigest                                   string
	SourceRelativePath, SourceSHA256                       string
	SourceSizeBytes                                        int64
	TargetObjectKey, TargetObjectVersion                   string
	Attempt                                                int32
	Archive                                                *ArchiveBinding
}

// wireTask preserves Task's JSON field layout without recursively calling its custom codec methods.
// RU: wireTask сохраняет JSON-поля Task, не вызывая рекурсивно его собственные методы кодирования.
type wireTask Task

// MarshalJSON serializes the task's wire fields without changing the bound values.
// RU: MarshalJSON сериализует wire-поля задания без изменения закреплённых значений.
func (task Task) MarshalJSON() ([]byte, error) { return json.Marshal(wireTask(task)) }

// UnmarshalJSON decodes the task wire layout; callers separately apply Validate before use.
// RU: UnmarshalJSON декодирует wire-поля задания; до использования вызывающая сторона отдельно применяет Validate.
func (task *Task) UnmarshalJSON(raw []byte) error { return json.Unmarshal(raw, (*wireTask)(task)) }

// FromProto maps the closed RPC task kinds and optional archive receipt, then validates the resulting binding.
// RU: FromProto преобразует закрытые виды RPC-заданий и необязательную квитанцию архива, затем проверяет привязку.
func FromProto(input *controlplanev1.SessionArchiveTask) (Task, error) {
	if input == nil {
		return Task{}, errors.New("session archive task is missing")
	}
	kinds := map[controlplanev1.SessionArchiveTaskKind]string{
		controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_SNAPSHOT:      "SNAPSHOT",
		controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_RESTORE:       "RESTORE",
		controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_DELETE_PVC:    "DELETE_PVC",
		controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_DELETE_OBJECT: "DELETE_OBJECT",
	}
	task := Task{TaskRef: input.GetTaskRef(), Kind: kinds[input.GetKind()], OrganizationRef: input.GetOrganizationRef(),
		ProjectRef: input.GetProjectRef(), SessionRef: input.GetSessionRef(), ProviderAccountRef: input.GetProviderAccountRef(),
		RuntimeRevisionRef: input.GetRuntimeRevisionRef(), RuntimeRevisionVersion: input.GetRuntimeRevisionVersion(),
		RuntimeRevisionDigest: input.GetRuntimeRevisionDigest(), CodexSessionID: input.GetCodexSessionId(),
		ContentGeneration: input.GetContentGeneration(), PVCName: input.GetPvcName(), InputDigest: input.GetInputDigest(),
		SourceRelativePath: input.GetSourceRelativePath(), SourceSHA256: input.GetSourceSha256(),
		SourceSizeBytes: input.GetSourceSizeBytes(), TargetObjectKey: input.GetTargetObjectKey(),
		TargetObjectVersion: input.GetTargetObjectVersion(), Attempt: input.GetAttempt()}
	if archive := input.GetArchive(); archive != nil {
		task.Archive = &ArchiveBinding{ArchiveRef: archive.GetArchiveRef(), FormatVersion: archive.GetFormatVersion(),
			ObjectKey: archive.GetObjectKey(), ObjectVersion: archive.GetObjectVersion(), ObjectETag: archive.GetObjectEtag(),
			ObjectDigest: archive.GetObjectDigest(), ObjectSizeBytes: archive.GetObjectSizeBytes(),
			SourceRelativePath: archive.GetSourceRelativePath(), SourceSHA256: archive.GetSourceSha256(),
			SourceSizeBytes: archive.GetSourceSizeBytes()}
	}
	return task, task.Validate()
}

// DecodeFile rejects empty or oversized task bytes and validates the decoded task before returning it.
// RU: DecodeFile отклоняет пустые либо чрезмерные байты задания и до возврата проверяет декодированное задание.
func DecodeFile(path string) (Task, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > MaximumTaskBytes {
		return Task{}, errors.New("read session archive task")
	}
	var task Task
	if json.Unmarshal(raw, &task) != nil || task.Validate() != nil {
		return Task{}, errors.New("decode session archive task")
	}
	return task, nil
}

// Validate checks required identities, size and generation bounds, canonical session path and kind-specific fields.
// RU: Validate проверяет обязательные идентичности, размеры, поколения, канонический путь сессии и поля вида задания.
func (task Task) Validate() error {
	for _, required := range []string{task.TaskRef, task.Kind, task.OrganizationRef, task.SessionRef,
		task.ProviderAccountRef, task.RuntimeRevisionRef, task.RuntimeRevisionDigest, task.CodexSessionID,
		task.PVCName, task.InputDigest, task.SourceRelativePath, task.SourceSHA256} {
		if strings.TrimSpace(required) == "" || len(required) > 1024 {
			return errors.New("session archive task identity is invalid")
		}
	}
	if task.RuntimeRevisionVersion < 1 || task.ContentGeneration < 1 || task.Attempt < 1 ||
		task.SourceSizeBytes < 1 || task.SourceSizeBytes > MaximumSourceBytes ||
		len(task.RuntimeRevisionDigest) != 64 || len(task.SourceSHA256) != 64 || len(task.InputDigest) != 64 ||
		runtimecontract.ValidateCodexArchiveIdentity(task.CodexSessionID, task.SourceRelativePath) != nil {
		return errors.New("session archive task binding is invalid")
	}
	expectedPVCName, err := runtimecontract.SessionPVCName(task.SessionRef)
	if err != nil || task.PVCName != expectedPVCName {
		return errors.New("session archive PVC binding is invalid")
	}
	switch task.Kind {
	case "SNAPSHOT":
		if task.TargetObjectKey == "" || task.Archive != nil {
			return errors.New("session snapshot task is invalid")
		}
	case "RESTORE":
		if task.Archive == nil || task.Archive.Validate() != nil {
			return errors.New("session restore task is invalid")
		}
	case "DELETE_PVC":
		if task.Archive == nil || task.Archive.Validate() != nil {
			return errors.New("session PVC deletion task is invalid")
		}
	case "DELETE_OBJECT":
		if task.TargetObjectKey == "" {
			return errors.New("session object deletion task is invalid")
		}
	default:
		return errors.New("session archive task kind is invalid")
	}
	return nil
}

// Validate checks the supported format, required receipt fields and source/object size and digest shapes.
// RU: Validate проверяет поддерживаемый формат, обязательные поля квитанции, размеры и форму дайджестов источника и объекта.
func (binding ArchiveBinding) Validate() error {
	if binding.ArchiveRef == "" || binding.FormatVersion != FormatVersion || binding.ObjectKey == "" ||
		binding.ObjectETag == "" || !strings.HasPrefix(binding.ObjectDigest, "sha256:") ||
		len(binding.ObjectDigest) != 71 || binding.ObjectSizeBytes < 1 || binding.ObjectSizeBytes > MaximumObjectBytes ||
		binding.SourceRelativePath == "" || len(binding.SourceSHA256) != 64 ||
		binding.SourceSizeBytes < 1 || binding.SourceSizeBytes > MaximumSourceBytes {
		return errors.New("session archive binding is invalid")
	}
	return nil
}

// FailureStage is a closed diagnostic stage independent of provider error text.
// RU: FailureStage представляет закрытый этап диагностики, независимый от текста ошибки провайдера.
type FailureStage string

const (
	// FailureStageUnknown represents failures without a recognized internal archive sentinel.
	// RU: FailureStageUnknown обозначает отказ без распознанной внутренней ошибки-маркера архива.
	FailureStageUnknown FailureStage = "UNKNOWN"
	// FailureStageSourceIdentity identifies a mismatch in the observed source file identity.
	// RU: FailureStageSourceIdentity обозначает несовпадение наблюдаемой идентичности исходного файла.
	FailureStageSourceIdentity FailureStage = "SOURCE_IDENTITY"
	// FailureStageSourceDigest identifies mismatched source bytes or their declared size.
	// RU: FailureStageSourceDigest обозначает несовпадение исходных байтов либо объявленного размера.
	FailureStageSourceDigest FailureStage = "SOURCE_DIGEST"
	// FailureStageObjectWrite identifies failure to persist an archive object.
	// RU: FailureStageObjectWrite обозначает отказ сохранения объекта архива.
	FailureStageObjectWrite FailureStage = "OBJECT_WRITE"
	// FailureStageObjectReadback identifies archive-object readback or receipt mismatch.
	// RU: FailureStageObjectReadback обозначает отказ перечитывания архива либо несовпадение квитанции.
	FailureStageObjectReadback FailureStage = "OBJECT_READBACK"
)

// NormalizeFailureStage maps unknown diagnostic input to the single bounded fallback.
// RU: NormalizeFailureStage отображает неизвестный диагностический ввод в единственное ограниченное запасное значение.
func NormalizeFailureStage(stage FailureStage) FailureStage {
	switch stage {
	case FailureStageSourceIdentity, FailureStageSourceDigest, FailureStageObjectWrite, FailureStageObjectReadback:
		return stage
	default:
		return FailureStageUnknown
	}
}

// Result reports a closed outcome and the archive/source receipt values observed by the worker.
// RU: Result сообщает закрытый исход и значения квитанций архива и источника, наблюдавшиеся worker.
type Result struct {
	Success         bool         `json:"success"`
	SafeErrorCode   string       `json:"safe_error_code,omitempty"`
	FailureStage    FailureStage `json:"failure_stage,omitempty"`
	FormatVersion   uint32       `json:"format_version,omitempty"`
	ObjectKey       string       `json:"object_key,omitempty"`
	ObjectVersion   string       `json:"object_version,omitempty"`
	ObjectETag      string       `json:"object_etag,omitempty"`
	ObjectDigest    string       `json:"object_digest,omitempty"`
	ObjectSizeBytes int64        `json:"object_size_bytes,omitempty"`
	SourceSHA256    string       `json:"source_sha256,omitempty"`
	SourceSizeBytes int64        `json:"source_size_bytes,omitempty"`
}
