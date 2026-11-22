package entity

import (
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// KubernetesIdentitySetupView contains safe addressed metadata without material, permit or hashes.
// A completed setup still requires the separate exact identity and resource-permission test.
// KubernetesIdentitySetupView содержит safe addressed metadata без material, permit либо hashes.
// Completed setup ещё требует отдельной exact identity и resource-permission test.
type KubernetesIdentitySetupView struct {
	Ref, ConnectionRef, IdentityRef, TargetRef, ReasonCode, CanonicalContext        string
	Version, ConnectionVersion, BindingVersion, BindingGeneration, IdentityRevision int64
	Policy                                                                          IntegrationCredentialPolicy
	State                                                                           enum.KubernetesIdentitySetupState
	ExpiresAt                                                                       time.Time
}

// KubernetesIdentityUploadEndpoint is operator-owned TLS metadata for direct owner-local upload.
// It carries no platform client identity and must not be overridden by caller URLs or proxy env.
// KubernetesIdentityUploadEndpoint — operator-owned TLS metadata для прямого owner-local upload.
// Не содержит platform client identity и не подменяется caller URL либо proxy env.
type KubernetesIdentityUploadEndpoint = value.KubernetesIdentityUploadEndpoint

// KubernetesIdentitySetupAdmission returns a permit only for the first durable admission.
// Receipt replay retains safe metadata and reconcile-only behavior without issuing another permit.
// KubernetesIdentitySetupAdmission возвращает permit лишь при первом durable admission.
// Receipt replay сохраняет safe metadata и reconcile-only без выдачи другого permit.
type KubernetesIdentitySetupAdmission struct {
	View          KubernetesIdentitySetupView
	Permit        []byte `json:"-"`
	Endpoint      KubernetesIdentityUploadEndpoint
	ReconcileOnly bool
}

// String conceals the short-lived upload permit even when the whole result is formatted.
// String скрывает краткоживущий upload permit при форматировании всего result.
func (KubernetesIdentitySetupAdmission) String() string {
	return "[Kubernetes identity setup admission]"
}

// GoString applies the same private boundary to detailed Go formatting.
// GoString применяет ту же private boundary при подробном Go formatting.
func (result KubernetesIdentitySetupAdmission) GoString() string { return result.String() }

// KubernetesIdentityUploadSnapshot is broker-private immutable setup input resolved from current rows.
// Resolve grants no write; exact target/content parsing and a separate durable Claim are mandatory.
// KubernetesIdentityUploadSnapshot — broker-private immutable setup input из current rows.
// Resolve не разрешает write; нужны exact target/content parsing и отдельный durable Claim.
type KubernetesIdentityUploadSnapshot struct {
	SetupRef, Digest string
	Provision        value.ManagedKubernetesIdentityProvision
	Target           value.ManagedKubernetesTarget
	ContentBytes     uint32
	ExpiresAt        time.Time
	ReconcileOnly    bool
	Completed        *KubernetesIdentitySetupView
	KnownIdentity    *value.ManagedKubernetesIdentityLocator `json:"-"`
}

// KubernetesIdentityUploadAdmission grants one WRITE or observation-only attempt, never both.
// Its expiry is bounded by the original setup and cannot refresh an old authorization on replay.
// KubernetesIdentityUploadAdmission разрешает одну WRITE либо observation-only attempt, не обе.
// Expiry ограничен исходным setup и не обновляет прежнюю authorization при replay.
type KubernetesIdentityUploadAdmission struct {
	SetupRef, AttemptRef string
	Generation           int64
	Fence                string `json:"-"`
	ExpiresAt            time.Time
	ReconcileOnly        bool
	Provision            value.ManagedKubernetesIdentityProvision `json:"-"`
	KnownIdentity        *value.ManagedKubernetesIdentityLocator  `json:"-"`
}

// String prevents accidental output of the attempt fence and private provision coordinates.
// String предотвращает случайную выдачу attempt fence и private provision coordinates.
func (KubernetesIdentityUploadAdmission) String() string {
	return "[Kubernetes private identity upload admission]"
}

// GoString preserves the same private boundary for detailed Go formatting.
// GoString сохраняет ту же private boundary при подробном Go formatting.
func (result KubernetesIdentityUploadAdmission) GoString() string { return result.String() }

// KubernetesIdentitySetupRequirements is safe pre-Begin metadata for explicit local export.
// It grants no target access and never substitutes Connection/identity validation during Begin.
// KubernetesIdentitySetupRequirements — safe pre-Begin metadata для explicit local export.
// Не даёт target access и не заменяет Connection/identity validation при Begin.
type KubernetesIdentitySetupRequirements struct {
	TargetRef, CanonicalContext       string
	TargetRevision, ConnectionVersion int64
	Policy                            IntegrationCredentialPolicy
}

// Valid checks safe descriptive coordinates only; the record grants no setup or target authority.
// Valid проверяет только safe descriptive coordinates; record не выдаёт setup либо target authority.
func (requirements KubernetesIdentitySetupRequirements) Valid() bool {
	return len(requirements.TargetRef) <= 96 && managedkubernetes.ValidLocator(requirements.TargetRef) && managedkubernetes.ValidLocator(requirements.CanonicalContext) &&
		requirements.TargetRevision > 0 && requirements.TargetRevision <= 9007199254740991 && requirements.ConnectionVersion > 0 && requirements.ConnectionVersion <= 9007199254740991 &&
		(requirements.Policy == IntegrationCredentialInitiatorRequired || requirements.Policy == IntegrationCredentialProjectShared)
}
