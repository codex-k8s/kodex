<script setup lang="ts">
import { CalendarClock, Save } from "@lucide/vue";
import { computed, reactive, watch } from "vue";
import { useI18n } from "vue-i18n";

import { scheduleInput } from "@/features/automations/model";
import type {
  Agent,
  Schedule,
  ScheduleInput,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const props = defineProps<{
  agents: readonly Agent[];
  busy?: boolean;
  problem?: AppProblem;
  schedule?: Schedule;
  workflows: readonly Workflow[];
}>();
const emit = defineEmits<{
  close: [];
  submit: [input: ScheduleInput, current?: Schedule];
}>();
const { locale } = useI18n();
const initial = props.schedule ? scheduleInput(props.schedule) : undefined;
const baseInput = { ...(initial?.input ?? {}) };
const form = reactive({
  name: initial?.name ?? "",
  targetType: initial?.targetType ?? ("AGENT" as "AGENT" | "WORKFLOW"),
  targetRef: initial?.targetRef ?? "",
  preset: initial?.preset ?? ("DAILY" as ScheduleInput["preset"]),
  timeOfDay: initial?.timeOfDay ?? "09:00",
  dayOfWeek:
    initial?.dayOfWeek ?? ("MONDAY" as NonNullable<ScheduleInput["dayOfWeek"]>),
  timezone:
    initial?.timezone ||
    Intl.DateTimeFormat().resolvedOptions().timeZone ||
    "UTC",
  task: typeof baseInput.task === "string" ? baseInput.task : "",
  sessionPolicy:
    initial?.sessionPolicy ??
    ("NEW_EACH_RUN" as ScheduleInput["sessionPolicy"]),
  notificationPolicy:
    initial?.notificationPolicy ??
    ("CONTROL_CENTER_ONLY" as ScheduleInput["notificationPolicy"]),
});
const custom = computed(() =>
  locale.value.startsWith("en")
    ? {
        agent: "AI employee",
        editTitle: "Edit automation",
        schedule: "Schedule",
        task: "Task",
        targetType: "Target type",
        versionHint: props.schedule
          ? `The update uses version ${String(props.schedule.version)} and will fail if the automation changed.`
          : "The automation will be created after server validation.",
        workflow: "Process",
      }
    : {
        agent: "ИИ-сотрудник",
        editTitle: "Изменить автоматизацию",
        schedule: "Расписание",
        task: "Задача",
        targetType: "Тип цели",
        versionHint: props.schedule
          ? `Изменение отправится с версией ${String(props.schedule.version)} и будет отклонено, если автоматизация уже изменилась.`
          : "Автоматизация будет создана только после проверки сервером.",
        workflow: "Процесс",
      },
);
const timezoneOptions = Array.from(
  new Set([
    form.timezone,
    "UTC",
    "Europe/Saratov",
    "Europe/Moscow",
    "Europe/Berlin",
    "Asia/Dubai",
    "Asia/Almaty",
    "Asia/Tokyo",
    "America/New_York",
    "America/Chicago",
    "America/Los_Angeles",
  ]),
);
const targets = computed(() =>
  form.targetType === "AGENT" ? props.agents : props.workflows,
);
const selectedTargetExists = computed(() =>
  targets.value.some((target) => target.ref === form.targetRef),
);

watch(
  () => form.targetType,
  () => {
    if (!selectedTargetExists.value) form.targetRef = "";
  },
);

function submit(): void {
  const input: ScheduleInput = {
    name: form.name.trim(),
    targetType: form.targetType,
    targetRef: form.targetRef,
    preset: form.preset,
    timeOfDay: form.preset === "HOURLY" ? "00:00" : form.timeOfDay,
    ...(form.preset === "WEEKLY" ? { dayOfWeek: form.dayOfWeek } : {}),
    timezone: form.timezone,
    input: { ...baseInput, task: form.task },
    sessionPolicy: form.sessionPolicy,
    notificationPolicy: form.notificationPolicy,
  };
  emit("submit", input, props.schedule);
}
</script>

<template>
  <ModalDialog
    :title="schedule ? custom.editTitle : $t('automations.new')"
    :busy="busy"
    @close="emit('close')"
  >
    <form
      id="automation-editor-form"
      class="automation-editor"
      @submit.prevent="submit"
    >
      <div class="automation-editor__notice">
        <CalendarClock :size="18" aria-hidden="true" />
        <span>{{ custom.versionHint }}</span>
      </div>

      <section>
        <label class="field">
          <span>{{ $t("common.name") }}</span>
          <input
            v-model="form.name"
            required
            maxlength="160"
            autocomplete="off"
          />
        </label>
        <div class="automation-editor__target-grid">
          <label class="field">
            <span>{{ custom.targetType }}</span>
            <select v-model="form.targetType">
              <option value="AGENT">{{ custom.agent }}</option>
              <option value="WORKFLOW">{{ custom.workflow }}</option>
            </select>
          </label>
          <label class="field">
            <span>{{ $t("common.target") }}</span>
            <select v-model="form.targetRef" required>
              <option value="" disabled>
                {{ $t("automations.chooseTarget") }}
              </option>
              <option
                v-if="schedule && !selectedTargetExists"
                :value="schedule.target.ref"
              >
                {{ schedule.target.displayName }}
              </option>
              <option
                v-for="target in targets"
                :key="target.ref"
                :value="target.ref"
              >
                {{ target.name }}
              </option>
            </select>
          </label>
        </div>
      </section>

      <section>
        <h3>{{ custom.schedule }}</h3>
        <div class="automation-editor__schedule-grid">
          <label class="field">
            <span>{{ $t("automations.preset") }}</span>
            <select v-model="form.preset">
              <option value="HOURLY">{{ $t("automations.hourly") }}</option>
              <option value="DAILY">{{ $t("automations.daily") }}</option>
              <option value="WEEKDAYS">{{ $t("automations.weekdays") }}</option>
              <option value="WEEKLY">{{ $t("automations.weekly") }}</option>
            </select>
          </label>
          <label v-if="form.preset !== 'HOURLY'" class="field">
            <span>{{ $t("automations.timeOfDay") }}</span>
            <input v-model="form.timeOfDay" type="time" required />
          </label>
          <label v-if="form.preset === 'WEEKLY'" class="field">
            <span>{{ $t("automations.dayOfWeek") }}</span>
            <select v-model="form.dayOfWeek">
              <option
                v-for="day in [
                  'MONDAY',
                  'TUESDAY',
                  'WEDNESDAY',
                  'THURSDAY',
                  'FRIDAY',
                  'SATURDAY',
                  'SUNDAY',
                ]"
                :key="day"
                :value="day"
              >
                {{ $t(`automations.day.${day}`) }}
              </option>
            </select>
          </label>
          <label class="field">
            <span>{{ $t("automations.timezone") }}</span>
            <select v-model="form.timezone" required>
              <option
                v-for="timezone in timezoneOptions"
                :key="timezone"
                :value="timezone"
              >
                {{ timezone }}
              </option>
            </select>
          </label>
        </div>
      </section>

      <section>
        <label class="field">
          <span>{{ custom.task }}</span>
          <textarea
            v-model="form.task"
            rows="5"
            required
            maxlength="6000"
          ></textarea>
        </label>
        <div class="automation-editor__policy-grid">
          <label class="field">
            <span>{{ $t("automations.sessionPolicy") }}</span>
            <select v-model="form.sessionPolicy">
              <option value="NEW_EACH_RUN">
                {{ $t("automations.newSession") }}
              </option>
              <option value="CONTINUE_ONE">
                {{ $t("automations.continueSession") }}
              </option>
            </select>
          </label>
          <label class="field">
            <span>{{ $t("automations.notifications") }}</span>
            <select v-model="form.notificationPolicy">
              <option value="CONTROL_CENTER_ONLY">
                {{ $t("automations.controlCenterOnly") }}
              </option>
              <option value="CONTROL_CENTER_AND_OPTIONAL_CHANNELS">
                {{ $t("automations.optionalChannels") }}
              </option>
            </select>
          </label>
        </div>
      </section>
      <ProblemNotice v-if="problem" :problem="problem" compact />
    </form>
    <template #actions>
      <button
        class="button"
        type="button"
        :disabled="busy"
        @click="emit('close')"
      >
        {{ $t("common.cancel") }}
      </button>
      <button
        class="button button--primary"
        form="automation-editor-form"
        type="submit"
        :disabled="busy"
      >
        <Save :size="16" aria-hidden="true" />
        {{ schedule ? $t("common.save") : $t("common.create") }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.automation-editor {
  display: grid;
  width: min(760px, 76vw);
  gap: 0;
}
.automation-editor__notice {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 7px;
  color: var(--muted);
  background: var(--panel);
  font-size: 0.82rem;
}
.automation-editor section {
  display: grid;
  gap: 12px;
  padding: 18px 0;
  border-bottom: 1px solid var(--border);
}
.automation-editor section:last-of-type {
  border-bottom: 0;
}
.automation-editor h3 {
  margin: 0;
  font-size: 0.88rem;
}
.automation-editor__target-grid,
.automation-editor__policy-grid {
  display: grid;
  grid-template-columns: minmax(160px, 0.7fr) minmax(240px, 1.3fr);
  gap: 12px;
}
.automation-editor__schedule-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(130px, 1fr));
  gap: 12px;
}
.automation-editor .field {
  min-width: 0;
}
.automation-editor .field > span {
  display: block;
  margin-bottom: 6px;
  color: var(--muted);
  font-size: 0.78rem;
  font-weight: 600;
}
.automation-editor input,
.automation-editor select,
.automation-editor textarea {
  width: 100%;
}
.automation-editor textarea {
  resize: vertical;
}
@media (max-width: 760px) {
  .automation-editor {
    width: auto;
  }
  .automation-editor__target-grid,
  .automation-editor__policy-grid,
  .automation-editor__schedule-grid {
    grid-template-columns: 1fr;
  }
}
</style>
