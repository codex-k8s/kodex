<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { searchProjects } from "@/features/projects/api";
import { usePlatformStore } from "@/features/platform/store";
import type { Project } from "@/shared/api/generated/openapi/types.gen";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";

const props = defineProps<{
  project?: Project;
  placeholder?: string;
  clearable?: boolean;
}>();
const emit = defineEmits<{ select: [ref: string] }>();
const { t } = useI18n();
const platform = usePlatformStore();
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
  pageSize = 20,
): Promise<AsyncEntityOptionPage> {
  const snapshot = platform.realtimeSnapshot("PROJECT", props.project?.ref);
  if (!query.trim() && !cursor && snapshot) {
    return {
      items: [
        { ref: "__all_projects__", title: t("app.allProjects") },
        ...platform.projectList.map(option),
      ],
      nextPageToken: snapshot.nextPageToken,
      total: snapshot.total === undefined ? undefined : snapshot.total + 1,
    };
  }
  const page = await searchProjects(query, cursor, signal, pageSize);
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
    :clearable="clearable"
    :trigger-label="$t('app.project')"
    :placeholder="placeholder ?? $t('app.allProjects')"
    :search-placeholder="$t('app.chooseProject')"
    :popover-max-height="370"
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
