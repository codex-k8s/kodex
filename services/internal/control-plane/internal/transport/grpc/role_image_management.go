package grpc

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	roleimagerepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
)

// roleImageManagementView carries the already resolved management projection for the distinct
// project and organization RPC envelopes. Optional build/artifact nils do not imply a claim,
// admission or promotion; Reused is copied from the domain result.
// roleImageManagementView несёт уже разрешённую проекцию управления для отдельных проектного
// и организационного RPC envelopes. Nil сборки/артефакта не означает claim, admission либо
// promotion; Reused переносится из доменного результата.
type roleImageManagementView struct {
	Recipe        *controlplanev1.RoleImageRecipe
	Reused        bool
	ImageBuild    *controlplanev1.ImageBuild
	ImageArtifact *controlplanev1.ImageArtifact
}

// castRoleImageManagement preserves recipe/reuse and optional build/artifact presence using
// the existing pure casters in their original order. It receives no principal, performs no read
// or mutation and does not share the project/organization authorization paths.
// castRoleImageManagement сохраняет рецепт, reuse и presence необязательных сборки/артефакта
// прежними чистыми casters в исходном порядке. Principal не принимает, чтения/изменения не
// выполняет и не объединяет проектный/организационный пути авторизации.
func castRoleImageManagement(result roleimagerepository.ManageResult) roleImageManagementView {
	response := roleImageManagementView{Recipe: castRoleImageRecipe(result.Recipe), Reused: result.Reused}
	if result.Build != nil {
		response.ImageBuild = castImageBuild(*result.Build)
	}
	if result.Artifact != nil {
		response.ImageArtifact = castImageArtifact(*result.Artifact)
	}
	return response
}
