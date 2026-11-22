package value

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// KubernetesTarget contains operator-owned route metadata, never caller-selected endpoints.
// Source aliases are validation inputs only; transport always uses the pinned reachable route.
// KubernetesTarget содержит operator-owned route metadata без caller-selected endpoints.
// Source aliases нужны лишь проверке; transport использует закреплённый reachable route.
type KubernetesTarget struct {
	ID, ClusterUID, ServerURL, TLSServerName, CertificateAuthorityHash, Context string
	Revision                                                                    uint64
	AddressPins, KubeconfigSourceOrigins                                        []string
	ResourcePolicy                                                              *managedkubernetes.ResourcePolicy `json:"resourcePolicy,omitempty"`
	ResourcePolicySHA256                                                        string                            `json:"resourcePolicySHA256,omitempty"`
}

// KubernetesIdentitySetupView is the finite safe result shared by direct upload and owner reads.
// Completion records binding only and does not imply target RBAC qualification.
// KubernetesIdentitySetupView — конечный safe result прямой загрузки и owner reads.
// Completion означает лишь binding и не подтверждает target RBAC qualification.
type KubernetesIdentitySetupView struct {
	CanonicalContext  string    `json:"canonicalContext"`
	SetupRef          string    `json:"setupRef"`
	ConnectionRef     string    `json:"connectionRef"`
	ConnectionVersion int64     `json:"connectionVersion"`
	BindingVersion    int64     `json:"bindingVersion"`
	BindingGeneration int64     `json:"bindingGeneration"`
	IdentityRef       string    `json:"identityRef"`
	IdentityRevision  int64     `json:"identityRevision"`
	TargetRef         string    `json:"targetRef"`
	State             string    `json:"state"`
	ReasonCode        string    `json:"reasonCode"`
	Policy            string    `json:"credentialPolicy"`
	Version           int64     `json:"version"`
	ExpiresAt         time.Time `json:"expiresAt"`
}

// Valid admits only the finite safe view, preserving absence of shared binding coordinates.
// Valid допускает лишь finite safe view, сохраняя отсутствие shared binding coordinates.
func (input KubernetesIdentitySetupView) Valid() bool {
	if !managedkubernetes.ValidLocator(input.CanonicalContext) || !managedkubernetes.ValidLocator(input.SetupRef) || !managedkubernetes.ValidLocator(input.ConnectionRef) || !managedkubernetes.ValidLocator(input.IdentityRef) || !managedkubernetes.ValidLocator(input.TargetRef) ||
		input.Version < 1 || input.Version > 9007199254740991 || input.ConnectionVersion < 1 || input.ConnectionVersion > 9007199254740991 || input.IdentityRevision < 1 || input.IdentityRevision > 9007199254740991 || input.ExpiresAt.IsZero() {
		return false
	}
	if input.Policy == "INITIATOR_REQUIRED" {
		if input.BindingVersion < 1 || input.BindingVersion > 9007199254740991 || input.BindingGeneration < 1 || input.BindingGeneration > 9007199254740991 {
			return false
		}
	} else if input.Policy != "PROJECT_SHARED" || input.BindingVersion != 0 || input.BindingGeneration != 0 {
		return false
	}
	switch input.State {
	case "PREPARED", "ATTEMPTING", "COMPLETED", "CANCELLED", "EXPIRED", "UNKNOWN", "ACK_REJECTED":
	default:
		return false
	}
	switch input.ReasonCode {
	case "", "SETUP_EXPIRED_PENDING_CLOSURE", "SETUP_COMPLETION_REQUIRED", "SETUP_ACK_REJECTED", "SETUP_ACK_CONFLICT":
		return true
	default:
		return false
	}
}

// KubernetesIdentityUploadSnapshot carries private immutable owner input without material or permit.
// A completed snapshot contains only safe history and no executable provision.
// KubernetesIdentityUploadSnapshot передаёт private immutable owner input без material либо permit.
// Completed snapshot содержит лишь safe history без исполняемого provision.
type KubernetesIdentityUploadSnapshot struct {
	SetupRef, Digest string
	Provision        KubernetesIdentityProvision
	Target           KubernetesTarget
	ContentBytes     uint32
	ExpiresAt        time.Time
	ReconcileOnly    bool
	Completed        *KubernetesIdentitySetupView
	KnownIdentity    *KubernetesIdentityLocator
}

// KubernetesIdentityUploadAdmission contains exactly one current WRITE or OBSERVE credit.
// Its fence and private pins never enter browser responses or diagnostic output.
// KubernetesIdentityUploadAdmission содержит ровно один current WRITE либо OBSERVE credit.
// Fence и private pins не попадают в browser responses либо diagnostic output.
type KubernetesIdentityUploadAdmission struct {
	SetupRef, AttemptRef, Fence string
	Generation                  int64
	ExpiresAt                   time.Time
	ReconcileOnly               bool
	Provision                   KubernetesIdentityProvision
	KnownIdentity               *KubernetesIdentityLocator
}

// String prevents whole-admission formatting from revealing private owner storage coordinates.
// String не допускает раскрытия private owner storage coordinates при форматировании admission.
func (KubernetesIdentityUploadAdmission) String() string { return "[Kubernetes identity admission]" }

// GoString preserves the private admission boundary under detailed Go formatting.
// GoString сохраняет private admission boundary при подробном Go formatting.
func (input KubernetesIdentityUploadAdmission) GoString() string { return input.String() }
