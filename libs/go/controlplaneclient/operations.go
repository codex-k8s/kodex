package controlplaneclient

import controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"

func AgentRunnerOperations() map[string]string {
	return map[string]string{
		"control.agent-runner.readiness":               controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.agent-work-claim.manage":              controlplanev1.ControlPlaneService_ManageWorkClaim_FullMethodName,
		"control.turn.claim":                           controlplanev1.ControlPlaneService_ClaimTurn_FullMethodName,
		"control.turn.renew":                           controlplanev1.ControlPlaneService_RenewTurn_FullMethodName,
		"control.turn.complete":                        controlplanev1.ControlPlaneService_CompleteTurn_FullMethodName,
		"control.integration-continuation.get":         controlplanev1.ControlPlaneService_GetIntegrationContinuation_FullMethodName,
		"control.integration-continuation.acknowledge": controlplanev1.ControlPlaneService_AcknowledgeIntegrationContinuation_FullMethodName,
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
		"control.runtime-controller.readiness":      controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.runtime-resource.get":              controlplanev1.ControlPlaneService_GetResource_FullMethodName,
		"control.runtime-revision.get":              controlplanev1.ControlPlaneService_GetRuntimeRevision_FullMethodName,
		"control.runtime-execution.claim":           controlplanev1.ControlPlaneService_ClaimRuntimeExecution_FullMethodName,
		"control.runtime-execution.get":             controlplanev1.ControlPlaneService_GetRuntimeExecution_FullMethodName,
		"control.runtime-execution.admit":           controlplanev1.ControlPlaneService_AdmitRuntimeExecution_FullMethodName,
		"control.runtime-execution.heartbeat":       controlplanev1.ControlPlaneService_HeartbeatRuntimeExecution_FullMethodName,
		"control.runtime-execution.incident":        controlplanev1.ControlPlaneService_RecordRuntimeIncident_FullMethodName,
		"control.runtime-execution.complete":        controlplanev1.ControlPlaneService_CompleteRuntimeExecution_FullMethodName,
		"control.runtime-execution.reschedule":      controlplanev1.ControlPlaneService_RescheduleRuntimeExecution_FullMethodName,
		"control.runtime-execution.expire":          controlplanev1.ControlPlaneService_ExpireRuntimeExecution_FullMethodName,
		"control.runtime-execution.cleanup.consume": controlplanev1.ControlPlaneService_ConsumeRuntimeCleanupAuthorization_FullMethodName,
	}
}

// BotServiceRuntimeBindingOperations выдаёт legacy owner только закрытую
// materialization-команду и readiness; generic mutation в профиль не входит.
func BotServiceRuntimeBindingOperations() map[string]string {
	return map[string]string{
		"control.bot-service.readiness":               controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.runtime-execution.agent.materialize": controlplanev1.ControlPlaneService_MaterializeRuntimeAgentTurn_FullMethodName,
		"control.runtime-execution.agent.resolve":     controlplanev1.ControlPlaneService_ResolveRuntimeAgentBindingIntent_FullMethodName,
		"control.runtime-execution.agent.bind":        controlplanev1.ControlPlaneService_BindRuntimeAgentSession_FullMethodName,
	}
}

// RuntimeOwnerOperations возвращает только owner lifecycle full methods.
func RuntimeOwnerOperations() map[string]string {
	return map[string]string{
		"control.runtime-execution.cancel": controlplanev1.ControlPlaneService_CancelRuntimeExecution_FullMethodName,
		"control.runtime-execution.retry":  controlplanev1.ControlPlaneService_RetryRuntimeExecution_FullMethodName,
	}
}

// RuntimeArchiveOperations выдаёт archive worker только readiness и запись archive evidence.
func RuntimeArchiveOperations() map[string]string {
	return map[string]string{
		"control.runtime-archive.readiness":        controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.runtime-execution.archive.record": controlplanev1.ControlPlaneService_RecordRuntimeArchive_FullMethodName,
	}
}

// RuntimeRestoreVerifierOperations отделяет independent proof от owner/cleanup.
func RuntimeRestoreVerifierOperations() map[string]string {
	return map[string]string{
		"control.runtime-restore-verifier.readiness":   controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.runtime-execution.restore.verify":     controlplanev1.ControlPlaneService_VerifyRuntimeRestore_FullMethodName,
		"control.runtime-execution.restore.bind":       controlplanev1.ControlPlaneService_BindRuntimeRestoreTarget_FullMethodName,
		"control.runtime-execution.rehydrate.complete": controlplanev1.ControlPlaneService_CompleteRuntimeRehydrate_FullMethodName,
	}
}

// RuntimeCleanupAuthorizerOperations выдаёт/истекает destructive authorization.
func RuntimeCleanupAuthorizerOperations() map[string]string {
	return map[string]string{
		"control.runtime-cleanup-authorizer.readiness": controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.runtime-execution.cleanup.authorize":  controlplanev1.ControlPlaneService_AuthorizeRuntimeCleanup_FullMethodName,
		"control.runtime-execution.cleanup.expire":     controlplanev1.ControlPlaneService_ExpireRuntimeCleanupAuthorization_FullMethodName,
	}
}

// IntegrationGatewayOperations возвращает только approval/execution full methods.
func IntegrationGatewayOperations() map[string]string {
	return map[string]string{
		"control.integration-gateway.readiness":    controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.integration-session.resolve":      controlplanev1.ControlPlaneService_ResolveIntegrationSession_FullMethodName,
		"control.integration-continuation.suspend": controlplanev1.ControlPlaneService_SuspendForIntegrationApproval_FullMethodName,
		"control.integration-invocation.approve":   controlplanev1.ControlPlaneService_ApproveIntegrationInvocation_FullMethodName,
		"control.integration-invocation.reject":    controlplanev1.ControlPlaneService_RejectIntegrationInvocation_FullMethodName,
		"control.integration-invocation.expire":    controlplanev1.ControlPlaneService_ExpireIntegrationInvocation_FullMethodName,
		"control.integration-invocation.cancel":    controlplanev1.ControlPlaneService_CancelIntegrationInvocation_FullMethodName,
		"control.integration-execution.begin":      controlplanev1.ControlPlaneService_BeginIntegrationExecution_FullMethodName,
		"control.integration-execution.complete":   controlplanev1.ControlPlaneService_CompleteIntegrationExecution_FullMethodName,
		"control.integration-execution.fail":       controlplanev1.ControlPlaneService_FailIntegrationExecution_FullMethodName,
		"control.integration-result.validate":      controlplanev1.ControlPlaneService_ValidateIntegrationResultAccess_FullMethodName,
	}
}

// ControlAPIGatewayOperations возвращает закрытый набор owner HTTP/WS mappings.
// Actor, tenant, project и ownership разрешаются proof resolver на стороне
// control-plane; adapter не принимает эти значения из HTTP payload.
func ControlAPIGatewayOperations() map[string]string {
	return map[string]string{
		"control.readiness.check":            controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.project.create":             controlplanev1.ControlPlaneService_CreateProject_FullMethodName,
		"control.project.list":               controlplanev1.ControlPlaneService_ListProjects_FullMethodName,
		"control.resource.create":            controlplanev1.ControlPlaneService_CreateResource_FullMethodName,
		"control.resource.update":            controlplanev1.ControlPlaneService_UpdateResource_FullMethodName,
		"control.resource.transition":        controlplanev1.ControlPlaneService_TransitionResource_FullMethodName,
		"control.resource.delete":            controlplanev1.ControlPlaneService_DeleteResource_FullMethodName,
		"control.access.manage":              controlplanev1.ControlPlaneService_ManageAccessResource_FullMethodName,
		"control.access.detach":              controlplanev1.ControlPlaneService_DetachAccessResource_FullMethodName,
		"control.access.copy":                controlplanev1.ControlPlaneService_CopyAccessResource_FullMethodName,
		"control.resource.get":               controlplanev1.ControlPlaneService_GetResource_FullMethodName,
		"control.resource.list":              controlplanev1.ControlPlaneService_ListResources_FullMethodName,
		"control.resource.search":            controlplanev1.ControlPlaneService_SearchResources_FullMethodName,
		"control.audit.list":                 controlplanev1.ControlPlaneService_ListAuditEvents_FullMethodName,
		"control.runtime-incident.list":      controlplanev1.ControlPlaneService_ListRuntimeIncidents_FullMethodName,
		"control.diagnostics.get":            controlplanev1.ControlPlaneService_GetDiagnostics_FullMethodName,
		"control.owner-session.admit":        controlplanev1.ControlPlaneService_AdmitOwnerSession_FullMethodName,
		"control.owner-session.revoke":       controlplanev1.ControlPlaneService_RevokeOwnerSession_FullMethodName,
		"control.gateway-public-tls.prepare": controlplanev1.ControlPlaneService_PrepareGatewayPublicTLS_FullMethodName,
		"control.gateway-public-tls.confirm": controlplanev1.ControlPlaneService_ConfirmGatewayPublicTLS_FullMethodName,
		"control.gateway-public-tls.check":   controlplanev1.ControlPlaneService_CheckGatewayPublicTLS_FullMethodName,
	}
}

func OwnerGateDeliveryOperations() map[string]string {
	return map[string]string{
		"control.owner-gate-delivery.readiness": controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.owner-gate.claim-delivery":     controlplanev1.ControlPlaneService_ClaimOwnerGateDelivery_FullMethodName,
		"control.owner-gate.deliver":            controlplanev1.ControlPlaneService_RecordOwnerGateDelivery_FullMethodName,
		"control.owner-gate.expire":             controlplanev1.ControlPlaneService_ExpireOwnerGate_FullMethodName,
	}
}

// InteractionGatewayOperations объединяет два независимых credential contour:
// server-owned owner-gate delivery и короткоживущий Mattermost event.
func InteractionGatewayOperations() map[string]string {
	return map[string]string{
		"control.owner-gate-delivery.readiness":  controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.owner-gate.claim-delivery":      controlplanev1.ControlPlaneService_ClaimOwnerGateDelivery_FullMethodName,
		"control.owner-gate.deliver":             controlplanev1.ControlPlaneService_RecordOwnerGateDelivery_FullMethodName,
		"control.owner-gate.expire":              controlplanev1.ControlPlaneService_ExpireOwnerGate_FullMethodName,
		"control.interaction.session.create":     controlplanev1.ControlPlaneService_ManageSession_FullMethodName,
		"control.interaction.turn.enqueue":       controlplanev1.ControlPlaneService_EnqueueTurn_FullMethodName,
		"control.interaction.artifact.register":  controlplanev1.ControlPlaneService_RegisterArtifact_FullMethodName,
		"control.interaction.resource.read":      controlplanev1.ControlPlaneService_GetResource_FullMethodName,
		"control.interaction.owner-gate.resolve": controlplanev1.ControlPlaneService_ResolveOwnerGate_FullMethodName,
	}
}

func MemoryIndexerOperations() map[string]string {
	return map[string]string{
		"control.memory-indexer.readiness": controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
		"control.memory.index":             controlplanev1.ControlPlaneService_RecordMemoryEmbedding_FullMethodName,
	}
}
