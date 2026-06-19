<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import { useDisplay } from 'vuetify';

import { actorTypeOptions, isGatewayActorContextReady, isLocalDevActorHeadersEnabled } from '@/shared/api/context';
import { routeNames } from '@/shared/lib/routes';
import { useOperatorContextStore } from '@/features/operator-context/store';
import { useProjectContextStore } from '@/features/project-context/store';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const context = useOperatorContextStore();
const projectContext = useProjectContextStore();
const localDevActorHeaders = isLocalDevActorHeadersEnabled();
const { mdAndUp } = useDisplay();
const drawerOpen = ref(mdAndUp.value);

const navItems = computed(() => [
  {
    title: t('navigation.commandCenter'),
    icon: 'mdi-view-dashboard-outline',
    routeName: routeNames.commandCenter,
  },
  {
    title: t('navigation.ownerInbox'),
    icon: 'mdi-inbox-outline',
    routeName: routeNames.ownerInbox,
  },
  {
    title: t('navigation.executions'),
    icon: 'mdi-pulse',
    routeName: routeNames.executions,
  },
  {
    title: t('navigation.projects'),
    icon: 'mdi-source-repository-multiple',
    routeName: routeNames.projects,
  },
]);

const currentTitle = computed(() => {
  const titleKey = route.meta.titleKey;
  return typeof titleKey === 'string' ? t(titleKey) : t('navigation.commandCenter');
});
const canLoadProjectContext = computed(() => isGatewayActorContextReady(context.asContext));
const projectItems = computed(() =>
  projectContext.projects.map((project) => ({
    title: project.display_name,
    subtitle: project.slug,
    value: project.project_id,
  })),
);
const repositoryItems = computed(() =>
  projectContext.repositories.map((repository) => ({
    title: `${repository.provider_owner}/${repository.provider_name}`,
    subtitle: repository.default_branch,
    value: repository.repository_id,
  })),
);
const selectedProjectId = computed({
  get: () => projectContext.selectedProjectId,
  set: (projectId: string) => {
    void selectProject(projectId);
  },
});
const selectedRepositoryId = computed({
  get: () => projectContext.selectedRepositoryId || undefined,
  set: (repositoryId: string | undefined) => {
    selectRepository(repositoryId);
  },
});
const contextLabel = computed(() => {
  if (context.scopeRef.trim() === '') {
    return t('context.notSelected');
  }
  return `${t(`context.scopeLabels.${context.scopeType}`)} / ${context.scopeRef}`;
});

onMounted(() => {
  void loadProjectContext();
});

function goTo(routeName: string) {
  void router.push({ name: routeName });
  if (!mdAndUp.value) {
    drawerOpen.value = false;
  }
}

watch(mdAndUp, (isDesktop) => {
  drawerOpen.value = isDesktop;
});

watch(
  () => [context.localDevActorType, context.localDevActorId],
  () => {
    void loadProjectContext();
  },
);

async function loadProjectContext() {
  if (!canLoadProjectContext.value || projectContext.isLoadingProjects) {
    return;
  }
  if (context.scopeType === 'project' && context.scopeRef.trim()) {
    projectContext.selectedProjectId = context.scopeRef.trim();
  }
  await projectContext.loadProjects(context.asContext);
  applySelectedProjectScope();
}

async function selectProject(projectId: string) {
  if (!projectId || !canLoadProjectContext.value) {
    return;
  }
  await projectContext.selectProject(context.asContext, projectId);
  applySelectedProjectScope();
}

function selectRepository(repositoryId: string | undefined) {
  projectContext.selectRepository(repositoryId);
  if (repositoryId) {
    context.scopeType = 'repository';
    context.scopeRef = repositoryId;
    return;
  }
  applySelectedProjectScope();
}

function applySelectedProjectScope() {
  if (!projectContext.selectedProjectId) {
    return;
  }
  context.scopeType = 'project';
  context.scopeRef = projectContext.selectedProjectId;
}
</script>

<template>
  <v-app>
    <v-navigation-drawer
      v-model="drawerOpen"
      class="app-drawer"
      :permanent="mdAndUp"
      :temporary="!mdAndUp"
      width="260"
    >
      <div class="brand-row">
        <div class="brand-mark">K</div>
        <div class="brand-text">{{ t('app.name') }}</div>
      </div>

      <v-list nav density="comfortable" class="px-3">
        <v-list-item
          v-for="item in navItems"
          :key="item.routeName"
          :active="route.name === item.routeName"
          :prepend-icon="item.icon"
          :title="item.title"
          rounded="lg"
          @click="goTo(item.routeName)"
        />
      </v-list>

      <template #append>
        <v-list density="comfortable" class="px-3 pb-4">
          <v-list-item prepend-icon="mdi-help-circle-outline" :title="t('navigation.help')" />
          <v-list-item prepend-icon="mdi-cog-outline" :title="t('navigation.settings')" />
        </v-list>
      </template>
    </v-navigation-drawer>

    <v-app-bar class="app-bar" flat height="72">
      <v-app-bar-nav-icon v-if="!mdAndUp" :aria-label="t('navigation.menu')" @click="drawerOpen = true" />
      <div class="app-bar__title">{{ currentTitle }}</div>
      <v-spacer />
      <v-text-field
        class="app-bar__search"
        density="compact"
        prepend-inner-icon="mdi-magnify"
        :placeholder="t('app.search')"
        readonly
      />
      <v-chip class="ml-3" color="success" label variant="tonal">
        <v-icon icon="mdi-circle" size="10" start />
        {{ t('app.online') }}
      </v-chip>
    </v-app-bar>

    <v-main>
      <div class="context-strip">
        <v-select
          v-model="selectedProjectId"
          class="toolbar-field"
          :items="projectItems"
          :label="t('context.project')"
          :loading="projectContext.isLoadingProjects"
          :disabled="!canLoadProjectContext"
        />
        <v-select
          v-model="selectedRepositoryId"
          class="toolbar-field"
          :items="repositoryItems"
          :label="t('context.repository')"
          :loading="projectContext.isLoadingRepositories"
          :disabled="!projectContext.selectedProjectId"
          clearable
        />
        <v-chip class="toolbar-status" color="info" label variant="tonal">
          <v-icon icon="mdi-crosshairs-gps" start />
          {{ contextLabel }}
        </v-chip>
        <v-chip v-if="!localDevActorHeaders" class="toolbar-status" color="info" label variant="tonal">
          <v-icon icon="mdi-shield-account-outline" start />
          {{ t('context.trustedActorBoundary') }}
        </v-chip>
        <template v-else>
          <v-select
            v-model="context.localDevActorType"
            class="toolbar-field"
            :items="actorTypeOptions"
            :label="t('context.localDevActorType')"
          />
          <v-text-field
            v-model.trim="context.localDevActorId"
            class="toolbar-field"
            :label="t('context.localDevActorId')"
          />
          <v-chip class="toolbar-status" color="warning" label variant="tonal">
            <v-icon icon="mdi-alert-outline" start />
            {{ t('context.localDevActorBoundary') }}
          </v-chip>
        </template>
      </div>
      <div class="app-content">
        <RouterView />
      </div>
    </v-main>
  </v-app>
</template>

<style scoped>
.app-drawer {
  border-right: 1px solid #e4e7ec;
}

.brand-row {
  align-items: center;
  display: flex;
  gap: 10px;
  height: 72px;
  padding: 0 24px;
}

.brand-mark {
  align-items: center;
  background: #ff5a14;
  border-radius: 8px;
  color: #ffffff;
  display: inline-flex;
  font-weight: 800;
  height: 32px;
  justify-content: center;
  width: 32px;
}

.brand-text {
  color: #121826;
  font-size: 1.4rem;
  font-weight: 800;
}

.app-bar {
  border-bottom: 1px solid #e4e7ec;
}

.app-bar__title {
  color: #121826;
  font-size: 1rem;
  font-weight: 700;
  margin-left: 18px;
}

.app-bar__search {
  max-width: 520px;
}

.context-strip {
  align-items: center;
  background: #ffffff;
  border-bottom: 1px solid #e4e7ec;
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  min-height: 72px;
  padding: 14px 24px;
}

.toolbar-field {
  max-width: 260px;
}

.toolbar-status {
  min-height: 40px;
}

.app-content {
  padding: 24px;
}

@media (max-width: 780px) {
  .app-bar__search {
    display: none;
  }

  .context-strip {
    align-items: stretch;
    padding: 12px 16px;
  }

  .toolbar-field {
    max-width: none;
    min-width: 100%;
  }

  .app-content {
    padding: 16px;
  }
}
</style>
