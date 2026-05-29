import {
  getAgentRunRuntimeStatus,
  getOwnerInboxItem,
  listAgentRunActivities,
  listOwnerInboxItems,
  respondOwnerInboxItem,
  type AgentActivityKind,
  type AgentActivityStatus,
  type AgentRunActivitiesResponse,
  type AgentRunRuntimeStatusResponse,
  type OwnerInboxItemResponse,
  type OwnerInboxListResponse,
  type OwnerInboxRespondRequest,
  type OwnerInboxRespondResponse,
  type RequestKind,
  type RequestStatus,
} from './generated';
import { client as staffGatewayClient } from './generated/client.gen';
import { type OperatorContext, gatewayHeaders } from './context';
import { normalizeApiError } from './errors';

const defaultBaseURL = import.meta.env.VITE_STAFF_GATEWAY_BASE_URL ?? '';
const requestTimeoutMs = Number(import.meta.env.VITE_STAFF_GATEWAY_TIMEOUT_MS ?? 15000);

staffGatewayClient.setConfig({
  baseURL: defaultBaseURL,
  timeout: requestTimeoutMs,
  withCredentials: true,
});

staffGatewayClient.instance.interceptors.request.use((config) => {
  config.headers.set('Accept-Language', 'ru');
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

export async function fetchOwnerInboxItems(
  context: OperatorContext,
  query: OwnerInboxListQuery,
): Promise<OwnerInboxListResponse> {
  try {
    const response = await listOwnerInboxItems({
      client: staffGatewayClient,
      throwOnError: true,
      headers: gatewayHeaders(context),
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
      headers: gatewayHeaders(context),
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
      headers: gatewayHeaders(context),
      path: { request_id: requestId },
      body,
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
      headers: gatewayHeaders(context),
      path: { run_id: runId },
    });
    return response.data;
  } catch (error) {
    throw normalizeApiError(error);
  }
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
      headers: gatewayHeaders(context),
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
