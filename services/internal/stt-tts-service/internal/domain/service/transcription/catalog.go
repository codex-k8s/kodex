package transcription

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

// GetModelCatalog читает возможности adapter без policy, credential, decoder
// и provider probe. Административное право не выдаёт право на транскрипцию.
func (service *Service) GetModelCatalog(ctx context.Context, principal value.Principal) (modelprofile.Catalog, error) {
	if ctx == nil || principal.Permission != value.PermissionManageConfiguration || principal.ActorID == "" || principal.TenantID == "" ||
		principal.ProjectID != "" || principal.Project != (value.AuthorityProvenance{}) || principal.RequestID == "" ||
		principal.AuthorityRevision == 0 || !validSHA256(principal.AuthorityDigestSHA256) || !service.now().Before(principal.ExpiresAt) {
		return modelprofile.Catalog{}, errs.ErrPermissionDenied
	}
	if err := ctx.Err(); err != nil {
		return modelprofile.Catalog{}, err
	}
	catalog := service.Catalog()
	if catalog.Version == "" || len(catalog.Version) > 128 || catalog.ObservedAt.IsZero() || catalog.ObservedAt.After(service.now()) ||
		len(catalog.Models) == 0 || len(catalog.Models) > 128 {
		return modelprofile.Catalog{}, errs.ErrProviderUnavailable
	}
	if err := ctx.Err(); err != nil {
		return modelprofile.Catalog{}, err
	}
	if !service.now().Before(principal.ExpiresAt) {
		return modelprofile.Catalog{}, errs.ErrPermissionDenied
	}
	return catalog, nil
}
