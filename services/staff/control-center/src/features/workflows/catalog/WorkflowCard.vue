<script setup lang="ts">
import { ArrowUpRight, List, Pencil, Play } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import type { Workflow } from "@/shared/api/generated/openapi/types.gen";
import { workflowLaunchReadiness } from "@/features/platform/workflow-launch";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ workflow: Workflow }>();
const { t, locale } = useI18n();
const projectPath = computed(
  () => `/projects/${encodeURIComponent(props.workflow.projectRef)}`,
);
const path = computed(
  () =>
    `${projectPath.value}/workflows/${encodeURIComponent(props.workflow.ref)}`,
);
const readiness = computed(() => workflowLaunchReadiness(props.workflow));
const launchReason = computed(() =>
  !readiness.value
    ? t("workflowLaunch.missing")
    : t(`workflowLaunch.reasons.${readiness.value.reason}`),
);
const counts = [
  "stageCount",
  "uniqueAgentCount",
  "parallelGroupCount",
  "activeRunCount",
  "pendingGateCount",
] as const;
</script>

<template>
  <article class="workflow-card">
    <header>
      <RouterLink
        :to="path"
        class="workflow-card__name"
        :title="workflow.name"
        >{{ workflow.name }}</RouterLink
      >
      <StatusBadge :state="workflow.state" />
    </header>
    <p class="workflow-card__purpose" :title="workflow.purpose">
      {{ workflow.purpose }}
    </p>
    <dl class="workflow-card__metrics">
      <div v-for="metric in counts" :key="metric" :data-metric="metric">
        <dt>{{ t(`entityCards.${metric}`) }}</dt>
        <dd>{{ workflow.cardSummary[metric] }}</dd>
      </div>
      <div data-metric="hasHumanGate">
        <dt>{{ t("entityCards.hasHumanGate") }}</dt>
        <dd>
          {{
            t(
              workflow.cardSummary.hasHumanGate
                ? "entityCards.yes"
                : "entityCards.no",
            )
          }}
        </dd>
      </div>
    </dl>
    <footer>
      <dl class="workflow-card__activity">
        <div data-metric="lastActivityAt">
          <dt>{{ t("entityCards.lastActivityAt") }}</dt>
          <dd>
            <time
              v-if="workflow.cardSummary.lastActivityAt"
              :datetime="workflow.cardSummary.lastActivityAt"
              >{{
                new Date(workflow.cardSummary.lastActivityAt).toLocaleString(
                  locale,
                )
              }}</time
            >
            <span v-else>{{ t("common.noData") }}</span>
          </dd>
        </div>
      </dl>
      <nav :aria-label="workflow.name">
        <RouterLink
          class="icon-button"
          :to="path"
          :aria-label="t('common.open')"
          :title="t('common.open')"
          ><ArrowUpRight :size="18"
        /></RouterLink>
        <RouterLink
          v-if="readiness?.allowedToSubmit"
          class="icon-button"
          :to="{
            path: `${projectPath}/runs/new`,
            query: { targetType: 'WORKFLOW', targetRef: workflow.ref },
          }"
          :aria-label="t('common.launch')"
          :title="t('common.launch')"
          ><Play :size="18"
        /></RouterLink>
        <button
          v-else
          class="icon-button"
          type="button"
          disabled
          :aria-label="t('common.launch')"
          :title="launchReason"
        >
          <Play :size="18" />
        </button>
        <RouterLink
          class="icon-button"
          :to="`${projectPath}/runs`"
          :aria-label="t('nav.runs')"
          :title="t('nav.runs')"
          ><List :size="18"
        /></RouterLink>
        <RouterLink
          v-if="workflow.nextActions.includes('EDIT')"
          class="icon-button"
          :to="`${path}#workflow-editor`"
          :aria-label="t('common.edit')"
          :title="t('common.edit')"
          ><Pencil :size="18"
        /></RouterLink>
      </nav>
    </footer>
  </article>
</template>

<style scoped>
.workflow-card {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 300px;
  gap: 12px;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
header {
  display: flex;
  align-items: center;
  gap: 12px;
}
.workflow-card__name {
  flex: 1;
  min-width: 0;
  color: var(--text);
  font-weight: 600;
  text-decoration: none;
}
.workflow-card__name,
.workflow-card__purpose {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow: hidden;
  overflow-wrap: anywhere;
}
.workflow-card__purpose {
  margin: 0;
  color: var(--muted);
  min-height: 40px;
}
.workflow-card__metrics {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin: 0;
}
dt {
  color: var(--muted);
  font-size: 12px;
  overflow-wrap: anywhere;
}
dd {
  margin: 4px 0 0;
  font-weight: 600;
}
footer {
  margin-top: auto;
  display: flex;
  flex-wrap: wrap;
  align-items: end;
  justify-content: space-between;
  gap: 8px;
}
.workflow-card__activity {
  margin: 0;
  display: grid;
  gap: 3px;
  color: var(--muted);
  font-size: 12px;
}
.workflow-card__activity dd {
  font-weight: 400;
}
nav {
  display: flex;
  gap: 2px;
}
@media (max-width: 600px) {
  .workflow-card__metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
