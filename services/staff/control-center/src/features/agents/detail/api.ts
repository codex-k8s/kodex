import { listTemplateVariables } from "@/shared/api/generated/openapi/sdk.gen";
import { unwrap } from "@/shared/api/problem";
import type { AsyncEntityLoader } from "@/shared/ui/async-entity-picker";

import {
  toTemplateVariablePickerItem,
  type TemplateVariablePickerItem,
} from "./model";

export function createTemplateVariableLoader(
  projectRef: string,
): AsyncEntityLoader<TemplateVariablePickerItem> {
  return async ({ cursor, query, signal }) => {
    const result = await unwrap(
      listTemplateVariables({
        path: { projectRef },
        query: {
          pageSize: 50,
          ...(query.trim() ? { query: query.trim() } : {}),
          ...(cursor ? { pageToken: cursor } : {}),
        },
        signal,
      }),
    );
    return {
      items: result.data.items.map(toTemplateVariablePickerItem),
      nextCursor: result.data.nextPageToken ?? null,
    };
  };
}
