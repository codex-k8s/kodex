package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (server *Server) ManageRoleImageRecipe(
	ctx context.Context,
	request *controlplanev1.ManageRoleImageRecipeRequest,
) (*controlplanev1.ManageRoleImageRecipeResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ManageRoleImageRecipe_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	var recipeInput *entity.RoleImageRecipeInput
	if request.GetInput() != nil {
		converted, convertErr := roleImageInputFromProto(request.GetInput())
		if convertErr != nil {
			return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
		}
		recipeInput = &converted
	}
	result, err := server.service.ManageRoleImageRecipe(ctx, resource.ManageRoleImageRecipeInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		Action:   resource.RoleImageRecipeAction(trimEnum(request.GetAction().String(), "ROLE_IMAGE_RECIPE_ACTION_")),
		RecipeID: request.GetRecipeId(), ExpectedVersion: request.GetExpectedVersion(),
		Name: request.GetName(), Input: recipeInput,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	recipe, err := toProtoResource(result.Recipe)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	response := &controlplanev1.ManageRoleImageRecipeResponse{Recipe: recipe, Reused: result.Reused}
	if result.ImageBuild.ID != "" {
		response.ImageBuild, err = toProtoResource(result.ImageBuild)
	}
	if err == nil && result.ImageArtifact.ID != "" {
		response.ImageArtifact, err = toProtoResource(result.ImageArtifact)
	}
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return response, nil
}

func (server *Server) GetRoleImageRecipe(
	ctx context.Context,
	request *controlplanev1.GetRoleImageRecipeRequest,
) (*controlplanev1.GetRoleImageRecipeResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_GetRoleImageRecipe_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.GetRoleImageRecipe(ctx, resource.GetRoleImageRecipeInput{
		Principal: principal, RecipeID: request.GetRecipeId(), ExpectedVersion: request.GetExpectedVersion(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	recipe, err := toProtoResource(result)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.GetRoleImageRecipeResponse{Recipe: recipe}, nil
}

func (server *Server) ClaimImageBuild(
	ctx context.Context,
	request *controlplanev1.ClaimImageBuildRequest,
) (*controlplanev1.ClaimImageBuildResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ClaimImageBuild_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	claim, err := server.service.ClaimImageBuild(ctx, resource.ClaimImageBuildInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	build, err := toProtoResource(claim.ImageBuild)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ClaimImageBuildResponse{
		ImageBuild: build,
		Input:      roleImageBuildInput(claim),
		LeaseToken: claim.LeaseToken, Fence: claim.Fence,
		AuthorityGeneration: claim.AuthorityGeneration, LeaseExpiresAt: timestamppb.New(claim.LeaseExpiresAt),
	}, nil
}

func roleImageBuildInput(claim resource.ImageBuildClaim) *controlplanev1.RoleImageBuildInput {
	input := roleImageInputToProto(claim.RecipeInput)
	spec := claim.ImageBuild.Spec.(entity.ImageBuildSpec)
	return &controlplanev1.RoleImageBuildInput{
		RecipeId: spec.RecipeID, RecipeVersion: spec.RecipeVersion, RecipeGeneration: spec.RecipeGeneration,
		SpecSha256: spec.SpecSHA256, BaseImageReference: input.GetBaseImageReference(), BaseImageDigest: input.GetBaseImageDigest(),
		SourceRef: input.GetSourceRef(), SourceRevision: input.GetSourceRevision(), SourceSha256: input.GetSourceSha256(),
		ContextRef: input.GetContextRef(), ContextSha256: input.GetContextSha256(), BuilderSha256: input.GetBuilderSha256(),
		FrontendSha256: input.GetFrontendSha256(), Platforms: input.GetPlatforms(), Packages: input.GetPackages(),
		Tools: input.GetTools(), InstallationBlock: input.GetInstallationBlock(), BuildSecretRefs: input.GetBuildSecretRefs(),
		ToolchainSha256: input.GetToolchainSha256(), PolicyRevision: claim.PolicyRevision,
		PolicySha256: claim.PolicySHA256, ImmutableBuildSha256: spec.ImmutableBuildSHA256,
		RoleRuntimeContractRevision: claim.RoleRuntimeContractRevision,
		RoleRuntimeContractSha256:   claim.RoleRuntimeContractSHA256,
		ProjectId:                   claim.ImageBuild.ProjectID,
	}
}

func (server *Server) RenewImageBuild(
	ctx context.Context,
	request *controlplanev1.RenewImageBuildRequest,
) (*controlplanev1.RenewImageBuildResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_RenewImageBuild_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.RenewImageBuild(ctx, resource.ImageBuildLeaseInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), ImageBuildID: request.GetImageBuildId(),
		ExpectedVersion: request.GetExpectedVersion(), ExpectedAttempt: request.GetExpectedAttempt(),
		ExpectedFence: request.GetExpectedFence(), LeaseToken: request.GetLeaseToken(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	build, err := toProtoResource(result.ImageBuild)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.RenewImageBuildResponse{ImageBuild: build, LeaseToken: result.LeaseToken,
		LeaseExpiresAt: timestamppb.New(result.LeaseExpiresAt)}, nil
}

func (server *Server) ReportImageBuildProgress(
	ctx context.Context,
	request *controlplanev1.ReportImageBuildProgressRequest,
) (*controlplanev1.ReportImageBuildProgressResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ReportImageBuildProgress_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.ReportImageBuildProgress(ctx, resource.ReportImageBuildProgressInput{
		ImageBuildLeaseInput: resource.ImageBuildLeaseInput{Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ImageBuildID: request.GetImageBuildId(), ExpectedVersion: request.GetExpectedVersion(),
			ExpectedAttempt: request.GetExpectedAttempt(), ExpectedFence: request.GetExpectedFence(), LeaseToken: request.GetLeaseToken()},
		Stage:           entity.ImageBuildStage(trimEnum(request.GetStage().String(), "IMAGE_BUILD_STAGE_")),
		ProgressPercent: request.GetProgressPercent(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	build, err := toProtoResource(result)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ReportImageBuildProgressResponse{ImageBuild: build}, nil
}

func (server *Server) CompleteImageBuild(
	ctx context.Context,
	request *controlplanev1.CompleteImageBuildRequest,
) (*controlplanev1.CompleteImageBuildResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_CompleteImageBuild_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.CompleteImageBuild(ctx, resource.CompleteImageBuildInput{
		ImageBuildLeaseInput: resource.ImageBuildLeaseInput{Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ImageBuildID: request.GetImageBuildId(), ExpectedVersion: request.GetExpectedVersion(),
			ExpectedAttempt: request.GetExpectedAttempt(), ExpectedFence: request.GetExpectedFence(), LeaseToken: request.GetLeaseToken()},
		StagingReference: request.GetStagingReference(), ManifestDigest: request.GetManifestDigest(),
		ProvenanceSHA256: request.GetProvenanceSha256(), ImmutableBuildSHA256: request.GetImmutableBuildSha256(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	build, err := toProtoResource(result.ImageBuild)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	artifact, err := toProtoResource(result.ImageArtifact)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.CompleteImageBuildResponse{ImageBuild: build, ImageArtifact: artifact}, nil
}

func (server *Server) FailImageBuild(
	ctx context.Context,
	request *controlplanev1.FailImageBuildRequest,
) (*controlplanev1.FailImageBuildResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_FailImageBuild_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.FailImageBuild(ctx, resource.FailImageBuildInput{
		ImageBuildLeaseInput: resource.ImageBuildLeaseInput{Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ImageBuildID: request.GetImageBuildId(), ExpectedVersion: request.GetExpectedVersion(),
			ExpectedAttempt: request.GetExpectedAttempt(), ExpectedFence: request.GetExpectedFence(), LeaseToken: request.GetLeaseToken()},
		ErrorCode: request.GetErrorCode(), DiagnosticCode: request.GetDiagnosticCode(),
		DiagnosticSummary: request.GetDiagnosticSummary(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	build, err := toProtoResource(result)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.FailImageBuildResponse{ImageBuild: build}, nil
}

func (server *Server) ManageImageBuild(
	ctx context.Context,
	request *controlplanev1.ManageImageBuildRequest,
) (*controlplanev1.ManageImageBuildResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ManageImageBuild_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.ManageImageBuild(ctx, resource.ManageImageBuildInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), ImageBuildID: request.GetImageBuildId(),
		ExpectedVersion: request.GetExpectedVersion(),
		Action:          resource.ImageBuildOwnerAction(trimEnum(request.GetAction().String(), "IMAGE_BUILD_OWNER_ACTION_")),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	build, err := toProtoResource(result)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ManageImageBuildResponse{ImageBuild: build}, nil
}

func (server *Server) ClaimImageAdmission(
	ctx context.Context,
	request *controlplanev1.ClaimImageAdmissionRequest,
) (*controlplanev1.ClaimImageAdmissionResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ClaimImageAdmission_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.ClaimImageAdmission(ctx, resource.ClaimImageAdmissionInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	artifact, err := toProtoResource(result.ImageArtifact)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ClaimImageAdmissionResponse{ImageArtifact: artifact, ClaimToken: result.ClaimToken,
		Fence: result.Fence, AuthorityGeneration: result.AuthorityGeneration,
		ClaimExpiresAt: timestamppb.New(result.ClaimExpiresAt)}, nil
}

func (server *Server) RecordImageAdmission(
	ctx context.Context,
	request *controlplanev1.RecordImageAdmissionRequest,
) (*controlplanev1.RecordImageAdmissionResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_RecordImageAdmission_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.RecordImageAdmission(ctx, resource.RecordImageAdmissionInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), ImageArtifactID: request.GetImageArtifactId(),
		ExpectedVersion: request.GetExpectedVersion(), ExpectedFence: request.GetExpectedFence(), ClaimToken: request.GetClaimToken(),
		ManifestDigest: request.GetManifestDigest(), ImmutableBuildSHA256: request.GetImmutableBuildSha256(),
		ProvenanceSHA256: request.GetProvenanceSha256(), SBOMSHA256: request.GetSbomSha256(),
		VulnerabilityEvidenceSHA256: request.GetVulnerabilityEvidenceSha256(), PolicyRevision: request.GetPolicyRevision(),
		PolicySHA256:      request.GetPolicySha256(),
		Verdict:           entity.ImageAdmissionVerdict(trimEnum(request.GetVerdict().String(), "IMAGE_ADMISSION_VERDICT_")),
		SignatureIdentity: request.GetSignatureIdentity(), SignatureSHA256: request.GetSignatureSha256(),
		AdmissionReceiptSHA256:            request.GetAdmissionReceiptSha256(),
		AdmissionReceiptOCIManifestDigest: request.GetAdmissionReceiptOciManifestDigest(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	artifact, err := toProtoResource(result.ImageArtifact)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.RecordImageAdmissionResponse{ImageArtifact: artifact}, nil
}

func (server *Server) ClaimImagePromotion(
	ctx context.Context,
	request *controlplanev1.ClaimImagePromotionRequest,
) (*controlplanev1.ClaimImagePromotionResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ClaimImagePromotion_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.ClaimImagePromotion(ctx, resource.ClaimImagePromotionInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	artifact, err := toProtoResource(result.ImageArtifact)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ClaimImagePromotionResponse{ImageArtifact: artifact,
		PromotionClaim: result.PromotionClaim, Fence: result.Fence,
		AuthorityGeneration: result.AuthorityGeneration,
		ClaimExpiresAt:      timestamppb.New(result.ClaimExpiresAt)}, nil
}

func (server *Server) CompleteImagePromotion(
	ctx context.Context,
	request *controlplanev1.CompleteImagePromotionRequest,
) (*controlplanev1.CompleteImagePromotionResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_CompleteImagePromotion_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.CompleteImagePromotion(ctx, resource.CompleteImagePromotionInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), ImageArtifactID: request.GetImageArtifactId(),
		ExpectedVersion: request.GetExpectedVersion(), AuthorizationToken: request.GetAuthorizationToken(),
		PromotedReference: request.GetPromotedReference(), ManifestDigest: request.GetManifestDigest(),
		PromotionReadbackSHA256: request.GetPromotionReadbackSha256(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	artifact, err := toProtoResource(result)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.CompleteImagePromotionResponse{ImageArtifact: artifact}, nil
}

func (server *Server) AuthorizeImagePromotion(
	ctx context.Context,
	request *controlplanev1.AuthorizeImagePromotionRequest,
) (*controlplanev1.AuthorizeImagePromotionResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_AuthorizeImagePromotion_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.AuthorizeImagePromotion(ctx, resource.AuthorizeImagePromotionInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), ImageArtifactID: request.GetImageArtifactId(),
		ExpectedVersion: request.GetExpectedVersion(), PromotionClaim: request.GetPromotionClaim(),
		ManifestDigest: request.GetManifestDigest(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	artifact, err := toProtoResource(result.ImageArtifact)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.AuthorizeImagePromotionResponse{ImageArtifact: artifact,
		AuthorizationToken:     result.AuthorizationToken,
		AuthorizationExpiresAt: timestamppb.New(result.AuthorizationExpiresAt)}, nil
}

func (server *Server) GetRoleImageBuild(
	ctx context.Context,
	request *controlplanev1.GetRoleImageBuildRequest,
) (*controlplanev1.GetRoleImageBuildResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_GetRoleImageBuild_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.GetRoleImageBuild(ctx, resource.GetRoleImageBuildInput{
		Principal: principal, ImageBuildID: request.GetImageBuildId(), ExpectedVersion: request.GetExpectedVersion(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	build, err := toProtoResource(result)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.GetRoleImageBuildResponse{ImageBuild: build}, nil
}
