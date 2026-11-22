package kubernetestarget

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// IdentitySetupVerifier composes the strict embedded-only parser and selected-identity cluster read.
// No material is returned to the caller and no administrative identity is available here.
// IdentitySetupVerifier соединяет strict embedded-only parser и selected-identity cluster read.
// Material не возвращается caller; administrative identity здесь отсутствует.
type IdentitySetupVerifier struct{}

// VerifyKubernetesIdentity rejects parser failures before network access and verifies actual ClusterUID.
// This read is preflight evidence, not an atomic incarnation fence or a resource RBAC qualification.
// VerifyKubernetesIdentity отклоняет parser failures до сети и проверяет actual ClusterUID.
// Read — preflight evidence, не atomic incarnation fence либо resource RBAC qualification.
func (IdentitySetupVerifier) VerifyKubernetesIdentity(ctx context.Context, raw []byte, target value.KubernetesTarget) error {
	credential, err := ParseCredential(raw, target, time.Now().UTC())
	if err != nil {
		return ErrPrivateIdentityRejected
	}
	return credential.VerifyClusterIdentity(ctx)
}
