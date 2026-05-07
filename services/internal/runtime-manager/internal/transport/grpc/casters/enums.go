package casters

import (
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
)

// RuntimeModeFromProto maps the runtime mode enum.
func RuntimeModeFromProto(mode runtimev1.RuntimeMode) (enum.RuntimeMode, error) {
	switch mode {
	case runtimev1.RuntimeMode_RUNTIME_MODE_CODE_ONLY:
		return enum.RuntimeModeCodeOnly, nil
	case runtimev1.RuntimeMode_RUNTIME_MODE_FULL_ENV:
		return enum.RuntimeModeFullEnv, nil
	case runtimev1.RuntimeMode_RUNTIME_MODE_READ_ONLY_PRODUCTION:
		return enum.RuntimeModeReadOnlyProduction, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

// RuntimeModeToProto maps the runtime mode enum.
func RuntimeModeToProto(mode enum.RuntimeMode) runtimev1.RuntimeMode {
	switch mode {
	case enum.RuntimeModeCodeOnly:
		return runtimev1.RuntimeMode_RUNTIME_MODE_CODE_ONLY
	case enum.RuntimeModeFullEnv:
		return runtimev1.RuntimeMode_RUNTIME_MODE_FULL_ENV
	case enum.RuntimeModeReadOnlyProduction:
		return runtimev1.RuntimeMode_RUNTIME_MODE_READ_ONLY_PRODUCTION
	default:
		return runtimev1.RuntimeMode_RUNTIME_MODE_UNSPECIFIED
	}
}

// SlotStatusFromProto maps slot status filters.
func SlotStatusFromProto(status runtimev1.SlotStatus) (enum.SlotStatus, error) {
	switch status {
	case runtimev1.SlotStatus_SLOT_STATUS_PREWARMED:
		return enum.SlotStatusPrewarmed, nil
	case runtimev1.SlotStatus_SLOT_STATUS_RESERVED:
		return enum.SlotStatusReserved, nil
	case runtimev1.SlotStatus_SLOT_STATUS_MATERIALIZING:
		return enum.SlotStatusMaterializing, nil
	case runtimev1.SlotStatus_SLOT_STATUS_READY:
		return enum.SlotStatusReady, nil
	case runtimev1.SlotStatus_SLOT_STATUS_IN_USE:
		return enum.SlotStatusInUse, nil
	case runtimev1.SlotStatus_SLOT_STATUS_RELEASING:
		return enum.SlotStatusReleasing, nil
	case runtimev1.SlotStatus_SLOT_STATUS_FAILED:
		return enum.SlotStatusFailed, nil
	case runtimev1.SlotStatus_SLOT_STATUS_CLEANUP_PENDING:
		return enum.SlotStatusCleanupPending, nil
	case runtimev1.SlotStatus_SLOT_STATUS_CLEANED:
		return enum.SlotStatusCleaned, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

// SlotStatusToProto maps slot status.
func SlotStatusToProto(status enum.SlotStatus) runtimev1.SlotStatus {
	switch status {
	case enum.SlotStatusPrewarmed:
		return runtimev1.SlotStatus_SLOT_STATUS_PREWARMED
	case enum.SlotStatusReserved:
		return runtimev1.SlotStatus_SLOT_STATUS_RESERVED
	case enum.SlotStatusMaterializing:
		return runtimev1.SlotStatus_SLOT_STATUS_MATERIALIZING
	case enum.SlotStatusReady:
		return runtimev1.SlotStatus_SLOT_STATUS_READY
	case enum.SlotStatusInUse:
		return runtimev1.SlotStatus_SLOT_STATUS_IN_USE
	case enum.SlotStatusReleasing:
		return runtimev1.SlotStatus_SLOT_STATUS_RELEASING
	case enum.SlotStatusFailed:
		return runtimev1.SlotStatus_SLOT_STATUS_FAILED
	case enum.SlotStatusCleanupPending:
		return runtimev1.SlotStatus_SLOT_STATUS_CLEANUP_PENDING
	case enum.SlotStatusCleaned:
		return runtimev1.SlotStatus_SLOT_STATUS_CLEANED
	default:
		return runtimev1.SlotStatus_SLOT_STATUS_UNSPECIFIED
	}
}

// WorkspaceMaterializationStatusFromProto maps materialization status.
func WorkspaceMaterializationStatusFromProto(status runtimev1.WorkspaceMaterializationStatus) (enum.WorkspaceMaterializationStatus, error) {
	switch status {
	case runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_PENDING:
		return enum.WorkspaceMaterializationStatusPending, nil
	case runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_RUNNING:
		return enum.WorkspaceMaterializationStatusRunning, nil
	case runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_COMPLETED:
		return enum.WorkspaceMaterializationStatusCompleted, nil
	case runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_FAILED:
		return enum.WorkspaceMaterializationStatusFailed, nil
	case runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_CANCELLED:
		return enum.WorkspaceMaterializationStatusCancelled, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

// WorkspaceMaterializationStatusToProto maps materialization status.
func WorkspaceMaterializationStatusToProto(status enum.WorkspaceMaterializationStatus) runtimev1.WorkspaceMaterializationStatus {
	switch status {
	case enum.WorkspaceMaterializationStatusPending:
		return runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_PENDING
	case enum.WorkspaceMaterializationStatusRunning:
		return runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_RUNNING
	case enum.WorkspaceMaterializationStatusCompleted:
		return runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_COMPLETED
	case enum.WorkspaceMaterializationStatusFailed:
		return runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_FAILED
	case enum.WorkspaceMaterializationStatusCancelled:
		return runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_CANCELLED
	default:
		return runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_UNSPECIFIED
	}
}

// WorkspaceSourceKindFromProto maps a workspace source kind.
func WorkspaceSourceKindFromProto(kind runtimev1.WorkspaceSourceKind) (enum.WorkspaceSourceKind, error) {
	switch kind {
	case runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_CODE:
		return enum.WorkspaceSourceKindCode, nil
	case runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_DOCUMENTATION:
		return enum.WorkspaceSourceKindDocumentation, nil
	case runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_GUIDANCE_PACKAGE:
		return enum.WorkspaceSourceKindGuidancePackage, nil
	case runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_GENERATED_CONTEXT:
		return enum.WorkspaceSourceKindGeneratedContext, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

// WorkspaceSourceKindToProto maps a workspace source kind.
func WorkspaceSourceKindToProto(kind enum.WorkspaceSourceKind) runtimev1.WorkspaceSourceKind {
	switch kind {
	case enum.WorkspaceSourceKindCode:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_CODE
	case enum.WorkspaceSourceKindDocumentation:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_DOCUMENTATION
	case enum.WorkspaceSourceKindGuidancePackage:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_GUIDANCE_PACKAGE
	case enum.WorkspaceSourceKindGeneratedContext:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_GENERATED_CONTEXT
	default:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_UNSPECIFIED
	}
}

// WorkspaceSourceAccessModeFromProto maps source access mode.
func WorkspaceSourceAccessModeFromProto(mode runtimev1.WorkspaceSourceAccessMode) (enum.WorkspaceSourceAccessMode, error) {
	switch mode {
	case runtimev1.WorkspaceSourceAccessMode_WORKSPACE_SOURCE_ACCESS_MODE_READ:
		return enum.WorkspaceSourceAccessModeRead, nil
	case runtimev1.WorkspaceSourceAccessMode_WORKSPACE_SOURCE_ACCESS_MODE_WRITE:
		return enum.WorkspaceSourceAccessModeWrite, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

// WorkspaceSourceAccessModeToProto maps source access mode.
func WorkspaceSourceAccessModeToProto(mode enum.WorkspaceSourceAccessMode) runtimev1.WorkspaceSourceAccessMode {
	switch mode {
	case enum.WorkspaceSourceAccessModeRead:
		return runtimev1.WorkspaceSourceAccessMode_WORKSPACE_SOURCE_ACCESS_MODE_READ
	case enum.WorkspaceSourceAccessModeWrite:
		return runtimev1.WorkspaceSourceAccessMode_WORKSPACE_SOURCE_ACCESS_MODE_WRITE
	default:
		return runtimev1.WorkspaceSourceAccessMode_WORKSPACE_SOURCE_ACCESS_MODE_UNSPECIFIED
	}
}
