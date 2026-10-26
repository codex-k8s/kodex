package httptransport

import (
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// roleImageDetailResponse exposes the common read-only fields of the two generated recipe
// replies. Their callers retain distinct RPCs and recipe ownership checks before projection.
// roleImageDetailResponse задаёт общие поля чтения двух generated ответов рецепта.
// Разные RPC и проверки принадлежности рецепта остаются у callers до преобразования.
type roleImageDetailResponse interface {
	// GetRecipe returns the recipe already accepted by the caller's route ownership guard.
	// GetRecipe возвращает рецепт, уже допущенный проверкой принадлежности в маршруте caller.
	GetRecipe() *controlplanev1.RoleImageRecipe
	// GetBuilds returns owner-provided builds in their original order, including invalid nils.
	// GetBuilds возвращает сборки владельца в исходном порядке, включая недопустимые nil.
	GetBuilds() []*controlplanev1.ImageBuild
	// GetActiveArtifact returns the optional current artifact without interpreting its authority.
	// GetActiveArtifact возвращает необязательный текущий артефакт без толкования его полномочий.
	GetActiveArtifact() *controlplanev1.ImageArtifact
	// GetPromotionCandidate returns the optional candidate independently of the active artifact.
	// GetPromotionCandidate возвращает необязательного кандидата независимо от текущего артефакта.
	GetPromotionCandidate() *controlplanev1.ImageArtifact
	// GetAdmissionFailure returns optional technical failure metadata for the final checked writer.
	// GetAdmissionFailure возвращает необязательные metadata технического отказа для конечного writer.
	GetAdmissionFailure() *controlplanev1.RoleImageAdmissionFailure
}

// writeRoleImageRecipeDetail projects a recipe after the caller's ownership guard, preserving
// lineage/source, build, active-artifact, candidate and ETag/admission-failure ordering. A nil
// validBuild retains the project route's existing checks; the organization route supplies its
// exact build owner rule. The required validArtifact preserves each route's existing risk and
// ownership checks. These rules validate owner readback and grant no permission or promotion.
// writeRoleImageRecipeDetail преобразует рецепт после проверки принадлежности у caller,
// сохраняя порядок lineage/source, сборок, текущего артефакта, кандидата и ETag/admission failure.
// Nil validBuild сохраняет прежние проверки проектного маршрута; организационный маршрут
// передаёт точное правило владельца сборки. Обязательный validArtifact сохраняет прежние
// проверки риска и владельца каждого маршрута. Правила сверяют ответ владельца, не выдают
// полномочий и не выполняют продвижение.
func writeRoleImageRecipeDetail(writer http.ResponseWriter, response roleImageDetailResponse,
	validBuild func(*controlplanev1.ImageBuild) bool,
	validArtifact func(*controlplanev1.ImageArtifact) bool,
) {
	if !validRoleImageLineage(response.GetRecipe().GetManagedLineage()) || !validRoleImageSource(response.GetRecipe()) {
		writeLocalProblem(writer, http.StatusBadGateway, invalidUpstreamResponseCode, false)
		return
	}
	result := generated.RoleImageRecipeDetail{
		Recipe: publicRoleImageRecipe(response.GetRecipe()),
		Builds: make([]generated.RoleImageBuild, 0, len(response.GetBuilds())),
	}
	for _, build := range response.GetBuilds() {
		if build == nil || validBuild != nil && !validBuild(build) || !validRoleImageBuildSource(build) || build.GetConfigurationRevisionRef() != "" && !effectiveCapabilityRef(build.GetConfigurationRevisionRef()) {
			writeLocalProblem(writer, http.StatusBadGateway, invalidUpstreamResponseCode, false)
			return
		}
		result.Builds = append(result.Builds, publicRoleImageBuild(build))
	}
	if response.GetActiveArtifact() != nil {
		if !validArtifact(response.GetActiveArtifact()) {
			writeLocalProblem(writer, http.StatusBadGateway, invalidUpstreamResponseCode, false)
			return
		}
		artifact := publicRoleImageArtifact(response.GetActiveArtifact())
		result.ActiveArtifact = &artifact
	}
	if response.GetPromotionCandidate() != nil {
		if !validArtifact(response.GetPromotionCandidate()) {
			writeLocalProblem(writer, http.StatusBadGateway, invalidUpstreamResponseCode, false)
			return
		}
		artifact := publicRoleImageArtifact(response.GetPromotionCandidate())
		result.PromotionCandidate = &artifact
	}
	setVersionETag(writer, response.GetRecipe().GetVersion())
	writeRoleImageDetail(writer, result, response.GetAdmissionFailure(), response.GetRecipe(), response.GetBuilds())
}
