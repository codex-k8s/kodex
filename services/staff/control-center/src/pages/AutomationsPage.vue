<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";

import AutomationsWorkspace from "@/features/automations/AutomationsWorkspace.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";

const route = useRoute();
const projectRef = computed(() => String(route.params.projectRef));
const initialScheduleRef = computed(() =>
  typeof route.query.scheduleRef === "string"
    ? route.query.scheduleRef
    : undefined,
);
</script>

<template>
  <PageFrame
    :title="$t('automations.title')"
    :subtitle="$t('automations.subtitle')"
  >
    <AutomationsWorkspace
      :key="projectRef"
      :project-ref="projectRef"
      :initial-schedule-ref="initialScheduleRef"
    />
  </PageFrame>
</template>
