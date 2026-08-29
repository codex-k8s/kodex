<script setup lang="ts">
import { CircleSlash2 } from "@lucide/vue";
import { useI18n } from "vue-i18n";

import {
  roleImageApiGaps,
  type RoleImageApiGap,
} from "@/features/role-images/model";

withDefaults(
  defineProps<{
    gaps?: readonly RoleImageApiGap[];
    compact?: boolean;
  }>(),
  {
    gaps: () => roleImageApiGaps,
    compact: false,
  },
);

const { t } = useI18n();
</script>

<template>
  <section class="contract-gap" :class="{ 'is-compact': compact }" role="note">
    <header>
      <CircleSlash2 :size="18" aria-hidden="true" />
      <div>
        <strong>{{ t("roleImages.unavailable.title") }}</strong>
        <p>{{ t("roleImages.unavailable.description") }}</p>
      </div>
    </header>
    <ul>
      <li v-for="gap in gaps" :key="gap.key">
        <strong>{{ t(`roleImages.unavailable.${gap.key}`) }}</strong>
        <code>{{ gap.contract }}</code>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.contract-gap {
  display: grid;
  gap: 12px;
  padding: 16px;
  border: 1px solid var(--warning-border, var(--border-strong));
  border-radius: 8px;
  background: var(--warning-soft);
}
.contract-gap header {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}
.contract-gap header svg {
  flex: 0 0 auto;
  margin-top: 2px;
}
.contract-gap p {
  margin: 4px 0 0;
  color: var(--text-secondary);
}
.contract-gap ul {
  display: grid;
  gap: 10px;
  padding: 0;
  margin: 0;
  list-style: none;
}
.contract-gap li {
  display: grid;
  gap: 4px;
}
.contract-gap code {
  white-space: normal;
  color: var(--text-secondary);
  font-size: 0.78rem;
  overflow-wrap: anywhere;
}
.is-compact ul {
  gap: 7px;
}
.is-compact code {
  display: none;
}
</style>
