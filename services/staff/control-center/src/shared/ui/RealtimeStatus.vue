<script setup lang="ts">
import { LoaderCircle, RefreshCw, Wifi, WifiOff } from "@lucide/vue";
import { computed } from "vue";
import type { Component } from "vue";

import {
  realtimeConnectionState,
  realtimeStatusPresentation,
  type RealtimeStatusLabels,
  type RealtimeStatusState,
} from "@/shared/ui/realtime-status";

const props = withDefaults(
  defineProps<{
    state: RealtimeStatusState;
    labels: RealtimeStatusLabels;
    compact?: boolean;
    detail?: string;
  }>(),
  { compact: false },
);

const presentation = computed(() => realtimeStatusPresentation(props.state));
const connectionState = computed(() => realtimeConnectionState(props.state));
const icon = computed<Component>(() => {
  if (props.state === "offline") return WifiOff;
  if (props.state === "live") return Wifi;
  if (props.state === "initial-loading") return LoaderCircle;
  return RefreshCw;
});
</script>

<template>
  <span
    class="realtime-status"
    :class="`realtime-status--${presentation.tone}`"
    role="status"
    aria-live="polite"
    :aria-label="labels[state]"
    :title="detail || labels[state]"
    :data-state="connectionState"
    :data-presentation-state="state"
    :data-preserves-current-data="presentation.preservesCurrentData"
  >
    <component
      :is="icon"
      :size="15"
      aria-hidden="true"
      :class="{ 'realtime-status__icon--animated': presentation.animated }"
    />
    <span v-if="!compact">{{ labels[state] }}</span>
  </span>
</template>

<style scoped>
.realtime-status {
  display: inline-flex;
  min-height: 26px;
  align-items: center;
  gap: 6px;
  padding: 3px 8px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface);
  color: var(--text-secondary);
  font-size: 12px;
  white-space: nowrap;
}
.realtime-status--success {
  border-color: color-mix(in srgb, var(--success) 24%, var(--border));
  background: var(--success-soft);
  color: var(--success);
}
.realtime-status--accent {
  border-color: color-mix(in srgb, var(--accent) 24%, var(--border));
  background: var(--accent-soft);
  color: var(--accent);
}
.realtime-status--warning {
  border-color: color-mix(in srgb, var(--warning) 24%, var(--border));
  background: var(--warning-soft);
  color: var(--warning);
}
.realtime-status--danger {
  border-color: color-mix(in srgb, var(--danger) 24%, var(--border));
  background: var(--danger-soft);
  color: var(--danger);
}
.realtime-status__icon--animated {
  animation: realtime-status-spin 1s linear infinite;
}
@keyframes realtime-status-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (prefers-reduced-motion: reduce) {
  .realtime-status__icon--animated {
    animation: none;
  }
}
</style>
