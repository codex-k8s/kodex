package httptransport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

type handlers struct {
	interactionHub InteractionHubClient
	agentManager   AgentManagerClient
	governance     GovernanceManagerClient
	openAPI        *OpenAPIContract
}

func newHandlers(clients routeClients, openAPI *OpenAPIContract) handlers {
	return handlers{interactionHub: clients.interactionHub, agentManager: clients.agentManager, governance: clients.governance, openAPI: openAPI}
}

func (h handlers) listOwnerInboxItems(w http.ResponseWriter, req *http.Request) {
	handleQuery(w, req, ListOwnerInboxItemsRequest, h.interactionHub.ListOwnerInboxItems, ListOwnerInboxItemsResponse, interactionHubError)
}

func (h handlers) getOwnerInboxItem(w http.ResponseWriter, req *http.Request) {
	handleQuery(w, req, GetOwnerInboxItemRequest, h.interactionHub.GetOwnerInboxItem, OwnerInboxItemResponse, interactionHubError)
}

func (h handlers) respondOwnerInboxItem(w http.ResponseWriter, req *http.Request) {
	body, safeErr := decodeOwnerInboxRespondBody(req)
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	input, err := RecordInteractionResponseRequest(req, body)
	if err != nil {
		WriteSafeError(w, req, err)
		return
	}
	response, callErr := h.interactionHub.RecordInteractionResponse(req.Context(), input)
	if callErr != nil {
		WriteSafeError(w, req, interactionHubError(callErr))
		return
	}
	output, err := OwnerInboxRespondResponse(response, requestIDFromContext(req.Context()))
	if err != nil {
		WriteSafeError(w, req, err)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func (h handlers) getAgentRunRuntimeStatus(w http.ResponseWriter, req *http.Request) {
	handleQuery(w, req, GetAgentRunRuntimeStatusRequest, h.agentManager.GetAgentRunRuntimeStatus, AgentRunRuntimeStatusResponse, agentManagerError)
}

func (h handlers) listAgentSessions(w http.ResponseWriter, req *http.Request) {
	handleQuery(w, req, ListAgentSessionsRequest, h.agentManager.ListAgentSessions, AgentSessionListResponse, agentManagerError)
}

func (h handlers) listAgentRunSummaries(w http.ResponseWriter, req *http.Request) {
	handleQuery(w, req, ListAgentRunSummariesRequest, h.agentManager.ListAgentRunSummaries, AgentRunSummaryListResponse, agentManagerError)
}

func (h handlers) listAgentRunActivities(w http.ResponseWriter, req *http.Request) {
	input, safeErr := ListAgentActivitiesRequest(req)
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	response, err := h.agentManager.ListAgentActivities(req.Context(), input)
	if err != nil {
		WriteSafeError(w, req, agentManagerError(err))
		return
	}
	output, safeErr := AgentRunActivitiesResponse(response, input.GetRunId(), requestIDFromContext(req.Context()))
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func (h handlers) getGovernanceSummary(w http.ResponseWriter, req *http.Request) {
	handleQuery(w, req, GetGovernanceSummaryRequest, h.governance.GetGovernanceSummary, GovernanceSummaryResponse, governanceManagerError)
}

func decodeOwnerInboxRespondBody(req *http.Request) (OwnerInboxRespondBody, *SafeError) {
	var body OwnerInboxRespondBody
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return OwnerInboxRespondBody{}, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "request body is invalid", false)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return OwnerInboxRespondBody{}, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "request body must contain one JSON object", false)
	}
	return body, nil
}

func (h handlers) openAPISpec(w http.ResponseWriter, req *http.Request) {
	data, err := h.openAPI.Read()
	if err != nil {
		WriteSafeError(w, req, WrapSafeError(http.StatusInternalServerError, CodeDownstreamUnavailable, "OpenAPI spec is unavailable", true, err))
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func handleQuery[Request any, Response any, Output any](
	w http.ResponseWriter,
	req *http.Request,
	build func(*http.Request) (*Request, *SafeError),
	call func(context.Context, *Request) (*Response, error),
	cast func(*Response, string) (Output, *SafeError),
	mapError func(error) *SafeError,
) {
	input, safeErr := build(req)
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	response, err := call(req.Context(), input)
	if err != nil {
		WriteSafeError(w, req, mapError(err))
		return
	}
	output, safeErr := cast(response, requestIDFromContext(req.Context()))
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	writeJSON(w, http.StatusOK, output)
}
