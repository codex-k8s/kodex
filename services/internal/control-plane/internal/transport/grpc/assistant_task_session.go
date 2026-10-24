package grpc

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/controlplaneapi"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// readAssistantTaskSession rejects unknown fields and mixed search selectors before the
// lease-bound session read, then seals the projected published-message page or fails unavailable.
// readAssistantTaskSession отклоняет unknown fields и смешанные search selectors до чтения сессии
// по lease, затем запечатывает страницу опубликованных сообщений либо отказывает как unavailable.
func (server *Server) readAssistantTaskSession(ctx context.Context, p value.Principal, request *controlplanev1.SearchAssistantResourcesRequest) (*controlplanev1.SearchAssistantResourcesResponse, error) {
	selector := request.GetAssistantTaskSessionRead()
	if selector == nil || len(request.ProtoReflect().GetUnknown()) != 0 || len(selector.ProtoReflect().GetUnknown()) != 0 || request.Query != "" || request.IntegrationDefinitionCatalog || request.DefinitionQuery != "" || request.DefinitionOffset != 0 || request.AssistantConfigurationCatalog != nil {
		return nil, transportError(errs.ErrInvalid)
	}
	result, err := server.service.ReadAssistantTaskSession(ctx, p, request.LeaseRef, request.Fence, request.Generation, query.AssistantTaskSessionRead{RunRef: selector.RunRef, Cursor: selector.Cursor})
	if err != nil {
		return nil, transportError(err)
	}
	page := &controlplanev1.AssistantTaskSessionPage{RunRef: result.RunRef, ProjectRef: result.ProjectRef, SessionRef: result.SessionRef, Title: result.Title, State: controlplanev1.RunState(controlplanev1.RunState_value["RUN_STATE_"+result.State]), RunVersion: result.RunVersion, ResultSummary: result.ResultSummary, SafeErrorCode: result.SafeErrorCode, SafeErrorMessage: result.SafeErrorMessage, SessionStorageState: result.SessionStorageState, SourceSha256: result.SourceSHA256, NextCursor: result.NextCursor, Truncated: result.Truncated}
	for _, item := range result.Messages {
		page.Messages = append(page.Messages, &controlplanev1.AssistantTaskPublishedMessage{EventRef: item.EventRef, MessageRef: item.MessageRef, Phase: controlplanev1.AssistantTaskMessagePhase(controlplanev1.AssistantTaskMessagePhase_value["ASSISTANT_TASK_MESSAGE_PHASE_"+item.Phase]), Origin: controlplanev1.AssistantTaskMessageOrigin(controlplanev1.AssistantTaskMessageOrigin_value["ASSISTANT_TASK_MESSAGE_ORIGIN_"+item.Origin]), Text: item.Text, SourceRunRef: item.SourceRunRef, SourceRunVersion: item.SourceRunVersion, SessionRef: item.SessionRef, NodeRef: item.NodeRef, TurnRef: item.TurnRef, TurnNumber: item.TurnNumber, Attempt: item.Attempt, EventSequence: item.EventSequence, MessageRevision: item.MessageRevision})
	}
	if controlplaneapi.SealTaskSessionPage(page) != nil {
		return nil, transportError(errs.ErrUnavailable)
	}
	return &controlplanev1.SearchAssistantResourcesResponse{AssistantTaskSession: page}, nil
}
