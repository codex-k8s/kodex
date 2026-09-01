<script setup lang="ts">
import { History } from "@lucide/vue";
import { nextTick, watch } from "vue";

import {
  formatTimestamp,
  type SessionPickerItem,
} from "@/features/new-run/model";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
import AsyncEntityPicker, {
  type AsyncEntityPickerLabels,
} from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityLoader } from "@/shared/ui/async-entity-picker";
import OverlayPanel from "@/shared/ui/OverlayPanel.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

export interface NewRunSessionPickerLabels {
  title: string;
  subtitle: string;
  close: string;
  states: Record<Run["state"], string>;
  picker: AsyncEntityPickerLabels;
}

const props = defineProps<{
  open: boolean;
  selectedSessionRef: string;
  loadItems: AsyncEntityLoader<SessionPickerItem>;
  labels: NewRunSessionPickerLabels;
  locale: string;
}>();
const emit = defineEmits<{
  close: [];
  select: [run: Run];
}>();

function requestClose(open: boolean): void {
  if (!open) emit("close");
}

function select(item: SessionPickerItem): void {
  emit("select", item.run);
}

function stateTone(
  state: Run["state"],
): "danger" | "neutral" | "success" | "warning" {
  if (state === "SUCCEEDED") return "success";
  if (state === "FAILED") return "danger";
  if (state === "CANCELLED") return "neutral";
  return "warning";
}

watch(
  () => props.open,
  async (open) => {
    if (!open) return;
    await nextTick();
    if (typeof document === "undefined") return;
    document
      .querySelector<HTMLInputElement>(
        ".new-run-session-picker__overlay input[type='search']",
      )
      ?.focus();
  },
  { immediate: true },
);
</script>

<template>
  <OverlayPanel
    class="new-run-session-picker__overlay"
    :open="open"
    mode="modal"
    :ariaLabel="labels.title"
    :close-label="labels.close"
    @update:open="requestClose"
  >
    <template #header>
      <div class="new-run-session-picker__heading">
        <h2>{{ labels.title }}</h2>
        <p>{{ labels.subtitle }}</p>
      </div>
    </template>

    <AsyncEntityPicker
      class="new-run-session-picker__picker"
      :model-value="selectedSessionRef || null"
      :load-items="loadItems"
      :labels="labels.picker"
      @select="select"
    >
      <template #option="{ item }">
        <span class="new-run-session-picker__icon" aria-hidden="true">
          <History :size="18" />
        </span>
        <span class="new-run-session-picker__copy">
          <strong>{{ item.run.title }}</strong>
          <span>
            {{ item.run.target.displayName }} ·
            {{ formatTimestamp(item.run.createdAt, locale) }}
          </span>
          <span
            v-if="item.run.resultSummary"
            class="new-run-session-picker__result"
          >
            {{ item.run.resultSummary }}
          </span>
        </span>
        <StatusBadge
          :state="item.run.state"
          :label="labels.states[item.run.state]"
          :tone="stateTone(item.run.state)"
        />
      </template>
    </AsyncEntityPicker>
  </OverlayPanel>
</template>

<style scoped>
.new-run-session-picker__heading h2 {
  margin: 0;
  font-size: 16px;
}
.new-run-session-picker__heading p {
  margin: 3px 0 0;
  color: var(--text-secondary);
  font-size: 12px;
}
.new-run-session-picker__picker {
  height: min(500px, calc(100dvh - 170px));
  border: 0;
  border-radius: 0;
}
.new-run-session-picker__icon {
  display: grid;
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
  place-items: center;
  border-radius: 50%;
  color: var(--accent);
  background: var(--accent-soft);
}
.new-run-session-picker__copy {
  display: grid;
  min-width: 0;
  flex: 1;
  gap: 3px;
}
.new-run-session-picker__copy strong,
.new-run-session-picker__copy span {
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow-wrap: anywhere;
}
.new-run-session-picker__copy span {
  color: var(--text-secondary);
  font-size: 12px;
}
.new-run-session-picker__result {
  color: var(--text) !important;
}
.new-run-session-picker__overlay :deep(.overlay-panel--modal) {
  width: min(580px, calc(100vw - 40px));
}
.new-run-session-picker__overlay :deep(.overlay-panel__body) {
  overflow: hidden;
  padding: 0;
}
@media (max-width: 760px) {
  .new-run-session-picker__overlay {
    align-items: flex-end;
    padding: 0;
  }
  .new-run-session-picker__overlay :deep(.overlay-panel--modal) {
    width: 100%;
    height: calc(100dvh - 40px);
    max-height: none;
    border-width: 1px 0 0;
    border-radius: 10px 10px 0 0;
  }
  .new-run-session-picker__picker {
    height: 100%;
  }
}
</style>
