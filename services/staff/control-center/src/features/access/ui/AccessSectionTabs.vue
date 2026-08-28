<script setup lang="ts">
import {
  Bot,
  KeyRound,
  ShieldCheck,
  UserRoundCog,
  UsersRound,
} from "@lucide/vue";
import { useI18n } from "vue-i18n";

import type { AccessSection } from "@/features/access/ui/model";

defineProps<{
  active: AccessSection;
  memberCount: number;
  roleCount: number;
}>();

const emit = defineEmits<{
  select: [section: AccessSection];
}>();

const { t } = useI18n();
const tabs: Array<{
  key: AccessSection;
  icon: typeof UsersRound;
  count: "members" | "roles" | "unavailable";
}> = [
  { key: "MEMBERS", icon: UsersRound, count: "members" },
  { key: "GROUPS", icon: KeyRound, count: "unavailable" },
  { key: "ROLES", icon: UserRoundCog, count: "roles" },
  { key: "EFFECTIVE", icon: ShieldCheck, count: "unavailable" },
  { key: "AGENT_SCOPE", icon: Bot, count: "unavailable" },
];
</script>

<template>
  <nav
    class="access-tabs"
    role="tablist"
    :aria-label="t('accessRedesign.tabsLabel')"
  >
    <button
      v-for="tab in tabs"
      :key="tab.key"
      class="access-tab"
      :class="{ 'access-tab--active': active === tab.key }"
      type="button"
      role="tab"
      :aria-selected="active === tab.key"
      @click="emit('select', tab.key)"
    >
      <component :is="tab.icon" :size="16" aria-hidden="true" />
      <span>{{ t(`accessRedesign.tabs.${tab.key}`) }}</span>
      <span v-if="tab.count === 'members'" class="tab-count">{{
        memberCount
      }}</span>
      <span v-else-if="tab.count === 'roles'" class="tab-count">{{
        roleCount
      }}</span>
      <span v-else class="tab-count tab-count--locked">—</span>
    </button>
  </nav>
</template>

<style scoped>
.access-tabs {
  display: flex;
  align-items: stretch;
  gap: 2px;
  min-height: 44px;
  margin-bottom: 16px;
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
  scrollbar-width: thin;
}
.access-tab {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 7px;
  min-height: 44px;
  padding: 0 12px;
  border: 0;
  border-bottom: 2px solid transparent;
  color: var(--muted);
  background: transparent;
  cursor: pointer;
  font-weight: 500;
}
.access-tab:hover,
.access-tab:focus-visible {
  color: var(--text);
  background: var(--panel);
}
.access-tab--active {
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
  color: var(--muted);
  background: var(--panel);
  font-family: var(--font-mono);
  font-size: 0.72rem;
}
.access-tab--active .tab-count {
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.tab-count--locked {
  color: var(--subtle);
}
</style>
