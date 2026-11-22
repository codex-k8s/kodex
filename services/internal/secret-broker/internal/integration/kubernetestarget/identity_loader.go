package kubernetestarget

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"time"

	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// ErrPrivateIdentityRejected conceals storage/parser details without converting them into auth success.
// ErrPrivateIdentityRejected скрывает storage/parser details без превращения ошибки в auth success.
var ErrPrivateIdentityRejected = errors.New("managed Kubernetes private identity rejected")

// LoadCredential joins the exact private purpose reader to the embedded-only parser.
// It never tries a generic Secret, ambient kubeconfig or service identity after a failed read.
// All original byte copies are cleared even on partial errors; only the broker-private parsed
// credential survives for the bounded selected-target operation.
// LoadCredential соединяет exact private purpose reader и embedded-only parser.
// Не пробует generic Secret, ambient kubeconfig либо service identity после ошибки чтения.
// Исходные byte copies очищаются даже при partial errors; сохраняется только broker-private
// parsed credential для bounded selected-target operation.
func LoadCredential(ctx context.Context, reader targetrepo.IdentityMaterialReader, locator value.KubernetesIdentityLocator, target Target, now time.Time) (Credential, error) {
	if ctx == nil || reader == nil || now.IsZero() || validateTarget(target) != nil || locator.Validate(locator.Namespace) != nil || locator.TargetRef != target.ID {
		return Credential{}, ErrPrivateIdentityRejected
	}
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	material, err := reader.ReadKubernetesIdentity(ctx, locator)
	defer clear(material)
	if ctx.Err() != nil {
		return Credential{}, ctx.Err()
	}
	if err != nil || len(material) == 0 || len(material) > maxCredentialBytes {
		return Credential{}, ErrPrivateIdentityRejected
	}
	digest := sha256.Sum256(material)
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(digest[:])), []byte(locator.ContentSHA256)) != 1 {
		return Credential{}, ErrPrivateIdentityRejected
	}
	credential, err := ParseCredential(material, target, now)
	if err != nil {
		return Credential{}, ErrPrivateIdentityRejected
	}
	return credential, nil
}
