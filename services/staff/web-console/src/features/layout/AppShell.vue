<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import { useDisplay } from 'vuetify';

import { actorTypeOptions, isLocalDevActorHeadersEnabled, scopeTypeOptions } from '@/shared/api/context';
import { routeNames } from '@/shared/lib/routes';
import { useOperatorContextStore } from '@/features/operator-context/store';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const context = useOperatorContextStore();
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
]);

const currentTitle = computed(() => {
  const titleKey = route.meta.titleKey;
  return typeof titleKey === 'string' ? t(titleKey) : t('navigation.commandCenter');
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
