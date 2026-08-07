package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (server *Server) GetLegacyConfigurationCutover(ctx context.Context,
	request *controlplanev1.GetLegacyConfigurationCutoverRequest,
) (*controlplanev1.GetLegacyConfigurationCutoverResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_GetLegacyConfigurationCutover_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	cutover, err := server.service.GetLegacyConfigurationCutover(ctx, principal, request.GetLegacyRoleId())
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.GetLegacyConfigurationCutoverResponse{Cutover: legacyCutoverToProto(cutover)}, nil
}

func (server *Server) ListLegacyConfigurationCutovers(ctx context.Context,
	request *controlplanev1.ListLegacyConfigurationCutoversRequest,
) (*controlplanev1.ListLegacyConfigurationCutoversResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ListLegacyConfigurationCutovers_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	limit := pageSize(request.GetPageSize())
	items, err := server.service.ListLegacyConfigurationCutovers(ctx, principal, request.GetPageToken(), limit)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListLegacyConfigurationCutoversResponse{
		Cutovers: make([]*controlplanev1.LegacyConfigurationCutover, 0, len(items))}
	for _, item := range items {
		response.Cutovers = append(response.Cutovers, legacyCutoverToProto(item))
	}
	if len(items) == limit {
		response.NextPageToken = items[len(items)-1].LegacyRoleID
	}
	return response, nil
}

func (server *Server) ResolveLegacyConfigurationCutover(ctx context.Context,
	request *controlplanev1.ResolveLegacyConfigurationCutoverRequest,
) (*controlplanev1.ResolveLegacyConfigurationCutoverResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ResolveLegacyConfigurationCutover_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.ResolveLegacyConfigurationCutover(ctx, resource.ResolveLegacyConfigurationCutoverInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), LegacyRoleID: request.GetLegacyRoleId(),
		ExpectedLegacyRoleVersion:   request.GetExpectedLegacyRoleVersion(),
		ExpectedLegacyPromptVersion: request.GetExpectedLegacyPromptVersion(),
		InstructionContent:          request.GetInstructionContent()})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	agent, err := toProtoResource(result.Agent)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ResolveLegacyConfigurationCutoverResponse{
		Cutover: legacyCutoverToProto(result.Cutover), Agent: agent}, nil
}

func legacyCutoverToProto(value domainrepo.LegacyConfigurationCutover) *controlplanev1.LegacyConfigurationCutover {
	result := &controlplanev1.LegacyConfigurationCutover{LegacyRoleId: value.LegacyRoleID,
		LegacyRoleVersion: value.LegacyRoleVersion, LegacyPromptProfileId: value.LegacyPromptProfileID,
		LegacyPromptVersion: value.LegacyPromptVersion, SourceRoleSha256: value.SourceRoleSHA256,
		SourcePromptSha256: value.SourcePromptSHA256, SourceCredentialIds: value.SourceCredentialIDs,
		TargetRoleDefinitionId: value.TargetRoleDefinitionID, TargetAgentId: value.TargetAgentID,
		TargetInstructionSetId: value.TargetInstructionSetID, TargetProviderPoolId: value.TargetProviderPoolID,
		TargetAgentAssignmentId:    value.TargetAgentAssignmentID,
		TargetProviderReferenceIds: value.TargetProviderReferenceIDs, State: value.State,
		BlockCode: value.BlockCode, ManualAction: value.ManualAction,
		ResultAgentVersion: value.ResultAgentVersion, ResultAgentSha256: value.ResultAgentSHA256,
		CreatedAt: timestamppb.New(value.CreatedAt)}
	if !value.ResolvedAt.IsZero() {
		result.ResolvedAt = timestamppb.New(value.ResolvedAt)
	}
	return result
}
