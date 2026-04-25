import { defineStore } from "pinia";

import { normalizeApiError, type ApiError } from "../../shared/api/errors";
import { getAccessMembershipGraph } from "./api";
import type { AccessMembershipGraph } from "./types";

const emptyGraph = (): AccessMembershipGraph => ({
  organizations: [],
  groups: [],
  organization_memberships: [],
  user_group_memberships: [],
});

export const useAccessGraphStore = defineStore("accessGraph", {
  state: () => ({
    graph: emptyGraph() as AccessMembershipGraph,
    loading: false,
    error: null as ApiError | null,
  }),
  actions: {
    async load(limit?: number): Promise<void> {
      this.loading = true;
      this.error = null;
      try {
        this.graph = await getAccessMembershipGraph(limit);
      } catch (e) {
        this.error = normalizeApiError(e);
        this.graph = emptyGraph();
      } finally {
        this.loading = false;
      }
    },
  },
});
