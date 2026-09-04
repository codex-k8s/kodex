package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func castContextBinding(input entity.AgentContextBinding) *controlplanev1.AgentContextBinding {
	return &controlplanev1.AgentContextBinding{Ref: input.Ref, Version: input.Version, AgentRef: input.AgentRef, ResourceRef: input.ResourceRef, RevisionRef: input.RevisionRef, Digest: input.Digest}
}

func (server *Server) BindAgentSkillBundle(ctx context.Context, request *controlplanev1.BindAgentSkillBundleRequest) (*controlplanev1.BindAgentSkillBundleResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_BindAgentSkillBundle_FullMethodName, command.BindAgentSkillBundle, request.GetMutation(), command.AgentContextBindingInput{AgentRef: request.GetAgentRef(), ResourceRef: request.GetBundleRef(), RevisionRef: request.GetRevisionRef(), ExpectedBindingVersion: request.GetExpectedBindingVersion()})
	if err != nil {
		return nil, err
	}
	if result.ContextBinding == nil || result.ContextBinding.Ref == "" || result.ContextBinding.Version < 1 {
		return nil, status.Error(codes.Internal, "context binding result is incomplete")
	}
	return &controlplanev1.BindAgentSkillBundleResponse{Binding: castContextBinding(*result.ContextBinding)}, nil
}

func (server *Server) UnbindAgentSkillBundle(ctx context.Context, request *controlplanev1.UnbindAgentSkillBundleRequest) (*controlplanev1.UnbindAgentSkillBundleResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_UnbindAgentSkillBundle_FullMethodName, command.UnbindAgentSkillBundle, request.GetMutation(), command.AgentContextBindingInput{AgentRef: request.GetAgentRef(), ResourceRef: request.GetBundleRef(), RevisionRef: request.GetRevisionRef(), ExpectedBindingVersion: request.GetExpectedBindingVersion()})
	if err != nil {
		return nil, err
	}
	if result.ContextBinding == nil || result.ContextBinding.Ref == "" || result.ContextBinding.Version < 1 {
		return nil, status.Error(codes.Internal, "context binding result is incomplete")
	}
	return &controlplanev1.UnbindAgentSkillBundleResponse{Binding: castContextBinding(*result.ContextBinding)}, nil
}

func (server *Server) BindAgentMemoryRecord(ctx context.Context, request *controlplanev1.BindAgentMemoryRecordRequest) (*controlplanev1.BindAgentMemoryRecordResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_BindAgentMemoryRecord_FullMethodName, command.BindAgentMemoryRecord, request.GetMutation(), command.AgentContextBindingInput{AgentRef: request.GetAgentRef(), ResourceRef: request.GetRecordRef(), RevisionRef: request.GetRevisionRef(), ExpectedBindingVersion: request.GetExpectedBindingVersion()})
	if err != nil {
		return nil, err
	}
	if result.ContextBinding == nil || result.ContextBinding.Ref == "" || result.ContextBinding.Version < 1 {
		return nil, status.Error(codes.Internal, "context binding result is incomplete")
	}
	return &controlplanev1.BindAgentMemoryRecordResponse{Binding: castContextBinding(*result.ContextBinding)}, nil
}

func (server *Server) UnbindAgentMemoryRecord(ctx context.Context, request *controlplanev1.UnbindAgentMemoryRecordRequest) (*controlplanev1.UnbindAgentMemoryRecordResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_UnbindAgentMemoryRecord_FullMethodName, command.UnbindAgentMemoryRecord, request.GetMutation(), command.AgentContextBindingInput{AgentRef: request.GetAgentRef(), ResourceRef: request.GetRecordRef(), RevisionRef: request.GetRevisionRef(), ExpectedBindingVersion: request.GetExpectedBindingVersion()})
	if err != nil {
		return nil, err
	}
	if result.ContextBinding == nil || result.ContextBinding.Ref == "" || result.ContextBinding.Version < 1 {
		return nil, status.Error(codes.Internal, "context binding result is incomplete")
	}
	return &controlplanev1.UnbindAgentMemoryRecordResponse{Binding: castContextBinding(*result.ContextBinding)}, nil
}
