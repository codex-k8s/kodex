package controlplaneclient

import controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"

func AgentRunnerOperations() map[string]string {
	return map[string]string{
		"control.agent-runner.readiness":  controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.agent-work-claim.manage": controlplanev1.ControlPlaneService_ManageWorkClaim_FullMethodName,
		"control.turn.claim":              controlplanev1.ControlPlaneService_ClaimTurn_FullMethodName,
		"control.turn.renew":              controlplanev1.ControlPlaneService_RenewTurn_FullMethodName,
		"control.turn.complete":           controlplanev1.ControlPlaneService_CompleteTurn_FullMethodName,
	}
}

func AutomationSchedulerOperations() map[string]string {
	return map[string]string{
		"control.automation-scheduler.readiness": controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.schedule-resource.get":          controlplanev1.ControlPlaneService_GetResource_FullMethodName,
		"control.schedule.claim-due":             controlplanev1.ControlPlaneService_ClaimDueSchedules_FullMethodName,
		"control.schedule.claim-occurrence":      controlplanev1.ControlPlaneService_ClaimScheduleOccurrence_FullMethodName,
		"control.schedule.complete-occurrence":   controlplanev1.ControlPlaneService_CompleteScheduleOccurrence_FullMethodName,
	}
}

func ArtifactScannerOperations() map[string]string {
	return map[string]string{
		"control.artifact-scanner.readiness": controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.artifact.scan":              controlplanev1.ControlPlaneService_RecordArtifactScan_FullMethodName,
	}
}

func RuntimeControllerOperations() map[string]string {
	return map[string]string{
		"control.runtime-controller.readiness": controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.runtime-resource.get":         controlplanev1.ControlPlaneService_GetResource_FullMethodName,
		"control.runtime-revision.get":         controlplanev1.ControlPlaneService_GetRuntimeRevision_FullMethodName,
	}
}

func OwnerGateDeliveryOperations() map[string]string {
	return map[string]string{
		"control.owner-gate-delivery.readiness": controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.owner-gate.claim-delivery":     controlplanev1.ControlPlaneService_ClaimOwnerGateDelivery_FullMethodName,
		"control.owner-gate.deliver":            controlplanev1.ControlPlaneService_RecordOwnerGateDelivery_FullMethodName,
	}
}

func MemoryIndexerOperations() map[string]string {
	return map[string]string{
		"control.memory-indexer.readiness": controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.memory.index":             controlplanev1.ControlPlaneService_RecordMemoryEmbedding_FullMethodName,
	}
}
