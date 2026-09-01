<script setup lang="ts">
import { Clock3, PackageOpen, PlugZap, ShieldCheck } from "@lucide/vue";
import { useI18n } from "vue-i18n";

import type { IntegrationsSection } from "@/features/integrations/ui/model";

defineProps<{
  active: IntegrationsSection;
  connectionCount: number;
  packageCount: number;
  grantCount: number;
}>();

const emit = defineEmits<{
  select: [section: IntegrationsSection];
}>();

const { t } = useI18n();

const tabs: Array<{
  key: IntegrationsSection;
  icon: typeof PlugZap;
  count: "connections" | "packages" | "grants" | "unavailable";
}> = [
  { key: "CONNECTIONS", icon: PlugZap, count: "connections" },
  { key: "CATALOG", icon: PackageOpen, count: "packages" },
  { key: "GRANTS", icon: ShieldCheck, count: "grants" },
  { key: "APPROVALS", icon: Clock3, count: "unavailable" },
];
</script>

<template>
  <nav
    class="integration-tabs"
    role="tablist"
    :aria-label="t('integrationsRedesign.tabsLabel')"
  >
    <button
      v-for="tab in tabs"
      :key="tab.key"
      class="integration-tab"
      :class="{ 'integration-tab--active': active === tab.key }"
      type="button"
      role="tab"
      :aria-selected="active === tab.key"
      @click="emit('select', tab.key)"
    >
      <component :is="tab.icon" :size="16" aria-hidden="true" />
      <span>{{ t(`integrationsRedesign.tabs.${tab.key}`) }}</span>
      <span v-if="tab.count === 'connections'" class="tab-count">{{
        connectionCount
      }}</span>
      <span v-else-if="tab.count === 'packages'" class="tab-count">{{
        packageCount
      }}</span>
      <span v-else-if="tab.count === 'grants'" class="tab-count">{{
        grantCount
      }}</span>
      <span v-else class="tab-count tab-count--locked">—</span>
    </button>
  </nav>
</template>

<style scoped>
.integration-tabs {
  display: flex;
  align-items: stretch;
  gap: 2px;
  min-height: 44px;
  margin-bottom: 16px;
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
  scrollbar-width: thin;
}
.integration-tab {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 7px;
  min-height: 44px;
  padding: 0 13px;
  border: 0;
  border-bottom: 2px solid transparent;
  color: var(--muted);
  background: transparent;
  cursor: pointer;
  font-weight: 500;
}
.integration-tab:hover,
.integration-tab:focus-visible {
  color: var(--text);
  background: var(--panel);
}
.integration-tab--active {
  color: var(--accent-strong);
  border-bottom-color: var(--accent);
}
.tab-count {
  display: inline-grid;
  place-items: center;
  min-width: 20px;
  height: 20px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--panel);
  color: var(--muted);
  font-family: var(--font-mono);
  font-size: 0.72rem;
}
.integration-tab--active .tab-count {
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.tab-count--locked {
  color: var(--subtle);
}
</style>
