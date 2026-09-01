<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import AutomationsWorkspace from "@/features/automations/AutomationsWorkspace.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";

const route = useRoute();
const projectRef = computed(() => String(route.params.projectRef));
</script>

<template>
  <PageFrame
    :title="$t('automations.title')"
    :subtitle="$t('automations.subtitle')"
  >
    <AutomationsWorkspace :key="projectRef" :project-ref="projectRef" />
  </PageFrame>
</template>
