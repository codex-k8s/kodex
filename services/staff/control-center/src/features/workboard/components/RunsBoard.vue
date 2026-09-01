<script setup lang="ts">
import { Columns3, List } from "@lucide/vue";
import { computed } from "vue";

import {
  groupRuns,
  type RunLane,
  type RunView,
} from "@/features/workboard/model";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
import RunWorkItem from "@/features/workboard/components/RunWorkItem.vue";

const props = defineProps<{
  runs: Run[];
  view: RunView;
  preserveProject?: boolean;
}>();
const emit = defineEmits<{ "update:view": [value: RunView] }>();
const lanes = computed(() => groupRuns(props.runs));
const order: RunLane[] = ["QUEUED", "RUNNING", "WAITING_HUMAN", "TERMINAL"];
</script>

<template>
  <div class="runs-board">
    <div class="runs-board__toolbar">
      <div
        class="runs-board__segmented"
        role="group"
        :aria-label="$t('workboard.viewMode')"
      >
        <button
          type="button"
          :aria-pressed="view === 'KANBAN'"
          @click="emit('update:view', 'KANBAN')"
        >
          <Columns3 :size="16" aria-hidden="true" />{{ $t("workboard.kanban") }}
        </button>
        <button
          type="button"
          :aria-pressed="view === 'LIST'"
          @click="emit('update:view', 'LIST')"
        >
          <List :size="16" aria-hidden="true" />{{ $t("workboard.list") }}
        </button>
      </div>
    </div>
    <div v-if="view === 'KANBAN'" class="runs-board__kanban">
      <section v-for="lane in order" :key="lane" class="runs-lane">
        <header>
          <h2>{{ $t(`workboard.lanes.${lane}`) }}</h2>
          <span>{{ lanes[lane].length }}</span>
        </header>
        <div class="runs-lane__body">
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
    <div v-else class="runs-board__list">
      <RunWorkItem
        v-for="run in runs"
        :key="run.ref"
        :run="run"
        :preserve-project="preserveProject"
      />
    </div>
  </div>
</template>

<style scoped>
.runs-board__toolbar {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 12px;
}
.runs-board__segmented {
  display: inline-flex;
  min-height: 36px;
  padding: 3px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.runs-board__segmented button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-width: 94px;
  border: 0;
  border-radius: 6px;
  color: var(--muted);
  background: transparent;
  cursor: pointer;
}
.runs-board__segmented button[aria-pressed="true"] {
  color: var(--accent-strong);
  background: var(--surface);
  box-shadow: 0 1px 2px rgb(16 22 30 / 10%);
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
  border-radius: 10px;
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
}
.runs-lane__body :deep(.run-work-item) {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
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
.runs-board__list {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface);
}
@media (max-width: 700px) {
  .runs-board__toolbar {
    justify-content: stretch;
  }
  .runs-board__segmented,
  .runs-board__segmented button {
    flex: 1;
  }
  .runs-board__kanban {
    grid-template-columns: minmax(0, 1fr);
    overflow-x: hidden;
  }
}
</style>
