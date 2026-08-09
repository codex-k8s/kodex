import { defineStore } from "pinia";
import { reactive } from "vue";

import { toSearchResult, type SearchResult } from "@/features/search/model";
import { searchAuthoritativeResources } from "@/shared/api/adapters/resources";
import type { ResourceKind } from "@/shared/api/generated/openapi/types.gen";
import { asProblem } from "@/shared/api/problem";
import { collectAllPages } from "@/shared/api/pagination";
import { resourceKinds } from "@/shared/lib/resources";
import {
  beginRequest,
  failRequest,
  finishRequest,
  remoteState,
} from "@/shared/lib/remote";

export const useSearchStore = defineStore("search", () => {
  const results = reactive(remoteState<SearchResult[]>([]));

  async function search(
    kind: ResourceKind | "ALL",
    query: string,
  ): Promise<void> {
    const request = beginRequest(results);
    try {
      const selectedKinds = kind === "ALL" ? resourceKinds : [kind];
      const pages = await Promise.all(
        selectedKinds.map(async (selectedKind) => {
          const { values } = await collectAllPages(
            (pageToken) =>
              searchAuthoritativeResources(selectedKind, query, pageToken),
            (page) => page.resources,
          );
          return values;
        }),
      );
      const values = pages
        .flat()
        .map(toSearchResult)
        .sort((left, right) => left.name.localeCompare(right.name));
      finishRequest(results, request, values, values.length === 0);
    } catch (error) {
      failRequest(results, request, asProblem(error));
    }
  }

  function reset(): void {
    results.data = [];
    results.phase = "idle";
    results.problem = null;
    results.requestVersion += 1;
  }

  return { results, search, reset };
});
