import { defineStore } from 'pinia';

import {
  canQueryAgentScope,
  fetchAgentRunSummaries,
  fetchAgentRunActivities,
  fetchAgentRunRuntimeStatus,
  fetchAgentSessions,
} from '@/shared/api/staff-gateway';
import type {
  AgentActivityKind,
  AgentActivityStatus,
  AgentRunActivity,
  AgentRunRuntimeStatus,
  AgentRunStatus,
  AgentRunSummary,
  AgentSessionStatus,
  AgentSessionSummary,
} from '@/shared/api/generated';
import type { OperatorContext } from '@/shared/api/context';
import type { ApiError } from '@/shared/api/errors';
import { runHasProblem, runIsCompleted, runIsLive, runIsWaiting } from '@/features/executions/observability';

export type ExecutionFilters = {
  runStatus?: AgentRunStatus;
  sessionStatus?: AgentSessionStatus;
  activityKind?: AgentActivityKind;
  activityStatus?: AgentActivityStatus;
  pageSize: number;
};

export const useExecutionsStore = defineStore('executions', {
  state: () => ({
    runId: '',
    sessions: [] as AgentSessionSummary[],
    runs: [] as AgentRunSummary[],
    runtimeStatus: undefined as AgentRunRuntimeStatus | undefined,
    activities: [] as AgentRunActivity[],
    sessionNextPageToken: undefined as string | undefined,
    runNextPageToken: undefined as string | undefined,
    nextPageToken: undefined as string | undefined,
    filters: {
      pageSize: 25,
    } as ExecutionFilters,
    detailRequestToken: 0,
    isLoadingList: false,
    isLoading: false,
    unsupportedAgentScope: false,
    error: undefined as ApiError | undefined,
  }),
  getters: {
    activeRunCount: (state) => state.runs.filter((run) => runIsLive(run) || runIsWaiting(run)).length,
    runningRunCount: (state) => state.runs.filter(runIsLive).length,
    waitingRunCount: (state) => state.runs.filter(runIsWaiting).length,
    problemRunCount: (state) => state.runs.filter(runHasProblem).length,
    completedRunCount: (state) => state.runs.filter(runIsCompleted).length,
    waitingSessionCount: (state) =>
      state.sessions.filter((session) => session.status === 'waiting').length,
    humanGateRunCount: (state) =>
      state.runs.filter((run) => run.human_gate_waiting).length,
    latestRun: (state) => state.runs[0],
  },
  actions: {
    async loadOverview(context: OperatorContext) {
      if (!canQueryAgentScope(context)) {
        this.unsupportedAgentScope = true;
        this.sessions = [];
        this.runs = [];
        this.error = undefined;
        return;
      }
      this.unsupportedAgentScope = false;
      this.isLoadingList = true;
      this.error = undefined;
      try {
        const [sessions, runs] = await Promise.all([
          fetchAgentSessions(context, {
            status: this.filters.sessionStatus,
            pageSize: this.filters.pageSize,
          }),
          fetchAgentRunSummaries(context, {
            status: this.filters.runStatus,
            pageSize: this.filters.pageSize,
          }),
        ]);
        this.sessions = sessions.sessions;
        this.runs = runs.runs;
        this.sessionNextPageToken = sessions.page.next_page_token;
        this.runNextPageToken = runs.page.next_page_token;
      } catch (error) {
        this.error = error as ApiError;
      } finally {
        this.isLoadingList = false;
      }
    },
    async loadMoreRuns(context: OperatorContext) {
      if (!canQueryAgentScope(context) || !this.runNextPageToken || this.isLoadingList) {
        return;
      }
      this.isLoadingList = true;
      this.error = undefined;
      try {
        const runs = await fetchAgentRunSummaries(context, {
          status: this.filters.runStatus,
          pageSize: this.filters.pageSize,
          pageToken: this.runNextPageToken,
        });
        this.runs = [...this.runs, ...runs.runs];
        this.runNextPageToken = runs.page.next_page_token;
      } catch (error) {
        this.error = error as ApiError;
      } finally {
        this.isLoadingList = false;
      }
    },
    async selectRun(context: OperatorContext, runId: string) {
      this.runId = runId;
      await this.load(context);
    },
    async load(context: OperatorContext, pageToken?: string) {
      const runId = this.runId.trim();
      if (!runId) {
        return;
      }
      const requestToken = this.detailRequestToken + 1;
      this.detailRequestToken = requestToken;
      const isInitialLoad = pageToken === undefined;
      if (isInitialLoad) {
        this.runtimeStatus = undefined;
        this.activities = [];
        this.nextPageToken = undefined;
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
        if (this.detailRequestToken !== requestToken || this.runId.trim() !== runId) {
          return;
        }
        this.runtimeStatus = runtime.runtime_status;
        this.activities = pageToken ? [...this.activities, ...activities.activities] : activities.activities;
        this.nextPageToken = activities.page.next_page_token;
      } catch (error) {
        if (this.detailRequestToken === requestToken && this.runId.trim() === runId) {
          this.error = error as ApiError;
        }
      } finally {
        if (this.detailRequestToken === requestToken && this.runId.trim() === runId) {
          this.isLoading = false;
        }
      }
    },
  },
});
