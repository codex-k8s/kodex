<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { useI18n } from 'vue-i18n';

import { isGatewayActorContextReady } from '@/shared/api/context';
import { useOperatorContextStore } from '@/features/operator-context/store';
import { useProjectContextStore } from '@/features/project-context/store';
import { compactRef } from '@/shared/lib/format';
import ApiErrorAlert from '@/shared/ui/ApiErrorAlert.vue';
import EmptyState from '@/shared/ui/EmptyState.vue';
import StatusChip from '@/shared/ui/StatusChip.vue';

const { t } = useI18n();
const context = useOperatorContextStore();
const projectContext = useProjectContextStore();

const canLoad = computed(() => isGatewayActorContextReady(context.asContext) && !projectContext.isLoadingProjects);
const selectedProject = computed(() => projectContext.selectedProject);
const selectedRepository = computed(() => projectContext.selectedRepository);

onMounted(() => {
  if (canLoad.value && projectContext.projects.length === 0) {
    void projectContext.loadProjects(context.asContext).then(applyProjectScope);
  }
});

async function reload() {
  if (!canLoad.value) {
    return;
  }
  await projectContext.loadProjects(context.asContext);
  applyProjectScope();
}

async function selectProject(projectId: string) {
  if (!isGatewayActorContextReady(context.asContext)) {
    return;
  }
  await projectContext.selectProject(context.asContext, projectId);
  applyProjectScope();
}

function selectRepository(repositoryId: string) {
  projectContext.selectRepository(repositoryId);
  context.scopeType = 'repository';
  context.scopeRef = repositoryId;
}

function applyProjectScope() {
  if (!projectContext.selectedProjectId) {
    return;
  }
  context.scopeType = 'project';
  context.scopeRef = projectContext.selectedProjectId;
}
</script>

<template>
  <div class="page-grid">
    <header class="page-header">
      <div>
        <h1>{{ t('projects.title') }}</h1>
        <p>{{ t('projects.description') }}</p>
      </div>
      <v-btn
        color="primary"
        prepend-icon="mdi-refresh"
        :disabled="!canLoad"
        :loading="projectContext.isLoadingProjects"
        @click="reload"
      >
        {{ t('app.refresh') }}
      </v-btn>
    </header>

    <v-alert v-if="!isGatewayActorContextReady(context.asContext)" type="warning" variant="tonal">
      {{ t('context.actorMissing') }}
    </v-alert>
    <ApiErrorAlert :error="projectContext.error" :retry-label="t('app.retry')" @retry="reload" />

    <section class="projects-layout">
      <v-card class="surface-panel pa-5">
        <div class="section-title">{{ t('projects.projectList') }}</div>
        <v-progress-linear v-if="projectContext.isLoadingProjects" class="mt-4" indeterminate color="primary" />
        <div v-if="projectContext.projects.length > 0" class="summary-list">
          <button
            v-for="project in projectContext.projects"
            :key="project.project_id"
            class="summary-list__item summary-list__button"
            :class="{ 'summary-list__button--selected': project.project_id === projectContext.selectedProjectId }"
            type="button"
            @click="selectProject(project.project_id)"
          >
            <div class="summary-list__main">
              <div class="item-title">{{ project.display_name }}</div>
              <div class="meta-text">{{ project.slug }} · {{ compactRef(project.project_id) }}</div>
              <p v-if="project.description" class="safe-summary">{{ project.description }}</p>
            </div>
            <StatusChip :label="t(`statuses.${project.status}`)" :tone="project.status === 'active' ? 'success' : 'neutral'" />
          </button>
        </div>
        <EmptyState v-else icon="mdi-folder-outline" :title="t('projects.noProjects')" />
      </v-card>

      <v-card class="surface-panel pa-5">
        <div class="section-title">{{ t('projects.repositoryList') }}</div>
        <v-progress-linear v-if="projectContext.isLoadingRepositories" class="mt-4" indeterminate color="primary" />
        <div v-if="projectContext.repositories.length > 0" class="summary-list">
          <button
            v-for="repository in projectContext.repositories"
            :key="repository.repository_id"
            class="summary-list__item summary-list__button"
            :class="{ 'summary-list__button--selected': repository.repository_id === projectContext.selectedRepositoryId }"
            type="button"
            @click="selectRepository(repository.repository_id)"
          >
            <div class="summary-list__main">
              <div class="item-title">{{ repository.provider_owner }}/{{ repository.provider_name }}</div>
              <div class="meta-text">
                {{ t('projects.defaultBranch') }}: {{ repository.default_branch }} · {{ compactRef(repository.repository_id) }}
              </div>
              <div class="ref-chip-row ref-chip-row--compact">
                <v-chip size="small" variant="tonal" color="info" label>{{ repository.provider }}</v-chip>
                <v-chip v-if="repository.provider_repository_id" size="small" variant="tonal" color="info" label>
                  provider / {{ compactRef(repository.provider_repository_id) }}
                </v-chip>
              </div>
            </div>
            <StatusChip :label="t(`statuses.${repository.status}`)" :tone="repository.status === 'active' ? 'success' : 'warning'" />
          </button>
        </div>
        <EmptyState v-else icon="mdi-source-repository" :title="t('projects.noRepositories')" />
      </v-card>
    </section>

    <section class="projects-detail-grid">
      <v-card class="surface-panel pa-5">
        <div class="section-title">{{ t('projects.selectedContext') }}</div>
        <div class="detail-grid mt-4">
          <div>
            <div class="meta-text">{{ t('context.project') }}</div>
            <div>{{ selectedProject?.display_name ?? t('app.unavailable') }}</div>
          </div>
          <div>
            <div class="meta-text">{{ t('context.repository') }}</div>
            <div>
              <template v-if="selectedRepository">
                {{ selectedRepository.provider_owner }}/{{ selectedRepository.provider_name }}
              </template>
              <template v-else>{{ t('projects.projectScopeActive') }}</template>
            </div>
          </div>
          <div>
            <div class="meta-text">{{ t('context.title') }}</div>
            <div>{{ t(`context.scopeLabels.${context.scopeType}`) }} / {{ compactRef(context.scopeRef) }}</div>
          </div>
        </div>
      </v-card>

      <v-card class="surface-panel pa-5">
        <div class="section-title">{{ t('projects.workspaceTitle') }}</div>
        <EmptyState
          class="mt-4"
          icon="mdi-briefcase-search-outline"
          :title="t('projects.workspaceUnavailable')"
          :text="t('projects.workspaceText')"
        />
      </v-card>
    </section>
  </div>
</template>

<style scoped>
.page-header {
  align-items: flex-start;
  display: flex;
  gap: 16px;
  justify-content: space-between;
}

.page-header h1 {
  color: #121826;
  font-size: 1.8rem;
  line-height: 1.2;
  margin: 0;
}

.page-header p {
  color: #667085;
  margin: 8px 0 0;
}

.projects-layout,
.projects-detail-grid {
  display: grid;
  gap: 16px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.summary-list {
  display: grid;
  gap: 10px;
  margin-top: 16px;
}

.summary-list__item {
  align-items: flex-start;
  background: #ffffff;
  border: 1px solid #e4e7ec;
  border-radius: 8px;
  display: flex;
  gap: 12px;
  justify-content: space-between;
  padding: 12px;
  text-align: left;
  width: 100%;
}

.summary-list__button {
  cursor: pointer;
}

.summary-list__button--selected {
  border-color: #ff5a14;
  box-shadow: 0 0 0 2px rgb(255 90 20 / 12%);
}

.summary-list__main {
  display: grid;
  gap: 6px;
  min-width: 0;
}

.safe-summary {
  color: #475467;
  line-height: 1.5;
  margin: 0;
}

.detail-grid {
  display: grid;
  gap: 14px;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
}

.ref-chip-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.ref-chip-row--compact {
  gap: 6px;
  margin-top: 4px;
}

@media (max-width: 980px) {
  .projects-layout,
  .projects-detail-grid {
    grid-template-columns: 1fr;
  }
}
</style>
