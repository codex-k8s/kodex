package httptransport

import (
	"errors"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// errRunSessionReadinessShape is the safe fixed rejection for inconsistent public
// session-readiness data; it never embeds storage payloads or upstream errors.
// errRunSessionReadinessShape задаёт безопасный фиксированный отказ для несогласованных публичных
// данных готовности сессии без storage payload и upstream ошибок.
var errRunSessionReadinessShape = errors.New("public run session readiness shape is invalid")

// runSessionReadinessEnumPrefixes registers only session storage, readiness and archive-task enums
// owned by this projection normalizer, keeping unrelated enum processing outside this map.
// runSessionReadinessEnumPrefixes регистрирует только enum хранилища сессии, готовности и задачи
// архива, принадлежащие этому normalizer проекции; прочие enum обрабатываются снаружи.
var runSessionReadinessEnumPrefixes = map[protoreflect.FullName]string{
	"controlplane.v1.RunSessionStorageState":     "RUN_SESSION_STORAGE_STATE_",
	"controlplane.v1.RunSessionReadinessReason":  "RUN_SESSION_READINESS_REASON_",
	"controlplane.v1.RunSessionArchiveTaskState": "RUN_SESSION_ARCHIVE_TASK_STATE_",
	"controlplane.v1.SessionArchiveTaskKind":     "SESSION_ARCHIVE_TASK_KIND_",
}

// normalizeRunSessionReadinessEnum returns a public suffix and ownership flag for registered
// nonzero enum values; archive-task kind is owned only inside RunSessionArchiveTask.
// Unknown/unowned fields are distinguished from invalid owned values.
// normalizeRunSessionReadinessEnum возвращает публичный суффикс и признак владения для
// зарегистрированных ненулевых enum; вид задачи архива принадлежит пути только внутри
// RunSessionArchiveTask. Неприменимые поля различаются с неверными значениями принадлежащих полей.
func normalizeRunSessionReadinessEnum(value any, field protoreflect.FieldDescriptor) (any, bool, error) {
	if field.Enum().FullName() == "controlplane.v1.SessionArchiveTaskKind" && field.ContainingMessage().FullName() != "controlplane.v1.RunSessionArchiveTask" {
		return nil, false, nil
	}
	prefix, owned := runSessionReadinessEnumPrefixes[field.Enum().FullName()]
	if !owned {
		return nil, false, nil
	}
	name, ok := value.(string)
	item := field.Enum().Values().ByName(protoreflect.Name(name))
	if !ok || item == nil || item.Number() == 0 || !strings.HasPrefix(name, prefix) {
		return nil, true, errRunSessionReadinessShape
	}
	return strings.TrimPrefix(name, prefix), true, nil
}

// validateRunSessionReadinessShape checks a run/readiness session-ref match, the closed
// storage/reason relationship and exact archive-task field set with bounded attempts and safe
// error codes. It validates an owner read projection without changing storage, leases or archive
// tasks.
// validateRunSessionReadinessShape проверяет совпадение session-ref запуска и готовности, закрытую
// связь storage/reason и точный набор полей задачи архива с ограниченными attempts и безопасными
// кодами ошибок. Проверяется проекция чтения владельца без изменения storage, leases и задач
// архива.
func validateRunSessionReadinessShape(value map[string]any, descriptor protoreflect.MessageDescriptor) error {
	switch descriptor.FullName() {
	case "controlplane.v1.Run":
		if field, present := value["sessionReadiness"]; present {
			readiness, ok := field.(map[string]any)
			if !ok || readiness["sessionRef"] != value["sessionRef"] {
				return errRunSessionReadinessShape
			}
		}
	case "controlplane.v1.RunSessionReadiness":
		if len(value) < 3 || len(value) > 4 {
			return errRunSessionReadinessShape
		}
		for key := range value {
			switch key {
			case "sessionRef", "storageState", "reason", "latestArchiveTask":
			default:
				return errRunSessionReadinessShape
			}
		}
		ref, ok := value["sessionRef"].(string)
		if !ok || !fileTargetRef(ref) {
			return errRunSessionReadinessShape
		}
		storage := value["storageState"]
		switch storage {
		case "UNTRACKED", "LIVE", "SNAPSHOT_READY", "SNAPSHOTTING", "DELETE_PVC_READY", "ARCHIVED", "RESTORE_READY", "RESTORING", "ERROR", "PURGED":
		default:
			return errRunSessionReadinessShape
		}
		if task, present := value["latestArchiveTask"]; present {
			if _, ok := task.(map[string]any); !ok {
				return errRunSessionReadinessShape
			}
		}
		reason := value["reason"]
		switch reason {
		case "STORAGE_NOT_LIVE", "SESSION_ACCOUNT_UNAVAILABLE", "EARLIER_EXECUTION", "NO_SESSION_BLOCKER":
		default:
			return errRunSessionReadinessShape
		}
		live := storage == "LIVE" || storage == "UNTRACKED"
		if (reason == "STORAGE_NOT_LIVE") == live {
			return errRunSessionReadinessShape
		}
	case "controlplane.v1.RunSessionArchiveTask":
		if len(value) != 6 {
			return errRunSessionReadinessShape
		}
		for key := range value {
			switch key {
			case "ref", "kind", "state", "attempt", "maximumAttempts", "safeErrorCode":
			default:
				return errRunSessionReadinessShape
			}
		}
		ref, ok := value["ref"].(string)
		if !ok || !fileTargetRef(ref) {
			return errRunSessionReadinessShape
		}
		switch value["kind"] {
		case "SNAPSHOT", "RESTORE", "DELETE_PVC":
		default:
			return errRunSessionReadinessShape
		}
		switch value["state"] {
		case "READY", "CLAIMED", "SUCCEEDED", "DEAD_LETTER", "CANCELLED":
		default:
			return errRunSessionReadinessShape
		}
		attempt, aok := value["attempt"].(float64)
		maximum, mok := value["maximumAttempts"].(float64)
		if !aok || !mok || maximum < 1 || maximum > 5 || attempt < 0 || attempt > maximum || attempt != float64(int32(attempt)) || maximum != float64(int32(maximum)) {
			return errRunSessionReadinessShape
		}
		switch value["safeErrorCode"] {
		case "NONE", "UNKNOWN", "SESSION_ARCHIVE_SOURCE_INVALID", "SESSION_ARCHIVE_OBJECT_WRITE_FAILED", "SESSION_ARCHIVE_OBJECT_READBACK_FAILED", "SESSION_ARCHIVE_OBJECT_DELETE_FAILED", "SESSION_ARCHIVE_RESTORE_INVALID", "SESSION_ARCHIVE_PVC_BUSY", "SESSION_ARCHIVE_PVC_MISSING", "SESSION_ARCHIVE_PVC_REPLACED", "SESSION_ARCHIVE_KUBERNETES_UNAVAILABLE", "SESSION_ARCHIVE_WORKER_FAILED", "SESSION_ARCHIVE_TIMEOUT", "SESSION_ARCHIVE_LEASE_EXPIRED", "SESSION_BECAME_ACTIVE":
		default:
			return errRunSessionReadinessShape
		}
	}
	return nil
}
