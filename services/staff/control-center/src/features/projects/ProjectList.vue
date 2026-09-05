<script setup lang="ts">
import {
  ArrowUpRight,
  Bot,
  Files,
  GitBranch,
  Play,
  Workflow,
} from "@lucide/vue";
import type { Project } from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { nearScrollEnd } from "@/shared/ui/async-entity-picker";
defineProps<{ items: Project[]; expanded?: boolean }>();
const emit = defineEmits<{ more: [] }>();
function scroll(event: Event): void {
  if (
    event.currentTarget instanceof HTMLElement &&
    nearScrollEnd(event.currentTarget)
  )
    emit("more");
}
</script>
<template>
  <div
    class="project-list"
    :class="{ 'project-list--expanded': expanded }"
    @scroll="scroll"
  >
    <article
      v-for="project in items"
      :key="project.ref"
      class="project-list__item"
    >
      <div class="project-list__identity">
        <RouterLink :to="`/projects/${encodeURIComponent(project.ref)}`"
          ><strong>{{ project.name }}</strong></RouterLink
        >
        <p>{{ project.purpose }}</p>
      </div>
      <StatusBadge :state="project.lifecycle" />
      <dl>
        <div>
          <dt>{{ $t("project.agents") }}</dt>
          <dd>{{ project.agentCount }}</dd>
        </div>
        <div>
          <dt>{{ $t("project.workflows") }}</dt>
          <dd>{{ project.workflowCount }}</dd>
        </div>
        <div>
          <dt>{{ $t("project.activeRuns") }}</dt>
          <dd>{{ project.activeRunCount }}</dd>
        </div>
        <div>
          <dt>{{ $t("home.pending") }}</dt>
          <dd>{{ project.pendingGateCount }}</dd>
        </div>
      </dl>
      <footer>
        <time
          :datetime="project.updatedAt"
          :title="new Date(project.updatedAt).toLocaleString()"
          >{{ new Date(project.updatedAt).toLocaleDateString() }}</time
        >
        <nav :aria-label="project.name">
          <RouterLink
            class="icon-button"
            :to="`/projects/${encodeURIComponent(project.ref)}`"
            :title="$t('projects.open')"
            :aria-label="$t('projects.open')"
            ><ArrowUpRight :size="18"
          /></RouterLink>
          <RouterLink
            class="icon-button"
            :to="`/projects/${encodeURIComponent(project.ref)}/agents`"
            :title="$t('project.agents')"
            :aria-label="$t('project.agents')"
            ><Bot :size="18"
          /></RouterLink>
          <RouterLink
            class="icon-button"
            :to="`/projects/${encodeURIComponent(project.ref)}/workflows`"
            :title="$t('project.workflows')"
            :aria-label="$t('project.workflows')"
            ><Workflow :size="18"
          /></RouterLink>
          <RouterLink
            class="icon-button"
            :to="`/projects/${encodeURIComponent(project.ref)}/files`"
            :title="$t('nav.files')"
            :aria-label="$t('nav.files')"
            ><Files :size="18"
          /></RouterLink>
          <template v-if="project.nextActions.includes('CREATE_RUN')">
            <RouterLink
              class="icon-button"
              :to="{
                path: `/projects/${encodeURIComponent(project.ref)}/runs/new`,
                query: { targetType: 'AGENT' },
              }"
              :title="$t('projects.runAgent')"
              :aria-label="$t('projects.runAgent')"
              ><Play :size="18"
            /></RouterLink>
            <RouterLink
              class="icon-button"
              :to="{
                path: `/projects/${encodeURIComponent(project.ref)}/runs/new`,
                query: { targetType: 'WORKFLOW' },
              }"
              :title="$t('projects.runWorkflow')"
              :aria-label="$t('projects.runWorkflow')"
              ><GitBranch :size="18"
            /></RouterLink>
          </template>
        </nav>
      </footer>
    </article>
  </div>
</template>
<style scoped>
.project-list {
  --row-height: 246px;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  max-height: calc(6 * var(--row-height));
  overflow: auto;
  min-width: 0;
}
.project-list--expanded {
  max-height: 65dvh;
}
.project-list__item {
  min-height: var(--row-height);
  box-sizing: border-box;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 16px;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  color: inherit;
  text-decoration: none;
}
.project-list__identity {
  min-width: 0;
}
.project-list__identity strong,
.project-list__identity p {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow: hidden;
  overflow-wrap: anywhere;
}
.project-list__identity p {
  color: var(--muted);
  margin: 6px 0 0;
}
dl {
  grid-column: 1 / -1;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin: 0;
}
dt {
  font-size: 12px;
  color: var(--muted);
  overflow-wrap: anywhere;
}
dd {
  margin: 4px 0 0;
  font-weight: 600;
}
@media (max-width: 720px) {
  .project-list {
    grid-template-columns: minmax(0, 1fr);
  }
  .project-list__item {
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 12px;
  }
  dl {
    grid-column: 1 / -1;
  }
}
footer {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
}
footer nav {
  display: flex;
  gap: 2px;
  flex-wrap: wrap;
}
time {
  color: var(--muted);
  font-size: 12px;
}
.project-list__identity a {
  color: inherit;
  text-decoration: none;
}
</style>
