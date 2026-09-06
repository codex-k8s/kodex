<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type {
  Schedule,
  ScheduleInput,
  SchedulePreview,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import { useServerMessage } from "@/shared/ui/server-message";
import PromptContextDetails from "@/features/agents/detail/PromptContextDetails.vue";
import { loadSchedulePreview } from "./api";
import { scheduleInput } from "./model";
import {
  checkedScheduleMaterialization,
  scheduleMaterializationInput,
} from "./prompt-preview";
const props = defineProps<{
  projectRef: string;
  draft: ScheduleInput;
  schedule?: Schedule;
  disabled?: boolean;
}>();
const { t, locale } = useI18n();
const serverMessage = useServerMessage();
const mode = ref<"DRAFT" | "CURRENT_REVISION">("DRAFT");
const full = ref(false);
const busy = ref(false);
const preview = ref<SchedulePreview>();
const problem = ref<AppProblem>();
const input = computed(() =>
  mode.value === "CURRENT_REVISION" && props.schedule
    ? scheduleInput(props.schedule)
    : props.draft,
);
const available = computed(
  () =>
    !!input.value.name.trim() &&
    !!input.value.targetRef &&
    !!input.value.automationText.trim() &&
    (mode.value === "DRAFT" || !!props.schedule),
);
const identity = computed(() =>
  JSON.stringify([
    props.projectRef,
    input.value,
    props.schedule?.ref,
    props.schedule?.version,
    mode.value,
    full.value,
    props.disabled,
  ]),
);
let active: AbortController | undefined;
function invalidate() {
  active?.abort();
  active = undefined;
  preview.value = undefined;
  problem.value = undefined;
  busy.value = false;
}
watch(identity, invalidate, { flush: "sync" });
onBeforeUnmount(invalidate);
async function refresh() {
  if (busy.value || props.disabled || !available.value) return;
  invalidate();
  const controller = new AbortController();
  active = controller;
  const key = identity.value;
  busy.value = true;
  try {
    const body = scheduleMaterializationInput(
      props.projectRef,
      input.value,
      mode.value,
      props.schedule,
      full.value,
    );
    const result = await loadSchedulePreview(body, controller.signal);
    if (
      active === controller &&
      !controller.signal.aborted &&
      identity.value === key
    )
      preview.value = checkedScheduleMaterialization(
        result,
        body,
        props.schedule,
      );
  } catch (error) {
    if (active === controller && !controller.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (active === controller) {
      active = undefined;
      busy.value = false;
    }
  }
}
</script>
<template>
  <section class="automation-prompt-preview stack">
    <h3>{{ t("automationPreview.title") }}</h3>
    <label class="field"
      ><span>{{ t("automationPreview.revision") }}</span
      ><select v-model="mode" :disabled="disabled || busy">
        <option value="DRAFT">{{ t("automationPreview.draft") }}</option>
        <option v-if="schedule" value="CURRENT_REVISION">
          {{ t("automationPreview.current") }}
        </option>
      </select></label
    >
    <p>
      {{
        t(
          mode === "DRAFT"
            ? "automationPreview.draftHelp"
            : "automationPreview.currentHelp",
        )
      }}
    </p>
    <template v-if="mode === 'CURRENT_REVISION'"
      ><h4>{{ t("automationPreview.savedTask") }}</h4>
      <p class="literal">{{ input.automationText }}</p></template
    >
    <label class="checkbox-label"
      ><input
        v-model="full"
        type="checkbox"
        :disabled="disabled || busy || !available"
      /><span>{{ t("promptContext.full") }}</span></label
    >
    <button
      type="button"
      class="button button--secondary"
      :disabled="disabled || busy || !available"
      @click="refresh"
    >
      {{ busy ? t("common.loading") : t("automationPreview.preview") }}
    </button>
    <p v-if="!available">{{ t("automationPreview.required") }}</p>
    <ProblemNotice v-if="problem" :problem="problem" />
    <template v-if="preview?.materializedPrompt && preview.materializationPin">
      <dl>
        <dt>
          {{
            t(
              mode === "DRAFT"
                ? "automationPreview.futureActor"
                : "automationPreview.executionActor",
            )
          }}
        </dt>
        <dd>
          <code>{{ preview.materializationPin.executionActorRef }}</code>
        </dd>
        <dt>{{ t("automationPreview.revision") }}</dt>
        <dd>
          {{
            preview.materializationPin.revisionAvailable
              ? preview.materializationPin.revisionRef
              : t("automationPreview.notSaved")
          }}
        </dd>
        <template v-if="preview.materializationPin.baseRevisionRef"
          ><dt>{{ t("automationPreview.base") }}</dt>
          <dd>
            <code>{{ preview.materializationPin.baseRevisionRef }}</code>
          </dd></template
        >
      </dl>
      <h4>{{ t("automationPreview.occurrences") }}</h4>
      <ol>
        <li v-for="time in preview.occurrences" :key="time">
          <time :datetime="time">{{
            new Date(time).toLocaleString(locale, {
              timeZone: preview.materializationPin.timezone,
            })
          }}</time>
        </li>
      </ol>
      <SafeMarkdown
        :content="
          preview.materializedPrompt.fullMaterializedPrompt ??
          preview.materializedPrompt.safePreview
        "
      />
      <PromptContextDetails :preview="preview.materializedPrompt" />
      <details>
        <summary>{{ t("automationPreview.variables") }}</summary>
        <dl>
          <template
            v-for="variable in preview.automationVariables"
            :key="variable.name"
            ><dt>
              <code>{{ variable.name }}</code>
            </dt>
            <dd>
              {{ serverMessage(variable.description) }} ·
              {{ t(`templateAvailability.${variable.reason}`) }}
              <pre v-if="variable.available">{{ variable.example }}</pre>
            </dd></template
          >
        </dl>
      </details>
    </template>
  </section>
</template>
<style scoped>
.automation-prompt-preview {
  min-width: 0;
  overflow-wrap: anywhere;
}
.literal,
pre {
  white-space: pre-wrap;
}
dd {
  margin-inline-start: 0;
}
</style>
