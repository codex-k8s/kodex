<script setup lang="ts">
import { Activity, Bot, Info, UserRound, Wrench, X } from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  buildRunActivityItems,
  type PresentedRunEvent,
} from "@/features/runs/run-activity";
import RunToolDetails from "@/features/runs/RunToolDetails.vue";
import type { Run, RunNode } from "@/shared/api/generated/openapi/types.gen";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
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
const detailOpen = ref(false);
const selectedToolRef = ref<string>();

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
    ? items.value.filter((item) => item.nodeRef === selectedNodeRef.value)
    : items.value,
);
const toolNodes = computed(() =>
  props.nodes.filter(
    (node) =>
      node.type === "EXTERNAL_ACTION" &&
      (!selectedNodeRef.value ||
        node.ref === selectedNodeRef.value ||
        node.parentNodeRef === selectedNodeRef.value),
  ),
);
const selectedToolNode = computed(() =>
  props.nodes.find((node) => node.ref === selectedToolRef.value),
);

watch(
  () => props.initialNodeRef,
  (nodeRef) => {
    selectedNodeRef.value = nodeRef ?? "";
  },
  { immediate: true },
);
watch(
  () => props.open,
  (open) => {
    if (!open) {
      detailOpen.value = false;
      selectedToolRef.value = undefined;
    }
  },
);

function formatTime(value: string): string {
  return new Date(value).toLocaleTimeString(locale.value, {
    hour: "2-digit",
    minute: "2-digit",
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
        <h2>
          {{ detailOpen ? $t("common.details") : $t("runs.activity") }}
        </h2>
        <p>{{ events.length }}</p>
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

    <RunToolDetails
      v-if="detailOpen"
      :node="selectedToolNode"
      @back="detailOpen = false"
    />

    <template v-else>
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
        <span>{{ filteredItems.length }}</span>
      </div>

      <div class="run-activity-drawer__body" aria-live="polite">
        <ol v-if="filteredItems.length" class="run-activity-list">
          <li
            v-for="item in filteredItems"
            :key="item.id"
            class="run-activity-item"
            :class="`run-activity-item--${item.kind}`"
          >
            <span class="run-activity-item__icon">
              <UserRound
                v-if="item.kind === 'initiator'"
                :size="17"
                aria-hidden="true"
              />
              <Bot
                v-else-if="item.kind === 'agent'"
                :size="17"
                aria-hidden="true"
              />
              <Activity v-else :size="17" aria-hidden="true" />
            </span>
            <div class="run-activity-item__content">
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
            </div>
          </li>
        </ol>
        <p v-else class="run-activity-drawer__empty">
          {{ $t("runs.noNodeActivity") }}
        </p>

        <article
          v-for="toolNode in toolNodes"
          :key="toolNode.ref"
          class="run-tool-call"
        >
          <span class="run-tool-call__icon">
            <Wrench :size="17" aria-hidden="true" />
          </span>
          <div>
            <header>
              <strong>{{ toolNode.displayName }}</strong>
              <StatusBadge :state="toolNode.state" />
            </header>
            <SafeMarkdown
              v-if="toolNode.progressSummary || toolNode.inputSummary"
              :content="
                toolNode.progressSummary ||
                toolNode.inputSummary ||
                $t('common.noData')
              "
            />
            <p v-else>{{ $t("common.noData") }}</p>
          </div>
          <button
            class="button button--ghost"
            type="button"
            @click="
              selectedToolRef = toolNode.ref;
              detailOpen = true;
            "
          >
            {{ $t("common.details") }}
          </button>
        </article>

        <article
          v-if="!toolNodes.length"
          class="run-tool-call run-tool-call--unavailable"
        >
          <span class="run-tool-call__icon">
            <Wrench :size="17" aria-hidden="true" />
          </span>
          <div>
            <header>
              <strong>{{ $t("runs.nodeTypes.EXTERNAL_ACTION") }}</strong>
              <Info :size="15" aria-hidden="true" />
            </header>
            <p>{{ $t("common.unavailable") }}</p>
          </div>
          <button
            class="button button--ghost"
            type="button"
            @click="
              selectedToolRef = undefined;
              detailOpen = true;
            "
          >
            {{ $t("common.details") }}
          </button>
        </article>
      </div>
    </template>
  </aside>
</template>

<style scoped>
.run-activity-drawer {
  position: absolute;
  z-index: 8;
  top: 0;
  right: 0;
  bottom: 0;
  display: flex;
  flex-direction: column;
  width: min(520px, 50%);
  min-width: 420px;
  min-height: 0;
  border-left: 1px solid var(--border);
  background: var(--surface);
  box-shadow: -18px 0 44px rgba(16, 22, 30, 0.16);
}
.run-activity-drawer__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  min-height: 58px;
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
  font-family: var(--font-mono);
  font-size: 0.74rem;
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
  width: 100%;
  max-width: 280px;
}
.run-activity-drawer__tools > span {
  flex: 0 0 auto;
  color: var(--muted);
  font-family: var(--font-mono);
  font-size: 0.76rem;
}
.run-activity-drawer__body {
  min-height: 0;
  padding: 6px 16px 20px;
  overflow: auto;
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
  grid-template-columns: 30px minmax(0, 1fr);
  gap: 10px;
  padding: 12px 0;
}
.run-activity-item:not(:last-child)::before {
  position: absolute;
  left: 14px;
  top: 38px;
  bottom: -10px;
  width: 1px;
  background: var(--border);
  content: "";
}
.run-activity-item__icon,
.run-tool-call__icon {
  z-index: 1;
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border: 1px solid var(--border);
  border-radius: 50%;
  color: var(--accent);
  background: var(--surface);
}
.run-activity-item--initiator .run-activity-item__icon {
  color: var(--text);
  background: var(--panel);
}
.run-activity-item__content {
  min-width: 0;
}
.run-activity-item__content > header {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  margin-bottom: 5px;
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
  margin-top: 6px;
  padding: 7px 9px;
  border-left: 2px solid var(--accent);
  background: var(--panel);
  color: var(--muted);
}
.run-activity-item__empty,
.run-activity-drawer__empty {
  color: var(--muted);
}
.run-tool-call {
  display: grid;
  grid-template-columns: 30px minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  margin-top: 10px;
  padding: 10px;
  border: 1px dashed var(--border-strong);
  border-radius: 8px;
  background: var(--panel);
}
.run-tool-call header {
  display: flex;
  align-items: center;
  gap: 6px;
}
.run-tool-call p {
  margin: 3px 0 0;
  color: var(--muted);
  font-size: 0.78rem;
}
.run-tool-call :deep(.safe-markdown > p) {
  margin: 3px 0 0;
  color: var(--muted);
  font-size: 0.78rem;
}
.run-tool-call--unavailable {
  color: var(--muted);
}
@media (max-width: 760px) {
  .run-activity-drawer {
    position: relative;
    z-index: 1;
    inset: auto;
    grid-column: 1;
    width: 100%;
    min-width: 0;
    height: 100%;
    border-left: 0;
    box-shadow: none;
  }
  .run-activity-drawer__body {
    padding-inline: 14px;
  }
  .run-tool-call {
    grid-template-columns: 30px minmax(0, 1fr);
  }
  .run-tool-call .button {
    grid-column: 2;
    width: fit-content;
  }
}
</style>
