<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';

import EmptyState from '@/shared/ui/EmptyState.vue';
import StatusChip from '@/shared/ui/StatusChip.vue';
import { useExecutionsStore } from '@/features/executions/store';
import { useOperatorContextStore } from '@/features/operator-context/store';
import { useOwnerInboxStore } from '@/features/owner-inbox/store';

const { t } = useI18n();
const context = useOperatorContextStore();
const inbox = useOwnerInboxStore();
const executions = useExecutionsStore();

const runtimeLabel = computed(() => executions.runtimeStatus?.run_status ?? 'unspecified');

onMounted(() => {
  if (context.isReady && inbox.items.length === 0) {
    void inbox.load(context.asContext);
  }
});
</script>

<template>
  <div class="page-grid command-center">
    <section class="summary-grid">
      <v-card class="surface-panel summary-card">
        <v-icon icon="mdi-alert-circle-outline" color="error" size="34" />
        <div>
          <div class="meta-text">{{ t('commandCenter.decisions') }}</div>
          <div class="summary-card__value">{{ inbox.pendingCount }}</div>
        </div>
      </v-card>
      <v-card class="surface-panel summary-card">
        <v-icon icon="mdi-play-circle-outline" color="success" size="34" />
        <div>
          <div class="meta-text">{{ t('commandCenter.activeRuns') }}</div>
          <div class="summary-card__value">{{ executions.runtimeStatus ? 1 : 0 }}</div>
        </div>
      </v-card>
      <v-card class="surface-panel summary-card">
        <v-icon icon="mdi-server-outline" color="info" size="34" />
        <div>
          <div class="meta-text">{{ t('commandCenter.runtime') }}</div>
          <StatusChip :label="t(`statuses.${runtimeLabel}`)" tone="info" />
        </div>
      </v-card>
      <v-card class="surface-panel summary-card">
        <v-icon icon="mdi-clock-outline" color="warning" size="34" />
        <div>
          <div class="meta-text">{{ t('commandCenter.confirmations') }}</div>
          <div class="summary-card__value">
            {{ inbox.items.filter((item) => item.request_kind === 'approval').length }}
          </div>
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
          <EmptyState
            class="mt-4"
            icon="mdi-clipboard-text-clock-outline"
            :title="t('commandCenter.noAggregate')"
          />
        </v-card>
        <v-card class="surface-panel pa-5">
          <div class="section-title">{{ t('commandCenter.myChecks') }}</div>
          <div v-if="inbox.items.length > 0" class="compact-list">
            <button
              v-for="item in inbox.items.slice(0, 5)"
              :key="item.request_id"
              class="compact-list__item"
              type="button"
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

.main-grid {
  display: grid;
  gap: 16px;
  grid-template-columns: minmax(0, 2fr) minmax(320px, 0.9fr);
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
  .main-grid {
    grid-template-columns: 1fr;
  }
}
</style>
