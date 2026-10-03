<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import RoleImageEditor from "@/features/role-images/RoleImageEditor.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import { usePlatformStore } from "@/features/platform/store";
import {
  organizationRuntimeResourceScope,
  runtimeResourceCatalogPath,
} from "@/features/runtime/resource-scope";

const route = useRoute();
const assistantForm = computed(() => route.query.assistantForm === "1");
const platform = usePlatformStore();
const projectRef = computed(() =>
  typeof route.params.projectRef === "string"
    ? route.params.projectRef
    : undefined,
);
const organizationScope = computed(() =>
  !projectRef.value
    ? organizationRuntimeResourceScope(platform.bootstrap)
    : undefined,
);
const catalogPath = computed(() =>
  projectRef.value
    ? runtimeResourceCatalogPath(
        { kind: "PROJECT", projectRef: projectRef.value },
        "role-images",
      )
    : organizationScope.value
      ? runtimeResourceCatalogPath(organizationScope.value, "role-images")
      : undefined,
);
const recipeRef = computed(() => {
  const value = route.params.recipeRef;
  return typeof value === "string" ? value : undefined;
});
</script>

<template>
  <Teleport to="#assistant-form-slot" :disabled="!assistantForm" defer>
    <PageFrame
      :title="recipeRef ? $t('roleImages.editorTitle') : $t('roleImages.new')"
      :subtitle="$t('roleImages.editorSubtitle')"
    >
      <template #actions>
        <RouterLink
          v-if="catalogPath"
          class="button"
          :to="{ path: catalogPath, query: route.query }"
        >
          {{ $t("roleImages.backToCatalog") }}
        </RouterLink>
      </template>
      <RoleImageEditor
        v-if="projectRef || organizationScope"
        :project-ref="projectRef"
        :organization-scope="organizationScope"
        :recipe-ref="recipeRef"
      />
    </PageFrame>
  </Teleport>
</template>
