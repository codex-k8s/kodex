package value

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// KubernetesOperationSnapshot contains broker-private owner metadata, never credentials or a caller permit.
// Resolve is a current read; effects and frames still require their separate one-shot admissions.
// KubernetesOperationSnapshot содержит broker-private owner metadata без credentials либо caller permit.
// Resolve является current read; effects и frames требуют отдельных one-shot admissions.
type KubernetesOperationSnapshot struct {
	Digest, Operation, Risk, ApprovalPolicy string
	IdentityRef, SubjectDigest              string
	IdentityGeneration                      int64
	Identity                                KubernetesIdentityLocator
	Target                                  KubernetesTarget
	Input                                   managedkubernetes.OperationInput `json:"-"`
	NormalizedInput                         []byte                           `json:"-"`
	Resource                                *managedkubernetes.ResourceAddress
	Discovery                               *managedkubernetes.DiscoveryAddress
	NodeTransport                           *KubernetesNodeTransport
	ExpiresAt                               time.Time
	ReconcileOnly                           bool
	PlanRef, PlanDigest                     string
	PlanRevision                            int64
	ProtectedNamespaces                     []string
}

// String prevents accidental disclosure of private object bodies and owner storage pins.
// String предотвращает случайное раскрытие private object bodies и owner storage pins.
func (KubernetesOperationSnapshot) String() string { return "[managed Kubernetes operation snapshot]" }

// GoString applies the same boundary to detailed diagnostic formatting.
// GoString сохраняет ту же границу при подробном diagnostic formatting.
func (snapshot KubernetesOperationSnapshot) GoString() string { return snapshot.String() }

// KubernetesNodeTransport carries an operator-pinned route to be revalidated by the native transport.
// It does not prove RBAC, current registry eligibility or a remote Pod/Node identity by itself.
// KubernetesNodeTransport содержит operator-pinned route для повторной проверки native transport.
// Сам по себе не доказывает RBAC, current registry eligibility либо remote Pod/Node identity.
type KubernetesNodeTransport struct {
	Ref, TargetRef, NodeName, NodeUID                                              string
	Generation, TargetRevision                                                     int64
	ServerOrigin, TLSServerName, CertificateAuthoritySHA256, ServerPublicKeySHA256 string
	CertificateAuthorityPEM                                                        []byte
	AddressIPs                                                                     []string
	NotBefore, ExpiresAt                                                           time.Time
	Protocol                                                                       managedkubernetes.PrivilegedProtocol
}
