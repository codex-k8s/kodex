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
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GetRuntimeEnvironmentDraft reads the draft through the principal verified for this query and
// projects the returned domain snapshot.
// GetRuntimeEnvironmentDraft читает черновик с principal, проверенным для данного query, и
// преобразует возвращённый доменный снимок.
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

// CreateRuntimeEnvironmentDraft requires a convertible specification and assigns PROJECT scope
// before the dedicated draft-create command.
// CreateRuntimeEnvironmentDraft требует преобразуемую спецификацию и назначает область PROJECT до
// отдельной команды создания черновика.
func (server *Server) CreateRuntimeEnvironmentDraft(ctx context.Context, request *controlplanev1.CreateRuntimeEnvironmentDraftRequest) (*controlplanev1.CreateRuntimeEnvironmentDraftResponse, error) {
	spec, err := domainEnvironmentDraftSpecification(request.GetSpecification())
	if err != nil {
		return nil, transportError(errs.ErrInvalid)
	}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_CreateRuntimeEnvironmentDraft_FullMethodName, command.CreateRuntimeEnvironmentDraft, request.GetMutation(), command.RuntimeEnvironmentDraftInput{ScopeKind: "PROJECT", ProjectRef: request.GetProjectRef(), EnvironmentRef: request.GetEnvironmentRef(), ExpectedEnvironmentVersion: request.GetExpectedEnvironmentVersion(), Specification: spec}, func(result command.Result) *controlplanev1.CreateRuntimeEnvironmentDraftResponse {
		return &controlplanev1.CreateRuntimeEnvironmentDraftResponse{Draft: castEnvironmentDraft(result.RuntimeEnvironmentDraft)}
	})
}

// CreateOrganizationRuntimeEnvironmentDraft requires a convertible specification and assigns
// ORGANIZATION scope without taking organization identity from request fields.
// CreateOrganizationRuntimeEnvironmentDraft требует преобразуемую спецификацию и назначает область
// ORGANIZATION без получения identity организации из полей запроса.
func (server *Server) CreateOrganizationRuntimeEnvironmentDraft(ctx context.Context, request *controlplanev1.CreateOrganizationRuntimeEnvironmentDraftRequest) (*controlplanev1.CreateOrganizationRuntimeEnvironmentDraftResponse, error) {
	spec, err := domainEnvironmentDraftSpecification(request.GetSpecification())
	if err != nil {
		return nil, transportError(errs.ErrInvalid)
	}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_CreateOrganizationRuntimeEnvironmentDraft_FullMethodName, command.CreateOrganizationRuntimeEnvironmentDraft, request.GetMutation(), command.RuntimeEnvironmentDraftInput{ScopeKind: "ORGANIZATION", EnvironmentRef: request.GetEnvironmentRef(), ExpectedEnvironmentVersion: request.GetExpectedEnvironmentVersion(), Specification: spec}, func(result command.Result) *controlplanev1.CreateOrganizationRuntimeEnvironmentDraftResponse {
		return &controlplanev1.CreateOrganizationRuntimeEnvironmentDraftResponse{Draft: castEnvironmentDraft(result.RuntimeEnvironmentDraft)}
	})
}

// SaveRuntimeEnvironmentDraft converts the specification before sending the draft locator and
// replacement specification to the dedicated save command.
// SaveRuntimeEnvironmentDraft преобразует спецификацию до передачи ссылки черновика и замещающей
// спецификации отдельной команде сохранения.
func (server *Server) SaveRuntimeEnvironmentDraft(ctx context.Context, request *controlplanev1.SaveRuntimeEnvironmentDraftRequest) (*controlplanev1.SaveRuntimeEnvironmentDraftResponse, error) {
	spec, err := domainEnvironmentDraftSpecification(request.GetSpecification())
	if err != nil {
		return nil, transportError(errs.ErrInvalid)
	}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_SaveRuntimeEnvironmentDraft_FullMethodName, command.SaveRuntimeEnvironmentDraft, request.GetMutation(), command.RuntimeEnvironmentDraftInput{DraftRef: request.GetDraftRef(), Specification: spec}, func(result command.Result) *controlplanev1.SaveRuntimeEnvironmentDraftResponse {
		return &controlplanev1.SaveRuntimeEnvironmentDraftResponse{Draft: castEnvironmentDraft(result.RuntimeEnvironmentDraft)}
	})
}

// ValidateRuntimeEnvironmentDraft executes the dedicated validation command for the draft locator
// and projects its resulting draft.
// ValidateRuntimeEnvironmentDraft выполняет отдельную команду проверки по ссылке черновика и
// преобразует полученный черновик.
func (server *Server) ValidateRuntimeEnvironmentDraft(ctx context.Context, request *controlplanev1.ValidateRuntimeEnvironmentDraftRequest) (*controlplanev1.ValidateRuntimeEnvironmentDraftResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_ValidateRuntimeEnvironmentDraft_FullMethodName, command.ValidateRuntimeEnvironmentDraft, request.GetMutation(), command.RuntimeEnvironmentDraftInput{DraftRef: request.GetDraftRef()}, func(result command.Result) *controlplanev1.ValidateRuntimeEnvironmentDraftResponse {
		return &controlplanev1.ValidateRuntimeEnvironmentDraftResponse{Draft: castEnvironmentDraft(result.RuntimeEnvironmentDraft)}
	})
}

// PublishRuntimeEnvironmentDraft copies selected impact items into the dedicated publication
// command and requires a consistent published result.
// PublishRuntimeEnvironmentDraft копирует выбранные элементы влияния в отдельную команду
// публикации и требует согласованный опубликованный результат.
func (server *Server) PublishRuntimeEnvironmentDraft(ctx context.Context, request *controlplanev1.PublishRuntimeEnvironmentDraftRequest) (*controlplanev1.PublishRuntimeEnvironmentDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_PublishRuntimeEnvironmentDraft_FullMethodName,
		command.PublishRuntimeEnvironmentDraft, request.GetMutation(), command.RuntimeEnvironmentDraftInput{DraftRef: request.GetDraftRef(), PlanRef: request.GetPlanRef(), SelectedItemRefs: append([]string{}, request.GetSelectedItemRefs()...)})
	if err != nil {
		return nil, err
	}
	return castPublishedEnvironmentDraft(result)
}

// castPublishedEnvironmentDraft rejects incomplete or mismatched published draft, environment and
// applied impact-plan pins before returning all three projections.
// castPublishedEnvironmentDraft отклоняет неполные либо несогласованные привязки опубликованного
// черновика, среды и применённого плана влияния до возврата трёх представлений.
func castPublishedEnvironmentDraft(result command.Result) (*controlplanev1.PublishRuntimeEnvironmentDraftResponse, error) {
	if result.RuntimeEnvironment == nil || result.RuntimeEnvironmentDraft == nil || result.RuntimeEnvironment.Ref == "" ||
		result.RuntimeEnvironment.Version < 1 || result.RuntimeEnvironmentDraft.Version < 1 || result.RuntimeEnvironmentDraft.ValidationDigest == "" ||
		result.RuntimeEnvironmentDraft.State != "PUBLISHED" || result.RuntimeEnvironmentDraft.PublishedEnvironmentRef != result.RuntimeEnvironment.Ref ||
		result.RevisionImpactPlan == nil || result.RevisionImpactPlan.Ref == "" || result.RevisionImpactPlan.Digest == "" || result.RevisionImpactPlan.State != "APPLIED" || result.RevisionImpactPlan.Version != 2 ||
		result.RevisionImpactPlan.Kind != "RUNTIME_ENVIRONMENT" || result.RevisionImpactPlan.DraftRef != result.RuntimeEnvironmentDraft.Ref ||
		result.RevisionImpactPlan.DraftVersion != result.RuntimeEnvironmentDraft.Version-1 ||
		result.RevisionImpactPlan.PublishedRevisionRef != result.RuntimeEnvironment.CurrentVersion.Ref ||
		result.RevisionImpactPlan.TargetDigest != result.RuntimeEnvironment.CurrentVersion.Digest || result.RevisionImpactPlan.TargetDigest != result.RuntimeEnvironmentDraft.ValidationDigest {
		return nil, status.Error(codes.Internal, "runtime environment publication result is incomplete")
	}
	return &controlplanev1.PublishRuntimeEnvironmentDraftResponse{Draft: castEnvironmentDraft(result.RuntimeEnvironmentDraft), Environment: castRuntimeEnvironment(*result.RuntimeEnvironment), Plan: castRevisionImpactPlan(result.RevisionImpactPlan)}, nil
}

// DiscardRuntimeEnvironmentDraft passes the draft locator through the dedicated discard command
// and returns its resulting draft projection.
// DiscardRuntimeEnvironmentDraft передаёт ссылку черновика отдельной команде отбрасывания и
// возвращает полученное представление черновика.
func (server *Server) DiscardRuntimeEnvironmentDraft(ctx context.Context, request *controlplanev1.DiscardRuntimeEnvironmentDraftRequest) (*controlplanev1.DiscardRuntimeEnvironmentDraftResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_DiscardRuntimeEnvironmentDraft_FullMethodName, command.DiscardRuntimeEnvironmentDraft, request.GetMutation(), command.RuntimeEnvironmentDraftInput{DraftRef: request.GetDraftRef()}, func(result command.Result) *controlplanev1.DiscardRuntimeEnvironmentDraftResponse {
		return &controlplanev1.DiscardRuntimeEnvironmentDraftResponse{Draft: castEnvironmentDraft(result.RuntimeEnvironmentDraft)}
	})
}

// domainEnvironmentDraftSpecification requires input, copies values, secret references and tools,
// and validates policy only when policy is explicitly present.
// domainEnvironmentDraftSpecification требует вход, копирует значения, ссылки секретов и
// инструменты, проверяя политику только при её явном наличии.
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

// castEnvironmentDraft preserves absent draft and saved time, projects nonzero policy with
// distinct network destinations and copies draft version and publication pins.
// castEnvironmentDraft сохраняет отсутствие черновика и времени сохранения, преобразует ненулевую
// политику с уникальными сетевыми направлениями и копирует версии и привязки публикации.
func castEnvironmentDraft(input *entity.RuntimeEnvironmentDraft) *controlplanev1.RuntimeEnvironmentDraft {
	if input == nil {
		return nil
	}
	spec := input.Specification
	specification := &controlplanev1.RuntimeEnvironmentDraftSpecification{Name: spec.Name, Description: spec.Description, ImageArtifactRef: spec.ImageArtifactRef}
	if !reflect.DeepEqual(spec.Policy, runtimecontract.RuntimeEnvironmentPolicy{}) {
		policy := castRuntimeEnvironmentPolicy(spec.Policy)
		specification.Policy = &controlplanev1.RuntimeEnvironmentPolicyInput{Resources: policy.Resources, KubernetesAccess: policy.KubernetesAccess.Kind, WebAccess: policy.Network.WebAccess}
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
	var savedAt *timestamppb.Timestamp
	if !input.SavedAt.IsZero() {
		savedAt = timestamppb.New(input.SavedAt)
	}
	return &controlplanev1.RuntimeEnvironmentDraft{Ref: input.Ref, Version: input.Version, ProjectRef: input.ProjectRef,
		ScopeKind: roleImageScopeKind(input.ScopeKind), OrganizationRef: input.OrganizationRef,
		BaseVersionRef: input.BaseVersionRef, BaseRevision: input.BaseRevision, SavedAt: savedAt,
		EnvironmentRef: input.EnvironmentRef, ExpectedEnvironmentVersion: input.ExpectedEnvironmentVersion, State: input.State,
		Specification: specification, ValidationDigest: input.ValidationDigest, Diagnostics: input.Diagnostics, PublishedEnvironmentRef: input.PublishedEnvironmentRef}
}
