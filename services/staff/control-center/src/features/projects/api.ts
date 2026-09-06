import {
  getProject,
  listProjects,
} from "@/shared/api/generated/openapi/sdk.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";

export async function loadProject(projectRef: string, signal: AbortSignal) {
  return (
    await unwrap(
      getProject({ path: { projectRef }, signal: requestSignal(signal) }),
    )
  ).data;
}

export async function searchProjects(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
) {
  return (
    await unwrap(
      listProjects({
        query: { query, pageToken, pageSize: 30 },
        signal: requestSignal(signal),
      }),
    )
  ).data;
}
