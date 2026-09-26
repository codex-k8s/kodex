<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { readSchedule } from "@/features/automations/api";
import { assistantCreatedScheduleTarget } from "@/features/assistant/model";
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
  if (!current?.nextRunAt) return undefined;
  const date = new Date(current.nextRunAt);
  if (Number.isNaN(date.getTime())) return current.nextRunAt;
  return `${new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: current.timezone,
  }).format(date)} (${current.timezone})`;
});
let refresh: (() => Promise<void>) | undefined;

watch(
  target,
  (value, _previous, onCleanup) => {
    schedule.value = undefined;
    loading.value = false;
    problem.value = false;
    if (!value) return;
    const controller = new AbortController();
    onCleanup(() => {
      controller.abort();
      refresh = undefined;
    });
    refresh = async () => {
      if (controller.signal.aborted) return;
      loading.value = true;
      try {
        const next = await readSchedule(value.scheduleRef, controller.signal);
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (controller.signal.aborted) return;
        if (
          next.ref !== value.scheduleRef ||
          next.projectRef !== value.projectRef
        )
          throw new Error("Assistant schedule readback mismatch");
        schedule.value = next;
        problem.value = false;
      } catch {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) problem.value = true;
      } finally {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) loading.value = false;
      }
    };
    void refresh();
  },
  { immediate: true },
);
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
        @click="refresh?.()"
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

<style scoped>
.assistant-schedule-card {
  display: grid;
  gap: 8px;
  min-width: 0;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.assistant-schedule-card header,
.assistant-schedule-card__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.assistant-schedule-card p {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
