import {
  listArtifacts,
  listProjects,
  listRuns,
  getRun,
} from "@/shared/api/generated/openapi/sdk.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
export async function projects(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  const page = (
    await unwrap(
      listProjects({
        query: { query, pageToken, pageSize: 40 },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      title: item.name,
      description: item.purpose,
      meta: item.lifecycle,
    })),
    nextPageToken: page.nextPageToken,
  };
}
export async function artifacts(
  projectRef: string,
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
): Promise<{ items: Artifact[]; nextPageToken?: string }> {
  const page = (
    await unwrap(
      listArtifacts({
        path: { projectRef },
        query: { query, pageToken, pageSize: 40, lifecycleState: "ACTIVE" },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  if (
    page.items.some(
      (item) =>
        item.projectRef !== projectRef ||
        item.lifecycleState !== "ACTIVE" ||
        !Number.isSafeInteger(item.revision) ||
        item.revision < 1,
    )
  )
    throw new Error("Invalid Skill artifact selection");
  return page;
}

export async function runs(
  projectRef: string,
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  const page = (
    await unwrap(
      listRuns({
        query: { projectRef, query, pageToken, pageSize: 40 },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  if (page.items.some((item) => item.projectRef !== projectRef))
    throw new Error("Invalid memory source run scope");
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      title: item.title,
      description: item.activitySummary,
      meta: item.state,
    })),
    nextPageToken: page.nextPageToken,
  };
}
export async function sourceRun(
  projectRef: string,
  runRef: string,
  signal: AbortSignal,
): Promise<AsyncEntityOption> {
  const item = (
    await unwrap(getRun({ path: { runRef }, signal: requestSignal(signal) }))
  ).data;
  if (item.projectRef !== projectRef || item.ref !== runRef)
    throw new Error("Invalid memory source run scope");
  return {
    ref: item.ref,
    title: item.title,
    description: item.activitySummary,
    meta: item.state,
  };
}
