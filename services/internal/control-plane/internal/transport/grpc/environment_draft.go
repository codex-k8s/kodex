package grpc

import (
	"context"
	"reflect"
	"slices"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (server *Server) GetRuntimeEnvironmentDraft(ctx context.Context, request *controlplanev1.GetRuntimeEnvironmentDraftRequest) (*controlplanev1.GetRuntimeEnvironmentDraftResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetRuntimeEnvironmentDraft_FullMethodName)
	if err != nil {
		return nil, err
	}
	draft, err := server.service.GetRuntimeEnvironmentDraft(ctx, p, request.GetDraftRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetRuntimeEnvironmentDraftResponse{Draft: castEnvironmentDraft(&draft)}, nil
}

func (server *Server) CreateRuntimeEnvironmentDraft(ctx context.Context, request *controlplanev1.CreateRuntimeEnvironmentDraftRequest) (*controlplanev1.CreateRuntimeEnvironmentDraftResponse, error) {
	spec, err := domainEnvironmentDraftSpecification(request.GetSpecification())
	if err != nil {
		return nil, transportError(err)
	}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateRuntimeEnvironmentDraft_FullMethodName,
		command.CreateRuntimeEnvironmentDraft, request.GetMutation(), command.RuntimeEnvironmentDraftInput{ProjectRef: request.GetProjectRef(),
			EnvironmentRef: request.GetEnvironmentRef(), ExpectedEnvironmentVersion: request.GetExpectedEnvironmentVersion(), Specification: spec})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateRuntimeEnvironmentDraftResponse{Draft: castEnvironmentDraft(result.RuntimeEnvironmentDraft)}, nil
}
func (server *Server) SaveRuntimeEnvironmentDraft(ctx context.Context, request *controlplanev1.SaveRuntimeEnvironmentDraftRequest) (*controlplanev1.SaveRuntimeEnvironmentDraftResponse, error) {
	spec, err := domainEnvironmentDraftSpecification(request.GetSpecification())
	if err != nil {
		return nil, transportError(err)
	}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_SaveRuntimeEnvironmentDraft_FullMethodName,
		command.SaveRuntimeEnvironmentDraft, request.GetMutation(), command.RuntimeEnvironmentDraftInput{DraftRef: request.GetDraftRef(), Specification: spec})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.SaveRuntimeEnvironmentDraftResponse{Draft: castEnvironmentDraft(result.RuntimeEnvironmentDraft)}, nil
}
func (server *Server) ValidateRuntimeEnvironmentDraft(ctx context.Context, request *controlplanev1.ValidateRuntimeEnvironmentDraftRequest) (*controlplanev1.ValidateRuntimeEnvironmentDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ValidateRuntimeEnvironmentDraft_FullMethodName,
		command.ValidateRuntimeEnvironmentDraft, request.GetMutation(), command.RuntimeEnvironmentDraftInput{DraftRef: request.GetDraftRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ValidateRuntimeEnvironmentDraftResponse{Draft: castEnvironmentDraft(result.RuntimeEnvironmentDraft)}, nil
}
func (server *Server) PublishRuntimeEnvironmentDraft(ctx context.Context, request *controlplanev1.PublishRuntimeEnvironmentDraftRequest) (*controlplanev1.PublishRuntimeEnvironmentDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_PublishRuntimeEnvironmentDraft_FullMethodName,
		command.PublishRuntimeEnvironmentDraft, request.GetMutation(), command.RuntimeEnvironmentDraftInput{DraftRef: request.GetDraftRef()})
	if err != nil {
		return nil, err
	}
	return castPublishedEnvironmentDraft(result)
}

func castPublishedEnvironmentDraft(result command.Result) (*controlplanev1.PublishRuntimeEnvironmentDraftResponse, error) {
	if result.RuntimeEnvironment == nil || result.RuntimeEnvironmentDraft == nil || result.RuntimeEnvironment.Ref == "" ||
		result.RuntimeEnvironment.Version < 1 || result.RuntimeEnvironmentDraft.Version < 1 || result.RuntimeEnvironmentDraft.ValidationDigest == "" ||
		result.RuntimeEnvironmentDraft.State != "PUBLISHED" || result.RuntimeEnvironmentDraft.PublishedEnvironmentRef != result.RuntimeEnvironment.Ref {
		return nil, status.Error(codes.Internal, "runtime environment publication result is incomplete")
	}
	return &controlplanev1.PublishRuntimeEnvironmentDraftResponse{Draft: castEnvironmentDraft(result.RuntimeEnvironmentDraft), Environment: castRuntimeEnvironment(*result.RuntimeEnvironment)}, nil
}
func (server *Server) DiscardRuntimeEnvironmentDraft(ctx context.Context, request *controlplanev1.DiscardRuntimeEnvironmentDraftRequest) (*controlplanev1.DiscardRuntimeEnvironmentDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_DiscardRuntimeEnvironmentDraft_FullMethodName,
		command.DiscardRuntimeEnvironmentDraft, request.GetMutation(), command.RuntimeEnvironmentDraftInput{DraftRef: request.GetDraftRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.DiscardRuntimeEnvironmentDraftResponse{Draft: castEnvironmentDraft(result.RuntimeEnvironmentDraft)}, nil
}

func domainEnvironmentDraftSpecification(input *controlplanev1.RuntimeEnvironmentDraftSpecification) (entity.RuntimeEnvironmentDraftSpecification, error) {
	if input == nil {
		return entity.RuntimeEnvironmentDraftSpecification{}, errs.ErrInvalid
	}
	values, secrets, tools := domainEnvironment(input.GetValues(), input.GetSecretBindings(), input.GetTools())
	result := entity.RuntimeEnvironmentDraftSpecification{Name: input.GetName(), Description: input.GetDescription(),
		ImageArtifactRef: input.GetImageArtifactRef(), Values: values, SecretBindings: secrets, Tools: tools}
	if input.GetPolicy() != nil {
		policy, err := domainRuntimeEnvironmentPolicy(input.GetPolicy())
		if err != nil {
			return result, err
		}
		result.Policy = policy
	}
	return result, nil
}

func castEnvironmentDraft(input *entity.RuntimeEnvironmentDraft) *controlplanev1.RuntimeEnvironmentDraft {
	if input == nil {
		return nil
	}
	spec := input.Specification
	specification := &controlplanev1.RuntimeEnvironmentDraftSpecification{Name: spec.Name, Description: spec.Description, ImageArtifactRef: spec.ImageArtifactRef}
	if !reflect.DeepEqual(spec.Policy, runtimecontract.RuntimeEnvironmentPolicy{}) {
		policy := castRuntimeEnvironmentPolicy(spec.Policy)
		specification.Policy = &controlplanev1.RuntimeEnvironmentPolicyInput{Resources: policy.Resources, KubernetesAccess: policy.KubernetesAccess.Kind}
		for _, volume := range policy.Volumes {
			specification.Policy.Volumes = append(specification.Policy.Volumes, &controlplanev1.RuntimeVolumeInput{Name: volume.Name, Kind: volume.Kind, SizeMib: volume.SizeMib})
		}
		for _, egress := range policy.Network.Egress {
			if !slices.Contains(specification.Policy.NetworkDestinations, egress.Destination) {
				specification.Policy.NetworkDestinations = append(specification.Policy.NetworkDestinations, egress.Destination)
			}
		}
	}
	for _, value := range spec.Values {
		specification.Values = append(specification.Values, &controlplanev1.RuntimeEnvironmentValue{Name: value.Name, Value: value.Value})
	}
	for _, secret := range spec.SecretBindings {
		specification.SecretBindings = append(specification.SecretBindings, &controlplanev1.RuntimeSecretBinding{Name: secret.Name, SecretRef: secret.SecretRef, Revision: secret.Revision})
	}
	for _, tool := range spec.Tools {
		specification.Tools = append(specification.Tools, &controlplanev1.RuntimeEnvironmentTool{Name: tool.Name, Command: tool.Command, Description: tool.Description, UsageHint: tool.UsageHint})
	}
	return &controlplanev1.RuntimeEnvironmentDraft{Ref: input.Ref, Version: input.Version, ProjectRef: input.ProjectRef,
		EnvironmentRef: input.EnvironmentRef, ExpectedEnvironmentVersion: input.ExpectedEnvironmentVersion, State: input.State,
		Specification: specification, ValidationDigest: input.ValidationDigest, Diagnostics: input.Diagnostics, PublishedEnvironmentRef: input.PublishedEnvironmentRef}
}
