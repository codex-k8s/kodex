import { getAccessMembershipGraph as getAccessMembershipGraphRequest } from "../../shared/api/sdk";

import type { AccessMembershipGraph } from "./types";

export async function getAccessMembershipGraph(limit = 500): Promise<AccessMembershipGraph> {
  const resp = await getAccessMembershipGraphRequest({ query: { limit }, throwOnError: true });
  return resp.data;
}
