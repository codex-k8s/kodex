package platform

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// errContextWindowImageUnsupported distinguishes a selected-image feature failure from catalog validation.
// errContextWindowImageUnsupported отличает неподдержку выбранным образом от ошибки проверки каталога.
var errContextWindowImageUnsupported = fmt.Errorf("selected image does not support context window tokens: %w", errs.ErrConflict)

// queryRuntimeOverlaySelectedImage reads the exact currently bound image in the owner tenant without granting access.
// queryRuntimeOverlaySelectedImage читает точный текущий образ привязанного окружения в организации владельца без выдачи доступа.
//
//go:embed sql/runtime_overlay__selected_image.sql
var queryRuntimeOverlaySelectedImage string

// requireRuntimeOverlayImageFeature checks only explicit optional input against immutable admitted image evidence.
// Legacy unset overlays require no inventory. Bootstrap, missing, mixed-platform or foreign evidence fails closed.
// It does not authorize the actor or select a fallback image; callers retain those responsibilities.
// requireRuntimeOverlayImageFeature проверяет только явный необязательный вход по неизменяемому evidence допущенного образа.
// Прежним overlay без настройки inventory не нужен. Bootstrap, отсутствие, разные платформы или чужое evidence дают отказ.
// Метод не авторизует субъекта и не выбирает запасной образ; это остаётся обязанностью вызывающего.
func requireRuntimeOverlayImageFeature(ctx context.Context, tx pgx.Tx, organizationID, artifactRef, imageDigest, overlay string) error {
	parsed, err := runtimecontract.ParseConfigOverlay(overlay)
	if err != nil {
		return errs.ErrConflict
	}
	if parsed.Context == nil || parsed.Context.WindowTokens == nil {
		return nil
	}
	if artifactRef == "" || imageDigest == "" {
		return errContextWindowImageUnsupported
	}
	artifact, err := scanRoleImageArtifact(tx.QueryRow(ctx, queryRoleImagesGetActiveArtifact, organizationID, artifactRef))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errContextWindowImageUnsupported
		}
		return errs.ErrUnavailable
	}
	if artifact.ManifestDigest != imageDigest || artifact.AdmissionVerdict != "ACCEPTED" || artifact.PromotionState != "PROMOTED" || artifact.SignatureIdentity == "platform-owned-bootstrap" || !artifact.ToolInventory.SupportsInputFeature(runtimecontract.ContextWindowInputFeature) {
		return errContextWindowImageUnsupported
	}
	return nil
}

// requireCurrentRuntimeOverlayImageFeature resolves the current binding only for an explicit optional input.
// Owner commands and launch/session preflight use this read; execution claim separately checks its frozen image pins.
// requireCurrentRuntimeOverlayImageFeature разрешает текущую привязку только для явного необязательного входа.
// Команды владельца и запуск/сессия используют это чтение; claim отдельно сверяет закреплённый образ выполнения.
func requireCurrentRuntimeOverlayImageFeature(ctx context.Context, tx pgx.Tx, organizationID, agentRef, overlay string) error {
	parsed, err := runtimecontract.ParseConfigOverlay(overlay)
	if err != nil {
		return errs.ErrConflict
	}
	if parsed.Context == nil || parsed.Context.WindowTokens == nil {
		return nil
	}
	var artifactRef, digest string
	if err := tx.QueryRow(ctx, queryRuntimeOverlaySelectedImage, organizationID, agentRef).Scan(&artifactRef, &digest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errContextWindowImageUnsupported
		}
		return errs.ErrUnavailable
	}
	return requireRuntimeOverlayImageFeature(ctx, tx, organizationID, artifactRef, digest, overlay)
}
