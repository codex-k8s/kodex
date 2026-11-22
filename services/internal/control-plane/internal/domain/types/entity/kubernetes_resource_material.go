package entity

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"time"
)

// KubernetesResourceMaterialView contains only safe owner metadata for an exact private artifact.
// AVAILABLE means material storage, never a created target Secret or execution/RBAC permission.
// KubernetesResourceMaterialView содержит лишь safe owner metadata exact private artifact.
// AVAILABLE означает material storage, не created target Secret либо execution/RBAC permission.
type KubernetesResourceMaterialView struct {
	Ref, ConnectionRef, IdentityRef, TargetRef, Namespace, Name, GenerateName string
	Operation, SecretType, PatchType, ReasonCode                              string
	Version, ConnectionVersion, IdentityRevision, BindingVersion              int64
	Policy                                                                    IntegrationCredentialPolicy
	State                                                                     enum.KubernetesResourceMaterialState
	ExpiresAt                                                                 time.Time
}

// KubernetesResourceMaterialAdmission carries one first-admission upload permit privately.
// Safe history/replay cannot return another permit or become an external effect authorization.
// KubernetesResourceMaterialAdmission privately несёт один first-admission upload permit.
// Safe history/replay не возвращает другой permit и не разрешает external effect.
type KubernetesResourceMaterialAdmission struct {
	View          KubernetesResourceMaterialView
	Permit        []byte `json:"-"`
	Endpoint      KubernetesIdentityUploadEndpoint
	ReconcileOnly bool
}

// String prevents one-shot upload authority from reaching formatted diagnostics.
// String исключает one-shot upload authority из formatted diagnostics.
func (KubernetesResourceMaterialAdmission) String() string {
	return "[Kubernetes resource material admission]"
}

// GoString applies the same boundary to detailed Go formatting.
// GoString применяет ту же границу при detailed Go formatting.
func (admission KubernetesResourceMaterialAdmission) GoString() string { return admission.String() }
