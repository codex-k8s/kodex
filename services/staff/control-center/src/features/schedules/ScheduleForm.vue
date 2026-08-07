<script setup lang="ts">
import { computed, reactive } from "vue";

import type { ScheduleCatalogs } from "@/features/schedules/store";
import type {
  Resource,
  ScheduleInput,
} from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{
  catalogs: ScheduleCatalogs;
  initial?: Resource | null;
  busy: boolean;
}>();
const emit = defineEmits<{
  submit: [value: { name: string; input: ScheduleInput }];
}>();

const original = props.initial?.spec.schedule;
const form = reactive({
  name: props.initial?.name ?? "",
  targetResourceId:
    original?.targetResourceId ?? props.catalogs.targets[0]?.id ?? "",
  mode: original?.cron ? "cron" : "interval",
  cron: original?.cron ?? "0 9 * * 1-5",
  intervalSeconds: original?.intervalSeconds ?? 3600,
  timezone: original?.timezone ?? "UTC",
  calendar: original?.calendar ?? "GREGORIAN",
  overlapPolicy: original?.overlapPolicy ?? "FORBID",
  misfirePolicy: original?.misfirePolicy ?? "WITHIN_GRACE",
  misfireGraceSeconds: original?.misfireGraceSeconds ?? 300,
  deliveryPolicy: original?.deliveryPolicy ?? "AT_LEAST_ONCE",
  maximumAttempts: original?.maximumAttempts ?? 3,
  initialBackoffSeconds: original?.initialBackoffSeconds ?? 30,
  maximumBackoffSeconds: original?.maximumBackoffSeconds ?? 300,
  deadLetterAfterSeconds: original?.deadLetterAfterSeconds ?? 86400,
  promptProfileId:
    original?.promptProfileId ?? props.catalogs.prompts[0]?.id ?? "",
  sessionPolicy: original?.sessionPolicy ?? "NEW",
  roomId: original?.roomId ?? "",
  notificationPolicy: original?.notificationPolicy ?? "ON_ACTION_OR_FAILURE",
  maximumExecutionSeconds: original?.maximumExecutionSeconds ?? 3600,
  coalesce: original?.coalesce ?? true,
  runtimeRevisionId:
    original?.runtimeRevisionId ?? props.catalogs.runtimes[0]?.id ?? "",
  targetType: original?.targetType ?? "AGENT",
  playbookRef: original?.playbookRef ?? "",
  playbookVersion: original?.playbookVersion ?? 1,
  promptArtifactId:
    original?.promptArtifactId ?? props.catalogs.artifacts[0]?.id ?? "",
  executionSessionId: original?.executionSessionId ?? "",
});

const selectedPrompt = computed(() =>
  props.catalogs.prompts.find((item) => item.id === form.promptProfileId),
);

function submit(): void {
  if (
    !form.targetResourceId ||
    !form.promptProfileId ||
    !form.promptArtifactId ||
    !form.runtimeRevisionId ||
    !selectedPrompt.value
  )
    return;
  const input: ScheduleInput = {
    targetResourceId: form.targetResourceId,
    ...(form.mode === "cron"
      ? { cron: form.cron.trim() }
      : { intervalSeconds: form.intervalSeconds }),
    timezone: form.timezone.trim(),
    calendar: form.calendar,
    overlapPolicy: form.overlapPolicy,
    misfirePolicy: form.misfirePolicy,
    misfireGraceSeconds: form.misfireGraceSeconds,
    deliveryPolicy: form.deliveryPolicy,
    maximumAttempts: form.maximumAttempts,
    initialBackoffSeconds: form.initialBackoffSeconds,
    maximumBackoffSeconds: form.maximumBackoffSeconds,
    deadLetterAfterSeconds: form.deadLetterAfterSeconds,
    promptProfileId: form.promptProfileId,
    promptRevision: selectedPrompt.value.version,
    sessionPolicy: form.sessionPolicy,
    ...(form.roomId ? { roomId: form.roomId } : {}),
    notificationPolicy: form.notificationPolicy,
    maximumExecutionSeconds: form.maximumExecutionSeconds,
    coalesce: form.coalesce,
    runtimeRevisionId: form.runtimeRevisionId,
    targetType: form.targetType,
    ...(form.targetType === "PLAYBOOK"
      ? {
          playbookRef: form.playbookRef.trim(),
          playbookVersion: form.playbookVersion,
        }
      : {}),
    promptArtifactId: form.promptArtifactId,
    ...(form.executionSessionId
      ? { executionSessionId: form.executionSessionId }
      : {}),
  };
  emit("submit", { name: form.name.trim(), input });
}
</script>

<template>
  <form class="form-grid" @submit.prevent="submit">
    <label class="form-field form-field--full"
      ><span>{{ $t("common.name") }}</span
      ><input v-model="form.name" required maxlength="128" autocomplete="off"
    /></label>
    <label class="form-field"
      ><span>{{ $t("schedules.target") }}</span
      ><select v-model="form.targetResourceId" required>
        <option disabled value="">{{ $t("common.select") }}</option>
        <option
          v-for="item in catalogs.targets"
          :key="item.id"
          :value="item.id"
        >
          {{ item.name }}
        </option>
      </select></label
    >
    <label class="form-field"
      ><span>{{ $t("schedules.targetType") }}</span
      ><select v-model="form.targetType">
        <option value="AGENT">{{ $t("schedules.agentTarget") }}</option>
        <option value="PLAYBOOK">Playbook</option>
      </select></label
    >
    <template v-if="form.targetType === 'PLAYBOOK'">
      <label class="form-field"
        ><span>{{ $t("schedules.playbookRef") }}</span
        ><input
          v-model="form.playbookRef"
          required
          maxlength="256"
          autocomplete="off"
      /></label>
      <label class="form-field"
        ><span>{{ $t("schedules.playbookVersion") }}</span
        ><input
          v-model.number="form.playbookVersion"
          type="number"
          min="1"
          required
      /></label>
    </template>
    <label class="form-field"
      ><span>{{ $t("schedules.mode") }}</span
      ><select v-model="form.mode">
        <option value="cron">Cron</option>
        <option value="interval">{{ $t("schedules.intervalMode") }}</option>
      </select></label
    >
    <label v-if="form.mode === 'cron'" class="form-field"
      ><span>{{ $t("schedules.cron") }}</span
      ><input v-model="form.cron" required maxlength="128" autocomplete="off"
    /></label>
    <label v-else class="form-field"
      ><span>{{ $t("schedules.interval") }}</span
      ><input
        v-model.number="form.intervalSeconds"
        type="number"
        min="1"
        max="31536000"
        required
    /></label>
    <label class="form-field"
      ><span>{{ $t("schedules.timezone") }}</span
      ><input
        v-model="form.timezone"
        required
        maxlength="64"
        autocomplete="off"
    /></label>
    <label class="form-field"
      ><span>{{ $t("schedules.prompt") }}</span
      ><select v-model="form.promptProfileId" required>
        <option disabled value="">{{ $t("common.select") }}</option>
        <option
          v-for="item in catalogs.prompts"
          :key="item.id"
          :value="item.id"
        >
          {{ item.name }} · v{{ item.version }}
        </option>
      </select></label
    >
    <label class="form-field"
      ><span>{{ $t("schedules.artifact") }}</span
      ><select v-model="form.promptArtifactId" required>
        <option disabled value="">{{ $t("common.select") }}</option>
        <option
          v-for="item in catalogs.artifacts"
          :key="item.id"
          :value="item.id"
        >
          {{ item.name }}
        </option>
      </select></label
    >
    <label class="form-field"
      ><span>{{ $t("schedules.runtime") }}</span
      ><select v-model="form.runtimeRevisionId" required>
        <option disabled value="">{{ $t("common.select") }}</option>
        <option
          v-for="item in catalogs.runtimes"
          :key="item.id"
          :value="item.id"
        >
          {{ item.name }} · v{{ item.version }}
        </option>
      </select></label
    >
    <label class="form-field"
      ><span>{{ $t("schedules.room") }}</span
      ><select v-model="form.roomId">
        <option value="">{{ $t("common.noValue") }}</option>
        <option v-for="item in catalogs.rooms" :key="item.id" :value="item.id">
          {{ item.name }}
        </option>
      </select></label
    >
    <details class="advanced">
      <summary>{{ $t("common.advanced") }}</summary>
      <div class="form-grid">
        <label class="form-field"
          ><span>{{ $t("schedules.calendar") }}</span
          ><select v-model="form.calendar">
            <option value="GREGORIAN">GREGORIAN</option>
            <option value="BUSINESS">BUSINESS</option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("schedules.overlap") }}</span
          ><select v-model="form.overlapPolicy">
            <option value="FORBID">FORBID</option>
            <option value="SKIP">SKIP</option>
            <option value="QUEUE">QUEUE</option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("schedules.misfire") }}</span
          ><select v-model="form.misfirePolicy">
            <option value="SKIP">SKIP</option>
            <option value="RUN_ONCE">RUN_ONCE</option>
            <option value="CATCH_UP">CATCH_UP</option>
            <option value="WITHIN_GRACE">WITHIN_GRACE</option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("schedules.grace") }}</span
          ><input
            v-model.number="form.misfireGraceSeconds"
            type="number"
            min="0"
            required
        /></label>
        <label class="form-field"
          ><span>{{ $t("schedules.delivery") }}</span
          ><select v-model="form.deliveryPolicy">
            <option value="AT_LEAST_ONCE">AT_LEAST_ONCE</option>
            <option value="EXACTLY_ONCE_EFFECT">EXACTLY_ONCE_EFFECT</option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("schedules.maximumAttempts") }}</span
          ><input
            v-model.number="form.maximumAttempts"
            type="number"
            min="1"
            max="100"
            required
        /></label>
        <label class="form-field"
          ><span>{{ $t("schedules.initialBackoff") }}</span
          ><input
            v-model.number="form.initialBackoffSeconds"
            type="number"
            min="1"
            required
        /></label>
        <label class="form-field"
          ><span>{{ $t("schedules.maximumBackoff") }}</span
          ><input
            v-model.number="form.maximumBackoffSeconds"
            type="number"
            min="1"
            required
        /></label>
        <label class="form-field"
          ><span>{{ $t("schedules.deadLetter") }}</span
          ><input
            v-model.number="form.deadLetterAfterSeconds"
            type="number"
            min="1"
            required
        /></label>
        <label class="form-field"
          ><span>{{ $t("schedules.sessionPolicy") }}</span
          ><select v-model="form.sessionPolicy">
            <option value="NEW">NEW</option>
            <option value="PERSISTENT">PERSISTENT</option>
            <option value="ROLLING">ROLLING</option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("schedules.executionSession") }}</span
          ><select v-model="form.executionSessionId">
            <option value="">{{ $t("common.noValue") }}</option>
            <option
              v-for="item in catalogs.sessions"
              :key="item.id"
              :value="item.id"
            >
              {{ item.name }}
            </option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("schedules.notification") }}</span
          ><select v-model="form.notificationPolicy">
            <option value="ALWAYS">ALWAYS</option>
            <option value="ON_ACTION">ON_ACTION</option>
            <option value="ON_FAILURE">ON_FAILURE</option>
            <option value="ON_ACTION_OR_FAILURE">ON_ACTION_OR_FAILURE</option>
            <option value="AUDIT_ONLY">AUDIT_ONLY</option>
          </select></label
        >
        <label class="form-field"
          ><span>{{ $t("schedules.maximumExecution") }}</span
          ><input
            v-model.number="form.maximumExecutionSeconds"
            type="number"
            min="1"
            required
        /></label>
        <label class="form-field checkbox-field"
          ><input v-model="form.coalesce" type="checkbox" /><span>{{
            $t("schedules.coalesce")
          }}</span></label
        >
      </div>
    </details>
    <div class="button-row form-field--full">
      <button class="button button--primary" type="submit" :disabled="busy">
        {{ $t("common.save") }}
      </button>
    </div>
  </form>
</template>
