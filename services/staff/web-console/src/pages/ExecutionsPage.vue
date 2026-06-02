<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';

import type { AgentActivityKind, AgentActivityStatus, AgentRunStatus, AgentSessionStatus } from '@/shared/api/generated';
import { useExecutionsStore } from '@/features/executions/store';
import { useOperatorContextStore } from '@/features/operator-context/store';
import { compactRef, formatDateTime, formatDurationMs, prettySafeJSON } from '@/shared/lib/format';
import ApiErrorAlert from '@/shared/ui/ApiErrorAlert.vue';
import EmptyState from '@/shared/ui/EmptyState.vue';
import SurfaceStateCard from '@/shared/ui/SurfaceStateCard.vue';
import StatusChip from '@/shared/ui/StatusChip.vue';

const { t } = useI18n();
const context = useOperatorContextStore();
const executions = useExecutionsStore();

const activityKindOptions: AgentActivityKind[] = [
  'lifecycle',
  'tool_use',
  'tool_result',
  'permission',
  'provider_signal',
  'runtime_signal',
  'checkpoint',
  'other',
];
const activityStatusOptions: AgentActivityStatus[] = [
  'planned',
  'started',
  'succeeded',
  'failed',
  'denied',
  'waiting',
  'cancelled',
  'skipped',
];
const runStatusOptions: AgentRunStatus[] = [
  'requested',
  'starting',
  'running',
  'waiting',
  'completed',
  'failed',
  'cancelled',
];
const sessionStatusOptions: AgentSessionStatus[] = ['open', 'waiting', 'completed', 'failed', 'cancelled'];

const runtimeStatus = computed(() => executions.runtimeStatus);
const selectedRun = computed(() => executions.runs.find((run) => run.run_id === executions.runId.trim()));
const canLoad = computed(
  () => context.isReady && executions.runId.trim().length > 0 && !executions.isLoading,
);
const canLoadOverview = computed(() => context.isReady && !executions.isLoadingList);

onMounted(() => {
  if (context.isReady && executions.runs.length === 0 && !executions.unsupportedAgentScope) {
    void executions.loadOverview(context.asContext);
  }
});

function statusTone(status?: string): 'neutral' | 'success' | 'warning' | 'error' | 'info' {
  if (status === 'succeeded' || status === 'completed') {
    return 'success';
  }
  if (status === 'running' || status === 'started' || status === 'waiting' || status === 'pending') {
    return 'warning';
  }
  if (status === 'failed' || status === 'cancelled' || status === 'timed_out') {
    return 'error';
  }
  if (status === 'requested' || status === 'starting' || status === 'claimed') {
    return 'info';
  }
  return 'neutral';
}

function loadRun(pageToken?: string) {
  if (!context.isReady || executions.isLoading || executions.runId.trim().length === 0) {
    return;
  }
  void executions.load(context.asContext, pageToken);
}

function loadOverview() {
  if (!context.isReady || executions.isLoadingList) {
    return;
  }
  void executions.loadOverview(context.asContext);
}

function loadMoreRuns() {
  if (!context.isReady || executions.isLoadingList || !executions.runNextPageToken) {
    return;
  }
  void executions.loadMoreRuns(context.asContext);
}

function selectRun(runId: string) {
  if (!context.isReady || executions.isLoading) {
    return;
  }
  void executions.selectRun(context.asContext, runId);
}
</script>

<template>
  <div class="page-grid">
    <header class="page-header">
      <div>
        <h1>{{ t('executions.title') }}</h1>
        <p>{{ t('executions.description') }}</p>
      </div>
      <v-btn
        color="primary"
        prepend-icon="mdi-refresh"
        :disabled="!canLoadOverview"
        :loading="executions.isLoadingList"
        @click="loadOverview"
      >
        {{ t('app.refresh') }}
      </v-btn>
    </header>

    <v-alert v-if="!context.isReady" type="warning" variant="tonal">
      {{ t('context.missing') }}
    </v-alert>
    <v-alert v-if="executions.unsupportedAgentScope" type="warning" variant="tonal">
      {{ t('executions.unsupportedScope') }}
    </v-alert>
    <ApiErrorAlert :error="executions.error" :retry-label="t('app.retry')" @retry="loadOverview" />

    <v-card class="surface-panel pa-4">
      <SurfaceStateCard
        icon="mdi-format-list-bulleted-square"
        :title="t('executions.listTitle')"
        :text="t('executions.listText')"
        :status="t('app.live')"
        tone="live"
      />
    </v-card>

    <section class="summary-grid">
      <v-card class="surface-panel summary-card">
        <div class="meta-text">{{ t('executions.sessions') }}</div>
        <div class="summary-value">{{ executions.sessions.length }}</div>
      </v-card>
      <v-card class="surface-panel summary-card">
        <div class="meta-text">{{ t('executions.activeRuns') }}</div>
        <div class="summary-value">{{ executions.activeRunCount }}</div>
      </v-card>
      <v-card class="surface-panel summary-card">
        <div class="meta-text">{{ t('executions.latestRun') }}</div>
        <StatusChip
          :label="t(`statuses.${executions.latestRun?.status ?? 'unspecified'}`)"
          :tone="statusTone(executions.latestRun?.status)"
        />
      </v-card>
      <v-card class="surface-panel summary-card">
        <div class="meta-text">{{ t('executions.humanGate') }}</div>
        <div class="summary-value">{{ executions.humanGateRunCount }}</div>
      </v-card>
    </section>

    <v-card class="surface-panel pa-4">
      <div class="section-title mb-3">{{ t('executions.listFilters') }}</div>
      <div class="filter-row">
        <v-select
          v-model="executions.filters.sessionStatus"
          :items="sessionStatusOptions"
          clearable
          :label="t('executions.sessionStatus')"
        >
          <template #item="{ props, item }">
            <v-list-item v-bind="props" :title="t(`statuses.${item.value}`)" />
          </template>
          <template #selection="{ item }">{{ t(`statuses.${item.value}`) }}</template>
        </v-select>
        <v-select
          v-model="executions.filters.runStatus"
          :items="runStatusOptions"
          clearable
          :label="t('executions.runStatus')"
        >
          <template #item="{ props, item }">
            <v-list-item v-bind="props" :title="t(`statuses.${item.value}`)" />
          </template>
          <template #selection="{ item }">{{ t(`statuses.${item.value}`) }}</template>
        </v-select>
        <v-btn
          color="primary"
          prepend-icon="mdi-format-list-bulleted"
          :disabled="!canLoadOverview"
          :loading="executions.isLoadingList"
          @click="loadOverview"
        >
          {{ t('executions.loadLists') }}
        </v-btn>
      </div>
    </v-card>

    <section class="list-layout">
      <v-card class="surface-panel pa-5">
        <div class="section-title">{{ t('executions.sessions') }}</div>
        <v-progress-linear v-if="executions.isLoadingList" class="mt-4" indeterminate color="primary" />
        <div v-if="executions.sessions.length > 0" class="summary-list">
          <article v-for="session in executions.sessions" :key="session.session_id" class="summary-list__item">
            <div>
              <div class="item-title">{{ compactRef(session.session_id) }}</div>
              <div class="meta-text">{{ session.latest_run_safe_summary ?? compactRef(session.provider_work_item_ref) }}</div>
              <div class="meta-text">{{ formatDateTime(session.updated_at) }}</div>
            </div>
            <div class="summary-list__actions">
              <StatusChip :label="t(`statuses.${session.status}`)" :tone="statusTone(session.status)" />
              <v-chip v-if="session.human_gate_waiting" size="small" color="warning" variant="tonal" label>
                {{ t('executions.humanGate') }}
              </v-chip>
            </div>
          </article>
        </div>
        <EmptyState v-else icon="mdi-account-clock-outline" :title="t('executions.noSessions')" />
      </v-card>

      <v-card class="surface-panel pa-5">
        <div class="section-title">{{ t('executions.runs') }}</div>
        <v-progress-linear v-if="executions.isLoadingList" class="mt-4" indeterminate color="primary" />
        <div v-if="executions.runs.length > 0" class="summary-list">
          <button
            v-for="run in executions.runs"
            :key="run.run_id"
            class="summary-list__item summary-list__button"
            :class="{ 'summary-list__button--selected': run.run_id === executions.runId }"
            type="button"
            @click="selectRun(run.run_id)"
          >
            <div>
              <div class="item-title">{{ compactRef(run.run_id) }}</div>
              <div class="meta-text">{{ run.result_summary ?? run.runtime_safe_summary ?? compactRef(run.runtime_job_ref) }}</div>
              <div class="meta-text">{{ formatDateTime(run.updated_at) }}</div>
            </div>
            <div class="summary-list__actions">
              <StatusChip :label="t(`statuses.${run.status}`)" :tone="statusTone(run.status)" />
              <v-chip v-if="run.human_gate_waiting" size="small" color="warning" variant="tonal" label>
                {{ t('executions.humanGate') }}
              </v-chip>
            </div>
          </button>
        </div>
        <EmptyState v-else icon="mdi-clipboard-text-clock-outline" :title="t('executions.noRuns')" />
        <div class="list-footer">
          <v-btn variant="tonal" :disabled="!executions.runNextPageToken" @click="loadMoreRuns">
            {{ t('inbox.nextPage') }}
          </v-btn>
        </div>
      </v-card>
    </section>

    <v-card class="surface-panel pa-4">
      <div class="section-title mb-3">{{ t('executions.timelineFilters') }}</div>
      <div class="filter-row">
        <v-text-field
          v-model.trim="executions.runId"
          :label="t('executions.runId')"
          placeholder="00000000-0000-0000-0000-000000000000"
        />
        <v-select
          v-model="executions.filters.activityKind"
          :items="activityKindOptions"
          clearable
          :label="t('executions.activityKind')"
        >
          <template #item="{ props, item }">
            <v-list-item v-bind="props" :title="t(`statuses.${item.value}`)" />
          </template>
          <template #selection="{ item }">{{ t(`statuses.${item.value}`) }}</template>
        </v-select>
        <v-select
          v-model="executions.filters.activityStatus"
          :items="activityStatusOptions"
          clearable
          :label="t('executions.activityStatus')"
        >
          <template #item="{ props, item }">
            <v-list-item v-bind="props" :title="t(`statuses.${item.value}`)" />
          </template>
          <template #selection="{ item }">{{ t(`statuses.${item.value}`) }}</template>
        </v-select>
        <v-btn
          color="primary"
          prepend-icon="mdi-play-circle-outline"
          :disabled="!canLoad"
          :loading="executions.isLoading"
          @click="loadRun()"
        >
          {{ t('executions.loadRun') }}
        </v-btn>
      </div>
    </v-card>

    <section class="execution-layout">
      <v-card class="surface-panel pa-5">
        <div class="section-title">{{ t('executions.runtimeSummary') }}</div>
        <template v-if="runtimeStatus">
          <div class="detail-grid mt-4">
            <div>
              <div class="meta-text">Run</div>
              <div>{{ compactRef(runtimeStatus.run_id) }}</div>
            </div>
            <div v-if="selectedRun">
              <div class="meta-text">{{ t('executions.role') }}</div>
              <div>{{ compactRef(selectedRun.role_profile_id) }}</div>
            </div>
            <div>
              <div class="meta-text">Version</div>
              <div>{{ runtimeStatus.run_version }}</div>
            </div>
            <div>
              <div class="meta-text">{{ t('executions.job') }}</div>
              <div>{{ compactRef(runtimeStatus.runtime_job_ref) }}</div>
            </div>
            <div>
              <div class="meta-text">Updated</div>
              <div>{{ formatDateTime(runtimeStatus.run_updated_at) }}</div>
            </div>
            <div>
              <div class="meta-text">{{ t('executions.followUp') }}</div>
              <StatusChip
                :label="runtimeStatus.follow_up_waiting ? t('statuses.waiting') : t('statuses.unspecified')"
                :tone="runtimeStatus.follow_up_waiting ? 'warning' : 'neutral'"
              />
            </div>
          </div>
          <div class="detail-section">
            <div class="section-title">{{ t('executions.runtimeRefs') }}</div>
            <div class="ref-chip-row">
              <v-chip v-if="runtimeStatus.runtime_slot_ref" size="small" variant="tonal" color="info" label>
                slot / {{ compactRef(runtimeStatus.runtime_slot_ref) }}
              </v-chip>
              <v-chip v-if="runtimeStatus.runtime_context_ref" size="small" variant="tonal" color="info" label>
                context / {{ compactRef(runtimeStatus.runtime_context_ref) }}
              </v-chip>
              <v-chip v-if="runtimeStatus.runtime_job_command_ref" size="small" variant="tonal" color="info" label>
                command / {{ compactRef(runtimeStatus.runtime_job_command_ref) }}
              </v-chip>
              <v-chip v-if="runtimeStatus.human_gate_request_ref" size="small" variant="tonal" color="warning" label>
                Human gate / {{ compactRef(runtimeStatus.human_gate_request_ref) }}
              </v-chip>
            </div>
          </div>
          <v-alert v-if="runtimeStatus.safe_error_code" class="mt-4" type="error" variant="tonal">
            {{ runtimeStatus.safe_error_code }}
          </v-alert>
          <p v-if="runtimeStatus.safe_summary" class="safe-summary">{{ runtimeStatus.safe_summary }}</p>
        </template>
        <EmptyState v-else icon="mdi-timeline-clock-outline" :title="t('executions.noRun')" />
      </v-card>

      <v-card class="surface-panel pa-5">
        <div class="section-title">{{ t('executions.activityTimeline') }}</div>
        <div v-if="executions.activities.length > 0" class="timeline-list">
          <article v-for="activity in executions.activities" :key="activity.activity_id" class="timeline-item">
            <div class="timeline-dot" />
            <div class="timeline-item__body">
              <div class="timeline-item__header">
                <div>
                  <div class="item-title">
                    {{ t(`statuses.${activity.activity_kind}`) }}
                    <span v-if="activity.tool_name">· {{ activity.tool_name }}</span>
                  </div>
                  <div class="meta-text">
                    {{ formatDateTime(activity.started_at ?? activity.created_at) }} ·
                    {{ formatDurationMs(activity.duration_ms) }}
                  </div>
                  <div class="meta-text">
                    {{ t('executions.activityDigest') }}: {{ compactRef(activity.payload_digest) }}
                  </div>
                </div>
                <StatusChip :label="t(`statuses.${activity.status}`)" :tone="statusTone(activity.status)" />
              </div>
              <p v-if="activity.safe_summary" class="safe-summary">{{ activity.safe_summary }}</p>
              <p v-if="activity.bounded_error" class="bounded-error">{{ activity.bounded_error }}</p>
              <pre v-if="activity.safe_refs_json" class="safe-json">{{ prettySafeJSON(activity.safe_refs_json) }}</pre>
              <pre v-if="activity.safe_details_json" class="safe-json">{{ prettySafeJSON(activity.safe_details_json) }}</pre>
            </div>
          </article>
        </div>
        <EmptyState v-else icon="mdi-history" :title="t('executions.noActivities')" />
        <p v-if="executions.activities.length === 0" class="empty-hint">{{ t('executions.noActivitiesText') }}</p>
        <div class="list-footer">
          <v-btn
            variant="tonal"
            :disabled="!executions.nextPageToken"
            @click="loadRun(executions.nextPageToken)"
          >
            {{ t('inbox.nextPage') }}
          </v-btn>
        </div>
      </v-card>
    </section>
  </div>
</template>

<style scoped>
.page-header {
  align-items: flex-start;
  display: flex;
  gap: 16px;
  justify-content: space-between;
}

.page-header h1 {
  color: #121826;
  font-size: 1.8rem;
  line-height: 1.2;
  margin: 0;
}

.page-header p {
  color: #667085;
  margin: 8px 0 0;
}

.summary-grid {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.summary-card {
  display: grid;
  gap: 10px;
  min-height: 104px;
  padding: 18px;
}

.summary-value {
  color: #121826;
  font-weight: 700;
}

.filter-row {
  align-items: center;
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
}

.list-layout {
  display: grid;
  gap: 16px;
  grid-template-columns: minmax(320px, 0.9fr) minmax(0, 1.1fr);
}

.summary-list {
  display: grid;
  gap: 10px;
  margin-top: 16px;
}

.summary-list__item {
  align-items: flex-start;
  background: #ffffff;
  border: 1px solid #e4e7ec;
  border-radius: 8px;
  display: flex;
  gap: 12px;
  justify-content: space-between;
  padding: 12px;
  text-align: left;
  width: 100%;
}

.summary-list__button {
  cursor: pointer;
}

.summary-list__button--selected {
  border-color: #ff5a14;
  box-shadow: 0 0 0 2px rgb(255 90 20 / 12%);
}

.summary-list__actions {
  align-items: flex-end;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.execution-layout {
  display: grid;
  gap: 16px;
  grid-template-columns: minmax(320px, 0.8fr) minmax(0, 1.2fr);
}

.detail-grid {
  display: grid;
  gap: 14px;
  grid-template-columns: 1fr 1fr;
}

.detail-section {
  display: grid;
  gap: 10px;
  margin-top: 18px;
}

.ref-chip-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.safe-summary {
  color: #475467;
  line-height: 1.55;
  margin: 12px 0 0;
}

.bounded-error {
  border-left: 3px solid #dc2626;
  color: #b42318;
  margin: 12px 0;
  padding-left: 10px;
}

.timeline-list {
  display: grid;
  gap: 12px;
  margin-top: 18px;
}

.timeline-item {
  display: grid;
  gap: 12px;
  grid-template-columns: 16px minmax(0, 1fr);
}

.timeline-dot {
  background: #ff5a14;
  border-radius: 999px;
  height: 10px;
  margin-top: 12px;
  width: 10px;
}

.timeline-item__body {
  border: 1px solid #e4e7ec;
  border-radius: 8px;
  padding: 12px;
}

.timeline-item__header {
  align-items: flex-start;
  display: flex;
  gap: 12px;
  justify-content: space-between;
}

.item-title {
  color: #121826;
  font-weight: 700;
}

.list-footer {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

.empty-hint {
  color: #667085;
  font-size: 0.875rem;
  margin: 12px 0 0;
}

@media (max-width: 1180px) {
  .summary-grid,
  .list-layout,
  .filter-row,
  .execution-layout {
    grid-template-columns: 1fr;
  }

  .detail-grid {
    grid-template-columns: 1fr;
  }
}
</style>
