package command

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"

// KubernetesIdentitySetupBegin supplies content preconditions only, never another subject or target.
// Current policy, owner, private namespace and next revision belong to the locked Connection owner.
// KubernetesIdentitySetupBegin задаёт только content preconditions, не чужой subject либо target.
// Current policy, owner, private namespace и next revision разрешает locked Connection owner.
type KubernetesIdentitySetupBegin struct {
	ConnectionRef, ContentSHA256 string
	ExpectedConnectionVersion    int64
	ContentBytes                 uint32
}

// KubernetesIdentitySetupRead selects one self-owned setup or its exact Begin idempotency receipt.
// Both selectors cannot be present; their shape supplies no owner or lifecycle authority.
// KubernetesIdentitySetupRead выбирает один self-owned setup либо exact Begin idempotency receipt.
// Оба selector одновременно запрещены; их форма не даёт owner либо lifecycle authority.
type KubernetesIdentitySetupRead struct {
	ConnectionRef, SetupRef, BeginIdempotencyKey string
}

// KubernetesIdentitySetupCancel addresses one owner-resolved setup before version/idempotency checks.
// KubernetesIdentitySetupCancel адресует owner-resolved setup до version/idempotency checks.
type KubernetesIdentitySetupCancel struct {
	ConnectionRef, SetupRef string
}

// KubernetesIdentityUploadResolve contains only a locator and digest of the one-shot bearer permit.
// Kubeconfig material and raw permit bytes never enter this owner command.
// KubernetesIdentityUploadResolve содержит только locator и digest одноразового bearer permit.
// Kubeconfig material и raw permit bytes не входят в эту owner command.
type KubernetesIdentityUploadResolve struct {
	SetupRef, PermitSHA256 string
}

// KubernetesIdentityUploadClaim requests one exact owner snapshot under the broker's workload.
// State decides WRITE versus OBSERVE; no caller flag can obtain another write attempt.
// KubernetesIdentityUploadClaim запрашивает exact owner snapshot под broker workload.
// State определяет WRITE либо OBSERVE; caller flag не разрешает другую write attempt.
type KubernetesIdentityUploadClaim struct {
	SetupRef, PermitSHA256, SnapshotDigest, ContentSHA256, WorkloadInstance string
}

// KubernetesIdentityUploadComplete reports either exact private pins or an uncertain external effect.
// UNKNOWN has no placeholder locator and never grants a second write or revives expired authority.
// KubernetesIdentityUploadComplete передаёт exact private pins либо неопределённый external effect.
// UNKNOWN не содержит фиктивного locator, не разрешает второй write и не восстанавливает authority.
type KubernetesIdentityUploadComplete struct {
	SetupRef, AttemptRef, WorkloadInstance string
	Fence                                  string `json:"-"`
	Generation                             int64
	Identity                               *value.ManagedKubernetesIdentityLocator `json:"-"`
	Unknown                                bool
	RejectedAcknowledgement                *value.ManagedKubernetesAcknowledgementRejection `json:"-"`
}

// ValidOutcome admits exactly one complete typed outcome without inferring UNKNOWN from absence.
// A rejected successful ACK is not a material locator or an observation-eligible transport loss.
// ValidOutcome допускает ровно один полный typed outcome без inference UNKNOWN из отсутствия.
// Rejected successful ACK не является material locator либо transport loss, допускающим observation.
func (input KubernetesIdentityUploadComplete) ValidOutcome() bool {
	count := 0
	if input.Unknown {
		count++
	}
	if input.Identity != nil {
		count++
		if input.Identity.Validate(input.Identity.Namespace) != nil {
			return false
		}
	}
	if input.RejectedAcknowledgement != nil {
		count++
		if !input.RejectedAcknowledgement.Valid() {
			return false
		}
	}
	return count == 1
}

// String conceals the attempt fence and private material locator from diagnostics.
// String скрывает attempt fence и private material locator из диагностики.
func (KubernetesIdentityUploadComplete) String() string {
	return "[Kubernetes identity upload completion]"
}

// GoString preserves the same private boundary for detailed Go formatting.
// GoString сохраняет ту же private boundary при подробном Go formatting.
func (input KubernetesIdentityUploadComplete) GoString() string { return input.String() }
