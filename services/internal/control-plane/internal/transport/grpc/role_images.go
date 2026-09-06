package grpc

import (
	"context"
	"errors"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	roleimagerepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// RoleImageServer материализует специализированный supply-chain contract.
type RoleImageServer struct {
	controlplanev1.UnimplementedRoleImageServiceServer
	service *roleimageservice.Service
}

func NewRoleImageServer(service *roleimageservice.Service) (*RoleImageServer, error) {
	if service == nil {
		return nil, errors.New("role image service is required")
	}
	return &RoleImageServer{service: service}, nil
}

func castRoleImageRecipeInput(input entity.RoleImageRecipeInput) *controlplanev1.RoleImageRecipeInput {
	result := &controlplanev1.RoleImageRecipeInput{
		BaseImageReference: input.BaseImageReference,
		BaseImageDigest:    input.BaseImageDigest,
		SourceRef:          input.SourceRef,
		SourceRevision:     input.SourceRevision,
		SourceSha256:       input.SourceSHA256,
		ContextRef:         input.ContextRef,
		ContextSha256:      input.ContextSHA256,
		BuilderSha256:      input.BuilderSHA256,
		FrontendSha256:     input.FrontendSHA256,
		InstallationBlock:  input.InstallationBlock,
		ToolchainSha256:    input.ToolchainSHA256,
		EnvironmentKey:     input.EnvironmentKey,
		PackageKeys:        append([]string(nil), input.PackageKeys...),
		ToolKeys:           append([]string(nil), input.ToolKeys...),
		Dockerfile:         input.Dockerfile,
	}
	for _, item := range input.Platforms {
		result.Platforms = append(result.Platforms, &controlplanev1.RoleImagePlatform{
			Os: item.OS, Architecture: item.Architecture, Variant: item.Variant,
		})
	}
	for _, item := range input.Packages {
		result.Packages = append(result.Packages, &controlplanev1.RoleImagePackage{
			Manager: item.Manager, Name: item.Name, Version: item.Version,
			Digest: item.Digest, SourceRef: item.SourceRef,
		})
	}
	for _, item := range input.Tools {
		result.Tools = append(result.Tools, &controlplanev1.RoleImageTool{
			Name: item.Name, Version: item.Version, SourceRef: item.SourceRef, Sha256: item.SHA256,
		})
	}
	return result
}

func castRoleImageRecipe(input entity.RoleImageRecipe) *controlplanev1.RoleImageRecipe {
	var lineage *controlplanev1.RoleImageManagedLineage
	if input.ManagedLineage != nil {
		value := input.ManagedLineage
		lineage = &controlplanev1.RoleImageManagedLineage{ConfigurationRef: value.ConfigurationRef, RevisionRef: value.RevisionRef, Revision: value.Revision, ManagedBy: value.ManagedBy, SourceRef: value.SourceRef, SourceRevision: value.SourceRevision, Origin: value.Origin}
	}
	return &controlplanev1.RoleImageRecipe{
		SourceAvailable: input.SourceAvailable,
		ManagedLineage:  lineage,
		Ref:             input.Ref, Version: input.Version, ProjectRef: input.ProjectRef, RoleDefinitionRef: input.RoleDefinitionRef,
		Name: input.Name, State: input.State,
		Generation: input.Generation, SpecSha256: input.SpecSHA256,
		PolicyRevision: input.PolicyRevision, PolicySha256: input.PolicySHA256,
		RoleRuntimeContractRevision: input.RoleRuntimeContractRevision,
		RoleRuntimeContractSha256:   input.RoleRuntimeContractSHA256,
		ActiveImageArtifactRef:      input.ActiveImageArtifactRef,
		PromotedImageReference:      input.PromotedImageReference,
		CreatedAt:                   timestamp(input.CreatedAt), UpdatedAt: timestamp(input.UpdatedAt),
		NextActions: append([]string(nil), input.NextActions...),
		Environment: &controlplanev1.RoleEnvironmentSelection{
			EnvironmentKey:    input.Input.EnvironmentKey,
			PackageKeys:       append([]string(nil), input.Input.PackageKeys...),
			ToolKeys:          append([]string(nil), input.Input.ToolKeys...),
			InstallationBlock: input.Input.InstallationBlock,
			Dockerfile:        input.Input.Dockerfile,
		},
	}
}

func castRoleEnvironment(input roleimageservice.Environment) *controlplanev1.RoleEnvironment {
	result := &controlplanev1.RoleEnvironment{
		Key: input.Key, NameMessageKey: input.NameMessageKey,
		DescriptionMessageKey: input.DescriptionMessageKey,
		SoftwareMessageKeys:   append([]string(nil), input.SoftwareMessageKeys...),
		Recommended:           input.Recommended, Available: input.Available,
		UnavailableMessageKey:     input.UnavailableMessageKey,
		CustomInstallationAllowed: input.CustomInstallationAllowed,
		DockerfileTemplate:        input.DockerfileTemplate,
	}
	for _, item := range input.Input.Platforms {
		result.Platforms = append(result.Platforms, &controlplanev1.RoleImagePlatform{
			Os: item.OS, Architecture: item.Architecture, Variant: item.Variant,
		})
	}
	return result
}

func domainRoleEnvironmentSelection(input *controlplanev1.RoleEnvironmentSelection) entity.RoleEnvironmentSelection {
	if input == nil {
		return entity.RoleEnvironmentSelection{}
	}
	return entity.RoleEnvironmentSelection{
		EnvironmentKey:    input.GetEnvironmentKey(),
		PackageKeys:       append([]string(nil), input.GetPackageKeys()...),
		ToolKeys:          append([]string(nil), input.GetToolKeys()...),
		InstallationBlock: input.GetInstallationBlock(),
		Dockerfile:        input.GetDockerfile(),
	}
}

func imageBuildStage(stage string) controlplanev1.ImageBuildStage {
	value, exists := controlplanev1.ImageBuildStage_value["IMAGE_BUILD_STAGE_"+stage]
	if !exists {
		return controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_UNSPECIFIED
	}
	return controlplanev1.ImageBuildStage(value)
}

func castImageBuild(input entity.ImageBuild) *controlplanev1.ImageBuild {
	result := &controlplanev1.ImageBuild{
		SourceAvailable:          input.SourceAvailable,
		ConfigurationRevisionRef: input.ConfigurationRevisionRef,
		Ref:                      input.Ref, Version: input.Version, RecipeRef: input.RecipeRef,
		RecipeVersion: input.RecipeVersion, RecipeGeneration: input.RecipeGeneration,
		SpecSha256: input.SpecSHA256, Attempt: input.Attempt, Fence: input.Fence,
		Stage: imageBuildStage(input.Stage), ProgressPercent: input.ProgressPercent,
		StagingReference: input.StagingReference, ManifestDigest: input.ManifestDigest,
		ProvenanceSha256: input.ProvenanceSHA256, ImmutableBuildSha256: input.ImmutableBuildSHA256,
		SafeErrorCode: input.SafeErrorCode, DiagnosticCode: input.DiagnosticCode,
		DiagnosticSummary: input.DiagnosticSummary, CreatedAt: timestamp(input.CreatedAt),
		UpdatedAt: timestamp(input.UpdatedAt), Dockerfile: input.Dockerfile,
	}
	if input.LeaseExpiresAt != nil {
		result.LeaseExpiresAt = timestamp(*input.LeaseExpiresAt)
	}
	return result
}

func imageAdmissionVerdict(verdict string) controlplanev1.ImageAdmissionVerdict {
	value, exists := controlplanev1.ImageAdmissionVerdict_value["IMAGE_ADMISSION_VERDICT_"+verdict]
	if !exists {
		return controlplanev1.ImageAdmissionVerdict_IMAGE_ADMISSION_VERDICT_UNSPECIFIED
	}
	return controlplanev1.ImageAdmissionVerdict(value)
}

func castImageArtifact(input entity.ImageArtifact) *controlplanev1.ImageArtifact {
	result := &controlplanev1.ImageArtifact{
		Ref: input.Ref, Version: input.Version, RecipeRef: input.RecipeRef,
		RecipeVersion: input.RecipeVersion, RecipeGeneration: input.RecipeGeneration,
		SpecSha256: input.SpecSHA256, BuildRef: input.BuildRef, BuildVersion: input.BuildVersion,
		BuildAttempt: input.BuildAttempt, StagingReference: input.StagingReference,
		ManifestDigest: input.ManifestDigest, ImmutableBuildSha256: input.ImmutableBuildSHA256,
		ProvenanceSha256: input.ProvenanceSHA256, BaseImageDigest: input.BaseImageDigest,
		SourceSha256: input.SourceSHA256, ContextSha256: input.ContextSHA256,
		BuilderSha256: input.BuilderSHA256, FrontendSha256: input.FrontendSHA256,
		ToolchainSha256: input.ToolchainSHA256, PolicyRevision: input.PolicyRevision,
		PolicySha256: input.PolicySHA256, SbomSha256: input.SBOMSHA256,
		VulnerabilityEvidenceSha256: input.VulnerabilityEvidenceSHA256,
		AdmissionVerdict:            imageAdmissionVerdict(input.AdmissionVerdict),
		SignatureIdentity:           input.SignatureIdentity, SignatureSha256: input.SignatureSHA256,
		AdmissionRevision:                 input.AdmissionRevision,
		AdmissionReceiptSha256:            input.AdmissionReceiptSHA256,
		AdmissionReceiptOciManifestDigest: input.AdmissionReceiptOCIManifestDigest,
		PromotedReference:                 input.PromotedReference,
		PromotionReadbackSha256:           input.PromotionReadbackSHA256,
		RoleRuntimeContractRevision:       input.RoleRuntimeContractRevision,
		RoleRuntimeContractSha256:         input.RoleRuntimeContractSHA256,
		CreatedAt:                         timestamp(input.CreatedAt), UpdatedAt: timestamp(input.UpdatedAt),
	}
	if input.PromotedAt != nil {
		result.PromotedAt = timestamp(*input.PromotedAt)
	}
	for _, item := range input.Platforms {
		result.Platforms = append(result.Platforms, &controlplanev1.RoleImagePlatform{
			Os: item.OS, Architecture: item.Architecture, Variant: item.Variant,
		})
	}
	for _, item := range input.Tools {
		result.Tools = append(result.Tools, &controlplanev1.RoleImageTool{Name: item.Name, Version: item.Version})
	}
	return result
}

func castRoleImageBuildInput(input entity.RoleImageBuildInput) *controlplanev1.RoleImageBuildInput {
	return &controlplanev1.RoleImageBuildInput{
		RecipeRef: input.RecipeRef, RecipeVersion: input.RecipeVersion,
		RecipeGeneration: input.RecipeGeneration, SpecSha256: input.SpecSHA256,
		BaseImageReference: input.BaseImageReference, BaseImageDigest: input.BaseImageDigest,
		SourceRef: input.SourceRef, SourceRevision: input.SourceRevision, SourceSha256: input.SourceSHA256,
		ContextRef: input.ContextRef, ContextSha256: input.ContextSHA256,
		BuilderSha256: input.BuilderSHA256, FrontendSha256: input.FrontendSHA256,
		Platforms:         castRoleImageRecipeInput(entity.RoleImageRecipeInput{Platforms: input.Platforms}).GetPlatforms(),
		Packages:          castRoleImageRecipeInput(entity.RoleImageRecipeInput{Packages: input.Packages}).GetPackages(),
		Tools:             castRoleImageRecipeInput(entity.RoleImageRecipeInput{Tools: input.Tools}).GetTools(),
		InstallationBlock: input.InstallationBlock, ToolchainSha256: input.ToolchainSHA256,
		PolicyRevision: input.PolicyRevision, PolicySha256: input.PolicySHA256,
		ImmutableBuildSha256:        input.ImmutableBuildSHA256,
		RoleRuntimeContractRevision: input.RoleRuntimeContractRevision,
		RoleRuntimeContractSha256:   input.RoleRuntimeContractSHA256,
		Dockerfile:                  input.Dockerfile,
	}
}

func roleImagePrincipal(ctx context.Context, method string) (value.Principal, error) {
	return principal(ctx, method)
}

func roleImageLeaseInput(principal value.Principal, key, ref, token string, version, fence uint64, attempt uint32) roleimagerepository.BuildLeaseInput {
	return roleimagerepository.BuildLeaseInput{
		Principal: principal, IdempotencyKey: key, BuildRef: ref, LeaseToken: token,
		ExpectedVersion: version, ExpectedAttempt: attempt, ExpectedFence: fence,
	}
}

func roleImageAction(action controlplanev1.RoleImageRecipeAction) string {
	return strings.TrimPrefix(action.String(), "ROLE_IMAGE_RECIPE_ACTION_")
}

func (server *RoleImageServer) ListRoleEnvironments(ctx context.Context, _ *controlplanev1.ListRoleEnvironmentsRequest) (*controlplanev1.ListRoleEnvironmentsResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ListRoleEnvironments_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, err := server.service.ListEnvironments(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRoleEnvironmentsResponse{}
	for _, item := range items {
		response.Environments = append(response.Environments, castRoleEnvironment(item))
	}
	return response, nil
}

func (server *RoleImageServer) ListRoleImageRecipes(ctx context.Context, request *controlplanev1.ListRoleImageRecipesRequest) (*controlplanev1.ListRoleImageRecipesResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ListRoleImageRecipes_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, nextPage, total, err := server.service.List(ctx, p, roleimagerepository.Filter{
		ProjectRef: request.GetProjectRef(), RoleDefinitionRef: request.GetRoleDefinitionRef(), Page: page(request.GetPage()), Query: request.GetQuery(), State: request.GetState(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRoleImageRecipesResponse{Page: &controlplanev1.PageInfo{NextPageToken: nextPage}, Total: total}
	for _, item := range items {
		response.Recipes = append(response.Recipes, castRoleImageRecipe(item))
	}
	return response, nil
}

func (server *RoleImageServer) GetRoleImageRecipe(ctx context.Context, request *controlplanev1.GetRoleImageRecipeRequest) (*controlplanev1.GetRoleImageRecipeResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_GetRoleImageRecipe_FullMethodName)
	if err != nil {
		return nil, err
	}
	detail, err := server.service.Get(ctx, p, request.GetRecipeRef())
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.GetRoleImageRecipeResponse{Recipe: castRoleImageRecipe(detail.Recipe)}
	for _, item := range detail.Builds {
		response.Builds = append(response.Builds, castImageBuild(item))
	}
	if detail.ActiveArtifact != nil {
		response.ActiveArtifact = castImageArtifact(*detail.ActiveArtifact)
	}
	if detail.PromotionCandidate != nil {
		response.PromotionCandidate = castImageArtifact(*detail.PromotionCandidate)
	}
	return response, nil
}

func (server *RoleImageServer) ManageRoleImageRecipe(ctx context.Context, request *controlplanev1.ManageRoleImageRecipeRequest) (*controlplanev1.ManageRoleImageRecipeResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ManageRoleImageRecipe_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.Manage(ctx, roleimagerepository.ManageInput{
		Principal: p, Mutation: mutation(request.GetMutation()), Action: roleImageAction(request.GetAction()),
		RecipeRef: request.GetRecipeRef(), ProjectRef: request.GetProjectRef(),
		RoleDefinitionRef: request.GetRoleDefinitionRef(), Name: request.GetName(),
		Environment: domainRoleEnvironmentSelection(request.GetEnvironment()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ManageRoleImageRecipeResponse{Recipe: castRoleImageRecipe(result.Recipe), Reused: result.Reused}
	if result.Build != nil {
		response.ImageBuild = castImageBuild(*result.Build)
	}
	if result.Artifact != nil {
		response.ImageArtifact = castImageArtifact(*result.Artifact)
	}
	return response, nil
}

func (server *RoleImageServer) ClaimImageBuild(ctx context.Context, request *controlplanev1.ClaimImageBuildRequest) (*controlplanev1.ClaimImageBuildResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ClaimImageBuild_FullMethodName)
	if err != nil {
		return nil, err
	}
	claim, err := server.service.ClaimBuild(ctx, p, request.GetIdempotencyKey())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ClaimImageBuildResponse{
		ImageBuild: castImageBuild(claim.Build), Input: castRoleImageBuildInput(claim.Input),
		LeaseToken: claim.LeaseToken, Fence: claim.Fence, AuthorityGeneration: claim.AuthorityGeneration,
		LeaseExpiresAt: timestamp(claim.LeaseExpiresAt),
	}, nil
}

func (server *RoleImageServer) RenewImageBuild(ctx context.Context, request *controlplanev1.RenewImageBuildRequest) (*controlplanev1.RenewImageBuildResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_RenewImageBuild_FullMethodName)
	if err != nil {
		return nil, err
	}
	claim, err := server.service.RenewBuild(ctx, roleImageLeaseInput(p, request.GetIdempotencyKey(),
		request.GetImageBuildRef(), request.GetLeaseToken(), request.GetExpectedVersion(), request.GetExpectedFence(), request.GetExpectedAttempt()))
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.RenewImageBuildResponse{
		ImageBuild: castImageBuild(claim.Build), LeaseToken: claim.LeaseToken,
		LeaseExpiresAt: timestamp(claim.LeaseExpiresAt),
	}, nil
}

func (server *RoleImageServer) ReportImageBuildProgress(ctx context.Context, request *controlplanev1.ReportImageBuildProgressRequest) (*controlplanev1.ReportImageBuildProgressResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ReportImageBuildProgress_FullMethodName)
	if err != nil {
		return nil, err
	}
	input := roleimagerepository.BuildProgressInput{
		BuildLeaseInput: roleImageLeaseInput(p, request.GetIdempotencyKey(), request.GetImageBuildRef(),
			request.GetLeaseToken(), request.GetExpectedVersion(), request.GetExpectedFence(), request.GetExpectedAttempt()),
		Stage:           strings.TrimPrefix(request.GetStage().String(), "IMAGE_BUILD_STAGE_"),
		ProgressPercent: request.GetProgressPercent(),
	}
	build, err := server.service.ReportBuildProgress(ctx, input)
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ReportImageBuildProgressResponse{ImageBuild: castImageBuild(build)}, nil
}

func (server *RoleImageServer) CompleteImageBuild(ctx context.Context, request *controlplanev1.CompleteImageBuildRequest) (*controlplanev1.CompleteImageBuildResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_CompleteImageBuild_FullMethodName)
	if err != nil {
		return nil, err
	}
	build, artifact, err := server.service.CompleteBuild(ctx, roleimagerepository.BuildCompletionInput{
		BuildLeaseInput: roleImageLeaseInput(p, request.GetIdempotencyKey(), request.GetImageBuildRef(),
			request.GetLeaseToken(), request.GetExpectedVersion(), request.GetExpectedFence(), request.GetExpectedAttempt()),
		StagingReference: request.GetStagingReference(), ManifestDigest: request.GetManifestDigest(),
		ProvenanceSHA256: request.GetProvenanceSha256(), ImmutableBuildSHA256: request.GetImmutableBuildSha256(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.CompleteImageBuildResponse{ImageBuild: castImageBuild(build), ImageArtifact: castImageArtifact(artifact)}, nil
}

func (server *RoleImageServer) FailImageBuild(ctx context.Context, request *controlplanev1.FailImageBuildRequest) (*controlplanev1.FailImageBuildResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_FailImageBuild_FullMethodName)
	if err != nil {
		return nil, err
	}
	build, err := server.service.FailBuild(ctx, roleimagerepository.BuildFailureInput{
		BuildLeaseInput: roleImageLeaseInput(p, request.GetIdempotencyKey(), request.GetImageBuildRef(),
			request.GetLeaseToken(), request.GetExpectedVersion(), request.GetExpectedFence(), request.GetExpectedAttempt()),
		ErrorCode: request.GetErrorCode(), DiagnosticCode: request.GetDiagnosticCode(), DiagnosticSummary: request.GetDiagnosticSummary(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.FailImageBuildResponse{ImageBuild: castImageBuild(build)}, nil
}

func (server *RoleImageServer) ClaimImageAdmission(ctx context.Context, request *controlplanev1.ClaimImageAdmissionRequest) (*controlplanev1.ClaimImageAdmissionResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ClaimImageAdmission_FullMethodName)
	if err != nil {
		return nil, err
	}
	claim, err := server.service.ClaimAdmission(ctx, p, request.GetIdempotencyKey())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ClaimImageAdmissionResponse{
		ImageArtifact: castImageArtifact(claim.Artifact), ClaimToken: claim.ClaimToken,
		Fence: claim.Fence, AuthorityGeneration: claim.AuthorityGeneration,
		ClaimExpiresAt: timestamp(claim.ClaimExpiresAt),
	}, nil
}

func (server *RoleImageServer) RecordImageAdmission(ctx context.Context, request *controlplanev1.RecordImageAdmissionRequest) (*controlplanev1.RecordImageAdmissionResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_RecordImageAdmission_FullMethodName)
	if err != nil {
		return nil, err
	}
	artifact, err := server.service.RecordAdmission(ctx, roleimagerepository.AdmissionRecordInput{
		Principal: p, IdempotencyKey: request.GetIdempotencyKey(), ArtifactRef: request.GetImageArtifactRef(),
		ExpectedVersion: request.GetExpectedVersion(), ExpectedFence: request.GetExpectedFence(),
		ClaimToken: request.GetClaimToken(), ManifestDigest: request.GetManifestDigest(),
		ImmutableBuildSHA256: request.GetImmutableBuildSha256(), ProvenanceSHA256: request.GetProvenanceSha256(),
		SBOMSHA256: request.GetSbomSha256(), VulnerabilityEvidenceSHA256: request.GetVulnerabilityEvidenceSha256(),
		PolicyRevision: request.GetPolicyRevision(), PolicySHA256: request.GetPolicySha256(),
		Verdict:           strings.TrimPrefix(request.GetVerdict().String(), "IMAGE_ADMISSION_VERDICT_"),
		SignatureIdentity: request.GetSignatureIdentity(), SignatureSHA256: request.GetSignatureSha256(),
		AdmissionReceiptSHA256:            request.GetAdmissionReceiptSha256(),
		AdmissionReceiptOCIManifestDigest: request.GetAdmissionReceiptOciManifestDigest(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.RecordImageAdmissionResponse{ImageArtifact: castImageArtifact(artifact)}, nil
}

func (server *RoleImageServer) ClaimImagePromotion(ctx context.Context, request *controlplanev1.ClaimImagePromotionRequest) (*controlplanev1.ClaimImagePromotionResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_ClaimImagePromotion_FullMethodName)
	if err != nil {
		return nil, err
	}
	claim, err := server.service.ClaimPromotion(ctx, p, request.GetIdempotencyKey())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.ClaimImagePromotionResponse{
		ImageArtifact: castImageArtifact(claim.Artifact), PromotionClaim: claim.PromotionClaim,
		Fence: claim.Fence, AuthorityGeneration: claim.AuthorityGeneration,
		ClaimExpiresAt: timestamp(claim.ClaimExpiresAt),
	}, nil
}

func (server *RoleImageServer) AuthorizeImagePromotion(ctx context.Context, request *controlplanev1.AuthorizeImagePromotionRequest) (*controlplanev1.AuthorizeImagePromotionResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_AuthorizeImagePromotion_FullMethodName)
	if err != nil {
		return nil, err
	}
	authorization, err := server.service.AuthorizePromotion(ctx, roleimagerepository.PromotionAuthorizeInput{
		Principal: p, IdempotencyKey: request.GetIdempotencyKey(), ArtifactRef: request.GetImageArtifactRef(),
		ExpectedVersion: request.GetExpectedVersion(), PromotionClaim: request.GetPromotionClaim(),
		ManifestDigest: request.GetManifestDigest(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.AuthorizeImagePromotionResponse{
		ImageArtifact:          castImageArtifact(authorization.Artifact),
		AuthorizationToken:     authorization.AuthorizationToken,
		AuthorizationExpiresAt: timestamp(authorization.AuthorizationExpiresAt),
	}, nil
}

func (server *RoleImageServer) CompleteImagePromotion(ctx context.Context, request *controlplanev1.CompleteImagePromotionRequest) (*controlplanev1.CompleteImagePromotionResponse, error) {
	p, err := roleImagePrincipal(ctx, controlplanev1.RoleImageService_CompleteImagePromotion_FullMethodName)
	if err != nil {
		return nil, err
	}
	artifact, err := server.service.CompletePromotion(ctx, roleimagerepository.PromotionCompleteInput{
		Principal: p, IdempotencyKey: request.GetIdempotencyKey(), ArtifactRef: request.GetImageArtifactRef(),
		ExpectedVersion: request.GetExpectedVersion(), AuthorizationToken: request.GetAuthorizationToken(),
		PromotedReference: request.GetPromotedReference(), ManifestDigest: request.GetManifestDigest(),
		PromotionReadbackSHA256: request.GetPromotionReadbackSha256(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.CompleteImagePromotionResponse{ImageArtifact: castImageArtifact(artifact)}, nil
}
