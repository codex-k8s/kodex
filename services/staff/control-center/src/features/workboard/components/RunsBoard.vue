<script setup lang="ts">
import { computed } from "vue";

import { groupRuns, type RunLane } from "@/features/workboard/model";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
import RunWorkItem from "@/features/workboard/components/RunWorkItem.vue";

const props = defineProps<{
  runs: Run[];
  hasMore?: boolean;
  loadingMore?: boolean;
  preserveProject?: boolean;
}>();
const emit = defineEmits<{ more: [] }>();
function onScroll(event: Event): void {
  const element = event.currentTarget;
  if (
    props.hasMore &&
    !props.loadingMore &&
    element instanceof HTMLElement &&
    element.scrollHeight - element.scrollTop - element.clientHeight <= 40
  )
    emit("more");
}
const lanes = computed(() => groupRuns(props.runs));
const order: RunLane[] = ["QUEUED", "RUNNING", "WAITING_HUMAN", "TERMINAL"];
</script>

<template>
  <div class="runs-board">
    <div class="runs-board__kanban">
      <section v-for="lane in order" :key="lane" class="runs-lane">
        <header>
          <h2>{{ $t(`workboard.lanes.${lane}`) }}</h2>
          <span>{{ lanes[lane].length }}</span>
        </header>
        <div
          class="runs-lane__body"
          tabindex="0"
          :aria-label="$t(`workboard.lanes.${lane}`)"
          @scroll="onScroll"
        >
          <RunWorkItem
            v-for="run in lanes[lane]"
            :key="run.ref"
            :run="run"
            :preserve-project="preserveProject"
            compact
          />
          <p v-if="lanes[lane].length === 0" class="runs-lane__empty">
            {{ $t("workboard.noRunsInLane") }}
          </p>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.runs-board {
  min-width: 0;
  max-width: 100%;
}
.runs-board__kanban {
  display: grid;
  grid-template-columns: repeat(4, minmax(250px, 1fr));
  gap: 12px;
  overflow-x: auto;
  padding-bottom: 8px;
}
.runs-lane {
  display: flex;
  flex-direction: column;
  min-height: 320px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.runs-lane > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  min-height: 44px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
}
.runs-lane h2 {
  margin: 0;
  font-size: 0.82rem;
}
.runs-lane header span {
  min-width: 24px;
  padding: 2px 7px;
  border-radius: 999px;
  color: var(--muted);
  background: var(--surface);
  font-family: var(--font-mono);
  text-align: center;
}
.runs-lane__body {
  display: grid;
  align-content: start;
  gap: 8px;
  padding: 8px;
  max-height: 1256px;
  box-sizing: border-box;
  overflow-y: auto;
  overscroll-behavior: contain;
  grid-auto-rows: 200px;
}
.runs-lane__body :deep(.run-work-item) {
  grid-template-columns: minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr) auto;
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.runs-lane__body :deep(.run-work-item h3) {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow: hidden;
  font-size: 14px;
  line-height: 1.3;
}
.runs-lane__body :deep(.run-work-item .safe-summary) {
  -webkit-line-clamp: 1;
}
.runs-lane__body :deep(.run-work-item dd) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.runs-lane__body :deep(.run-work-item__aside) {
  justify-content: space-between;
  flex-wrap: wrap;
  min-width: 0;
  gap: 4px;
}
.runs-lane__body :deep(.run-work-item__actors) {
  display: grid;
  gap: 3px;
}
.runs-lane__body :deep(.run-work-item__aside) {
  align-items: flex-start;
  flex-direction: row;
}
.runs-lane__empty {
  margin: 0;
  padding: 26px 10px;
  color: var(--muted);
  text-align: center;
}
</style>
