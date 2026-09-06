<script setup lang="ts">
import { searchProjects } from "@/features/projects/api";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";
const model = defineModel<string>({ required: true });
async function loadPage(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  const page = await searchProjects(query, cursor, signal);
  return {
    items: page.items.map((project) => ({
      ref: project.ref,
      title: project.name,
      description: project.purpose,
    })),
    nextPageToken: page.nextPageToken,
  };
}
</script>
<template>
  <AsyncEntityPicker
    :model-value="model"
    :load-page="loadPage"
    :trigger-label="$t('decisions.projectFilter')"
    :placeholder="$t('decisions.allProjects')"
    :search-placeholder="$t('common.search')"
    @update:model-value="model = typeof $event === 'string' ? $event : ''"
  />
</template>
