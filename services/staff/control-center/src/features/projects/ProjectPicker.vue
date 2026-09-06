<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { searchProjects } from "@/features/projects/api";
import type { Project } from "@/shared/api/generated/openapi/types.gen";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";

const props = defineProps<{ project?: Project }>();
const emit = defineEmits<{ select: [ref: string] }>();
const { t } = useI18n();
const selected = computed(() =>
  props.project ? option(props.project) : undefined,
);
function option(project: Project) {
  return {
    ref: project.ref,
    title: project.name,
    description: project.purpose,
    meta: `${t(`states.${project.lifecycle}`)} · ${t("workboard.projectActivity", { runs: project.activeRunCount, gates: project.pendingGateCount })}`,
  };
}
async function loadPage(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  const page = await searchProjects(query, cursor, signal);
  return {
    items: [
      ...(!cursor && !query
        ? [{ ref: "__all_projects__", title: t("app.allProjects") }]
        : []),
      ...page.items.map(option),
    ],
    nextPageToken: page.nextPageToken,
  };
}
</script>
<template>
  <AsyncEntityPicker
    :model-value="project?.ref"
    :selected="selected"
    :load-page="loadPage"
    :trigger-label="$t('app.project')"
    :placeholder="$t('app.allProjects')"
    :search-placeholder="$t('app.chooseProject')"
    @update:model-value="
      emit(
        'select',
        typeof $event === 'string' && $event !== '__all_projects__'
          ? $event
          : '',
      )
    "
  />
</template>
