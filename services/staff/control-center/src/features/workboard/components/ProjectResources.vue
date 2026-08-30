<script setup lang="ts">
import {
  Bot,
  CalendarClock,
  ChevronRight,
  FileStack,
  Layers3,
  Play,
  ShieldQuestion,
  Workflow,
} from "@lucide/vue";

import type {
  Project,
  RuntimeEnvironmentSet,
  Schedule,
} from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

withDefaults(
  defineProps<{
    project: Project;
    schedules: Schedule[];
    environments: RuntimeEnvironmentSet[];
    schedulesReady: boolean;
    environmentsReady: boolean;
    schedulesLoading?: boolean;
    environmentsLoading?: boolean;
    schedulesUnavailable?: boolean;
    environmentsUnavailable?: boolean;
    environmentsTruncated?: boolean;
  }>(),
  {
    schedulesLoading: false,
    environmentsLoading: false,
    schedulesUnavailable: false,
    environmentsUnavailable: false,
    environmentsTruncated: false,
  },
);

const emit = defineEmits<{
  retrySchedules: [];
  retryEnvironments: [];
}>();
</script>

<template>
  <div class="project-resources">
    <section class="project-resource-group">
      <h2>{{ $t("workboard.projectCollections") }}</h2>
      <nav
        class="project-resource-links"
        :aria-label="$t('workboard.projectCollections')"
      >
        <RouterLink :to="`/projects/${project.ref}/agents`">
          <Bot :size="17" aria-hidden="true" />
          <span>
            <strong>{{ $t("project.agents") }}</strong>
            <small>{{
              $t("workboard.resourceCount", { count: project.agentCount })
            }}</small>
          </span>
          <b>{{ project.agentCount }}</b>
        </RouterLink>
        <RouterLink :to="`/projects/${project.ref}/workflows`">
          <Workflow :size="17" aria-hidden="true" />
          <span>
            <strong>{{ $t("project.workflows") }}</strong>
            <small>{{
              $t("workboard.resourceCount", { count: project.workflowCount })
            }}</small>
          </span>
          <b>{{ project.workflowCount }}</b>
        </RouterLink>
        <RouterLink :to="`/projects/${project.ref}/runs`">
          <Play :size="17" aria-hidden="true" />
          <span>
            <strong>{{ $t("runs.title") }}</strong>
            <small>{{ $t("workboard.openCurrentWork") }}</small>
          </span>
          <ChevronRight :size="16" aria-hidden="true" />
        </RouterLink>
        <RouterLink
          :to="`/decisions?projectRef=${encodeURIComponent(project.ref)}`"
        >
          <ShieldQuestion :size="17" aria-hidden="true" />
          <span>
            <strong>{{ $t("project.decisions") }}</strong>
            <small>{{ $t("workboard.openDecisions") }}</small>
          </span>
          <ChevronRight :size="16" aria-hidden="true" />
        </RouterLink>
        <RouterLink :to="`/projects/${project.ref}/files`">
          <FileStack :size="17" aria-hidden="true" />
          <span>
            <strong>{{ $t("files.title") }}</strong>
            <small>{{ $t("workboard.openCollection") }}</small>
          </span>
          <ChevronRight :size="16" aria-hidden="true" />
        </RouterLink>
      </nav>
    </section>

    <section class="project-resource-group">
      <header>
        <span>
          <CalendarClock :size="17" aria-hidden="true" />
          <h2>{{ $t("automations.title") }}</h2>
        </span>
        <RouterLink :to="`/projects/${project.ref}/automations`">
          {{ $t("common.all") }}
        </RouterLink>
      </header>
      <p v-if="schedulesLoading && !schedulesReady" class="resource-state">
        {{ $t("common.loading") }}
      </p>
      <div
        v-else-if="schedulesUnavailable && !schedulesReady"
        class="resource-state resource-state--error"
      >
        <p>{{ $t("workboard.resourceUnavailable") }}</p>
        <button class="button" type="button" @click="emit('retrySchedules')">
          {{ $t("common.retry") }}
        </button>
      </div>
      <p v-else-if="!schedules.length" class="resource-state">
        {{ $t("workboard.noAutomations") }}
      </p>
      <ul v-else class="project-resource-items">
        <li v-for="schedule in schedules.slice(0, 4)" :key="schedule.ref">
          <RouterLink :to="`/projects/${project.ref}/automations`">
            <span>
              <strong>{{ schedule.name }}</strong>
              <small v-if="schedule.nextRunAt">
                {{
                  $t("workboard.nextRun", {
                    date: new Date(schedule.nextRunAt).toLocaleString(),
                  })
                }}
              </small>
              <small v-else>{{ schedule.target.displayName }}</small>
            </span>
            <StatusBadge :state="schedule.state" />
          </RouterLink>
        </li>
      </ul>
    </section>

    <section class="project-resource-group">
      <header>
        <span>
          <Layers3 :size="17" aria-hidden="true" />
          <h2>{{ $t("runtime.environmentsTitle") }}</h2>
        </span>
        <RouterLink :to="`/projects/${project.ref}/environments`">
          {{ $t("common.all") }}
        </RouterLink>
      </header>
      <p
        v-if="environmentsLoading && !environmentsReady"
        class="resource-state"
      >
        {{ $t("common.loading") }}
      </p>
      <div
        v-else-if="environmentsUnavailable && !environmentsReady"
        class="resource-state resource-state--error"
      >
        <p>{{ $t("workboard.resourceUnavailable") }}</p>
        <button class="button" type="button" @click="emit('retryEnvironments')">
          {{ $t("common.retry") }}
        </button>
      </div>
      <p v-else-if="!environments.length" class="resource-state">
        {{ $t("workboard.noEnvironments") }}
      </p>
      <ul v-else class="project-resource-items">
        <li
          v-for="environment in environments.slice(0, 4)"
          :key="environment.ref"
        >
          <RouterLink
            :to="`/projects/${project.ref}/environments/${environment.ref}`"
          >
            <span>
              <strong>{{ environment.name }}</strong>
              <small>{{ environment.description }}</small>
            </span>
            <StatusBadge :state="environment.state" />
          </RouterLink>
        </li>
      </ul>
      <p v-if="environmentsTruncated" class="resource-footnote">
        {{ $t("workboard.moreEnvironments") }}
      </p>
    </section>
  </div>
</template>

<style scoped>
.project-resources,
.project-resource-group {
  display: grid;
  gap: 12px;
}
.project-resource-group {
  gap: 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.project-resource-group > h2,
.project-resource-group > header {
  min-height: 44px;
  margin: 0;
  padding: 10px 12px;
  border-bottom: 1px solid var(--hairline);
  font-size: 0.87rem;
}
.project-resource-group > header,
.project-resource-group > header > span {
  display: flex;
  align-items: center;
  gap: 8px;
}
.project-resource-group > header {
  justify-content: space-between;
}
.project-resource-group > header h2 {
  margin: 0;
  font-size: inherit;
}
.project-resource-group > header svg {
  color: var(--accent-strong);
}
.project-resource-links,
.project-resource-items {
  display: grid;
  margin: 0;
  padding: 0;
  list-style: none;
}
.project-resource-links > a,
.project-resource-items a {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 9px;
  min-height: 54px;
  padding: 9px 12px;
  border-bottom: 1px solid var(--hairline);
  color: inherit;
  text-decoration: none;
}
.project-resource-items a {
  grid-template-columns: minmax(0, 1fr) auto;
}
.project-resource-links > a:last-child,
.project-resource-items li:last-child a {
  border-bottom: 0;
}
.project-resource-links > a:hover,
.project-resource-items a:hover {
  background: var(--panel);
  text-decoration: none;
}
.project-resource-links > a > svg:first-child {
  color: var(--accent-strong);
}
.project-resource-links span,
.project-resource-items span {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.project-resource-links small,
.project-resource-items small,
.resource-footnote {
  overflow: hidden;
  color: var(--muted);
  font-size: 0.75rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.project-resource-links b {
  font-family: var(--font-mono);
}
.resource-state {
  margin: 0;
  padding: 20px 12px;
  color: var(--muted);
  text-align: center;
}
.resource-state--error {
  display: grid;
  justify-items: center;
  gap: 8px;
}
.resource-state--error p {
  margin: 0;
}
.resource-footnote {
  margin: 0;
  padding: 8px 12px;
  border-top: 1px solid var(--hairline);
  white-space: normal;
}
</style>
