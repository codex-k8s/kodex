<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import RoleImageEditor from "@/features/role-images/RoleImageEditor.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";

const route = useRoute();
const projectRef = computed(() => String(route.params.projectRef));
const recipeRef = computed(() => {
  const value = route.params.recipeRef;
  return typeof value === "string" ? value : undefined;
});
</script>

<template>
  <PageFrame
    :title="recipeRef ? $t('roleImages.editorTitle') : $t('roleImages.new')"
    :subtitle="$t('roleImages.editorSubtitle')"
  >
    <template #actions>
      <RouterLink
        class="button"
        :to="`/projects/${encodeURIComponent(projectRef)}/role-images`"
      >
        {{ $t("roleImages.backToCatalog") }}
      </RouterLink>
    </template>
    <RoleImageEditor :project-ref="projectRef" :recipe-ref="recipeRef" />
  </PageFrame>
</template>
