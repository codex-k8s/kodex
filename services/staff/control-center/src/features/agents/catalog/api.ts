import { requestSignal } from "@/shared/api/client";
import { getAgent, listAgents } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Agent,
  AgentPage,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";

export interface AgentCatalogPageRequest {
  projectRef: string;
  query: string;
  pageToken?: string;
  pageSize?: number;
}

export async function loadAgentCatalogPage(
  request: AgentCatalogPageRequest,
  signal: AbortSignal = requestSignal(),
): Promise<AgentPage> {
  return (
    await unwrap(
      listAgents({
        path: { projectRef: request.projectRef },
        query: {
          pageSize: request.pageSize ?? 20,
          ...(request.query.trim() ? { query: request.query.trim() } : {}),
          ...(request.pageToken ? { pageToken: request.pageToken } : {}),
        },
        signal,
      }),
    )
  ).data;
}

export async function loadAssignedAgent(
  projectRef: string,
  agentRef: string,
  signal: AbortSignal,
): Promise<Agent> {
  const agent = (
    await unwrap(
      getAgent({
        path: { agentRef },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  if (agent.ref !== agentRef || agent.projectRef !== projectRef)
    throw new Error("Assigned workflow agent scope mismatch");
  return agent;
}
