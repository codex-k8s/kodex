package integrationpackage

// validExecutionProfile admits extended leases only for the single bounded SSH deployment.
// validExecutionProfile допускает продлённый lease только для ограниченного SSH deploy.
func validExecutionProfile(adapter string, capability Capability) bool {
	execution := capability.Execution
	if execution.LeaseProfile == "SSH_COMPOSE_V1" {
		return adapter == string(AdapterSSHCompose) && capability.Operation == "ssh.compose.deploy" &&
			capability.Risk == string(RiskWrite) && capability.ApprovalPolicy == string(ApprovalHumanEachEffect) &&
			len(capability.AllowedApprovalPolicies) == 1 && capability.AllowedApprovalPolicies[0] == string(ApprovalHumanEachEffect) &&
			execution.Idempotency == string(IdempotencyEffectKey) && execution.MaxAttempts == 1 &&
			execution.TimeoutSeconds >= 1 && execution.TimeoutSeconds <= 600
	}
	return execution.LeaseProfile == "" && execution.TimeoutSeconds >= 1 && execution.TimeoutSeconds <= 120
}
