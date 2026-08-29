<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import RoleImageCatalog from "@/features/role-images/RoleImageCatalog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";

const route = useRoute();
const projectRef = computed(() => String(route.params.projectRef));
</script>

<template>
  <PageFrame
    :title="$t('roleImages.title')"
    :subtitle="$t('roleImages.subtitle')"
  >
    <RoleImageCatalog :project-ref="projectRef" />
  </PageFrame>
</template>
