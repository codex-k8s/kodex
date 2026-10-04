package httptransport

import (
	"errors"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

var errRunSessionReadinessShape = errors.New("public run session readiness shape is invalid")

var runSessionReadinessEnumPrefixes = map[protoreflect.FullName]string{
	"controlplane.v1.RunSessionStorageState":     "RUN_SESSION_STORAGE_STATE_",
	"controlplane.v1.RunSessionReadinessReason":  "RUN_SESSION_READINESS_REASON_",
	"controlplane.v1.RunSessionArchiveTaskState": "RUN_SESSION_ARCHIVE_TASK_STATE_",
	"controlplane.v1.SessionArchiveTaskKind":     "SESSION_ARCHIVE_TASK_KIND_",
}

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
