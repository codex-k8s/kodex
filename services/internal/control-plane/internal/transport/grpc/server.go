// Package grpc реализует внутренний gRPC transport web-first control-plane.
package grpc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	controlPlaneErrorDomain           = "kodex.control-plane"
	freshAuthenticationRequiredReason = "FRESH_AUTHENTICATION_REQUIRED"
)

type Server struct {
	sttv1.UnimplementedTranscriptionPolicyProjectionServiceServer
	controlplanev1.UnimplementedPlatformQueryServiceServer
	controlplanev1.UnimplementedPlatformCommandServiceServer
	controlplanev1.UnimplementedSystemAssistantServiceServer
	controlplanev1.UnimplementedRuntimeWorkServiceServer
	controlplanev1.UnimplementedManagedConfigurationSourceWorkServiceServer
	controlplanev1.UnimplementedRuntimeSecretWorkServiceServer
	controlplanev1.UnimplementedRuntimeSecretDraftWorkServiceServer
	controlplanev1.UnimplementedSessionArchiveWorkServiceServer
	controlplanev1.UnimplementedInteractionWorkServiceServer
	controlplanev1.UnimplementedAccessServiceServer
	service    *platformservice.Service
	roleImages *roleimageservice.Service
}

func NewServer(service *platformservice.Service, roleImages *roleimageservice.Service) (*Server, error) {
	if service == nil || roleImages == nil {
		return nil, errors.New("platform and role image services are required")
	}
	return &Server{service: service, roleImages: roleImages}, nil
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
		Ref:                 input.GetRef(),
		Name:                input.GetName(),
		Purpose:             input.GetPurpose(),
		CoordinatorAgentRef: input.GetCoordinatorAgentRef(),
		VersionNumber:       input.GetRevision(),
		Concurrency:         input.GetMaxConcurrency(),
		TimeoutSeconds:      int64(input.GetTimeoutSeconds()),
		CompletionCriteria:  input.GetCompletionCriteria(),
	}
	knownInputKeys := make(map[string]struct{}, len(input.GetInputFields()))
	for _, item := range input.GetInputFields() {
		if item.GetKey() != "" {
			knownInputKeys[item.GetKey()] = struct{}{}
		}
	}
	nextInputKey := 1
	for _, item := range input.GetInputFields() {
		key := item.GetKey()
		if key == "" {
			for {
				key = fmt.Sprintf("field-%03d", nextInputKey)
				nextInputKey++
				if _, exists := knownInputKeys[key]; !exists {
					break
				}
			}
			knownInputKeys[key] = struct{}{}
		}
		result.Inputs = append(result.Inputs, entity.WorkflowInputField{Key: key, Label: item.GetLabel(), Type: item.GetValueType(), Help: item.GetDescription(), Required: item.GetRequired(), Options: append([]string(nil), item.GetOptions()...)})
	}
	parallelGroupDependencies := map[int32][]string{}
	frontier := []string{}
	for index, item := range input.GetSteps() {
		stepKey := item.GetRef()
		if stepKey == "" {
			stepKey = fmt.Sprintf("step-%03d", index+1)
		}
		dependencies := append([]string(nil), frontier...)
		if item.GetParallel() {
			if groupDependencies, exists := parallelGroupDependencies[item.GetParallelGroup()]; exists {
				dependencies = append([]string(nil), groupDependencies...)
			} else {
				parallelGroupDependencies[item.GetParallelGroup()] = append([]string(nil), frontier...)
				frontier = nil
			}
			frontier = append(frontier, stepKey)
		} else {
			parallelGroupDependencies = map[int32][]string{}
			frontier = []string{stepKey}
		}
		step := entity.WorkflowStep{
			Key: stepKey, Position: item.GetPosition(), Name: item.GetName(), AgentRef: item.GetAgentRef(),
			Instructions: item.GetPurpose(), Parallel: item.GetParallel(), ParallelGroup: item.GetParallelGroup(),
			TimeoutSeconds: item.GetTimeoutSeconds(), ExpectedResult: item.GetExpectedResult(), HumanGateAfter: item.GetHumanGate(),
			DependsOn: dependencies, RequiredCapabilityKeys: append([]string(nil), item.GetRequiredCapabilityKeys()...),
		}
		for _, decision := range item.GetGateDecisions() {
			step.GateDecisions = append(step.GateDecisions, enumSuffix(decision, "OWNER_GATE_DECISION_"))
		}
		result.Steps = append(result.Steps, step)
		result.AgentRefs = append(result.AgentRefs, item.GetAgentRef())
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
	case errors.Is(err, errs.ErrFreshAuthenticationRequired):
		return statusErrorWithReason(codes.PermissionDenied, "fresh authentication is required", freshAuthenticationRequiredReason)
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
	case errors.Is(err, errs.ErrCapabilityRequired):
		return status.Error(codes.FailedPrecondition, "required capability is not enabled")
	case errors.Is(err, errs.ErrAlreadyResolved):
		return status.Error(codes.FailedPrecondition, "resource is already resolved")
	case errors.Is(err, errs.ErrConflict):
		return status.Error(codes.Aborted, "resource state changed")
	case errors.Is(err, errs.ErrUnavailable):
		return status.Error(codes.Unavailable, "operation is temporarily unavailable")
	default:
		return status.Error(codes.Internal, "control-plane operation failed")
	}
}

func statusErrorWithReason(code codes.Code, message, reason string) error {
	base := status.New(code, message)
	withDetails, err := base.WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: controlPlaneErrorDomain})
	if err != nil {
		return base.Err()
	}
	return withDetails.Err()
}
