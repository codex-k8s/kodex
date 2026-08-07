import { defineStore } from "pinia";
import { reactive } from "vue";

import { searchAuthoritativeResources } from "@/shared/api/adapters/resources";
import type {
  Resource,
  ResourceKind,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem } from "@/shared/api/problem";
import {
  beginRequest,
  failRequest,
  finishRequest,
  remoteState,
} from "@/shared/lib/remote";

export const useSearchStore = defineStore("search", () => {
  const results = reactive(remoteState<Resource[]>([]));

  async function search(kind: ResourceKind, query: string): Promise<void> {
    const request = beginRequest(results);
    try {
      const page = await searchAuthoritativeResources(kind, query);
      finishRequest(
        results,
        request,
        page.resources,
        page.resources.length === 0,
      );
    } catch (error) {
      failRequest(results, request, asProblem(error));
    }
  }

  return { results, search };
});
