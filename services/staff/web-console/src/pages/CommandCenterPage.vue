<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import { useRouter } from 'vue-router';

import ApiErrorAlert from '@/shared/ui/ApiErrorAlert.vue';
import EmptyState from '@/shared/ui/EmptyState.vue';
import SurfaceStateCard from '@/shared/ui/SurfaceStateCard.vue';
import StatusChip from '@/shared/ui/StatusChip.vue';
import {
  runHasProblem,
  runPrimarySummary,
  runProblemCode,
  runWaitingCode,
  statusTone,
} from '@/features/executions/observability';
import { useExecutionsStore } from '@/features/executions/store';
import { useOperatorContextStore } from '@/features/operator-context/store';
import { useOwnerInboxStore } from '@/features/owner-inbox/store';
import { useSelfDeployStore } from '@/features/self-deploy/store';
import { compactRef } from '@/shared/lib/format';
import { routeNames } from '@/shared/lib/routes';

const { t } = useI18n();
const router = useRouter();
const context = useOperatorContextStore();
const inbox = useOwnerInboxStore();
const executions = useExecutionsStore();
const selfDeploy = useSelfDeployStore();

const visibleRuns = computed(() => {
  const attentionRuns = executions.runs.filter((run) => runHasProblem(run) || runWaitingCode(run));
  return (attentionRuns.length > 0 ? attentionRuns : executions.runs).slice(0, 5);
});

const selfDeployFields = computed(() => {
  const summary = selfDeploy.summary;
  return [
    {
      label: t('commandCenter.selfDeploy.githubSignal'),
      value: providerSignalValue(summary),
    },
    {
      label: t('commandCenter.selfDeploy.repository'),
      value: refsValue(summary?.project_ref, summary?.repository_ref),
    },
    {
      label: t('commandCenter.selfDeploy.branchCommit'),
      value: refsValue(summary?.source_ref, summary?.merge_commit_sha ? compactRef(summary.merge_commit_sha) : undefined),
    },
    {
      label: t('commandCenter.selfDeploy.changedServices'),
      value: affectedServicesValue(summary),
    },
    {
      label: t('commandCenter.selfDeploy.servicesFingerprint'),
      value: refsValue(summary?.services_yaml_digest, summary?.plan_fingerprint),
    },
    {
      label: t('commandCenter.selfDeploy.deployPlan'),
      value: summary ? planStatusLabel(summary.deploy_plan.status) : t('app.unavailable'),
    },
    {
      label: t('commandCenter.selfDeploy.ownerDecision'),
      value: governanceValue(summary),
    },
    {
      label: t('commandCenter.selfDeploy.runtimeJobs'),
      value: runtimeValue(summary),
    },
  ];
});

const selfDeployChip = computed(() => {
  if (selfDeploy.unsupportedAgentScope) {
    return { color: 'default', label: t('commandCenter.selfDeploy.unsupportedScope') };
  }
  if (selfDeploy.isLoading && !selfDeploy.summary) {
    return { color: 'info', label: t('app.loading') };
  }
  const status = selfDeploy.summary?.deploy_plan.status;
  if (status === 'approved') {
    return { color: 'success', label: planStatusLabel(status) };
  }
  if (status === 'pending_approval') {
    return { color: 'warning', label: planStatusLabel(status) };
  }
  if (status === 'failed' || status === 'rejected' || status === 'cancelled') {
    return { color: 'error', label: planStatusLabel(status) };
  }
  if (selfDeploy.summary?.availability === 'ready') {
    return { color: 'info', label: t('app.live') };
  }
  return { color: 'default', label: t('app.unavailable') };
});

const selfDeployReadiness = computed(() =>
  selfDeploy.unsupportedAgentScope
    ? { status: t('app.unavailable'), tone: 'waiting' as const }
    : { status: t('app.live'), tone: 'live' as const },
);

onMounted(() => {
  if (context.isReady && inbox.items.length === 0) {
    void inbox.load(context.asContext);
  }
  if (context.isReady && executions.runs.length === 0) {
    void executions.loadOverview(context.asContext);
  }
  if (context.isReady && !selfDeploy.summary) {
    void selfDeploy.load(context.asContext);
  }
});

function reloadInbox() {
  if (context.isReady) {
    void inbox.load(context.asContext);
    void executions.loadOverview(context.asContext);
    void selfDeploy.load(context.asContext);
  }
}

function openOwnerInbox() {
  void router.push({ name: routeNames.ownerInbox });
}

function openExecutions() {
  void router.push({ name: routeNames.executions });
}

function openRun(runId: string) {
  executions.runId = runId;
  void router.push({ name: routeNames.executions });
  if (context.isReady) {
    void executions.selectRun(context.asContext, runId);
  }
}

function reloadSelfDeploy() {
  if (context.isReady) {
    void selfDeploy.load(context.asContext);
  }
}

function providerSignalValue(summary = selfDeploy.summary) {
  if (!summary) {
    return t('app.unavailable');
  }
  const status = providerSignalStatusLabel(summary.provider_signal.status);
  return summary.provider_signal.ref ? `${status}: ${compactRef(summary.provider_signal.ref)}` : status;
}

function affectedServicesValue(summary = selfDeploy.summary) {
  if (!summary) {
    return t('app.unavailable');
  }
  if (summary.affected_service_keys.length > 0) {
    return summary.affected_service_keys.join(', ');
  }
  if (summary.path_categories.length > 0) {
    return summary.path_categories.map((category) => pathCategoryLabel(category)).join(', ');
  }
  return t('app.unavailable');
}

function governanceValue(summary = selfDeploy.summary) {
  if (!summary) {
    return t('app.unavailable');
  }
  const refs = [
    summary.governance.gate_request_ref,
    summary.governance.gate_decision_ref,
    summary.governance.release_decision_package_ref,
    summary.governance.release_decision_ref,
  ]
    .filter(Boolean)
    .map((ref) => compactRef(ref as string));
  const status = governanceStatusLabel(summary.governance.status);
  return refs.length > 0 ? `${status}: ${refs.join(' / ')}` : status;
}

function runtimeValue(summary = selfDeploy.summary) {
  if (!summary) {
    return t('app.unavailable');
  }
  const jobs = summary.expected_runtime_job_types.length > 0
    ? summary.expected_runtime_job_types.join(', ')
    : t('app.unavailable');
  return `${runtimeStatusLabel(summary.runtime.status)} · ${jobs}`;
}

function refsValue(...values: Array<string | undefined>) {
  const refs = values.filter(Boolean).map((value) => value as string);
  return refs.length > 0 ? refs.join(' / ') : t('app.unavailable');
}

function planStatusLabel(status: string) {
  return t(`commandCenter.selfDeploy.planStatuses.${status}`);
}

function providerSignalStatusLabel(status: string) {
  return t(`commandCenter.selfDeploy.providerSignalStatuses.${status}`);
}

function governanceStatusLabel(status: string) {
  return t(`commandCenter.selfDeploy.governanceStatuses.${status}`);
}

function runtimeStatusLabel(status: string) {
  return t(`commandCenter.selfDeploy.runtimeStatuses.${status}`);
}

function pathCategoryLabel(category: string) {
  return t(`commandCenter.selfDeploy.pathCategories.${category}`);
}
</script>

<template>
  <div class="page-grid command-center">
    <section class="summary-grid">
      <v-card class="surface-panel summary-card">
        <v-icon icon="mdi-alert-circle-outline" color="error" size="34" />
        <div>
          <div class="meta-text">{{ t('commandCenter.decisionsOnPage') }}</div>
          <div class="summary-card__value">{{ inbox.pendingCount }}</div>
          <div class="summary-card__hint">{{ t('commandCenter.currentInboxPageHint') }}</div>
        </div>
      </v-card>
      <v-card class="surface-panel summary-card">
        <v-icon icon="mdi-account-clock-outline" color="info" size="34" />
        <div>
          <div class="meta-text">{{ t('commandCenter.sessionsOnPage') }}</div>
          <div class="summary-card__value">{{ executions.sessions.length }}</div>
          <div class="summary-card__hint">{{ t('commandCenter.currentAgentPageHint') }}</div>
        </div>
      </v-card>
      <v-card class="surface-panel summary-card">
        <v-icon icon="mdi-play-circle-outline" color="success" size="34" />
        <div>
          <div class="meta-text">{{ t('commandCenter.runningRunsOnPage') }}</div>
          <div class="summary-card__value">{{ executions.runningRunCount }}</div>
          <div class="summary-card__hint">{{ t('commandCenter.currentAgentPageHint') }}</div>
        </div>
      </v-card>
      <v-card class="surface-panel summary-card">
        <v-icon icon="mdi-clock-outline" color="warning" size="34" />
        <div>
          <div class="meta-text">{{ t('commandCenter.waitingRunsOnPage') }}</div>
          <div class="summary-card__value">{{ executions.waitingRunCount }}</div>
          <div class="summary-card__hint">{{ t('commandCenter.currentAgentPageHint') }}</div>
        </div>
      </v-card>
      <v-card class="surface-panel summary-card">
        <v-icon icon="mdi-alert-octagon-outline" color="error" size="34" />
        <div>
          <div class="meta-text">{{ t('commandCenter.problemRunsOnPage') }}</div>
          <div class="summary-card__value summary-card__value--danger">{{ executions.problemRunCount }}</div>
          <div class="summary-card__hint">{{ t('commandCenter.currentAgentPageHint') }}</div>
        </div>
      </v-card>
    </section>

    <ApiErrorAlert :error="inbox.error" :retry-label="t('app.retry')" @retry="reloadInbox" />
    <ApiErrorAlert :error="selfDeploy.error" :retry-label="t('app.retry')" @retry="reloadSelfDeploy" />

    <section class="surface-readiness">
      <v-card class="surface-panel readiness-panel">
        <div class="section-title">{{ t('commandCenter.liveSurfaces') }}</div>
        <div class="readiness-grid">
          <SurfaceStateCard
            icon="mdi-inbox-outline"
            :title="t('commandCenter.ownerInboxLive')"
            :text="t('commandCenter.ownerInboxLiveText')"
            :status="t('app.live')"
            tone="live"
          />
          <SurfaceStateCard
            icon="mdi-timeline-clock-outline"
            :title="t('commandCenter.runListsLive')"
            :text="t('commandCenter.runListsLiveText')"
            :status="t('app.live')"
            tone="live"
          />
          <SurfaceStateCard
            icon="mdi-source-branch"
            :title="t('commandCenter.selfDeploy.readinessTitle')"
            :text="t('commandCenter.selfDeploy.readinessText')"
            :status="selfDeployReadiness.status"
            :tone="selfDeployReadiness.tone"
          />
        </div>
      </v-card>
      <v-card class="surface-panel readiness-panel">
        <div class="section-title">{{ t('commandCenter.waitingSurfaces') }}</div>
        <div class="readiness-grid">
          <SurfaceStateCard
            icon="mdi-view-dashboard-edit-outline"
            :title="t('commandCenter.aggregateWaiting')"
            :text="t('commandCenter.aggregateWaitingText')"
            :status="t('app.waitingEndpoint')"
            tone="waiting"
          />
          <SurfaceStateCard
            icon="mdi-message-processing-outline"
            :title="t('commandCenter.chatWaiting')"
            :text="t('commandCenter.chatWaitingText')"
            :status="t('app.disabled')"
            tone="waiting"
          />
        </div>
      </v-card>
    </section>

    <section>
      <v-card class="surface-panel self-deploy-panel">
        <div class="section-header">
          <div>
            <div class="section-title">{{ t('commandCenter.selfDeploy.title') }}</div>
            <p class="self-deploy-panel__subtitle">{{ t('commandCenter.selfDeploy.subtitle') }}</p>
          </div>
          <v-chip :color="selfDeployChip.color" variant="tonal" label>{{ selfDeployChip.label }}</v-chip>
        </div>
        <div class="self-deploy-panel__body">
          <div class="self-deploy-panel__fields">
            <div v-for="field in selfDeployFields" :key="field.label" class="self-deploy-field">
              <span>{{ field.label }}</span>
              <strong>{{ field.value }}</strong>
            </div>
          </div>
          <v-alert class="self-deploy-panel__notice" type="info" variant="tonal">
            {{ t('commandCenter.selfDeploy.safeBoundary') }}
          </v-alert>
        </div>
        <v-alert v-if="selfDeploy.unsupportedAgentScope" type="warning" variant="tonal">
          {{ t('commandCenter.selfDeploy.unsupportedScopeText') }}
        </v-alert>
        <v-alert v-else-if="selfDeploy.summary?.safe_error" type="warning" variant="tonal">
          {{ selfDeploy.summary.safe_error.summary }}
        </v-alert>
        <v-progress-linear v-if="selfDeploy.isLoading" indeterminate color="primary" />
        <div class="self-deploy-panel__actions">
          <v-btn
            prepend-icon="mdi-refresh"
            variant="tonal"
            :loading="selfDeploy.isLoading"
            :disabled="!context.isReady"
            @click="reloadSelfDeploy"
          >
            {{ t('commandCenter.selfDeploy.refreshStatus') }}
          </v-btn>
          <v-btn prepend-icon="mdi-inbox-arrow-down-outline" variant="tonal" @click="openOwnerInbox">
            {{ t('commandCenter.selfDeploy.openDecisions') }}
          </v-btn>
          <v-btn prepend-icon="mdi-pulse" variant="tonal" @click="openExecutions">
            {{ t('commandCenter.selfDeploy.openExecutions') }}
          </v-btn>
        </div>
      </v-card>
    </section>

    <section class="main-grid">
      <v-card class="surface-panel dialogue-panel">
        <div class="section-title">{{ t('commandCenter.dialogueTitle') }}</div>
        <EmptyState
          class="mt-4"
          icon="mdi-message-processing-outline"
          :title="t('commandCenter.dialogueUnavailable')"
          :text="t('commandCenter.noAggregate')"
        />
        <v-alert type="info" variant="tonal">
          {{ t('commandCenter.inputDisabledHint') }}
        </v-alert>
        <div class="quick-actions">
          <v-btn prepend-icon="mdi-play" variant="tonal" disabled>{{ t('commandCenter.startFlow') }}</v-btn>
          <v-btn prepend-icon="mdi-plus" variant="tonal" disabled>{{ t('commandCenter.createIssue') }}</v-btn>
          <v-btn prepend-icon="mdi-account-check-outline" variant="tonal" disabled>
            {{ t('commandCenter.requestReview') }}
          </v-btn>
          <v-btn prepend-icon="mdi-shield-check-outline" variant="tonal" disabled>
            {{ t('commandCenter.requestApproval') }}
          </v-btn>
        </div>
        <div class="chat-input">
          <input :placeholder="t('commandCenter.inputPlaceholder')" disabled />
          <v-btn :aria-label="t('app.microphone')" icon="mdi-microphone-outline" variant="text" disabled />
          <v-btn :aria-label="t('app.send')" icon="mdi-send" color="primary" disabled />
        </div>
      </v-card>

      <div class="side-stack">
        <v-card class="surface-panel pa-5">
          <div class="section-title">{{ t('commandCenter.activeWork') }}</div>
          <v-btn class="mt-4" prepend-icon="mdi-pulse" variant="tonal" @click="openExecutions">
            {{ t('commandCenter.runListsLive') }}
          </v-btn>
          <v-progress-linear v-if="executions.isLoadingList" class="mt-4" indeterminate color="primary" />
          <div v-if="visibleRuns.length > 0" class="compact-list">
            <button
              v-for="run in visibleRuns"
              :key="run.run_id"
              class="compact-list__item"
              type="button"
              @click="openRun(run.run_id)"
            >
              <span>
                <strong>{{ compactRef(run.run_id) }}</strong>
                <small>{{ runPrimarySummary(run) ?? t('executions.noRunSummary') }}</small>
                <small v-if="runWaitingCode(run)" class="attention-text">
                  {{ t('executions.waitingReason') }}: {{ runWaitingCode(run) }}
                </small>
                <small v-if="runProblemCode(run)" class="error-text">
                  {{ t('executions.safeError') }}: {{ runProblemCode(run) }}
                </small>
              </span>
              <StatusChip :label="t(`statuses.${run.status}`)" :tone="statusTone(run.status)" />
            </button>
          </div>
          <EmptyState v-else class="mt-4" icon="mdi-clipboard-text-clock-outline" :title="t('executions.noRuns')" />
        </v-card>
        <v-card class="surface-panel pa-5">
          <div class="section-header">
            <div class="section-title">{{ t('commandCenter.latestDecisions') }}</div>
            <v-btn size="small" variant="text" @click="openOwnerInbox">{{ t('app.view') }}</v-btn>
          </div>
          <v-progress-linear v-if="inbox.isLoadingList" class="mt-4" indeterminate color="primary" />
          <div v-if="inbox.items.length > 0" class="compact-list">
            <button
              v-for="item in inbox.items.slice(0, 5)"
              :key="item.request_id"
              class="compact-list__item"
              type="button"
              @click="openOwnerInbox"
            >
              <span>{{ item.title }}</span>
              <StatusChip :label="t(`statuses.${item.request_kind}`)" tone="warning" />
            </button>
          </div>
          <EmptyState
            v-else
            class="mt-4"
            icon="mdi-inbox-outline"
            :title="context.isReady ? t('inbox.empty') : t('context.missing')"
          />
        </v-card>
      </div>
    </section>
  </div>
</template>

<style scoped>
.summary-grid {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.summary-card {
  align-items: center;
  display: flex;
  gap: 16px;
  min-height: 112px;
  padding: 20px;
}

.summary-card__value {
  color: #121826;
  font-size: 1.7rem;
  font-weight: 800;
}

.summary-card__value--danger {
  color: #b42318;
}

.summary-card__hint,
.summary-card__placeholder {
  color: #667085;
  font-size: 0.82rem;
  margin-top: 4px;
}

.summary-card__placeholder {
  font-weight: 700;
}

.main-grid {
  display: grid;
  gap: 16px;
  grid-template-columns: minmax(0, 2fr) minmax(320px, 0.9fr);
}

.surface-readiness {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.readiness-panel {
  display: grid;
  gap: 14px;
  padding: 18px;
}

.readiness-grid {
  display: grid;
  gap: 10px;
}

.dialogue-panel {
  display: grid;
  gap: 16px;
  min-height: 520px;
  padding: 20px;
}

.quick-actions {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.side-stack {
  display: grid;
  gap: 16px;
}

.section-header {
  align-items: center;
  display: flex;
  gap: 12px;
  justify-content: space-between;
}

.self-deploy-panel {
  display: grid;
  gap: 18px;
  padding: 20px;
}

.self-deploy-panel__subtitle {
  color: #667085;
  font-size: 0.94rem;
  line-height: 1.45;
  margin: 6px 0 0;
}

.self-deploy-panel__body {
  display: grid;
  gap: 14px;
  grid-template-columns: minmax(0, 1.4fr) minmax(280px, 0.8fr);
}

.self-deploy-panel__fields {
  border: 1px solid #e4e7ec;
  border-radius: 8px;
  display: grid;
  overflow: hidden;
}

.self-deploy-field {
  align-items: center;
  display: grid;
  gap: 8px;
  grid-template-columns: minmax(160px, 0.7fr) minmax(0, 1fr);
  padding: 12px 14px;
}

.self-deploy-field + .self-deploy-field {
  border-top: 1px solid #e4e7ec;
}

.self-deploy-field span {
  color: #667085;
  font-size: 0.86rem;
}

.self-deploy-field strong {
  color: #182030;
  font-size: 0.9rem;
  font-weight: 700;
  overflow-wrap: anywhere;
}

.self-deploy-panel__notice {
  align-self: stretch;
}

.self-deploy-panel__actions {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.compact-list {
  display: grid;
  gap: 8px;
  margin-top: 16px;
}

.compact-list__item {
  align-items: center;
  background: #ffffff;
  border: 1px solid #e4e7ec;
  border-radius: 8px;
  color: #182030;
  display: flex;
  gap: 8px;
  justify-content: space-between;
  padding: 10px 12px;
  text-align: left;
}

.compact-list__item span {
  display: grid;
  gap: 3px;
  min-width: 0;
}

.compact-list__item small {
  color: #667085;
  overflow-wrap: anywhere;
}

.attention-text {
  color: #b54708;
}

.error-text {
  color: #b42318;
}

@media (max-width: 1180px) {
  .summary-grid,
  .surface-readiness,
  .main-grid,
  .self-deploy-panel__body {
    grid-template-columns: 1fr;
  }
}
</style>
