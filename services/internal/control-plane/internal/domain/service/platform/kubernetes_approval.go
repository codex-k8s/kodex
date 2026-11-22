package platform

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"

// KubernetesApprovalRequiredReason identifies the closed typed waiting precondition on the wire.
// KubernetesApprovalRequiredReason задаёт closed typed waiting precondition на wire.
const KubernetesApprovalRequiredReason = "KUBERNETES_APPROVAL_REQUIRED"

// KubernetesApprovalRequiredMessage is a fixed diagnostic without child input or material.
// KubernetesApprovalRequiredMessage — fixed diagnostic без child input либо material.
const KubernetesApprovalRequiredMessage = "Kubernetes child approval is required"

// KubernetesApprovalRequiredError identifies an existing exact child Gate without effect credit.
// Its refs are assigned by the owner after immutable plan persistence, never supplied by input.
// KubernetesApprovalRequiredError указывает existing exact child Gate без effect credit.
// Refs назначает owner после immutable plan persistence, а не invocation input.
type KubernetesApprovalRequiredError struct {
	GateRef, ChildRef, PlanRef string
}

// Error exposes a fixed diagnostic without request data, resource names or private metadata.
// Error выдаёт fixed diagnostic без request data, resource names либо private metadata.
func (*KubernetesApprovalRequiredError) Error() string {
	return KubernetesApprovalRequiredMessage
}

// Unwrap preserves the established precondition category while retaining typed Gate metadata.
// Unwrap сохраняет existing precondition category и typed Gate metadata.
func (*KubernetesApprovalRequiredError) Unwrap() error { return errs.ErrConflict }
