import {
  listRuns,
  listArtifacts,
  listOrganizationArtifacts,
} from "@/shared/api/generated/openapi/sdk.gen";
import type { Artifact, Run } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import { runFilterStates } from "@/features/workboard/run-catalog";
import type { RunFilter } from "@/features/workboard/model";
export interface HomeResultItem {
  ref: string;
  title: string;
  description: string;
  state: string;
  to?: string;
  artifact?: Artifact;
}
export interface HomeResultScope {
  kind: "RUN" | "ARTIFACT";
  projectRef: string;
  query: string;
  runFilter: RunFilter | "FAILED";
}
export async function loadHomeResultPage(
  scope: HomeResultScope,
  pageToken: string | undefined,
  signal: AbortSignal,
) {
  const query = { query: scope.query.trim(), pageToken, pageSize: 30 };
  let items: HomeResultItem[];
  let total: number;
  let nextPageToken: string | undefined;
  if (scope.kind === "RUN") {
    const states: Run["state"][] | undefined =
      scope.runFilter === "FAILED"
        ? ["FAILED"]
        : runFilterStates(scope.runFilter);
    const page = (
      await unwrap(
        listRuns({
          query: {
            ...query,
            ...(scope.projectRef ? { projectRef: scope.projectRef } : {}),
            ...(states ? { states } : {}),
          },
          signal: requestSignal(signal),
        }),
      )
    ).data;
    if (
      page.items.some(
        (item) =>
          (scope.projectRef && item.projectRef !== scope.projectRef) ||
          (states && !states.includes(item.state)),
      )
    )
      throw new Error("Home run scope mismatch");
    items = page.items.map((item) => ({
      ref: item.ref,
      title: item.title,
      description: item.projectRef,
      state: item.state,
      to: `/runs/${encodeURIComponent(item.ref)}`,
    }));
    total = page.total;
    nextPageToken = page.nextPageToken;
  } else {
    const request = {
      query: { ...query, lifecycleState: "ACTIVE" as const },
      signal: requestSignal(signal),
    };
    const page = (
      await unwrap(
        scope.projectRef
          ? listArtifacts({
              ...request,
              path: { projectRef: scope.projectRef },
            })
          : listOrganizationArtifacts(request),
      )
    ).data;
    if (
      page.items.some(
        (item) =>
          (scope.projectRef && item.projectRef !== scope.projectRef) ||
          item.lifecycleState !== "ACTIVE",
      )
    )
      throw new Error("Home artifact scope mismatch");
    items = page.items.map((item) => ({
      ref: item.ref,
      title: item.fileName,
      description: item.mediaType,
      state: item.scanState,
      artifact: item,
      ...(item.projectRef
        ? {
            to: `/projects/${encodeURIComponent(item.projectRef)}/files?artifactRef=${encodeURIComponent(item.ref)}`,
          }
        : {}),
    }));
    total = page.total;
    nextPageToken = page.nextPageToken;
  }
  signal.throwIfAborted();
  if (
    !Number.isSafeInteger(total) ||
    total < items.length ||
    new Set(items.map((item) => item.ref)).size !== items.length ||
    (nextPageToken && nextPageToken === pageToken)
  )
    throw new Error("Invalid Home result page");
  return { items, total, nextPageToken };
}
