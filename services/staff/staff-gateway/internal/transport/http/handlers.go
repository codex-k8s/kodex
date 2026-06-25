package httptransport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
)

type handlers struct {
	interactionHub       InteractionHubClient
	agentManager         AgentManagerClient
	governance           GovernanceManagerClient
	projectCatalog       ProjectCatalogClient
	openAPI              *OpenAPIContract
	selfDeployProjectRef string
}

func newHandlers(clients routeClients, openAPI *OpenAPIContract, selfDeployProjectRef string) handlers {
	return handlers{interactionHub: clients.interactionHub, agentManager: clients.agentManager, governance: clients.governance, projectCatalog: clients.projectCatalog, openAPI: openAPI, selfDeployProjectRef: selfDeployProjectRef}
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
	handleInputQuery(w, req, ListAgentActivitiesRequest, h.agentManager.ListAgentActivities, AgentRunActivitiesResponseForRequest, agentManagerError)
}

func (h handlers) listProjects(w http.ResponseWriter, req *http.Request) {
	handleQuery(w, req, ListProjectsRequest, h.projectCatalog.ListProjects, ProjectListResponse, projectCatalogError)
}

func (h handlers) listProjectRepositories(w http.ResponseWriter, req *http.Request) {
	handleInputQuery(w, req, ListProjectRepositoriesRequest, h.projectCatalog.ListRepositories, RepositoryListResponseForRequest, projectCatalogError)
}

func (h handlers) getGovernanceSummary(w http.ResponseWriter, req *http.Request) {
	handleQuery(w, req, GetGovernanceSummaryRequest, h.governance.GetGovernanceSummary, GovernanceSummaryResponse, governanceManagerError)
}

func (h handlers) getSelfDeploySummary(w http.ResponseWriter, req *http.Request) {
	input, safeErr := GetSelfDeploySummaryRequest(req, h.selfDeployProjectRef)
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	if !selfDeployPlanRequestHasBoundedFilter(input) {
		output, safeErr := SelfDeploySummaryResponse(&agentsv1.ListSelfDeployPlansResponse{}, nil, input, nil, requestIDFromContext(req.Context()))
		if safeErr != nil {
			WriteSafeError(w, req, safeErr)
			return
		}
		writeJSON(w, http.StatusOK, output)
		return
	}
	plans, err := h.agentManager.ListSelfDeployPlans(req.Context(), input)
	if err != nil {
		WriteSafeError(w, req, agentManagerError(err))
		return
	}
	var readiness *projectSelfDeployReadiness
	if len(plans.GetSelfDeployPlans()) == 0 {
		readiness, safeErr = h.selfDeployReadiness(req, input)
		if safeErr != nil {
			WriteSafeError(w, req, safeErr)
			return
		}
	}
	governanceSummary, safeErr := h.selfDeployGovernanceSummary(req, firstSelfDeployPlan(plans.GetSelfDeployPlans()))
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	output, safeErr := SelfDeploySummaryResponse(plans, readiness, input, governanceSummary, requestIDFromContext(req.Context()))
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func (h handlers) submitSelfDeployGateDecision(w http.ResponseWriter, req *http.Request) {
	body, safeErr := decodeSelfDeployGateDecisionBody(req)
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	input, safeErr := SubmitSelfDeployGateDecisionRequest(req, body)
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	response, err := h.governance.SubmitGateDecision(req.Context(), input)
	if err != nil {
		WriteSafeError(w, req, governanceManagerError(err))
		return
	}
	output, safeErr := SelfDeployGateDecisionResponse(response, body, input.GetGateRequestId(), requestIDFromContext(req.Context()))
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func (h handlers) selfDeployGovernanceSummary(req *http.Request, plan *agentsv1.SelfDeployPlan) (*governancev1.GovernanceSummaryResponse, *SafeError) {
	input, safeErr := GetSelfDeployGovernanceSummaryRequest(req, plan)
	if safeErr != nil || input == nil {
		return nil, safeErr
	}
	response, err := h.governance.GetGovernanceSummary(req.Context(), input)
	if err != nil {
		return nil, governanceManagerError(err)
	}
	return response, nil
}

func (h handlers) selfDeployReadiness(req *http.Request, input *agentsv1.ListSelfDeployPlansRequest) (*projectSelfDeployReadiness, *SafeError) {
	readiness := &projectSelfDeployReadiness{
		projectID:         selfDeployProjectCatalogID(input),
		repositoryID:      input.GetRepositoryRef(),
		providerSignalRef: input.GetProviderSignalRef(),
	}
	signalRequest, safeErr := GetSelfDeploySignalRequest(req, input)
	if safeErr != nil || signalRequest == nil {
		repositoriesRequest, safeErr := ListSelfDeployRepositoriesRequest(req, input)
		if safeErr != nil || repositoriesRequest == nil {
			return readiness, safeErr
		}
		repositories, err := h.projectCatalog.ListRepositories(req.Context(), repositoriesRequest)
		if err != nil {
			if downstreamNotFound(err) {
				readiness.projectMissing = true
				return readiness, nil
			}
			return nil, projectCatalogError(err)
		}
		readiness.repositories = repositories
		return readiness, nil
	}
	response, err := h.projectCatalog.GetSelfDeploySignal(req.Context(), signalRequest)
	if err != nil {
		if downstreamNotFound(err) {
			readiness.projectMissing = true
			return readiness, nil
		}
		return nil, projectCatalogSelfDeploySignalError(err)
	}
	readiness.signal = response
	return readiness, nil
}

func decodeSelfDeployGateDecisionBody(req *http.Request) (SelfDeployGateDecisionBody, *SafeError) {
	return decodeJSONBody[SelfDeployGateDecisionBody](req)
}

func decodeOwnerInboxRespondBody(req *http.Request) (OwnerInboxRespondBody, *SafeError) {
	return decodeJSONBody[OwnerInboxRespondBody](req)
}

func decodeJSONBody[Body any](req *http.Request) (Body, *SafeError) {
	var body Body
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return body, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "request body is invalid", false)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return body, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "request body must contain one JSON object", false)
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

func handleInputQuery[Request any, Response any, Output any](
	w http.ResponseWriter,
	req *http.Request,
	build func(*http.Request) (*Request, *SafeError),
	call func(context.Context, *Request) (*Response, error),
	cast func(*Response, *Request, string) (Output, *SafeError),
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
	output, safeErr := cast(response, input, requestIDFromContext(req.Context()))
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	writeJSON(w, http.StatusOK, output)
}
