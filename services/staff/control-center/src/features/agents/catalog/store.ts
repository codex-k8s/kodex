import { defineStore } from "pinia";
import { computed, ref } from "vue";

import { loadAgentCatalogPage } from "@/features/agents/catalog/api";
import type { Agent } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";

function appendUnique(current: Agent[], incoming: Agent[]): Agent[] {
  const byRef = new Map(current.map((agent) => [agent.ref, agent]));
  for (const agent of incoming) byRef.set(agent.ref, agent);
  return [...byRef.values()];
}

export const useAgentCatalogStore = defineStore("agent-catalog", () => {
  const projectRef = ref("");
  const query = ref("");
  const items = ref<Agent[]>([]);
  const nextPageToken = ref<string>();
  const loading = ref(false);
  const loadingMore = ref(false);
  const problem = ref<AppProblem>();
  const consumedPageTokens = new Set<string>();
  let generation = 0;

  const hasMore = computed(() => Boolean(nextPageToken.value));

  async function load(
    nextProjectRef: string,
    nextQuery = "",
    retain = false,
  ): Promise<void> {
    const current = ++generation;
    const normalizedQuery = nextQuery.trim();
    const keepItems =
      retain &&
      projectRef.value === nextProjectRef &&
      query.value === normalizedQuery;
    projectRef.value = nextProjectRef;
    query.value = normalizedQuery;
    if (!keepItems) items.value = [];
    nextPageToken.value = undefined;
    consumedPageTokens.clear();
    loading.value = true;
    loadingMore.value = false;
    problem.value = undefined;
    try {
      const page = await loadAgentCatalogPage({
        projectRef: nextProjectRef,
        query: normalizedQuery,
      });
      if (generation !== current) return;
      items.value = appendUnique([], page.items);
      nextPageToken.value = page.nextPageToken;
    } catch (error) {
      if (generation === current) problem.value = asProblem(error);
    } finally {
      if (generation === current) loading.value = false;
    }
  }

  async function loadMore(): Promise<void> {
    const pageToken = nextPageToken.value;
    if (
      !pageToken ||
      loading.value ||
      loadingMore.value ||
      consumedPageTokens.has(pageToken)
    )
      return;

    const current = generation;
    const currentProjectRef = projectRef.value;
    const currentQuery = query.value;
    consumedPageTokens.add(pageToken);
    loadingMore.value = true;
    problem.value = undefined;
    try {
      const page = await loadAgentCatalogPage({
        projectRef: currentProjectRef,
        query: currentQuery,
        pageToken,
      });
      if (generation !== current) return;
      items.value = appendUnique(items.value, page.items);
      nextPageToken.value =
        page.nextPageToken && !consumedPageTokens.has(page.nextPageToken)
          ? page.nextPageToken
          : undefined;
    } catch (error) {
      if (generation === current) {
        consumedPageTokens.delete(pageToken);
        problem.value = asProblem(error);
      }
    } finally {
      if (generation === current) loadingMore.value = false;
    }
  }

  function prepareRefresh(retain = false): void {
    generation += 1;
    if (!retain) items.value = [];
    nextPageToken.value = undefined;
    consumedPageTokens.clear();
    loading.value = true;
    loadingMore.value = false;
    problem.value = undefined;
  }

  function clear(): void {
    generation += 1;
    projectRef.value = "";
    query.value = "";
    items.value = [];
    nextPageToken.value = undefined;
    loading.value = false;
    loadingMore.value = false;
    problem.value = undefined;
    consumedPageTokens.clear();
  }

  return {
    projectRef,
    query,
    items,
    loading,
    loadingMore,
    problem,
    hasMore,
    load,
    loadMore,
    prepareRefresh,
    clear,
  };
});
