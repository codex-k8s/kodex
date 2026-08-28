import { requestSignal } from "@/shared/api/client";
import {
  listArtifacts,
  listRuns,
} from "@/shared/api/generated/openapi/sdk.gen";
import { unwrap } from "@/shared/api/problem";
import type {
  AsyncEntityLoader,
  AsyncEntityPage,
} from "@/shared/ui/async-entity-picker";
import {
  toArtifactPickerItem,
  toSessionPickerItem,
  uniqueResumableRuns,
  type ArtifactPickerItem,
  type NewRunTargetType,
  type SessionPickerItem,
} from "@/features/new-run/model";

const artifactPageSize = 40;
const runPageSize = 100;

function combinedSignal(signal: AbortSignal): AbortSignal {
  return AbortSignal.any([signal, requestSignal()]);
}

function optionalQuery(query: string): string | undefined {
  const value = query.trim();
  return value || undefined;
}

export function createArtifactPickerLoader(
  projectRef: string,
): AsyncEntityLoader<ArtifactPickerItem> {
  return async ({ cursor, query, signal }) => {
    const searchQuery = optionalQuery(query);
    const response = await unwrap(
      listArtifacts({
        path: { projectRef },
        query: {
          pageSize: artifactPageSize,
          ...(cursor ? { pageToken: cursor } : {}),
          ...(searchQuery ? { query: searchQuery } : {}),
        },
        signal: combinedSignal(signal),
      }),
    );
    return {
      items: response.data.items.map(toArtifactPickerItem),
      nextCursor: response.data.nextPageToken || null,
    };
  };
}

export function createSessionPickerLoader(scope: {
  projectRef: string;
  targetRef: string;
  targetType: NewRunTargetType;
}): AsyncEntityLoader<SessionPickerItem> {
  let activeQuery = "";
  let seenSessionRefs = new Set<string>();

  return async ({ cursor, query, signal }) => {
    if (!cursor || query !== activeQuery) {
      activeQuery = query;
      seenSessionRefs = new Set<string>();
    }

    const visitedPageTokens = new Set(cursor ? [cursor] : []);
    const searchQuery = optionalQuery(query);

    async function loadMatchingPage(
      pageToken?: string,
    ): Promise<AsyncEntityPage<SessionPickerItem>> {
      const response = await unwrap(
        listRuns({
          query: {
            projectRef: scope.projectRef,
            pageSize: runPageSize,
            ...(pageToken ? { pageToken } : {}),
            ...(searchQuery ? { query: searchQuery } : {}),
          },
          signal: combinedSignal(signal),
        }),
      );
      const runs = uniqueResumableRuns(
        response.data.items,
        scope,
        seenSessionRefs,
      );
      const candidateCursor = response.data.nextPageToken || null;
      const nextCursor =
        candidateCursor && !visitedPageTokens.has(candidateCursor)
          ? candidateCursor
          : null;
      if (runs.length > 0 || nextCursor === null) {
        return {
          items: runs.map(toSessionPickerItem),
          nextCursor,
        };
      }
      visitedPageTokens.add(nextCursor);
      return loadMatchingPage(nextCursor);
    }

    return loadMatchingPage(cursor);
  };
}
