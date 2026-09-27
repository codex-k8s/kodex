<script setup lang="ts">
import { ArrowRight } from "@lucide/vue";
import { computed, shallowRef } from "vue";
import { useI18n } from "vue-i18n";

import {
  groupRuns,
  runExecutor,
  type RunLane,
} from "@/features/workboard/model";
import type { AppProblem } from "@/shared/api/problem";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
import { runPath } from "@/shared/routes";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import EntityIcon from "@/shared/ui/EntityIcon.vue";
import SafeSummary from "@/shared/ui/SafeSummary.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  runs: Run[];
  hasMore?: boolean;
  loadingMore?: boolean;
  preserveProject?: boolean;
  columns?: Record<
    RunLane,
    { pageToken?: string; loading: boolean; problem?: AppProblem }
  >;
}>();
const emit = defineEmits<{ more: [lane: RunLane] }>();
const { locale, t } = useI18n();
const dateFormatter = computed(
  () =>
    new Intl.DateTimeFormat(locale.value, {
      dateStyle: "medium",
      timeStyle: "short",
    }),
);
const lanes = computed(() => groupRuns(props.runs));
const order: RunLane[] = ["QUEUED", "RUNNING", "WAITING_HUMAN", "TERMINAL"];
const scrollRoot = shallowRef<HTMLElement | null>(null);
const laneSentinels = Object.fromEntries(
  order.map((lane) => [lane, shallowRef<HTMLElement | null>(null)]),
) as Record<RunLane, ReturnType<typeof shallowRef<HTMLElement | null>>>;

function canLoad(lane: RunLane): boolean {
  const column = props.columns?.[lane];
  return column
    ? Boolean(column.pageToken) && !column.loading && !column.problem
    : props.hasMore && !props.loadingMore;
}

const visibleOrder = computed(() =>
  order.filter((lane) => lanes.value[lane].length > 0 || canLoad(lane)),
);

function formattedDate(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.valueOf())
    ? t("common.noData")
    : dateFormatter.value.format(date);
}

function link(run: Run): string {
  return runPath(run.ref, props.preserveProject ? run.projectRef : undefined);
}

for (const lane of order)
  useCursorInfiniteScroll({
    root: scrollRoot,
    sentinel: laneSentinels[lane],
    enabled: () => canLoad(lane),
    loadMore: () => emit("more", lane),
  });
</script>

<template>
  <div ref="scrollRoot" class="runs-board" tabindex="0">
    <table class="runs-board__table">
      <thead>
        <tr>
          <th>{{ t("common.name") }}</th>
          <th>{{ t("workboard.executor") }}</th>
          <th>{{ t("common.source") }}</th>
          <th>{{ t("common.status") }}</th>
          <th>{{ t("runs.createdAt") }}</th>
          <th>
            <span class="sr-only">{{ t("common.actions") }}</span>
          </th>
        </tr>
      </thead>
      <tbody v-for="lane in visibleOrder" :key="lane">
        <tr class="runs-board__group">
          <th colspan="6" scope="rowgroup">
            {{ t(`workboard.lanes.${lane}`) }}
            <span>{{ lanes[lane].length }}</span>
          </th>
        </tr>
        <tr v-for="run in lanes[lane]" :key="run.ref" class="run-table__row">
          <td>
            <div class="runs-board__identity">
              <EntityIcon kind="RUN" :size="16" />
              <div>
                <RouterLink :to="link(run)" :title="run.title">{{
                  run.title
                }}</RouterLink>
                <SafeSummary
                  :content="run.currentActivity ?? run.resultSummary"
                  :fallback="run.target.displayName"
                />
              </div>
            </div>
          </td>
          <td :title="runExecutor(run) ?? t('workboard.executorUnavailable')">
            {{ runExecutor(run) ?? t("workboard.executorUnavailable") }}
          </td>
          <td>
            <div class="runs-board__source">
              <span>{{ t(`runs.source.${run.source}`) }}</span>
              <small :title="run.initiator.displayName">{{
                run.initiator.displayName
              }}</small>
            </div>
          </td>
          <td><StatusBadge :state="run.state" /></td>
          <td>
            <time :datetime="run.createdAt">{{
              formattedDate(run.createdAt)
            }}</time>
          </td>
          <td>
            <RouterLink
              :to="link(run)"
              class="button button--ghost runs-board__action"
              :aria-label="`${t('common.open')}: ${run.title}`"
              :title="t('common.open')"
              ><ArrowRight :size="17" aria-hidden="true"
            /></RouterLink>
          </td>
        </tr>
        <tr
          v-if="canLoad(lane)"
          class="runs-board__sentinel-row"
          aria-hidden="true"
        >
          <td colspan="6">
            <span
              :ref="
                (element) =>
                  (laneSentinels[lane].value = element as HTMLElement | null)
              "
              class="runs-board__sentinel"
            />
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.runs-board {
  width: 100%;
  max-height: min(960px, calc(100vh - 300px));
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.runs-board__table {
  width: 100%;
  min-width: 900px;
  border-collapse: collapse;
  table-layout: fixed;
}
.runs-board__table thead th,
.runs-board__table tbody td {
  padding: 10px 12px;
  border-bottom: 1px solid var(--hairline);
  text-align: left;
  vertical-align: middle;
}
.runs-board__table thead th {
  position: sticky;
  z-index: 2;
  top: 0;
  color: var(--subtle);
  background: var(--panel);
  font-size: 0.72rem;
  font-weight: 600;
}
.runs-board__table thead th:nth-child(1) {
  width: 39%;
}
.runs-board__table thead th:nth-child(2) {
  width: 17%;
}
.runs-board__table thead th:nth-child(3) {
  width: 16%;
}
.runs-board__table thead th:nth-child(4) {
  width: 10%;
}
.runs-board__table thead th:nth-child(5) {
  width: 14%;
}
.runs-board__table thead th:nth-child(6) {
  width: 4%;
}
.runs-board__group th {
  padding: 7px 12px;
  border-bottom: 1px solid var(--hairline);
  color: var(--text);
  background: var(--panel);
  font-size: 0.78rem;
  text-align: left;
}
.runs-board__group span {
  margin-left: 6px;
  color: var(--subtle);
  font-weight: 400;
}
.run-table__row:hover {
  background: var(--panel);
}
.run-table__row td {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.runs-board__identity {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.runs-board__identity > div,
.runs-board__source {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.runs-board__identity a,
.runs-board__identity :deep(.safe-summary),
.runs-board__source span,
.runs-board__source small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.runs-board__identity a {
  font-weight: 600;
  text-decoration: none;
}
.runs-board__identity a:hover {
  color: var(--accent-strong);
  text-decoration: underline;
}
.runs-board__identity :deep(.safe-summary),
.runs-board__source small {
  color: var(--subtle);
  font-size: 0.74rem;
}
.runs-board__table time {
  color: var(--subtle);
  font-size: 0.72rem;
}
.runs-board__action {
  width: 32px;
  height: 32px;
  padding: 0;
}
.runs-board__sentinel-row td {
  padding: 0;
  border: 0;
}
.runs-board__sentinel {
  display: block;
  height: 1px;
}
</style>
