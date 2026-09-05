import { requestSignal } from "@/shared/api/client";
import { listArtifacts } from "@/shared/api/generated/openapi/sdk.gen";
import { unwrap } from "@/shared/api/problem";
import type { AsyncEntityLoader } from "@/shared/ui/async-entity-picker";
import {
  toArtifactPickerItem,
  toSessionPickerItem,
  type ArtifactPickerItem,
  type NewRunTargetType,
  type SessionPickerItem,
} from "@/features/new-run/model";
import { loadSessionCatalog } from "@/features/workboard/session-catalog";

const artifactPageSize = 40;

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
  return async ({ cursor, query, signal }) => {
    const page = await loadSessionCatalog({ ...scope, query }, cursor, signal);
    return {
      items: page.items.map(toSessionPickerItem),
      nextCursor: page.nextPageToken || null,
    };
  };
}
