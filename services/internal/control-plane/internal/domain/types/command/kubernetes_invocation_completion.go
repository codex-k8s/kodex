package command

import "github.com/codex-k8s/kodex/libs/go/managedkubernetes"

// ManagedKubernetesCompleteInvocation names the owner-assigned plan and an optional closed preflight hint, never a caller-assigned outcome or result.
// Empty plan pins require the repository to prove absence of every effect and open child Gate.
// ManagedKubernetesCompleteInvocation указывает owner-assigned plan и optional closed preflight hint, не caller-assigned outcome либо result.
// Empty plan pins требуют repository proof отсутствия effects и open child Gates.
type ManagedKubernetesCompleteInvocation struct {
	PlanRef            string
	PlanRevision       int64
	PlanDigest         string
	PreflightRejection string
}

// ValidPlanPins accepts one complete immutable plan tuple or the explicit noPlan variant.
// Structural validity alone grants no completion, identity selection or result delivery.
// ValidPlanPins принимает complete immutable plan tuple либо explicit noPlan variant.
// Structural validity не выдаёт completion, identity selection либо result delivery.
func (input ManagedKubernetesCompleteInvocation) ValidPlanPins() bool {
	if input.PreflightRejection != "" && input.PreflightRejection != managedkubernetes.ReasonPortForwardProfileUnsupported {
		return false
	}
	if input.PreflightRejection != "" && input.PlanRef == "" {
		return false
	}
	if input.PlanRef == "" {
		return input.PlanRevision == 0 && input.PlanDigest == ""
	}
	return managedkubernetes.ValidLocator(input.PlanRef) && len(input.PlanRef) <= 96 && input.PlanRevision > 0 && managedkubernetes.ValidDigest(input.PlanDigest)
}
