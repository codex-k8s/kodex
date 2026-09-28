<script setup lang="ts">
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";

import AutomationsWorkspace from "@/features/automations/AutomationsWorkspace.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";

const route = useRoute();
const assistantForm = computed(() => route.query.assistantForm === "1");
const router = useRouter();
const projectRef = computed(() => String(route.params.projectRef));
const initialScheduleRef = computed(() =>
  typeof route.query.scheduleRef === "string"
    ? route.query.scheduleRef
    : undefined,
);
function selectSchedule(scheduleRef: string): void {
  if (route.query.scheduleRef === scheduleRef) return;
  void router.replace({ query: { ...route.query, scheduleRef } });
}
</script>

<template>
  <Teleport to="#assistant-form-slot" :disabled="!assistantForm" defer>
    <PageFrame
      :title="$t('automations.title')"
      :subtitle="$t('automations.subtitle')"
    >
      <AutomationsWorkspace
        :key="projectRef"
        :project-ref="projectRef"
        :initial-schedule-ref="initialScheduleRef"
        @select="selectSchedule"
      />
    </PageFrame>
  </Teleport>
</template>
