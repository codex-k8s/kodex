<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';

import type { RequestKind, RequestStatus, ResponseAction } from '@/shared/api/generated';
import { useOperatorContextStore } from '@/features/operator-context/store';
import { useOwnerInboxStore, terminalRequestStatuses } from '@/features/owner-inbox/store';
import { compactRef, formatDateTime, formatRelativeTime } from '@/shared/lib/format';
import ApiErrorAlert from '@/shared/ui/ApiErrorAlert.vue';
import EmptyState from '@/shared/ui/EmptyState.vue';
import StatusChip from '@/shared/ui/StatusChip.vue';

const { t } = useI18n();
const context = useOperatorContextStore();
const inbox = useOwnerInboxStore();

const actionDialog = ref(false);
const selectedAction = ref<ResponseAction>('answer');
const responseSummary = ref('');
const responseReason = ref('');

const requestKindOptions: RequestKind[] = ['feedback', 'approval', 'human_gate'];
const requestStatusOptions: RequestStatus[] = [
  'created',
  'routed',
  'waiting',
  'answered',
  'expired',
  'cancelled',
  'failed',
];
const responseActions: ResponseAction[] = [
  'answer',
  'approve',
  'reject',
  'request_changes',
  'defer',
  'acknowledge',
  'custom',
];

const canLoad = computed(() => context.isReady && !inbox.isLoadingList);
const selectedItem = computed(() => inbox.selectedItem);
const selectedIsTerminal = computed(
  () => !!selectedItem.value && terminalRequestStatuses.includes(selectedItem.value.request_status),
);
const selectedAllowedActionKeys = computed(
  () => new Set(inbox.selectedAllowedActions.map((action) => action.action_key)),
);
const selectedActionNeedsSummary = computed(() =>
  ['answer', 'request_changes', 'custom'].includes(selectedAction.value),
);
const canSubmitAction = computed(() => {
  if (inbox.isResponding || !selectedItem.value || selectedIsTerminal.value) {
    return false;
  }
  if (!selectedAllowedActionKeys.value.has(selectedAction.value)) {
    return false;
  }
  return !selectedActionNeedsSummary.value || responseSummary.value.trim().length > 0;
});

onMounted(() => {
  if (context.isReady && inbox.items.length === 0) {
    void inbox.load(context.asContext);
  }
});

watch(
  () => [context.scopeType, context.scopeRef],
  () => {
    if (context.isReady) {
      void inbox.load(context.asContext);
    }
  },
);

function statusTone(status: RequestStatus): 'neutral' | 'success' | 'warning' | 'error' | 'info' {
  if (status === 'answered') {
    return 'success';
  }
  if (status === 'waiting' || status === 'routed' || status === 'created') {
    return 'warning';
  }
  if (status === 'failed' || status === 'expired') {
    return 'error';
  }
  return 'neutral';
}

function isResponseAction(action: string): action is ResponseAction {
  return responseActions.includes(action as ResponseAction);
}

function loadList(pageToken?: string) {
  if (!context.isReady) {
    return;
  }
  void inbox.load(context.asContext, pageToken);
}

function selectItem(requestId: string) {
  if (!context.isReady) {
    return;
  }
  void inbox.select(context.asContext, requestId);
}

function openAction(action: string) {
  if (!isResponseAction(action) || !selectedAllowedActionKeys.value.has(action)) {
    return;
  }
  selectedAction.value = action;
  responseSummary.value = '';
  responseReason.value = '';
  actionDialog.value = true;
}

async function submitAction() {
  if (!context.isReady || !canSubmitAction.value) {
    return;
  }
  await inbox.respond(
    context.asContext,
    selectedAction.value,
    responseSummary.value,
    responseReason.value,
  );
  if (!inbox.error) {
    actionDialog.value = false;
  }
}

function reloadCurrent() {
  if (!context.isReady) {
    return;
  }
  if (selectedItem.value) {
    void inbox.select(context.asContext, selectedItem.value.request_id);
    return;
  }
  void inbox.load(context.asContext);
}
</script>

<template>
  <div class="page-grid">
    <header class="page-header">
      <div>
        <h1>{{ t('inbox.title') }}</h1>
        <p>{{ t('inbox.description') }}</p>
      </div>
      <v-btn
        color="primary"
        prepend-icon="mdi-refresh"
        :disabled="!canLoad"
        :loading="inbox.isLoadingList"
        @click="loadList()"
      >
        {{ t('app.refresh') }}
      </v-btn>
    </header>

    <v-alert v-if="!context.isReady" type="warning" variant="tonal">
      {{ t('context.missing') }}
    </v-alert>
    <ApiErrorAlert :error="inbox.error" :retry-label="t('app.retry')" @retry="reloadCurrent" />
    <v-alert v-if="inbox.latestResponse" type="success" variant="tonal">
      {{ t('inbox.answered') }} · {{ t(`statuses.${inbox.latestResponse.response_action}`) }}
    </v-alert>

    <v-card class="surface-panel pa-4">
      <div class="filter-row">
        <v-select
          v-model="inbox.filters.kinds"
          :items="requestKindOptions"
          :label="t('inbox.kind')"
          multiple
          chips
          clearable
        >
          <template #chip="{ item }">
            <StatusChip :label="t(`statuses.${item.value}`)" tone="info" />
          </template>
          <template #item="{ props, item }">
            <v-list-item v-bind="props" :title="t(`statuses.${item.value}`)" />
          </template>
        </v-select>
        <v-select
          v-model="inbox.filters.statuses"
          :items="requestStatusOptions"
          :label="t('inbox.status')"
          multiple
          chips
          clearable
        >
          <template #chip="{ item }">
            <StatusChip :label="t(`statuses.${item.value}`)" :tone="statusTone(item.value)" />
          </template>
          <template #item="{ props, item }">
            <v-list-item v-bind="props" :title="t(`statuses.${item.value}`)" />
          </template>
        </v-select>
        <v-text-field
          v-model.number="inbox.filters.pageSize"
          class="filter-row__small"
          type="number"
          min="1"
          max="100"
          :label="t('inbox.pageSize')"
        />
        <v-switch
          v-model="inbox.filters.includeDiagnostics"
          color="primary"
          hide-details
          :label="t('inbox.diagnostics')"
        />
      </div>
    </v-card>

    <section class="inbox-layout">
      <v-card class="surface-panel inbox-list">
        <v-progress-linear v-if="inbox.isLoadingList" indeterminate color="primary" />
        <div v-if="inbox.items.length > 0" class="table-scroll">
          <v-table density="comfortable">
            <thead>
              <tr>
                <th>{{ t('inbox.kind') }}</th>
                <th>{{ t('inbox.details') }}</th>
                <th>{{ t('inbox.status') }}</th>
                <th>{{ t('inbox.deadline') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="item in inbox.items"
                :key="item.request_id"
                :class="{ 'table-row-active': item.request_id === selectedItem?.request_id }"
                @click="selectItem(item.request_id)"
              >
                <td>
                  <StatusChip :label="t(`statuses.${item.request_kind}`)" tone="info" />
                </td>
                <td>
                  <div class="item-title">{{ item.title }}</div>
                  <div class="meta-text">{{ item.summary }}</div>
                </td>
                <td>
                  <StatusChip :label="t(`statuses.${item.request_status}`)" :tone="statusTone(item.request_status)" />
                </td>
                <td>{{ formatRelativeTime(item.deadline_at) }}</td>
              </tr>
            </tbody>
          </v-table>
        </div>
        <EmptyState v-else icon="mdi-inbox-outline" :title="t('inbox.empty')" :text="t('inbox.noSelectionText')" />
        <div class="list-footer">
          <v-btn
            variant="tonal"
            :disabled="!inbox.nextPageToken"
            @click="loadList(inbox.nextPageToken)"
          >
            {{ t('inbox.nextPage') }}
          </v-btn>
        </div>
      </v-card>

      <v-card class="surface-panel detail-panel">
        <v-progress-linear v-if="inbox.isLoadingDetail" indeterminate color="primary" />
        <template v-if="selectedItem">
          <div class="detail-panel__header">
            <div>
              <div class="meta-text">{{ compactRef(selectedItem.request_id) }}</div>
              <h2>{{ selectedItem.title }}</h2>
            </div>
            <StatusChip
              :label="t(`statuses.${selectedItem.request_status}`)"
              :tone="statusTone(selectedItem.request_status)"
            />
          </div>

          <p class="detail-summary">{{ selectedItem.summary }}</p>

          <div class="detail-grid">
            <div>
              <div class="meta-text">{{ t('context.scopeType') }}</div>
              <div>{{ selectedItem.scope.type }} / {{ compactRef(selectedItem.scope.ref) }}</div>
            </div>
            <div>
              <div class="meta-text">{{ t('inbox.requester') }}</div>
              <div>{{ selectedItem.requester.kind }} / {{ compactRef(selectedItem.requester.ref) }}</div>
            </div>
            <div>
              <div class="meta-text">{{ t('inbox.deadline') }}</div>
              <div>{{ formatDateTime(selectedItem.deadline_at) }}</div>
            </div>
            <div>
              <div class="meta-text">{{ t('inbox.assignees') }}</div>
              <div>{{ selectedItem.assignee_refs.length }}</div>
            </div>
            <div>
              <div class="meta-text">Version</div>
              <div>{{ selectedItem.version }}</div>
            </div>
          </div>

          <div v-if="selectedItem.context_refs.length > 0" class="ref-chip-row">
            <v-chip
              v-for="ref in selectedItem.context_refs"
              :key="`${ref.ref_kind}:${ref.ref}`"
              size="small"
              variant="tonal"
              color="info"
              label
            >
              {{ ref.ref_kind }} / {{ compactRef(ref.ref) }}
            </v-chip>
          </div>

          <v-divider />

          <div class="detail-section">
            <div class="section-title">{{ t('inbox.delivery') }}</div>
            <div class="meta-text">
              {{ selectedItem.delivery_summary.latest_status }} ·
              {{ selectedItem.delivery_summary.attempt_count }}
              <span v-if="selectedItem.delivery_summary.latest_error_code">
                · {{ selectedItem.delivery_summary.latest_error_code }}
              </span>
            </div>
          </div>

          <div v-if="selectedItem.latest_callback" class="detail-section">
            <div class="section-title">{{ t('inbox.callback') }}</div>
            <div class="meta-text">
              {{ selectedItem.latest_callback.processing_status }} ·
              {{ formatDateTime(selectedItem.latest_callback.received_at) }}
            </div>
          </div>

          <div v-if="selectedItem.latest_response" class="detail-section">
            <div class="section-title">{{ t('inbox.response') }}</div>
            <div class="meta-text">
              {{ t(`statuses.${selectedItem.latest_response.response_action}`) }} ·
              {{ formatDateTime(selectedItem.latest_response.created_at) }}
            </div>
            <p v-if="selectedItem.latest_response.response_summary" class="detail-summary">
              {{ selectedItem.latest_response.response_summary }}
            </p>
          </div>

          <div class="detail-section">
            <div class="section-title">{{ t('inbox.allowedActions') }}</div>
            <v-alert v-if="selectedIsTerminal" type="info" variant="tonal">
              {{ t('inbox.terminalHint') }}
            </v-alert>
            <div v-if="inbox.selectedAllowedActions.length > 0" class="action-grid">
              <v-btn
                v-for="action in inbox.selectedAllowedActions"
                :key="action.action_key"
                color="primary"
                variant="tonal"
                :disabled="!isResponseAction(action.action_key)"
                @click="openAction(action.action_key)"
              >
                {{
                  isResponseAction(action.action_key)
                    ? t(`statuses.${action.action_key}`)
                    : action.action_key
                }}
              </v-btn>
            </div>
            <div v-else class="meta-text">{{ t('inbox.noActions') }}</div>
          </div>
        </template>
        <EmptyState v-else icon="mdi-format-list-checks" :title="t('inbox.selectItem')" />
      </v-card>
    </section>

    <v-dialog v-model="actionDialog" max-width="560">
      <v-card class="pa-5">
        <div class="section-title">{{ t(`statuses.${selectedAction}`) }}</div>
        <p v-if="selectedItem" class="dialog-subtitle">
          {{ selectedItem.title }} · {{ t('inbox.selectedVersion') }} {{ selectedItem.version }}
        </p>
        <v-alert class="mt-3" type="info" variant="tonal">
          {{ t('inbox.responseHint') }}
        </v-alert>
        <v-textarea
          v-model="responseSummary"
          class="mt-4"
          :label="t('inbox.responseSummary')"
          rows="4"
          maxlength="2000"
          counter
        />
        <v-text-field
          v-model="responseReason"
          class="mt-3"
          :label="t('inbox.responseReason')"
          maxlength="256"
        />
        <ApiErrorAlert :error="inbox.error" class="mt-3" :retry-label="t('inbox.reloadAfterConflict')" @retry="reloadCurrent" />
        <div class="dialog-actions">
          <v-btn variant="text" @click="actionDialog = false">{{ t('app.cancel') }}</v-btn>
          <v-btn color="primary" :disabled="!canSubmitAction" :loading="inbox.isResponding" @click="submitAction">
            {{ t('inbox.sendAction') }}
          </v-btn>
        </div>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.page-header {
  align-items: flex-start;
  display: flex;
  justify-content: space-between;
  gap: 16px;
}

.page-header h1,
.detail-panel h2 {
  color: #121826;
  font-size: 1.8rem;
  line-height: 1.2;
  margin: 0;
}

.page-header p {
  color: #667085;
  margin: 8px 0 0;
}

.filter-row {
  align-items: center;
  display: grid;
  gap: 12px;
  grid-template-columns: minmax(220px, 1fr) minmax(220px, 1fr) 130px auto;
}

.filter-row__small {
  max-width: 130px;
}

.inbox-layout {
  display: grid;
  gap: 16px;
  grid-template-columns: minmax(0, 1.45fr) minmax(360px, 0.8fr);
}

.inbox-list,
.detail-panel {
  overflow: hidden;
}

.inbox-list tr {
  cursor: pointer;
}

.table-scroll {
  overflow-x: auto;
}

.item-title {
  color: #121826;
  font-weight: 700;
  margin-bottom: 4px;
}

.list-footer {
  border-top: 1px solid #e4e7ec;
  display: flex;
  justify-content: flex-end;
  padding: 12px;
}

.detail-panel {
  display: grid;
  gap: 16px;
  padding: 20px;
}

.detail-panel__header {
  align-items: flex-start;
  display: flex;
  gap: 16px;
  justify-content: space-between;
}

.detail-summary {
  color: #475467;
  line-height: 1.55;
  margin: 0;
}

.detail-grid {
  display: grid;
  gap: 14px;
  grid-template-columns: 1fr 1fr;
}

.ref-chip-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.detail-section {
  display: grid;
  gap: 8px;
}

.action-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.dialog-actions {
  align-items: center;
  display: flex;
  gap: 10px;
  justify-content: flex-end;
  margin-top: 18px;
}

.dialog-subtitle {
  color: #667085;
  line-height: 1.45;
  margin: 8px 0 0;
}

@media (max-width: 1180px) {
  .filter-row,
  .inbox-layout {
    grid-template-columns: 1fr;
  }

  .detail-grid {
    grid-template-columns: 1fr;
  }
}
</style>
