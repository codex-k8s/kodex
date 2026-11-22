package kubernetes

import (
	"bytes"
	"context"
	"errors"

	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	// sshIdentityPurposeLabel selects only the separately produced SSH purpose lane, never a generic runtime-secret materialization.
	// sshIdentityPurposeLabel выбирает только отдельно произведённый SSH purpose lane, а не generic runtime-secret materialization.
	sshIdentityPurposeLabel = "integration-identities.kodex.dev/purpose"
	// sshIdentityRevisionAnnotation binds material to the exact common metadata revision owned by the control plane.
	// sshIdentityRevisionAnnotation связывает material с exact common metadata revision, принадлежащей control plane.
	sshIdentityRevisionAnnotation = "integration-identities.kodex.dev/revision-ref"
	// sshIdentityTargetAnnotation prevents selecting material from a different target even if another revision uses an identical SSH key.
	// sshIdentityTargetAnnotation запрещает выбор material другого target даже при совпадающем SSH key.
	sshIdentityTargetAnnotation = "integration-identities.kodex.dev/target-ref"
	// sshIdentityGenerationAnnotation records the positive immutable identity revision, not a caller-selected latest alias.
	// sshIdentityGenerationAnnotation хранит positive immutable identity revision, а не caller-selected latest alias.
	sshIdentityGenerationAnnotation = "integration-identities.kodex.dev/revision"
	// sshIdentityContentAnnotation pins the complete private value digest; only internal owner and broker metadata may contain it.
	// sshIdentityContentAnnotation закрепляет полный private value digest; он допустим только в internal owner и broker metadata.
	sshIdentityContentAnnotation = "integration-identities.kodex.dev/content-sha256"
	// sshIdentityPublicAnnotation binds the actual authentication public key without placing private material into ordinary product configuration.
	// sshIdentityPublicAnnotation связывает actual authentication public key без private material в обычной product configuration.
	sshIdentityPublicAnnotation = "integration-identities.kodex.dev/authentication-sha256"
	// sshIdentityMaterialKey is fixed by the SSH purpose producer; callers cannot request an alternate data key.
	// sshIdentityMaterialKey фиксируется SSH purpose producer; caller не может запросить другой data key.
	sshIdentityMaterialKey = "private_key"
)

// ReadSSHIdentity uses only the broker's configured namespace and exact immutable object tuple, checking purpose, metadata and bytes before returning one owned private copy.
// ReadSSHIdentity использует только configured namespace broker и exact immutable object tuple, проверяя purpose, metadata и bytes до возврата одной owned private copy.
func (store *Store) ReadSSHIdentity(ctx context.Context, locator value.SSHIdentityLocator) ([]byte, error) {
	if ctx == nil || !locator.Valid() || locator.Namespace != store.namespace {
		return nil, sshrepository.ErrIdentityRejected
	}
	if ctx.Err() != nil {
		return nil, sshrepository.ErrIdentityUnavailable
	}
	secret, err := store.sshIdentityGet(ctx, locator.SecretName)
	if apierrors.IsNotFound(err) {
		return nil, sshrepository.ErrIdentityAbsent
	}
	var rejection *sshrepository.IdentityAcknowledgementRejection
	if errors.As(err, &rejection) {
		return nil, rejection
	}
	if err != nil {
		return nil, sshrepository.ErrIdentityUnavailable
	}
	if secret == nil {
		return nil, sshrepository.ErrIdentityUnavailable
	}
	defer clear(secret.Data[sshIdentityMaterialKey])
	if string(secret.UID) != locator.SecretUID || secret.ResourceVersion != locator.SecretResourceVersion {
		return nil, sshrepository.ErrIdentityRejected
	}
	material, err := sshIdentityMaterial(locator.Provision(), secret)
	if ctx.Err() != nil {
		return nil, sshrepository.ErrIdentityUnavailable
	}
	if err != nil {
		return nil, sshrepository.ErrIdentityRejected
	}
	return bytes.Clone(material), nil
}
