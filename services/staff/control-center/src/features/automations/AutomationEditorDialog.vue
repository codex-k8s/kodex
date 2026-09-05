<script setup lang="ts">
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import { CalendarClock, Save } from "@lucide/vue";
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { scheduleInput } from "@/features/automations/model";
import { loadSchedulePreview } from "@/features/automations/api";
import CodeEditor from "@/shared/ui/CodeEditor.vue";
import {
  createExecutionTargetPickerLoader,
  targetRefAfterTypeChange,
  type ExecutionTargetPickerOption,
} from "@/shared/api/execution-target-picker";
import type {
  Schedule,
  ScheduleInput,
  SchedulePreview,
  SchedulePreviewInput,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOption } from "@/shared/ui/async-entity-picker";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const props = defineProps<{
  busy?: boolean;
  problem?: AppProblem;
  projectRef: string;
  schedule?: Schedule;
}>();
const emit = defineEmits<{
  close: [];
  submit: [input: ScheduleInput, current?: Schedule];
}>();
const { locale } = useI18n();
const initial = props.schedule ? scheduleInput(props.schedule) : undefined;
const baseInput = { ...(initial?.input ?? {}) };
const selectedTarget = ref<ExecutionTargetPickerOption>();
const form = reactive({
  name: initial?.name ?? "",
  targetType: initial?.targetType ?? ("AGENT" as "AGENT" | "WORKFLOW"),
  targetRef: initial?.targetRef ?? "",
  preset: initial?.preset ?? ("DAILY" as ScheduleInput["preset"]),
  cronExpression: initial?.cronExpression ?? "",
  misfirePolicy:
    initial?.misfirePolicy ?? ("COALESCE" as ScheduleInput["misfirePolicy"]),
  overlapPolicy:
    initial?.overlapPolicy ?? ("FORBID" as ScheduleInput["overlapPolicy"]),
  automationText: initial?.automationText ?? "",
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
          ? `Saving creates the next version from version ${String(props.schedule.version)} and fails if the automation changed.`
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
          ? `Сохранение создаст следующую версию на основе версии ${String(props.schedule.version)} и будет отклонено, если автоматизация уже изменилась.`
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
const preview = ref<SchedulePreview>();
const previewProblem = ref<AppProblem>();
const previewBusy = ref(false);
const schedulePreviewInput = computed<SchedulePreviewInput>(() => ({
  preset: form.preset,
  ...(form.preset === "CUSTOM"
    ? { cronExpression: form.cronExpression.trim() }
    : { timeOfDay: form.preset === "HOURLY" ? "00:00" : form.timeOfDay }),
  ...(form.preset === "WEEKLY" ? { dayOfWeek: form.dayOfWeek } : {}),
  timezone: form.timezone,
  dstGapPolicy: "SHIFT_FORWARD",
  dstFoldPolicy: "RUN_ONCE_EARLIEST",
  misfirePolicy: form.misfirePolicy,
  overlapPolicy: form.overlapPolicy,
  limit: 6,
}));
onMounted(() =>
  watch(
    schedulePreviewInput,
    (input, _previous, onCleanup) => {
      const controller = new AbortController();
      preview.value = undefined;
      previewProblem.value = undefined;
      previewBusy.value = true;
      const timer = setTimeout(() => {
        void loadSchedulePreview(input, controller.signal)
          .then((value) => {
            if (!controller.signal.aborted) preview.value = value;
          })
          .catch((error: unknown) => {
            if (!controller.signal.aborted)
              previewProblem.value = asProblem(error);
          })
          .finally(() => {
            if (!controller.signal.aborted) previewBusy.value = false;
          });
      }, 500);
      onCleanup(() => {
        clearTimeout(timer);
        controller.abort();
      });
    },
    { immediate: true },
  ),
);
const targetLoader = computed(() =>
  createExecutionTargetPickerLoader(props.projectRef, form.targetType),
);
const selectedTargetOption = computed<
  ExecutionTargetPickerOption | AsyncEntityOption | undefined
>(() => {
  if (selectedTarget.value?.ref === form.targetRef) return selectedTarget.value;
  if (
    props.schedule?.target.type === form.targetType &&
    props.schedule.target.ref === form.targetRef
  ) {
    return {
      ref: props.schedule.target.ref,
      title: props.schedule.target.displayName,
      meta: `v${String(props.schedule.target.version)}`,
    };
  }
  return undefined;
});

function selectTarget(
  option: ExecutionTargetPickerOption | AsyncEntityOption,
): void {
  if (!("target" in option) || !("targetType" in option)) return;
  if (option.targetType !== form.targetType) return;
  selectedTarget.value = option;
  form.targetRef = option.ref;
}

function selectTargetType(nextType: "AGENT" | "WORKFLOW"): void {
  const nextRef = targetRefAfterTypeChange(
    form.targetType,
    nextType,
    form.targetRef,
  );
  form.targetType = nextType;
  form.targetRef = nextRef;
  if (!nextRef) selectedTarget.value = undefined;
}

function handleTargetTypeChange(event: Event): void {
  const target = event.target;
  if (!(target instanceof HTMLSelectElement)) return;
  if (target.value === "AGENT" || target.value === "WORKFLOW")
    selectTargetType(target.value);
}

watch(
  () => props.projectRef,
  () => {
    form.targetRef = "";
    selectedTarget.value = undefined;
  },
);

function submit(): void {
  if (props.busy || previewBusy.value || !preview.value || previewProblem.value)
    return;
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
    ...(form.preset === "CUSTOM"
      ? { cronExpression: preview.value.normalizedCronExpression }
      : {}),
    dstGapPolicy: "SHIFT_FORWARD",
    dstFoldPolicy: "RUN_ONCE_EARLIEST",
    misfirePolicy: form.misfirePolicy,
    overlapPolicy: form.overlapPolicy,
    automationText: form.automationText,
    promptInputs: { ...initial?.promptInputs },
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
            <select :value="form.targetType" @change="handleTargetTypeChange">
              <option value="AGENT">{{ custom.agent }}</option>
              <option value="WORKFLOW">{{ custom.workflow }}</option>
            </select>
          </label>
          <div class="field">
            <span>{{ $t("common.target") }}</span>
            <AsyncEntityPicker
              :key="`${projectRef}:${form.targetType}`"
              v-model="form.targetRef"
              :load-page="targetLoader"
              :selected="selectedTargetOption"
              :trigger-label="$t('common.target')"
              :placeholder="$t('automations.chooseTarget')"
              :search-placeholder="$t('automations.chooseTarget')"
              @select="selectTarget"
            />
          </div>
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
              <option value="CUSTOM">Cron</option>
            </select>
          </label>
          <label v-if="form.preset === 'CUSTOM'" class="field"
            ><span>Cron</span
            ><input
              v-model="form.cronExpression"
              required
              maxlength="256"
              spellcheck="false"
          /></label>
          <label
            v-if="form.preset !== 'HOURLY' && form.preset !== 'CUSTOM'"
            class="field"
          >
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
        <div class="automation-editor__schedule-grid">
          <label class="field"
            ><span>{{ $t("automations.misfire") }}</span
            ><select v-model="form.misfirePolicy">
              <option
                v-for="value in ['COALESCE', 'CATCH_UP_ONE', 'SKIP']"
                :key="value"
                :value="value"
              >
                {{ $t(`automations.policies.${value}`) }}
              </option>
            </select></label
          >
          <label class="field"
            ><span>{{ $t("automations.overlap") }}</span
            ><select v-model="form.overlapPolicy">
              <option value="FORBID">
                {{ $t("automations.policies.FORBID") }}
              </option>
              <option value="ALLOW">
                {{ $t("automations.policies.ALLOW") }}
              </option>
            </select></label
          >
        </div>
        <ProblemNotice v-if="previewProblem" :problem="previewProblem" />
        <p v-if="previewBusy" role="status">{{ $t("common.loading") }}</p>
        <div v-else-if="preview" class="automation-editor__preview">
          <code>{{ preview.normalizedCronExpression }}</code>
          <ol>
            <li v-for="time in preview.occurrences" :key="time">
              <time>{{
                new Date(time).toLocaleString(locale, {
                  timeZone: form.timezone,
                })
              }}</time>
            </li>
          </ol>
        </div>
      </section>

      <section>
        <label class="field"
          ><span>{{ $t("automations.automationText") }}</span
          ><CodeEditor
            v-model="form.automationText"
            :label="$t('automations.automationText')"
            :disabled="busy"
        /></label>
        <label class="field">
          <span>{{ custom.task }}</span>
          <VoiceTextarea
            v-model="form.task"
            rows="5"
            required
            maxlength="6000"
          ></VoiceTextarea>
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
        :disabled="busy || previewBusy || !preview || Boolean(previewProblem)"
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
.automation-editor :deep(textarea) {
  width: 100%;
}
.automation-editor :deep(textarea) {
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
