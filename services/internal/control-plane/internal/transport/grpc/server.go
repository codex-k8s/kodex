// Package grpc реализует внутренний gRPC transport web-first control-plane.
package grpc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

type Server struct {
	controlplanev1.UnimplementedPlatformQueryServiceServer
	controlplanev1.UnimplementedPlatformCommandServiceServer
	controlplanev1.UnimplementedSystemAssistantServiceServer
	controlplanev1.UnimplementedRuntimeWorkServiceServer
	service *platformservice.Service
}

func NewServer(service *platformservice.Service) (*Server, error) {
	if service == nil {
		return nil, errors.New("platform service is required")
	}
	return &Server{service: service}, nil
}

func principal(ctx context.Context, method string) (value.Principal, error) {
	result, err := authorization.Principal(ctx, method)
	if err != nil {
		return value.Principal{}, status.Error(codes.Unauthenticated, "verified authorization context is required")
	}
	return result, nil
}

func mutation(input *controlplanev1.MutationContext) value.Mutation {
	if input == nil {
		return value.Mutation{}
	}
	return value.Mutation{IdempotencyKey: strings.TrimSpace(input.GetIdempotencyKey()), ExpectedVersion: input.ExpectedVersion}
}

func page(input *controlplanev1.PageRequest) query.Page {
	if input == nil {
		return query.Page{Size: 50}
	}
	size := input.GetPageSize()
	if size == 0 {
		size = 50
	}
	return query.Page{Size: size, Token: input.GetPageToken()}
}

func asMap(input *structpb.Struct) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	return input.AsMap()
}

func enumSuffix(value fmt.Stringer, prefix string) string {
	return strings.TrimPrefix(value.String(), prefix)
}

func domainProjectPermissions(values []controlplanev1.ProjectPermission) []string {
	result := make([]string, 0, len(values))
	for _, item := range values {
		if item == controlplanev1.ProjectPermission_PROJECT_PERMISSION_UNSPECIFIED {
			continue
		}
		result = append(result, enumSuffix(item, "PROJECT_PERMISSION_"))
	}
	return result
}

func runTarget(input *controlplanev1.RunTarget) entity.RunTarget {
	if input == nil {
		return entity.RunTarget{}
	}
	switch target := input.Target.(type) {
	case *controlplanev1.RunTarget_AgentRef:
		return entity.RunTarget{Type: "AGENT", Ref: target.AgentRef, Name: input.GetDisplayName()}
	case *controlplanev1.RunTarget_WorkflowRef:
		return entity.RunTarget{Type: "WORKFLOW", Ref: target.WorkflowRef, Name: input.GetDisplayName()}
	default:
		return entity.RunTarget{}
	}
}

func domainWorkflowVersion(input *controlplanev1.WorkflowVersion) *entity.WorkflowVersion {
	if input == nil {
		return nil
	}
	result := &entity.WorkflowVersion{
		Ref:                input.GetRef(),
		VersionNumber:      input.GetRevision(),
		Concurrency:        input.GetMaxConcurrency(),
		TimeoutSeconds:     int64(input.GetTimeoutSeconds()),
		CompletionCriteria: input.GetCompletionCriteria(),
	}
	for _, item := range input.GetInputFields() {
		result.Inputs = append(result.Inputs, entity.WorkflowInputField{Key: item.GetKey(), Label: item.GetLabel(), Type: item.GetValueType(), Help: item.GetDescription(), Required: item.GetRequired()})
	}
	for _, item := range input.GetSteps() {
		result.Steps = append(result.Steps, entity.WorkflowStep{Key: item.GetRef(), Name: item.GetName(), AgentRef: item.GetAgentRef(), Instructions: item.GetPurpose(), HumanGateAfter: item.GetHumanGate()})
		result.AgentRefs = append(result.AgentRefs, item.GetAgentRef())
		for _, decision := range item.GetGateDecisions() {
			result.GateDecisions = append(result.GateDecisions, enumSuffix(decision, "OWNER_GATE_DECISION_"))
		}
	}
	return result
}

func execute(ctx context.Context, service *platformservice.Service, method string, kind command.Kind, requestMutation *controlplanev1.MutationContext, payload any) (command.Result, error) {
	p, err := principal(ctx, method)
	if err != nil {
		return command.Result{}, err
	}
	commandMutation := mutation(requestMutation)
	if commandMutation.IdempotencyKey == "" {
		commandMutation.IdempotencyKey = "rpc-" + p.CorrelationRef
		if len(commandMutation.IdempotencyKey) > 128 {
			commandMutation.IdempotencyKey = commandMutation.IdempotencyKey[:128]
		}
	}
	result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: p, Mutation: commandMutation, Payload: payload})
	if err != nil {
		return command.Result{}, transportError(err)
	}
	return result, nil
}

func transportError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errs.ErrInvalid):
		return status.Error(codes.InvalidArgument, "request is invalid")
	case errors.Is(err, errs.ErrUnauthorized):
		return status.Error(codes.Unauthenticated, "authentication is required")
	case errors.Is(err, errs.ErrForbidden):
		return status.Error(codes.PermissionDenied, "operation is not permitted")
	case errors.Is(err, errs.ErrNotFound):
		return status.Error(codes.NotFound, "resource was not found")
	case errors.Is(err, errs.ErrVersionMismatch):
		return status.Error(codes.Aborted, "resource version is stale")
	case errors.Is(err, errs.ErrIdempotencyReuse):
		return status.Error(codes.AlreadyExists, "idempotency key was reused with a different intent")
	case errors.Is(err, errs.ErrProtected):
		return status.Error(codes.FailedPrecondition, "protected system resource cannot be changed")
	case errors.Is(err, errs.ErrConflict):
		return status.Error(codes.Aborted, "resource state changed")
	case errors.Is(err, errs.ErrUnavailable):
		return status.Error(codes.Unavailable, "operation is temporarily unavailable")
	default:
		return status.Error(codes.Internal, "control-plane operation failed")
	}
}
