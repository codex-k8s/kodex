<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import RoleImageCatalog from "@/features/role-images/RoleImageCatalog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import { usePlatformStore } from "@/features/platform/store";
import { organizationRuntimeResourceScope } from "@/features/runtime/resource-scope";

const route = useRoute();
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
</script>

<template>
  <PageFrame
    :title="
      projectRef
        ? $t('roleImages.title')
        : $t('assistant.resources.organizationImages')
    "
    :subtitle="
      organizationScope ? $t('assistant.resources.organizationHelp') : undefined
    "
  >
    <RoleImageCatalog
      v-if="projectRef || organizationScope"
      :project-ref="projectRef"
      :organization-scope="organizationScope"
    />
  </PageFrame>
</template>
