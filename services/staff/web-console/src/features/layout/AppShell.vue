<script setup lang="ts">
import { computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';

import { actorTypeOptions, scopeTypeOptions } from '@/shared/api/context';
import { routeNames } from '@/shared/lib/routes';
import { useOperatorContextStore } from '@/features/operator-context/store';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const context = useOperatorContextStore();

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
]);

const currentTitle = computed(() => {
  const titleKey = route.meta.titleKey;
  return typeof titleKey === 'string' ? t(titleKey) : t('navigation.commandCenter');
});

function goTo(routeName: string) {
  void router.push({ name: routeName });
}
</script>

<template>
  <v-app>
    <v-navigation-drawer class="app-drawer" permanent width="260">
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
          v-model="context.actorType"
          class="toolbar-field"
          :items="actorTypeOptions"
          :label="t('context.actorType')"
        />
        <v-text-field
          v-model.trim="context.actorId"
          class="toolbar-field"
          :label="t('context.actorId')"
        />
        <v-select
          v-model="context.scopeType"
          class="toolbar-field"
          :items="scopeTypeOptions"
          :label="t('context.scopeType')"
        />
        <v-text-field
          v-model.trim="context.scopeRef"
          class="toolbar-field"
          :label="t('context.scopeRef')"
        />
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
  margin-left: 24px;
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

.app-content {
  padding: 24px;
}
</style>
