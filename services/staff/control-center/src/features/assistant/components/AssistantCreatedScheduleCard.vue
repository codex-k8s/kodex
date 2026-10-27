<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import { readSchedule } from "@/features/automations/api";
import { assistantCreatedScheduleTarget } from "@/features/assistant/model";
import { watchAssistantReadback } from "@/features/assistant/readback-lifecycle";
import type {
  AssistantPlan,
  Schedule,
} from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const { locale } = useI18n();
const target = computed(() =>
  assistantCreatedScheduleTarget(props.plan, props.operationRef),
);
const updated = computed(() =>
  props.plan.operations.some(
    (operation) =>
      operation.ref === props.operationRef &&
      operation.type === "UPDATE_SCHEDULE",
  ),
);
const schedule = ref<Schedule>();
const loading = ref(false);
const problem = ref(false);
const nextRunLabel = computed(() => {
  const current = schedule.value;
  if (!current || current.state !== "ACTIVE" || !current.nextRunAt)
    return undefined;
  const date = new Date(current.nextRunAt);
  if (Number.isNaN(date.getTime())) return current.nextRunAt;
  return `${new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: current.timezone,
  }).format(date)} (${current.timezone})`;
});
const refresh = watchAssistantReadback(target, {
  loading,
  problem,
  reset: () => {
    schedule.value = undefined;
  },
  read: (value, signal) => readSchedule(value.scheduleRef, signal),
  apply: (value, next) => {
    if (next.ref !== value.scheduleRef || next.projectRef !== value.projectRef)
      throw new Error("Assistant schedule readback mismatch");
    schedule.value = next;
  },
});
</script>

<template>
  <section v-if="target" class="assistant-schedule-card" aria-live="polite">
    <header>
      <strong>{{
        $t(
          updated
            ? "assistant.createdSchedule.updatedTitle"
            : "assistant.createdSchedule.title",
        )
      }}</strong>
      <StatusBadge v-if="schedule" :state="schedule.state" />
    </header>
    <p v-if="loading && !schedule">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="field-error" role="alert">
      {{ $t("assistant.createdSchedule.loadFailed") }}
    </p>
    <template v-if="schedule">
      <p>{{ schedule.name }}</p>
      <p v-if="nextRunLabel">
        {{ $t("assistant.createdSchedule.nextRun", { time: nextRunLabel }) }}
      </p>
      <p v-else>{{ $t("assistant.createdSchedule.noNextRun") }}</p>
    </template>
    <div class="assistant-schedule-card__actions">
      <button
        class="button"
        type="button"
        :disabled="loading"
        @click="refresh()"
      >
        {{ $t("common.refresh") }}
      </button>
      <RouterLink
        class="button button--primary"
        :to="{
          name: 'automations',
          params: { projectRef: target.projectRef },
          query: { scheduleRef: target.scheduleRef, assistantForm: '1' },
        }"
        >{{ $t("assistant.createdSchedule.open") }}</RouterLink
      >
    </div>
  </section>
</template>

<style scoped src="./assistant-result-card.css"></style>
