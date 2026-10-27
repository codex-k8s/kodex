package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// LaunchWorkflowExecution passes execution lease, workflow publication/version/digest and input
// pins through the dedicated launch command and returns run, graph and launch/callback references.
// LaunchWorkflowExecution передаёт execution lease, привязки публикации, версии, digest и ввода
// процесса отдельной команде запуска и возвращает запуск, граф и ссылки launch и callback.
func (server *Server) LaunchWorkflowExecution(ctx context.Context, request *controlplanev1.LaunchWorkflowExecutionRequest) (*controlplanev1.LaunchWorkflowExecutionResponse, error) {
	payload := command.LaunchWorkflowInput{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), WorkflowRef: request.GetWorkflowRef(), Task: request.GetTask(), Title: request.GetTitle(), Input: asMap(request.GetInput()), ExpectedPublishedRef: request.GetExpectedPublishedRef(), ExpectedSpecDigest: request.GetExpectedSpecDigest(), ExpectedWorkflowVersion: request.GetExpectedWorkflowVersion()}
	result, err := execute(ctx, server.service, controlplanev1.RuntimeWorkService_LaunchWorkflowExecution_FullMethodName, command.LaunchWorkflowExecution, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	launchRef, _ := result.Runtime["launchRef"].(string)
	callbackRef, _ := result.Runtime["callbackEdgeRef"].(string)
	return &controlplanev1.LaunchWorkflowExecutionResponse{Run: castRun(*result.Run), Graph: castGraph(*result.Graph), LaunchRef: launchRef, CallbackEdgeRef: callbackRef}, nil
}

// GetExecutionWorkflowCatalog validates wire selectors before the authorized lease-bound catalog
// read and projects either pinned publication, active runs or catalog entries with readiness.
// GetExecutionWorkflowCatalog проверяет wire selectors до авторизованного чтения каталога по lease
// и преобразует закреплённую публикацию, активные запуски либо элементы каталога с готовностью.
func (server *Server) GetExecutionWorkflowCatalog(ctx context.Context, request *controlplanev1.GetExecutionWorkflowCatalogRequest) (*controlplanev1.GetExecutionWorkflowCatalogResponse, error) {
	return queryResponse(ctx, controlplanev1.RuntimeWorkService_GetExecutionWorkflowCatalog_FullMethodName, func(p value.Principal) (*controlplanev1.GetExecutionWorkflowCatalogResponse, error) {
		if !workflowCatalogRequestWireValid(request) {
			return nil, errs.ErrInvalid
		}
		input := query.ExecutionWorkflowCatalog{LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), Query: request.GetQuery(), PageToken: request.GetPageToken()}
		if selected := request.GetPublicationRead(); selected != nil {
			input.Publication = &query.ExecutionWorkflowPublicationRead{Pins: workflowReadPins(selected.GetPins())}
		}
		if selected := request.GetActiveRunsRead(); selected != nil {
			input.ActiveRuns = &query.ExecutionWorkflowActiveRunsRead{Pins: workflowReadPins(selected.GetPins())}
		}
		result, err := server.service.GetExecutionWorkflowCatalog(ctx, p, input)
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.GetExecutionWorkflowCatalogResponse{NextPageToken: result.NextPageToken}
		if result.Publication != nil {
			response.Read = &controlplanev1.GetExecutionWorkflowCatalogResponse_Publication{Publication: &controlplanev1.ExecutionWorkflowPublication{ConfigurationJson: result.Publication.ConfigurationJSON, ConfigurationSha256: result.Publication.ConfigurationSHA256}}
			return response, nil
		}
		if result.ActiveRuns != nil {
			source := result.ActiveRuns
			page := &controlplanev1.ExecutionWorkflowActiveRuns{Pins: &controlplanev1.ExecutionWorkflowReadPins{WorkflowRef: source.WorkflowRef, PublishedRef: source.PublishedRef, SpecDigest: source.SpecDigest, WorkflowVersion: source.WorkflowVersion}, NextPageToken: source.NextPageToken}
			for _, item := range source.Items {
				page.Items = append(page.Items, &controlplanev1.ExecutionWorkflowActiveRun{RunRef: item.RunRef, WorkflowRef: item.WorkflowRef, PublishedRef: item.PublishedRef, SpecDigest: item.SpecDigest, PublishedVersion: item.PublishedVersion, RunVersion: item.RunVersion, Title: item.Title, State: item.State, CreatedAt: timestamppb.New(item.CreatedAt)})
			}
			response.Read = &controlplanev1.GetExecutionWorkflowCatalogResponse_ActiveRuns{ActiveRuns: page}
			return response, nil
		}
		for _, item := range result.Items {
			entry := &controlplanev1.ExecutionWorkflowCatalogEntry{WorkflowRef: item.WorkflowRef, Name: item.Name, Purpose: item.Purpose, WorkflowVersion: item.WorkflowVersion, PublishedRef: item.PublishedRef, SpecDigest: item.SpecDigest, Readiness: &controlplanev1.WorkflowLaunchReadiness{AllowedToSubmit: item.Readiness.AllowedToSubmit, Reason: item.Readiness.Reason, WorkflowVersion: item.Readiness.WorkflowVersion, RevisionRef: item.Readiness.RevisionRef, ContextDigest: item.Readiness.ContextDigest, OperationalState: item.Readiness.OperationalState}}
			for _, field := range item.Inputs {
				entry.InputFields = append(entry.InputFields, &controlplanev1.WorkflowInputField{Key: field.Key, Label: field.Label, Description: field.Help, ValueType: field.Type, Required: field.Required, Options: field.Options})
			}
			response.Items = append(response.Items, entry)
		}
		return response, nil

	})
}

// workflowReadPins copies workflow, publication, specification digest and workflow version
// locators with generated getter defaults for absent input.
// workflowReadPins копирует ссылки процесса, публикации, digest спецификации и версию процесса с
// нулевыми generated getter значениями отсутствующего входа.
func workflowReadPins(pins *controlplanev1.ExecutionWorkflowReadPins) query.ExecutionWorkflowReadPins {
	return query.ExecutionWorkflowReadPins{WorkflowRef: pins.GetWorkflowRef(), PublishedRef: pins.GetPublishedRef(), SpecDigest: pins.GetSpecDigest(), WorkflowVersion: pins.GetWorkflowVersion()}
}

// workflowCatalogRequestWireValid rejects absent requests and unknown fields, requiring present
// known-field pins for publication or active-run selectors and otherwise no selector.
// workflowCatalogRequestWireValid отклоняет отсутствующий запрос и unknown fields, требуя явные
// pins без unknown fields для publication либо active-run selectors, иначе отсутствие selector.
func workflowCatalogRequestWireValid(request *controlplanev1.GetExecutionWorkflowCatalogRequest) bool {
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return false
	}
	if value := request.GetPublicationRead(); value != nil {
		return len(value.ProtoReflect().GetUnknown()) == 0 && value.Pins != nil && len(value.Pins.ProtoReflect().GetUnknown()) == 0
	}
	if value := request.GetActiveRunsRead(); value != nil {
		return len(value.ProtoReflect().GetUnknown()) == 0 && value.Pins != nil && len(value.Pins.ProtoReflect().GetUnknown()) == 0
	}
	return request.Read == nil
}
