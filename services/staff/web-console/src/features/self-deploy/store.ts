import { defineStore } from 'pinia';

import { canQueryAgentScope, fetchSelfDeploySummary } from '@/shared/api/staff-gateway';
import type { OperatorContext } from '@/shared/api/context';
import type { ApiError } from '@/shared/api/errors';
import type { SelfDeploySummary } from '@/shared/api/generated';

export const useSelfDeployStore = defineStore('self-deploy', {
  state: () => ({
    summary: undefined as SelfDeploySummary | undefined,
    isLoading: false,
    unsupportedAgentScope: false,
    error: undefined as ApiError | undefined,
  }),
  getters: {
    isReady: (state) => state.summary?.availability === 'ready',
    isUnavailable: (state) => state.summary?.availability === 'unavailable',
  },
  actions: {
    async load(context: OperatorContext) {
      if (!canQueryAgentScope(context)) {
        this.summary = undefined;
        this.unsupportedAgentScope = true;
        this.error = undefined;
        return;
      }
      this.unsupportedAgentScope = false;
      this.isLoading = true;
      this.error = undefined;
      try {
        const response = await fetchSelfDeploySummary(context);
        this.summary = response.summary;
      } catch (error) {
        this.error = error as ApiError;
      } finally {
        this.isLoading = false;
      }
    },
  },
});
