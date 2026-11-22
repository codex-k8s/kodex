package managedkubernetes

// ValidateInvocationApprovalPolicy only strengthens a registered operation's selected policy.
// Callers must first check the exact registry risk/policy and validated input, then call it again
// for each owner-resolved child using actual discovery/current-object coordinates. Core Secret
// effects require EACH even for a normally scoped metadata command; private material stays separate. It cannot
// promote SCOPED to EACH, authorize a child, or permit credential-owning/host-escalating objects.
// ValidateInvocationApprovalPolicy только усиливает selected policy registered operation.
// Caller сначала сверяет exact registry risk/policy и validated input, затем повторяет проверку
// для каждого owner-resolved child по actual discovery/current-object coordinates. Core Secret
// effects требуют EACH даже для обычно scoped metadata command; private material остаётся отдельно. Не повышает
// SCOPED до EACH, не разрешает child либо credential-owning/host-escalating objects.
func ValidateInvocationApprovalPolicy(input OperationInput, risk, selected string) error {
	if risk == "READ" {
		if selected != "NONE" {
			return ErrInvalidOperationInput
		}
		return nil
	}
	if (risk != "WRITE" && risk != "SENSITIVE" && risk != "DESTRUCTIVE") ||
		(selected != "HUMAN_SCOPED" && selected != "HUMAN_EACH_EFFECT") {
		return ErrInvalidOperationInput
	}
	requiresEach := input.Namespace == "" || risk == "SENSITIVE" || risk == "DESTRUCTIVE" ||
		input.Group == "" && input.Resource == "secrets"
	switch input.Group {
	case "rbac.authorization.k8s.io", "authentication.k8s.io", "authorization.k8s.io", "certificates.k8s.io":
		requiresEach = true
	}
	if requiresEach && selected != "HUMAN_EACH_EFFECT" {
		return ErrInvalidOperationInput
	}
	return nil
}
