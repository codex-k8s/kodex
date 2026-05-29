import { defineStore } from 'pinia';

import {
  fetchAgentRunActivities,
  fetchAgentRunRuntimeStatus,
} from '@/shared/api/staff-gateway';
import type {
  AgentActivityKind,
  AgentActivityStatus,
  AgentRunActivity,
  AgentRunRuntimeStatus,
} from '@/shared/api/generated';
import type { OperatorContext } from '@/shared/api/context';
import type { ApiError } from '@/shared/api/errors';

export type ExecutionFilters = {
  activityKind?: AgentActivityKind;
  activityStatus?: AgentActivityStatus;
  pageSize: number;
};

export const useExecutionsStore = defineStore('executions', {
  state: () => ({
    runId: '',
    runtimeStatus: undefined as AgentRunRuntimeStatus | undefined,
    activities: [] as AgentRunActivity[],
    nextPageToken: undefined as string | undefined,
    filters: {
      pageSize: 25,
    } as ExecutionFilters,
    isLoading: false,
    error: undefined as ApiError | undefined,
  }),
  actions: {
    async load(context: OperatorContext, pageToken?: string) {
      const runId = this.runId.trim();
      if (!runId) {
        return;
      }
      this.isLoading = true;
      this.error = undefined;
      try {
        const [runtime, activities] = await Promise.all([
          fetchAgentRunRuntimeStatus(context, runId),
          fetchAgentRunActivities(context, runId, {
            activityKind: this.filters.activityKind,
            status: this.filters.activityStatus,
            pageSize: this.filters.pageSize,
            pageToken,
          }),
        ]);
        this.runtimeStatus = runtime.runtime_status;
        this.activities = activities.activities;
        this.nextPageToken = activities.page.next_page_token;
      } catch (error) {
        this.error = error as ApiError;
      } finally {
        this.isLoading = false;
      }
    },
  },
});
