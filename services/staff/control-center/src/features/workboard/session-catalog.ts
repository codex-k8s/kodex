import { listRuns } from "@/shared/api/generated/openapi/sdk.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";

export interface SessionCatalogScope {
  projectRef?: string;
  query: string;
  targetType?: "AGENT" | "WORKFLOW";
  targetRef?: string;
}

export async function loadSessionCatalog(
  scope: SessionCatalogScope,
  pageToken: string | undefined,
  signal: AbortSignal,
) {
  if (Boolean(scope.targetType) !== Boolean(scope.targetRef))
    throw new Error("Incomplete session target scope");
  const page = (
    await unwrap(
      listRuns({
        query: {
          resumableSessionsOnly: true,
          projectRef: scope.projectRef || undefined,
          query: scope.query.trim(),
          targetType: scope.targetType,
          targetRef: scope.targetRef,
          pageToken,
          pageSize: 30,
        },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  signal.throwIfAborted();
  if (
    !Number.isSafeInteger(page.total) ||
    page.total < page.items.length ||
    new Set(page.items.map((item) => item.sessionRef)).size !==
      page.items.length ||
    page.items.some(
      (item) =>
        !item.sessionRef ||
        item.state !== "SUCCEEDED" ||
        !item.nextActions.includes("ADD_TURN") ||
        (scope.projectRef && item.projectRef !== scope.projectRef) ||
        (scope.targetRef &&
          (item.target.type !== scope.targetType ||
            item.target.ref !== scope.targetRef)),
    ) ||
    (page.nextPageToken &&
      (page.nextPageToken === pageToken || !page.items.length))
  )
    throw new Error("Invalid resumable session catalog");
  return page;
}
