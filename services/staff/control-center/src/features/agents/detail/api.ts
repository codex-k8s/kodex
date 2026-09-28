import { listTemplateVariables } from "@/shared/api/generated/openapi/sdk.gen";
import type { TemplateVariableSourceQuery } from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import type {
  AsyncEntityLoadRequest,
  AsyncEntityPage,
} from "@/shared/ui/async-entity-picker";

import {
  toTemplateVariablePickerItem,
  type TemplateVariablePickerItem,
} from "./model";

export interface TemplateVariableLoadRequest extends AsyncEntityLoadRequest {
  source?: TemplateVariableSourceQuery;
}

export type TemplateVariableLoader = (
  request: TemplateVariableLoadRequest,
) => Promise<AsyncEntityPage<TemplateVariablePickerItem>>;

export function createTemplateVariableLoader(
  projectRef: string,
  context: {
    agentRef?: string;
    runtimeRevisionRef?: string;
    source?: TemplateVariableSourceQuery;
  } = {},
): TemplateVariableLoader {
  return async ({ cursor, query, pageSize, signal, source }) => {
    const requestedSource = source ?? context.source;
    const result = await unwrap(
      listTemplateVariables({
        path: { projectRef },
        query: {
          pageSize: Math.min(100, Math.max(1, Math.floor(pageSize ?? 20))),
          ...(query.trim() ? { query: query.trim() } : {}),
          ...(requestedSource ? { source: requestedSource } : {}),
          ...(cursor ? { pageToken: cursor } : {}),
          ...(context.agentRef ? { agentRef: context.agentRef } : {}),
          ...(context.runtimeRevisionRef
            ? { runtimeRevisionRef: context.runtimeRevisionRef }
            : {}),
        },
        signal,
      }),
    );
    if (
      !Number.isSafeInteger(result.data.total) ||
      result.data.total < result.data.items.length
    )
      throw new Error("Invalid template variable total");
    return {
      items: result.data.items.map(toTemplateVariablePickerItem),
      nextCursor: result.data.nextPageToken ?? null,
    };
  };
}
