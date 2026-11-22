package entity

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// SSHIdentitySetupView exposes safe current metadata only; no physical Secret pins, content/authentication hashes, raw permits or key material may reach the public setup view.
// SSHIdentitySetupView предоставляет лишь safe current metadata; physical Secret pins, content/authentication hashes, raw permits и key material не входят в public setup view.
type SSHIdentitySetupView struct {
	Ref, ConnectionRef, IdentityRef, TargetRef, ReasonCode                          string
	Version, ConnectionVersion, BindingVersion, BindingGeneration, IdentityRevision int64
	Policy                                                                          IntegrationCredentialPolicy
	State                                                                           enum.SSHIdentitySetupState
	ExpiresAt                                                                       time.Time
}

// SSHIdentityUploadEndpoint is trusted operator-supplied browser upload metadata, not a caller URL or service credential; its eventual producer must enforce exact HTTPS origin and purpose routing.
// SSHIdentityUploadEndpoint содержит trusted operator-supplied browser upload metadata, а не caller URL либо service credential; producer обязан обеспечить exact HTTPS origin и purpose routing.
type SSHIdentityUploadEndpoint = value.SSHIdentityUploadEndpoint

// SSHIdentitySetupAdmission returns the raw permit only for the first durable Begin admission; an idempotency replay returns safe reconcile-only metadata without minting another upload capability.
// SSHIdentitySetupAdmission возвращает raw permit лишь при первом durable Begin admission; idempotency replay возвращает safe reconcile-only metadata без нового upload capability.
type SSHIdentitySetupAdmission struct {
	View          SSHIdentitySetupView
	Permit        []byte `json:"-"`
	Endpoint      SSHIdentityUploadEndpoint
	ReconcileOnly bool
}

// String conceals the upload capability even when a whole admission object is formatted.
// String скрывает upload capability при форматировании всего admission object.
func (SSHIdentitySetupAdmission) String() string { return "[SSH identity setup admission]" }

// GoString preserves the same private boundary for detailed Go formatting.
// GoString сохраняет ту же private boundary при подробном Go formatting.
func (admission SSHIdentitySetupAdmission) GoString() string { return admission.String() }

// SSHIdentityUploadSnapshot is the private immutable owner response before Claim; the pending preparation has no authentication digest until the broker derives and atomically pins it.
// SSHIdentityUploadSnapshot содержит private immutable owner response до Claim; pending preparation не имеет authentication digest до его вывода и atomic pin broker.
type SSHIdentityUploadSnapshot struct {
	SetupRef, Digest     string
	Preparation          managedssh.IdentityPreparation
	ContentBytes         uint32
	AuthenticationSHA256 string
	ExpiresAt            time.Time
	Mode                 enum.SSHIdentityUploadMode
}

// SSHIdentityUploadAdmission grants a single bounded WRITE or observation-only generation selected by owner state; no raw material or generic TOKEN descriptor exists in this response.
// SSHIdentityUploadAdmission выдаёт один bounded WRITE либо observation-only generation по owner state; raw material и generic TOKEN descriptor в response отсутствуют.
type SSHIdentityUploadAdmission struct {
	SetupRef, SnapshotDigest, AttemptRef, WorkloadInstance string
	Generation                                             int64
	Fence                                                  string `json:"-"`
	ExpiresAt                                              time.Time
	Mode                                                   enum.SSHIdentityUploadMode
	Provision                                              managedssh.IdentityProvision `json:"-"`
}

// String hides the fence and private immutable material coordinates from diagnostics.
// String скрывает fence и private immutable material coordinates из diagnostics.
func (SSHIdentityUploadAdmission) String() string { return "[SSH private identity upload admission]" }

// GoString preserves the same privacy boundary for detailed Go formatting.
// GoString сохраняет ту же privacy boundary при подробном Go formatting.
func (admission SSHIdentityUploadAdmission) GoString() string { return admission.String() }
