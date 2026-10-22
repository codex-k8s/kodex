package grpc

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	roleimagerepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
)

// roleImageRecipeDetailView is a private response projection shared by project
// and organization reads, not an authorization context or a protobuf envelope.
// roleImageRecipeDetailView — закрытая проекция ответа для проектного и
// организационного чтения, не контекст полномочий и не protobuf-конверт.
type roleImageRecipeDetailView struct {
	Recipe             *controlplanev1.RoleImageRecipe
	AdmissionFailure   *controlplanev1.RoleImageAdmissionFailure
	Builds             []*controlplanev1.ImageBuild
	ActiveArtifact     *controlplanev1.ImageArtifact
	PromotionCandidate *controlplanev1.ImageArtifact
}

// castRoleImageRecipeDetail maps an already resolved detail without changing
// build order, nil optional artifacts/failure or the nil result for zero builds.
// Each caller still owns its exact principal check, domain read and typed RPC
// envelope; this mapper performs no lookup, authorization or lifecycle effect.
// castRoleImageRecipeDetail преобразует уже разрешённые данные, не меняя порядок
// сборок, nil необязательных артефактов/ошибки и nil при отсутствии сборок.
// Каждый вызывающий сохраняет проверку principal, доменное чтение и типизированный
// RPC-конверт; mapper не делает поиска, авторизации или эффектов жизненного цикла.
func castRoleImageRecipeDetail(detail roleimagerepository.Detail) roleImageRecipeDetailView {
	response := roleImageRecipeDetailView{Recipe: castRoleImageRecipe(detail.Recipe), AdmissionFailure: castRoleImageAdmissionFailure(detail.AdmissionFailure)}
	for _, item := range detail.Builds {
		response.Builds = append(response.Builds, castImageBuild(item))
	}
	if detail.ActiveArtifact != nil {
		response.ActiveArtifact = castImageArtifact(*detail.ActiveArtifact)
	}
	if detail.PromotionCandidate != nil {
		response.PromotionCandidate = castImageArtifact(*detail.PromotionCandidate)
	}
	return response
}
