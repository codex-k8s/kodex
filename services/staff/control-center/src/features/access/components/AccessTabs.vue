<script setup lang="ts">
import type { AccessSection } from "@/features/access/model";

defineProps<{
  active: AccessSection;
  counts: Partial<Record<AccessSection, number>>;
}>();
const emit = defineEmits<{ select: [section: AccessSection] }>();

const sections: AccessSection[] = [
  "participants",
  "groups",
  "roles",
  "bindings",
  "effective",
];
</script>

<template>
  <nav class="access-tabs" :aria-label="$t('access.sections.label')">
    <button
      v-for="section in sections"
      :key="section"
      class="access-tab"
      :class="{ 'access-tab--active': active === section }"
      type="button"
      :aria-current="active === section ? 'page' : undefined"
      @click="emit('select', section)"
    >
      <span>{{ $t(`access.sections.${section}`) }}</span>
      <span v-if="counts[section] !== undefined" class="count-badge">{{
        counts[section]
      }}</span>
    </button>
  </nav>
</template>

<style scoped>
.access-tabs {
  display: flex;
  align-items: stretch;
  gap: 2px;
  margin-bottom: 18px;
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
  scrollbar-width: thin;
}
.access-tab {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 8px;
  min-height: 44px;
  padding: 0 14px;
  border: 0;
  border-bottom: 2px solid transparent;
  color: var(--muted);
  background: transparent;
  cursor: pointer;
}
.access-tab--active {
  border-bottom-color: var(--accent);
  color: var(--text);
  font-weight: 600;
}
@media (max-width: 720px) {
  .access-tabs {
    margin-inline: -16px;
    padding-inline: 16px;
  }
  .access-tab {
    padding-inline: 10px;
  }
}
</style>
