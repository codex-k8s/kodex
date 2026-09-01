import { requestSignal } from "@/shared/api/client";
import { listAgents } from "@/shared/api/generated/openapi/sdk.gen";
import type { AgentPage } from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";

export interface AgentCatalogPageRequest {
  projectRef: string;
  query: string;
  pageToken?: string;
  pageSize?: number;
}

export async function loadAgentCatalogPage(
  request: AgentCatalogPageRequest,
): Promise<AgentPage> {
  return (
    await unwrap(
      listAgents({
        path: { projectRef: request.projectRef },
        query: {
          pageSize: request.pageSize ?? 40,
          ...(request.query.trim() ? { query: request.query.trim() } : {}),
          ...(request.pageToken ? { pageToken: request.pageToken } : {}),
        },
        signal: requestSignal(),
      }),
    )
  ).data;
}
