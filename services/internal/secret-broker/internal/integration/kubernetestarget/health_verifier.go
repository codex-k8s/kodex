package kubernetestarget

import (
	"context"
	"time"

	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// IdentityHealthVerifier observes only the selected embedded credential using the trusted target.
// It reports an authenticated subject digest, never a global resource-RBAC qualification.
// IdentityHealthVerifier наблюдает лишь selected embedded credential через trusted target.
// Возвращает authenticated subject digest, не global resource-RBAC qualification.
type IdentityHealthVerifier struct{}

// ObserveKubernetesIdentity rejects unsafe kubeconfig before dialing and never exposes provider errors.
// ObserveKubernetesIdentity отклоняет unsafe kubeconfig до dial и не раскрывает provider errors.
func (IdentityHealthVerifier) ObserveKubernetesIdentity(ctx context.Context, raw []byte, target value.KubernetesTarget) (string, error) {
	credential, err := ParseCredential(raw, target, time.Now().UTC())
	if err != nil {
		return "", targetrepo.ErrHealthUnavailable
	}
	digest, err := credential.ObserveIdentity(ctx)
	if err != nil {
		// Forbidden cluster metadata is not proof of invalid authentication.
		// Запрет cluster metadata не доказывает неверную authentication.
		return "", targetrepo.ErrHealthUnavailable
	}
	return digest, nil
}
