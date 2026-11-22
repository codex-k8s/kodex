package value

import "github.com/codex-k8s/kodex/libs/go/managedkubernetes"

// ManagedKubernetesCaptureTaskBinding preserves a separate artifact-only task incarnation.
// Its shared validation never substitutes owner lookup, current lease or original actor eligibility.
// ManagedKubernetesCaptureTaskBinding сохраняет отдельный artifact-only task incarnation.
// Shared validation не заменяет owner lookup, current lease либо original actor eligibility.
type ManagedKubernetesCaptureTaskBinding = managedkubernetes.CaptureTaskBinding
