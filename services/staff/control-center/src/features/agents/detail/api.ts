import { listTemplateVariables } from "@/shared/api/generated/openapi/sdk.gen";
import { unwrap } from "@/shared/api/problem";
import type { AsyncEntityLoader } from "@/shared/ui/async-entity-picker";

import {
  toTemplateVariablePickerItem,
  type TemplateVariablePickerItem,
} from "./model";

export function createTemplateVariableLoader(
  projectRef: string,
  context: { agentRef?: string; runtimeRevisionRef?: string } = {},
): AsyncEntityLoader<TemplateVariablePickerItem> {
  return async ({ cursor, query, signal }) => {
    const result = await unwrap(
      listTemplateVariables({
        path: { projectRef },
        query: {
          pageSize: 50,
          ...(query.trim() ? { query: query.trim() } : {}),
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
