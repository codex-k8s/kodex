<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import { useRouter } from 'vue-router';

import ApiErrorAlert from '@/shared/ui/ApiErrorAlert.vue';
import EmptyState from '@/shared/ui/EmptyState.vue';
import SurfaceStateCard from '@/shared/ui/SurfaceStateCard.vue';
import StatusChip from '@/shared/ui/StatusChip.vue';
import { useExecutionsStore } from '@/features/executions/store';
import { useOperatorContextStore } from '@/features/operator-context/store';
import { useOwnerInboxStore } from '@/features/owner-inbox/store';
import { routeNames } from '@/shared/lib/routes';

const { t } = useI18n();
const router = useRouter();
const context = useOperatorContextStore();
const inbox = useOwnerInboxStore();
const executions = useExecutionsStore();

const approvalItemsOnCurrentPage = computed(
  () => inbox.items.filter((item) => item.request_kind === 'approval').length,
);
const lastRunStatus = computed(() => executions.runtimeStatus?.run_status);

onMounted(() => {
  if (context.isReady && inbox.items.length === 0) {
    void inbox.load(context.asContext);
  }
});

function reloadInbox() {
  if (context.isReady) {
    void inbox.load(context.asContext);
  }
}

function openOwnerInbox() {
  void router.push({ name: routeNames.ownerInbox });
}

function openExecutions() {
  void router.push({ name: routeNames.executions });
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
        <v-icon icon="mdi-inbox-multiple-outline" color="success" size="34" />
        <div>
          <div class="meta-text">{{ t('commandCenter.itemsOnPage') }}</div>
          <div class="summary-card__value">{{ inbox.items.length }}</div>
          <div class="summary-card__hint">{{ t('commandCenter.currentInboxPageHint') }}</div>
        </div>
      </v-card>
      <v-card class="surface-panel summary-card">
        <v-icon icon="mdi-server-outline" color="info" size="34" />
        <div>
          <div class="meta-text">{{ t('commandCenter.lastRunLookup') }}</div>
          <StatusChip v-if="lastRunStatus" :label="t(`statuses.${lastRunStatus}`)" tone="info" />
          <div v-else class="summary-card__placeholder">{{ t('commandCenter.noRunLookup') }}</div>
          <div class="summary-card__hint">{{ t('commandCenter.lastRunLookupHint') }}</div>
        </div>
      </v-card>
      <v-card class="surface-panel summary-card">
        <v-icon icon="mdi-clock-outline" color="warning" size="34" />
        <div>
          <div class="meta-text">{{ t('commandCenter.approvalsOnPage') }}</div>
          <div class="summary-card__value">{{ approvalItemsOnCurrentPage }}</div>
          <div class="summary-card__hint">{{ t('commandCenter.currentInboxPageHint') }}</div>
        </div>
      </v-card>
    </section>

    <ApiErrorAlert :error="inbox.error" :retry-label="t('app.retry')" @retry="reloadInbox" />

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
            :title="t('commandCenter.runLookupLive')"
            :text="t('commandCenter.runLookupLiveText')"
            :status="t('app.live')"
            tone="live"
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
            {{ t('commandCenter.runLookupLive') }}
          </v-btn>
          <EmptyState
            class="mt-4"
            icon="mdi-clipboard-text-clock-outline"
            :title="t('commandCenter.noAggregate')"
          />
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

@media (max-width: 1180px) {
  .summary-grid,
  .surface-readiness,
  .main-grid {
    grid-template-columns: 1fr;
  }
}
</style>
