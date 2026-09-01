<script setup lang="ts">
import { AlertTriangle, CalendarClock, ShieldQuestion } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type {
  OwnerGate,
  Project,
  Run,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import { runPath } from "@/shared/routes";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import SafeSummary from "@/shared/ui/SafeSummary.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  gates: OwnerGate[];
  failedRuns: Run[];
  projects: Project[];
  gatesReady: boolean;
  runsReady: boolean;
  gatesLoading?: boolean;
  runsLoading?: boolean;
  gatesProblem?: AppProblem;
  runsProblem?: AppProblem;
  refreshing?: boolean;
}>();
const emit = defineEmits<{ retryGates: []; retryRuns: [] }>();
const { locale, t } = useI18n();

const total = computed(() => props.gates.length + props.failedRuns.length);
const ready = computed(() => props.gatesReady && props.runsReady);
const initialLoading = computed(
  () =>
    (props.gatesLoading && !props.gatesReady) ||
    (props.runsLoading && !props.runsReady),
);
const projectNames = computed(
  () => new Map(props.projects.map((project) => [project.ref, project.name])),
);

function projectName(projectRef: string): string {
  return projectNames.value.get(projectRef) ?? t("common.unavailable");
}

function formatDate(value?: string): string {
  if (!value) return "";
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
</script>

<template>
  <section class="home-attention panel" aria-labelledby="home-attention-title">
    <header class="home-attention__header">
      <div class="home-attention__heading">
        <h2 id="home-attention-title">{{ $t("workboard.attention") }}</h2>
        <span class="home-attention__count">{{ total }}</span>
      </div>
      <span
        v-if="refreshing && ready"
        class="home-attention__refresh"
        role="status"
      >
        <span aria-hidden="true" />{{ $t("workboard.refreshing") }}
      </span>
    </header>

    <div
      v-if="initialLoading"
      class="home-attention__skeleton"
      role="status"
      :aria-label="$t('common.loading')"
    >
      <span /><span /><span />
    </div>
    <div v-else class="home-attention__body">
      <ProblemNotice
        v-if="gatesProblem"
        :problem="gatesProblem"
        @retry="emit('retryGates')"
      />
      <ProblemNotice
        v-if="runsProblem"
        :problem="runsProblem"
        @retry="emit('retryRuns')"
      />

      <div v-if="gates.length" class="home-attention__group">
        <div class="home-attention__group-head">
          <ShieldQuestion :size="18" aria-hidden="true" />
          <h3>{{ $t("home.pending") }}</h3>
          <span>{{ gates.length }}</span>
          <RouterLink to="/decisions">{{ $t("common.all") }}</RouterLink>
        </div>
        <RouterLink
          v-for="gate in gates"
          :key="gate.ref"
          :to="{
            path: runPath(gate.runRef, gate.projectRef),
            query: { nodeRef: gate.nodeRef },
          }"
          class="home-attention__item"
        >
          <div class="home-attention__copy">
            <h4>{{ gate.title }}</h4>
            <SafeSummary :content="gate.contextSummary" />
            <p>
              <span>{{ projectName(gate.projectRef) }}</span>
              <span
                >{{ $t("workboard.initiator") }}:
                {{ gate.requestedBy.displayName }}</span
              >
            </p>
          </div>
          <div class="home-attention__aside">
            <StatusBadge :state="gate.state" tone="warning" />
            <time v-if="gate.expiresAt" :datetime="gate.expiresAt">
              <CalendarClock :size="13" aria-hidden="true" />{{
                formatDate(gate.expiresAt)
              }}
            </time>
          </div>
        </RouterLink>
      </div>

      <div v-if="failedRuns.length" class="home-attention__group">
        <div
          class="home-attention__group-head home-attention__group-head--danger"
        >
          <AlertTriangle :size="18" aria-hidden="true" />
          <h3>{{ $t("runs.title") }} · {{ $t("workboard.attention") }}</h3>
          <span>{{ failedRuns.length }}</span>
          <RouterLink to="/runs">{{ $t("common.all") }}</RouterLink>
        </div>
        <RouterLink
          v-for="run in failedRuns"
          :key="run.ref"
          :to="runPath(run.ref, run.projectRef)"
          class="home-attention__item"
        >
          <div class="home-attention__copy">
            <h4>{{ run.title }}</h4>
            <SafeSummary
              :content="run.safeErrorMessage ?? run.resultSummary"
              :fallback="run.activitySummary"
            />
            <p>
              <span>{{ projectName(run.projectRef) }}</span>
              <span>{{ run.target.displayName }}</span>
            </p>
          </div>
          <div class="home-attention__aside">
            <StatusBadge :state="run.state" tone="danger" />
            <time :datetime="run.finishedAt ?? run.createdAt">
              {{ formatDate(run.finishedAt ?? run.createdAt) }}
            </time>
          </div>
        </RouterLink>
      </div>

      <p
        v-if="ready && !gatesProblem && !runsProblem && total === 0"
        class="home-attention__empty"
      >
        {{ $t("workboard.noAttention") }}
      </p>
    </div>
  </section>
</template>

<style scoped>
.home-attention {
  margin-top: 16px;
  overflow: hidden;
}
.home-attention__header,
.home-attention__heading,
.home-attention__refresh,
.home-attention__group-head,
.home-attention__aside time {
  display: flex;
  align-items: center;
}
.home-attention__header {
  justify-content: space-between;
  gap: 12px;
  min-height: 54px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--hairline);
}
.home-attention__heading,
.home-attention__refresh,
.home-attention__group-head,
.home-attention__aside time {
  gap: 8px;
}
.home-attention__heading h2,
.home-attention__group-head h3,
.home-attention__item h4,
.home-attention__item p {
  margin: 0;
}
.home-attention__heading h2 {
  font-size: 0.98rem;
}
.home-attention__count,
.home-attention__group-head > span {
  min-width: 24px;
  padding: 2px 7px;
  border-radius: 999px;
  color: var(--muted);
  background: var(--hairline);
  font-family: var(--font-mono);
  font-size: 0.72rem;
  text-align: center;
}
.home-attention__refresh {
  color: var(--muted);
  font-size: 0.75rem;
}
.home-attention__refresh > span {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--accent);
  animation: home-attention-pulse 1.2s ease-in-out infinite;
}
.home-attention__body {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0;
}
.home-attention__body > :deep(.problem-notice) {
  grid-column: 1 / -1;
  margin: 12px 16px 0;
}
.home-attention__group {
  min-width: 0;
}
.home-attention__group + .home-attention__group {
  border-left: 1px solid var(--hairline);
}
.home-attention__group-head {
  min-height: 44px;
  padding: 9px 16px;
  border-bottom: 1px solid var(--hairline);
  color: var(--warning);
}
.home-attention__group-head--danger {
  color: var(--danger);
}
.home-attention__group-head h3 {
  color: var(--text);
  font-size: 0.84rem;
}
.home-attention__group-head a {
  margin-left: auto;
  font-size: 0.78rem;
}
.home-attention__item {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 14px;
  min-height: 92px;
  padding: 13px 16px;
  border-bottom: 1px solid var(--hairline);
  color: inherit;
  text-decoration: none;
}
.home-attention__item:last-child {
  border-bottom: 0;
}
.home-attention__item:hover {
  background: var(--panel);
  text-decoration: none;
}
.home-attention__copy {
  min-width: 0;
}
.home-attention__copy h4 {
  margin-bottom: 4px;
  overflow-wrap: anywhere;
}
.home-attention__copy :deep(.safe-summary) {
  margin: 0 0 8px;
  color: var(--muted);
}
.home-attention__copy > p {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  color: var(--muted);
  font-size: 0.75rem;
}
.home-attention__aside {
  display: flex;
  align-items: flex-end;
  flex-direction: column;
  justify-content: space-between;
  gap: 10px;
}
.home-attention__aside time {
  color: var(--muted);
  font-size: 0.72rem;
  white-space: nowrap;
}
.home-attention__empty {
  grid-column: 1 / -1;
  margin: 0;
  padding: 32px 16px;
  color: var(--muted);
  text-align: center;
}
.home-attention__skeleton {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  padding: 16px;
}
.home-attention__skeleton span {
  height: 76px;
  border-radius: 8px;
  background: var(--hairline);
}
.home-attention__skeleton span:last-child {
  grid-column: 1 / -1;
}
@keyframes home-attention-pulse {
  50% {
    opacity: 0.35;
  }
}
@media (prefers-reduced-motion: reduce) {
  .home-attention__refresh > span {
    animation: none;
  }
}
@media (max-width: 900px) {
  .home-attention__body,
  .home-attention__skeleton {
    grid-template-columns: minmax(0, 1fr);
  }
  .home-attention__group + .home-attention__group {
    border-top: 1px solid var(--hairline);
    border-left: 0;
  }
  .home-attention__skeleton span:last-child {
    grid-column: auto;
  }
}
@media (max-width: 620px) {
  .home-attention__header {
    align-items: flex-start;
  }
  .home-attention__item {
    grid-template-columns: minmax(0, 1fr);
  }
  .home-attention__aside {
    align-items: flex-start;
    flex-direction: row;
  }
}
</style>
