<script setup lang="ts">
import {
  ArrowUpRight,
  Bot,
  Files,
  GitBranch,
  Flame,
  Play,
  RotateCcw,
  Trash2,
  Workflow,
} from "@lucide/vue";
import type { Project } from "@/shared/api/generated/openapi/types.gen";
import EntityIcon from "@/shared/ui/EntityIcon.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
withDefaults(defineProps<{ items: Project[]; trashed?: boolean }>(), {
  trashed: false,
});
const emit = defineEmits<{
  trash: [project: Project];
  restore: [project: Project];
  purge: [project: Project];
}>();
</script>
<template>
  <div class="project-list">
    <table
      class="project-list__table"
      :class="{ 'project-list__table--trash': trashed }"
    >
      <thead>
        <tr>
          <th scope="col">{{ $t("common.name") }}</th>
          <template v-if="!trashed">
            <th scope="col">{{ $t("projects.tableResources") }}</th>
            <th scope="col">{{ $t("projects.tableWork") }}</th>
          </template>
          <th scope="col">{{ $t("common.status") }}</th>
          <th scope="col">
            {{
              $t(trashed ? "projects.purgeAfter" : "entityCards.lastActivityAt")
            }}
          </th>
          <th scope="col">{{ $t("common.actions") }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="project in items"
          :key="project.ref"
          class="project-list__item"
        >
          <td>
            <div class="project-list__identity">
              <EntityIcon kind="PROJECT" />
              <div>
                <strong v-if="trashed" :title="project.name">{{
                  project.name
                }}</strong>
                <RouterLink
                  v-else
                  :to="`/projects/${encodeURIComponent(project.ref)}`"
                  :title="project.name"
                  >{{ project.name }}</RouterLink
                >
                <small v-if="project.purpose" :title="project.purpose">{{
                  project.purpose
                }}</small>
              </div>
            </div>
          </td>
          <template v-if="!trashed">
            <td>
              <div class="project-list__facts">
                <span
                  >{{ $t("project.agents") }}:
                  <strong>{{ project.agentCount }}</strong></span
                >
                <span
                  >{{ $t("project.workflows") }}:
                  <strong>{{ project.workflowCount }}</strong></span
                >
                <span data-metric="integrationState"
                  >{{ $t("entityCards.integrationState") }}:
                  <strong>{{
                    $t(`entityCards.integration.${project.integrationState}`)
                  }}</strong></span
                >
              </div>
            </td>
            <td>
              <div class="project-list__facts">
                <span
                  >{{ $t("project.activeRuns") }}:
                  <strong>{{ project.activeRunCount }}</strong></span
                >
                <span
                  >{{ $t("home.pending") }}:
                  <strong>{{ project.pendingGateCount }}</strong></span
                >
              </div>
            </td>
          </template>
          <td><StatusBadge :state="project.lifecycle" /></td>
          <td>
            <time
              v-if="
                trashed && project.lifecycle === 'TRASHED' && project.purgeAfter
              "
              :datetime="project.purgeAfter"
              >{{
                new Date(project.purgeAfter).toLocaleString($i18n.locale)
              }}</time
            >
            <span v-else-if="trashed">{{ $t("projects.purgePending") }}</span>
            <time
              v-else-if="project.lastActivityAt"
              data-metric="lastActivityAt"
              :datetime="project.lastActivityAt"
              >{{
                new Date(project.lastActivityAt).toLocaleString($i18n.locale)
              }}</time
            >
            <span v-else data-metric="lastActivityAt">{{
              $t("common.noData")
            }}</span>
          </td>
          <td>
            <nav
              v-if="trashed"
              class="project-list__actions"
              :aria-label="project.name"
            >
              <button
                v-if="project.nextActions.includes('RESTORE')"
                class="icon-button"
                type="button"
                :title="$t('projects.restore')"
                :aria-label="$t('projects.restore')"
                @click="emit('restore', project)"
              >
                <RotateCcw :size="18" aria-hidden="true" />
              </button>
              <button
                v-if="project.nextActions.includes('PURGE')"
                class="icon-button icon-button--danger"
                type="button"
                :title="$t('projects.purge')"
                :aria-label="$t('projects.purge')"
                @click="emit('purge', project)"
              >
                <Flame :size="18" aria-hidden="true" />
              </button>
            </nav>
            <nav
              v-else
              class="project-list__actions"
              :aria-label="project.name"
            >
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
              <button
                v-if="project.nextActions.includes('DELETE')"
                class="icon-button icon-button--danger"
                type="button"
                :title="$t('projects.trashProject')"
                :aria-label="$t('projects.trashProject')"
                @click="emit('trash', project)"
              >
                <Trash2 :size="18" aria-hidden="true" />
              </button>
            </nav>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
<style scoped>
.project-list {
  min-width: 0;
  overflow-x: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.project-list__table {
  width: 100%;
  min-width: 1050px;
  table-layout: fixed;
  border-collapse: collapse;
}
.project-list__table th,
.project-list__table td {
  padding: 10px 12px;
  text-align: left;
  vertical-align: middle;
}
.project-list__table th {
  color: var(--muted);
  font-size: 0.72rem;
  font-weight: 600;
}
.project-list__table th:first-child {
  width: 29%;
}
.project-list__table th:nth-child(2) {
  width: 18%;
}
.project-list__table th:nth-child(3) {
  width: 13%;
}
.project-list__table th:nth-child(4) {
  width: 10%;
}
.project-list__table th:nth-child(5) {
  width: 12%;
}
.project-list__table th:last-child {
  width: 18%;
}
.project-list__table--trash th:first-child {
  width: 50%;
}
.project-list__table--trash th:nth-child(2) {
  width: 16%;
}
.project-list__table--trash th:nth-child(3) {
  width: 24%;
}
.project-list__table--trash th:last-child {
  width: 10%;
}
.project-list__item + .project-list__item {
  border-top: 1px solid var(--border);
}
.project-list__identity {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.project-list__identity > div {
  min-width: 0;
  display: grid;
  gap: 3px;
}
.project-list__identity a,
.project-list__identity strong {
  color: inherit;
  font-weight: 600;
  text-decoration: none;
}
.project-list__identity a,
.project-list__identity strong,
.project-list__identity small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.project-list__identity small,
.project-list__facts {
  color: var(--muted);
  font-size: 12px;
}
.project-list__facts {
  display: grid;
  gap: 3px;
}
.project-list__facts strong {
  color: var(--text);
  font-weight: 600;
}
.project-list__actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 2px;
  white-space: nowrap;
}
.icon-button--danger {
  color: var(--danger);
}
</style>
