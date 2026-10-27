package grpc

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// castRoleImageRecipeRevision copies a stored recipe revision's identity, provenance, build,
// image and promotion pins into its wire snapshot. It preserves the existing uint64-to-int64
// conversions and timestamp projection without adding range, digest or ownership validation.
// The distinct platform and organization read adapters retain their own principals, owner
// queries and pagination; copying these pins neither authorizes nor performs promotion.
// castRoleImageRecipeRevision копирует идентичность, привязки происхождения, сборки, образа
// и продвижения сохранённой ревизии рецепта в wire-снимок. Сохраняет прежние преобразования
// uint64 в int64 и timestamp без новых проверок диапазона, digest либо владения. Отдельные
// адаптеры платформенного и организационного чтения сохраняют свои principal, запросы владельцу
// и пагинацию; копирование привязок не разрешает продвижение и не выполняет его.
func castRoleImageRecipeRevision(value entity.RoleImageRecipeRevision) *controlplanev1.RoleImageRecipeRevision {
	return &controlplanev1.RoleImageRecipeRevision{
		Ref: value.Ref, RecipeRef: value.RecipeRef, Revision: int64(value.Revision), RecipeVersion: int64(value.RecipeVersion),
		RecipeGeneration: int64(value.RecipeGeneration), SpecSha256: value.SpecSHA256,
		ProvenanceSha256: value.ProvenanceSHA256, SourceSha256: value.SourceSHA256,
		ImmutableBuildSha256: value.ImmutableBuildSHA256, ImageArtifactRef: value.ImageArtifactRef,
		ManifestDigest: value.ManifestDigest, PromotedReference: value.PromotedReference,
		PromotionReceiptSha256: value.PromotionReceiptSHA256, CreatedAt: timestamp(value.CreatedAt),
	}
}
