<script setup lang="ts">
import { X } from "@lucide/vue";
import { computed, ref, useId, watch } from "vue";

import {
  buildRunActivityItems,
  ordinaryRunActiveTranscriptItemId,
  type PresentedRunEvent,
} from "@/features/runs/run-activity";
import { isRunSessionNode } from "@/features/runs/run-session-graph";
import type {
  Artifact,
  Run,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import RunTranscript from "@/features/runs/RunTranscript.vue";

const props = withDefaults(
  defineProps<{
    open: boolean;
    run: Run;
    activityRuns?: readonly Run[];
    nodes: RunNode[];
    events: PresentedRunEvent[];
    artifacts: Artifact[];
    initiatorSummary?: string;
    initialNodeRef?: string;
  }>(),
  {
    initiatorSummary: undefined,
    activityRuns: () => [],
    initialNodeRef: undefined,
  },
);
const contextField = `run-activity-context-${useId()}`;
const emit = defineEmits<{ close: []; download: [artifact: Artifact] }>();
const selectedNodeRef = ref("");
const sessionNodes = computed(() => props.nodes.filter(isRunSessionNode));

const artifactsByRef = computed(
  () => new Map(props.artifacts.map((artifact) => [artifact.ref, artifact])),
);
const items = computed(() =>
  buildRunActivityItems(
    props.run,
    props.nodes,
    props.events,
    props.initiatorSummary,
  ).map((item) => ({
    ...item,
    artifact:
      item.artifact ??
      (item.artifactRef
        ? artifactsByRef.value.get(item.artifactRef)
        : undefined),
  })),
);
const filteredItems = computed(() =>
  selectedNodeRef.value
    ? items.value.filter(
        (item) => !item.nodeRef || item.nodeRef === selectedNodeRef.value,
      )
    : items.value,
);
const activeRun = computed(() => {
  const node = props.nodes.find((entry) => entry.ref === selectedNodeRef.value);
  const runRef = node?.runRef ?? props.run.ref;
  return runRef === props.run.ref
    ? props.run
    : props.activityRuns.find((entry) => entry.ref === runRef);
});
const activeItemId = computed(() => {
  if (!activeRun.value) return null;
  return activeRun.value.source === "SYSTEM_ASSISTANT"
    ? undefined
    : ordinaryRunActiveTranscriptItemId(
        activeRun.value,
        props.nodes,
        filteredItems.value,
      );
});

watch(
  () => props.initialNodeRef,
  (nodeRef) => {
    selectedNodeRef.value = nodeRef ?? "";
  },
  { immediate: true },
);
</script>

<template>
  <section
    v-if="open"
    id="run-activity-drawer"
    class="run-activity-drawer"
    role="region"
    :aria-label="$t('runs.activity')"
  >
    <header class="run-activity-drawer__header">
      <div>
        <h2>{{ $t("runs.activity") }}</h2>
        <p>{{ run.title }}</p>
      </div>
      <button
        class="icon-button"
        type="button"
        :aria-label="$t('common.close')"
        @click="emit('close')"
      >
        <X :size="19" aria-hidden="true" />
      </button>
    </header>
    <div class="run-activity-drawer__tools">
      <label class="run-activity-drawer__session-filter">
        <span>{{ $t("runs.sessionFilter") }}</span>
        <select
          v-model="selectedNodeRef"
          :id="contextField"
          :name="contextField"
        >
          <option value="">{{ $t("runs.allSessions") }}</option>
          <option
            v-for="node in sessionNodes"
            :key="node.ref"
            :value="node.ref"
          >
            {{ node.displayName }} · {{ $t(`states.${node.state}`) }}
          </option>
        </select>
      </label>
      <span>{{
        $t("runs.activityItemCount", { count: filteredItems.length })
      }}</span>
    </div>

    <RunTranscript
      class="run-activity-drawer__body"
      :items="filteredItems"
      :active-item-id="activeItemId"
      @download="emit('download', $event)"
    />
    <footer v-if="$slots.composer" class="run-activity-drawer__composer">
      <slot name="composer" />
    </footer>
  </section>
</template>

<style scoped>
.run-activity-drawer {
  display: flex;
  width: 100%;
  height: 100%;
  min-width: 0;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
  background: var(--surface);
}
.run-activity-drawer__header {
  display: flex;
  min-width: 0;
  flex: 0 0 auto;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 13px 16px;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.run-activity-drawer__header > div {
  display: grid;
  min-width: 0;
  gap: 2px;
}
.run-activity-drawer__header h2,
.run-activity-drawer__header p {
  overflow: hidden;
  margin: 0;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.run-activity-drawer__header h2 {
  font-size: 1rem;
}
.run-activity-drawer__header p {
  color: var(--muted);
  font-size: 0.75rem;
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
  display: flex;
  min-width: 0;
  flex: 1 1 auto;
  align-items: center;
  gap: 10px;
}
.run-activity-drawer__session-filter > span {
  flex: 0 0 auto;
  color: var(--muted);
  font-size: 0.76rem;
}
.run-activity-drawer__tools select {
  min-width: 0;
  max-width: 520px;
  flex: 1 1 auto;
}
.run-activity-drawer__tools > span {
  flex: 0 0 auto;
  color: var(--muted);
  font-family: var(--font-mono);
  font-size: 0.76rem;
}
.run-activity-drawer__body {
  flex: 1 1 auto;
  min-height: 0;
  padding: 8px 18px 24px;
  overflow: auto;
  overscroll-behavior: contain;
}
.run-activity-drawer__composer {
  flex: 0 0 auto;
  padding: 12px 16px 14px;
  border-top: 1px solid var(--border);
  background: var(--surface);
}
.run-activity-drawer__composer :deep(.field) {
  margin: 0;
}
.run-activity-drawer__composer :deep(textarea) {
  min-height: 78px;
  max-height: 180px;
  resize: vertical;
}
@media (max-width: 760px) {
  .run-activity-drawer__body {
    padding-inline: 14px;
  }
}
</style>
