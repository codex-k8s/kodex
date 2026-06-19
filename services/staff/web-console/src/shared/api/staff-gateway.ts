import {
  getAgentRunRuntimeStatus,
  getSelfDeploySummary,
  getOwnerInboxItem,
  listAgentRunActivities,
  listAgentRunSummaries,
  listAgentSessions,
  listOwnerInboxItems,
  respondOwnerInboxItem,
  submitSelfDeployGateDecision,
  type AgentActivityKind,
  type AgentActivityStatus,
  type AgentRunStatus,
  type AgentRunActivitiesResponse,
  type AgentRunRuntimeStatusResponse,
  type AgentRunSummaryListResponse,
  type AgentSessionListResponse,
  type AgentSessionStatus,
  type GetAgentRunRuntimeStatusData,
  type GetSelfDeploySummaryData,
  type GetOwnerInboxItemData,
  type ListAgentRunActivitiesData,
  type ListAgentRunSummariesData,
  type ListAgentSessionsData,
  type ListOwnerInboxItemsData,
  type OwnerInboxItemResponse,
  type OwnerInboxListResponse,
  type OwnerInboxRespondRequest,
  type OwnerInboxRespondResponse,
  type RespondOwnerInboxItemData,
  type RequestKind,
  type RequestStatus,
  type SelfDeploySummaryResponse,
  type SelfDeployGateDecisionRequest,
  type SelfDeployGateDecisionResponse,
  type SubmitSelfDeployGateDecisionData,
} from './generated';
import { client as staffGatewayClient } from './generated/client.gen';
import { isAgentScopeType, type OperatorContext, operationHeaders } from './context';
import { normalizeApiError } from './errors';
import { getAcceptLanguage } from '@/shared/lib/locale';

const defaultBaseURL = import.meta.env.VITE_STAFF_GATEWAY_BASE_URL ?? '';
const requestTimeoutMs = Number(import.meta.env.VITE_STAFF_GATEWAY_TIMEOUT_MS ?? 15000);

staffGatewayClient.setConfig({
  baseURL: defaultBaseURL,
  timeout: requestTimeoutMs,
  withCredentials: true,
});

staffGatewayClient.instance.interceptors.request.use((config) => {
  config.headers.set('Accept-Language', getAcceptLanguage());
  return config;
});

export type OwnerInboxListQuery = {
  requestKinds?: RequestKind[];
  statuses?: RequestStatus[];
  includeDiagnostics?: boolean;
  pageSize?: number;
  pageToken?: string;
};

export type ActivityTimelineQuery = {
  activityKind?: AgentActivityKind;
  status?: AgentActivityStatus;
  pageSize?: number;
  pageToken?: string;
};

export type AgentSessionListQuery = {
  status?: AgentSessionStatus;
  providerWorkItemRef?: string;
  createdByActorRef?: string;
  createdAfter?: string;
  createdBefore?: string;
  pageSize?: number;
  pageToken?: string;
};

export type AgentRunSummaryListQuery = {
  sessionId?: string;
  roleProfileId?: string;
  status?: AgentRunStatus;
  providerWorkItemRef?: string;
  providerPullRequestRef?: string;
  createdAfter?: string;
  createdBefore?: string;
  pageSize?: number;
  pageToken?: string;
};

export type SelfDeploySummaryQuery = {
  projectRef?: string;
  repositoryRef?: string;
  providerSignalRef?: string;
};

export function canQueryAgentScope(context: OperatorContext): boolean {
  return isAgentScopeType(context.scopeType);
}

export async function fetchOwnerInboxItems(
  context: OperatorContext,
  query: OwnerInboxListQuery,
): Promise<OwnerInboxListResponse> {
  try {
    const response = await listOwnerInboxItems({
      client: staffGatewayClient,
      throwOnError: true,
      headers: operationHeaders<ListOwnerInboxItemsData['headers']>(context),
      query: {
        scope_type: context.scopeType,
        scope_ref: context.scopeRef.trim(),
        request_kind: query.requestKinds,
        status: query.statuses,
        include_diagnostics: query.includeDiagnostics,
        page_size: query.pageSize,
        page_token: query.pageToken,
      },
    });
    return response.data;
  } catch (error) {
    throw normalizeApiError(error);
  }
}

export async function fetchOwnerInboxItem(
  context: OperatorContext,
  requestId: string,
  includeDiagnostics: boolean,
): Promise<OwnerInboxItemResponse> {
  try {
    const response = await getOwnerInboxItem({
      client: staffGatewayClient,
      throwOnError: true,
      headers: operationHeaders<GetOwnerInboxItemData['headers']>(context),
      path: { request_id: requestId },
      query: {
        scope_type: context.scopeType,
        scope_ref: context.scopeRef.trim(),
        include_diagnostics: includeDiagnostics,
      },
    });
    return response.data;
  } catch (error) {
    throw normalizeApiError(error);
  }
}

export async function sendOwnerInboxResponse(
  context: OperatorContext,
  requestId: string,
  body: OwnerInboxRespondRequest,
): Promise<OwnerInboxRespondResponse> {
  try {
    const response = await respondOwnerInboxItem({
      client: staffGatewayClient,
      throwOnError: true,
      headers: operationHeaders<RespondOwnerInboxItemData['headers']>(context),
      path: { request_id: requestId },
      body,
    });
    return response.data;
  } catch (error) {
    throw normalizeApiError(error);
  }
}

export async function fetchAgentSessions(
  context: OperatorContext,
  query: AgentSessionListQuery,
): Promise<AgentSessionListResponse> {
  if (!isAgentScopeType(context.scopeType)) {
    throw unsupportedAgentScopeError();
  }
  try {
    const response = await listAgentSessions({
      client: staffGatewayClient,
      throwOnError: true,
      headers: operationHeaders<ListAgentSessionsData['headers']>(context),
      query: {
        scope_type: context.scopeType,
        scope_ref: context.scopeRef.trim(),
        status: query.status,
        provider_work_item_ref: query.providerWorkItemRef,
        created_by_actor_ref: query.createdByActorRef,
        created_after: query.createdAfter,
        created_before: query.createdBefore,
        page_size: query.pageSize,
        page_token: query.pageToken,
      },
    });
    return response.data;
  } catch (error) {
    throw normalizeApiError(error);
  }
}

export async function fetchAgentRunSummaries(
  context: OperatorContext,
  query: AgentRunSummaryListQuery,
): Promise<AgentRunSummaryListResponse> {
  if (!isAgentScopeType(context.scopeType)) {
    throw unsupportedAgentScopeError();
  }
  try {
    const response = await listAgentRunSummaries({
      client: staffGatewayClient,
      throwOnError: true,
      headers: operationHeaders<ListAgentRunSummariesData['headers']>(context),
      query: {
        scope_type: context.scopeType,
        scope_ref: context.scopeRef.trim(),
        session_id: query.sessionId,
        role_profile_id: query.roleProfileId,
        status: query.status,
        provider_work_item_ref: query.providerWorkItemRef,
        provider_pull_request_ref: query.providerPullRequestRef,
        created_after: query.createdAfter,
        created_before: query.createdBefore,
        page_size: query.pageSize,
        page_token: query.pageToken,
      },
    });
    return response.data;
  } catch (error) {
    throw normalizeApiError(error);
  }
}

export async function fetchAgentRunRuntimeStatus(
  context: OperatorContext,
  runId: string,
): Promise<AgentRunRuntimeStatusResponse> {
  try {
    const response = await getAgentRunRuntimeStatus({
      client: staffGatewayClient,
      throwOnError: true,
      headers: operationHeaders<GetAgentRunRuntimeStatusData['headers']>(context),
      path: { run_id: runId },
    });
    return response.data;
  } catch (error) {
    throw normalizeApiError(error);
  }
}

function unsupportedAgentScopeError(): Error {
  return new Error('unsupported_agent_scope');
}

export async function fetchAgentRunActivities(
  context: OperatorContext,
  runId: string,
  query: ActivityTimelineQuery,
): Promise<AgentRunActivitiesResponse> {
  try {
    const response = await listAgentRunActivities({
      client: staffGatewayClient,
      throwOnError: true,
      headers: operationHeaders<ListAgentRunActivitiesData['headers']>(context),
      path: { run_id: runId },
      query: {
        activity_kind: query.activityKind,
        status: query.status,
        page_size: query.pageSize,
        page_token: query.pageToken,
      },
    });
    return response.data;
  } catch (error) {
    throw normalizeApiError(error);
  }
}

export async function fetchSelfDeploySummary(
  context: OperatorContext,
  query: SelfDeploySummaryQuery = {},
): Promise<SelfDeploySummaryResponse> {
  try {
    const response = await getSelfDeploySummary({
      client: staffGatewayClient,
      throwOnError: true,
      headers: operationHeaders<GetSelfDeploySummaryData['headers']>(context),
      query: {
        project_ref: query.projectRef,
        repository_ref: query.repositoryRef,
        provider_signal_ref: query.providerSignalRef,
      },
    });
    return response.data;
  } catch (error) {
    throw normalizeApiError(error);
  }
}

export async function sendSelfDeployGateDecision(
  context: OperatorContext,
  gateRequestId: string,
  body: SelfDeployGateDecisionRequest,
): Promise<SelfDeployGateDecisionResponse> {
  try {
    const response = await submitSelfDeployGateDecision({
      client: staffGatewayClient,
      throwOnError: true,
      headers: operationHeaders<SubmitSelfDeployGateDecisionData['headers']>(context),
      path: { gate_request_id: gateRequestId },
      body,
    });
    return response.data;
  } catch (error) {
    throw normalizeApiError(error);
  }
}
