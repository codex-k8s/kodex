<script setup lang="ts">
import {
  Activity,
  Bot,
  CircleDot,
  Info,
  UserRound,
  Wrench,
  X,
} from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  buildRunActivityItems,
  type PresentedRunEvent,
} from "@/features/runs/run-activity";
import type { Run, RunNode } from "@/shared/api/generated/openapi/types.gen";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import SafeStructuredData from "@/shared/ui/SafeStructuredData.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = withDefaults(
  defineProps<{
    open: boolean;
    run: Run;
    nodes: RunNode[];
    events: PresentedRunEvent[];
    initiatorSummary?: string;
    initialNodeRef?: string;
  }>(),
  {
    initiatorSummary: undefined,
    initialNodeRef: undefined,
  },
);
const emit = defineEmits<{ close: [] }>();
const { locale } = useI18n();
const selectedNodeRef = ref("");

const items = computed(() =>
  buildRunActivityItems(
    props.run,
    props.nodes,
    props.events,
    props.initiatorSummary,
  ),
);
const filteredItems = computed(() =>
  selectedNodeRef.value
    ? items.value.filter(
        (item) => !item.nodeRef || item.nodeRef === selectedNodeRef.value,
      )
    : items.value,
);
const recordedToolNodeRefs = computed(
  () =>
    new Set(
      props.events
        .filter((event) => event.toolCall && event.nodeRef)
        .map((event) => event.nodeRef ?? ""),
    ),
);
const unrecordedToolNodes = computed(() =>
  props.nodes.filter(
    (node) =>
      node.type === "EXTERNAL_ACTION" &&
      !recordedToolNodeRefs.value.has(node.ref) &&
      (!selectedNodeRef.value ||
        node.ref === selectedNodeRef.value ||
        node.parentNodeRef === selectedNodeRef.value),
  ),
);

watch(
  () => props.initialNodeRef,
  (nodeRef) => {
    selectedNodeRef.value = nodeRef ?? "";
  },
  { immediate: true },
);

function formatTime(value: string): string {
  return new Date(value).toLocaleTimeString(locale.value, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function handleKeydown(event: KeyboardEvent): void {
  if (props.open && event.key === "Escape") emit("close");
}

onMounted(() => window.addEventListener("keydown", handleKeydown));
onBeforeUnmount(() => window.removeEventListener("keydown", handleKeydown));
</script>

<template>
  <aside
    v-if="open"
    id="run-activity-drawer"
    class="run-activity-drawer"
    role="dialog"
    aria-modal="false"
    :aria-label="$t('runs.activity')"
  >
    <header class="run-activity-drawer__header">
      <div>
        <h2>{{ $t("runs.activity") }}</h2>
        <p>{{ filteredItems.length }} · {{ run.target.displayName }}</p>
      </div>
      <button
        class="icon-button"
        type="button"
        :aria-label="$t('common.close')"
        :title="$t('common.close')"
        @click="emit('close')"
      >
        <X :size="19" aria-hidden="true" />
      </button>
    </header>

    <div class="run-activity-drawer__tools">
      <label>
        <span class="sr-only">{{ $t("runs.context") }}</span>
        <select v-model="selectedNodeRef">
          <option value="">{{ $t("common.all") }}</option>
          <option v-for="node in nodes" :key="node.ref" :value="node.ref">
            {{ node.displayName }}
          </option>
        </select>
      </label>
      <span>{{ events.length }}</span>
    </div>

    <div class="run-activity-drawer__body" aria-live="polite">
      <ol v-if="filteredItems.length" class="run-activity-list">
        <li
          v-for="item in filteredItems"
          :key="item.id"
          class="run-activity-item"
          :class="`run-activity-item--${item.kind}`"
        >
          <span class="run-activity-item__icon" aria-hidden="true">
            <UserRound v-if="item.kind === 'initiator'" :size="17" />
            <Bot v-else-if="item.kind === 'agent'" :size="17" />
            <Wrench v-else-if="item.kind === 'tool'" :size="17" />
            <CircleDot v-else :size="16" />
          </span>
          <article class="run-activity-item__content">
            <header>
              <strong>{{ item.actor }}</strong>
              <StatusBadge v-if="item.state" :state="item.state" />
              <time :datetime="item.occurredAt">
                {{ formatTime(item.occurredAt) }}
                <template v-if="item.sequence">
                  · #{{ item.sequence }}</template
                >
              </time>
            </header>

            <section v-if="item.toolCall" class="run-tool-event">
              <div class="run-tool-event__heading">
                <strong>{{ item.toolCall.tool }}</strong>
                <StatusBadge :state="item.toolCall.state" />
              </div>
              <SafeMarkdown
                v-if="item.summary"
                :content="item.summary"
                class="run-activity-item__message"
              />
              <details v-if="Object.keys(item.toolCall.safeParameters).length">
                <summary>{{ $t("runs.toolParameters") }}</summary>
                <SafeStructuredData :value="item.toolCall.safeParameters" />
              </details>
              <SafeMarkdown
                v-if="item.toolCall.safeResult"
                :content="item.toolCall.safeResult"
              />
              <small>
                {{
                  $t("runs.toolDuration", {
                    duration: item.toolCall.durationMs,
                  })
                }}
              </small>
            </section>

            <template v-else>
              <SafeMarkdown
                v-if="item.summary"
                :content="item.summary"
                class="run-activity-item__message"
              />
              <p v-else class="run-activity-item__empty">
                {{ $t("common.noData") }}
              </p>
              <SafeMarkdown
                v-if="item.progress"
                :content="item.progress"
                class="run-activity-item__progress"
              />
            </template>
          </article>
        </li>
      </ol>
      <div v-else class="run-activity-drawer__empty">
        <Activity :size="24" aria-hidden="true" />
        <p>{{ $t("runs.noNodeActivity") }}</p>
      </div>

      <section
        v-if="unrecordedToolNodes.length"
        class="run-activity-unavailable"
      >
        <header>
          <Wrench :size="17" aria-hidden="true" />
          <strong>{{ $t("runs.nodeTypes.EXTERNAL_ACTION") }}</strong>
          <Info :size="15" aria-hidden="true" />
        </header>
        <article v-for="node in unrecordedToolNodes" :key="node.ref">
          <div>
            <strong>{{ node.displayName }}</strong>
            <p>{{ node.progressSummary || node.inputSummary }}</p>
          </div>
          <StatusBadge :state="node.state" />
          <small>{{ $t("common.unavailable") }}</small>
        </article>
      </section>
    </div>
  </aside>
</template>

<style scoped>
.run-activity-drawer {
  position: absolute;
  z-index: 30;
  top: 12px;
  right: 12px;
  bottom: 12px;
  display: flex;
  width: min(640px, calc(100% - 24px));
  min-width: 440px;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  box-shadow: -16px 18px 48px rgba(16, 22, 30, 0.18);
}
.run-activity-drawer__header {
  display: flex;
  min-height: 58px;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 10px 14px 10px 16px;
  border-bottom: 1px solid var(--border);
}
.run-activity-drawer__header h2,
.run-activity-drawer__header p {
  margin: 0;
}
.run-activity-drawer__header h2 {
  font-size: 1rem;
}
.run-activity-drawer__header p {
  margin-top: 2px;
  color: var(--muted);
  font-size: 0.76rem;
}
.run-activity-drawer__tools {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 16px;
  border-bottom: 1px solid var(--border);
  background: var(--panel);
}
.run-activity-drawer__tools label {
  min-width: 0;
  flex: 1 1 auto;
}
.run-activity-drawer__tools select {
  width: min(100%, 360px);
}
.run-activity-drawer__tools > span {
  flex: 0 0 auto;
  color: var(--muted);
  font-family: var(--font-mono);
  font-size: 0.76rem;
}
.run-activity-drawer__body {
  min-height: 0;
  padding: 8px 18px 24px;
  overflow: auto;
  overscroll-behavior: contain;
}
.run-activity-list {
  display: grid;
  margin: 0;
  padding: 0;
  list-style: none;
}
.run-activity-item {
  position: relative;
  display: grid;
  grid-template-columns: 32px minmax(0, 1fr);
  gap: 10px;
  padding: 11px 0;
}
.run-activity-item:not(:last-child)::before {
  position: absolute;
  left: 15px;
  top: 42px;
  bottom: -10px;
  width: 1px;
  background: var(--border);
  content: "";
}
.run-activity-item__icon {
  z-index: 1;
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  border: 1px solid var(--border);
  border-radius: 50%;
  color: var(--muted);
  background: var(--surface);
}
.run-activity-item--agent .run-activity-item__icon {
  color: var(--success);
}
.run-activity-item--tool .run-activity-item__icon {
  color: var(--warning);
}
.run-activity-item__content {
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.run-activity-item--initiator .run-activity-item__content {
  border-color: color-mix(in srgb, var(--accent) 48%, var(--border));
  background: var(--accent-soft);
}
.run-activity-item--agent .run-activity-item__content {
  border-left: 3px solid var(--success);
}
.run-activity-item--tool .run-activity-item__content {
  border-left: 3px solid var(--warning);
  background: color-mix(in srgb, var(--warning-soft) 55%, var(--surface));
}
.run-activity-item__content > header,
.run-tool-event__heading {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 7px;
}
.run-activity-item__content > header {
  margin-bottom: 6px;
}
.run-activity-item__content > header strong {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.84rem;
}
.run-activity-item__content time {
  margin-left: auto;
  color: var(--subtle);
  font-family: var(--font-mono);
  font-size: 0.7rem;
  white-space: nowrap;
}
.run-activity-item__message,
.run-activity-item__progress,
.run-activity-item__empty {
  font-size: 0.86rem;
}
.run-activity-item__message :deep(p),
.run-activity-item__progress :deep(p),
.run-activity-item__empty {
  margin: 0;
}
.run-activity-item__progress {
  margin-top: 7px;
  padding: 7px 9px;
  border-left: 2px solid var(--accent);
  background: var(--panel);
  color: var(--muted);
}
.run-activity-item__empty,
.run-activity-drawer__empty {
  color: var(--muted);
}
.run-activity-drawer__empty {
  display: grid;
  min-height: 220px;
  place-items: center;
  align-content: center;
  gap: 8px;
  text-align: center;
}
.run-tool-event {
  display: grid;
  gap: 8px;
}
.run-tool-event__heading {
  justify-content: space-between;
}
.run-tool-event details {
  min-width: 0;
}
.run-tool-event summary {
  cursor: pointer;
  font-size: 0.78rem;
  font-weight: 600;
}
.run-tool-event small {
  color: var(--subtle);
  font-size: 0.72rem;
}
.run-activity-unavailable {
  display: grid;
  gap: 8px;
  margin-top: 14px;
  padding: 12px;
  border: 1px dashed var(--border-strong);
  border-radius: 8px;
  background: var(--panel);
}
.run-activity-unavailable > header {
  display: flex;
  align-items: center;
  gap: 7px;
}
.run-activity-unavailable > article {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 6px 10px;
  padding-top: 8px;
  border-top: 1px solid var(--border);
}
.run-activity-unavailable p {
  margin: 3px 0 0;
  color: var(--muted);
  font-size: 0.78rem;
}
.run-activity-unavailable small {
  grid-column: 1 / -1;
  color: var(--subtle);
}
@media (max-width: 760px) {
  .run-activity-drawer {
    inset: 0;
    z-index: 40;
    width: 100%;
    min-width: 0;
    border: 0;
    border-radius: 0;
    box-shadow: none;
  }
  .run-activity-drawer__body {
    padding-inline: 14px;
  }
}
</style>
