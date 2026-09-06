<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import * as sdk from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Agent,
  Workflow,
  PromptTemplateScopeInput,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap, asProblem, type AppProblem } from "@/shared/api/problem";
import { requestSignal } from "@/shared/api/client";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import PromptTargetPreview from "@/features/agents/detail/PromptTargetPreview.vue";
import type { PromptTarget } from "@/features/agents/detail/prompt-context";

const props = defineProps<{
  modelValue?: PromptTemplateScopeInput;
  projectRef?: string;
  template: string;
  disabled: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: PromptTemplateScopeInput];
  valid: [value: boolean];
}>();
const { t } = useI18n();
const kind = ref<"AGENT" | "WORKFLOW_STAGE">("AGENT");
const selected = ref<Agent | Workflow>();
const problem = ref<AppProblem>();
const busy = ref(false);
let controller: AbortController | undefined;
const workflow = computed(() =>
  selected.value && "steps" in selected.value ? selected.value : undefined,
);
const body = computed(() => {
  const current = workflow.value,
    ref = props.modelValue?.workflowRevisionRef;
  if (!current || !ref) return current?.draft ?? current;
  if (current.draft?.ref === ref) return current.draft;
  return current.revisionRef === ref ? current : undefined;
});
const revisionRef = computed(
  () =>
    props.modelValue?.workflowRevisionRef ??
    workflow.value?.draft?.ref ??
    workflow.value?.revisionRef,
);
const stage = computed(() =>
  body.value?.steps.find(
    (item) => item.ref === props.modelValue?.workflowStageKey,
  ),
);
const target = computed<PromptTarget | undefined>(() => {
  const scope = props.modelValue,
    item = selected.value;
  if (
    !scope ||
    !item ||
    item.ref !== scope.targetRef ||
    (scope.targetKind === "WORKFLOW_STAGE" && !stage.value)
  )
    return undefined;
  return {
    projectRef: item.projectRef,
    targetKind: scope.targetKind,
    targetRef: item.ref,
    context:
      scope.targetKind === "AGENT"
        ? { expectedAgentVersion: item.version }
        : {
            expectedWorkflowVersion: item.version,
            workflowRevisionRef: revisionRef.value,
            workflowStageKey: stage.value?.ref,
          },
  };
});
watch(
  () => [props.modelValue, target.value],
  () => emit("valid", !props.modelValue || !!target.value),
  { immediate: true },
);
watch(
  () =>
    [
      props.projectRef,
      props.modelValue?.targetKind,
      props.modelValue?.targetRef,
    ] as const,
  async ([project, targetKind, ref]) => {
    controller?.abort();
    selected.value = undefined;
    problem.value = undefined;
    busy.value = false;
    if (targetKind) kind.value = targetKind;
    if (!project || !targetKind || !ref) return;
    const active = new AbortController();
    controller = active;
    busy.value = true;
    try {
      const item =
        targetKind === "AGENT"
          ? (
              await unwrap(
                sdk.getAgent({
                  path: { agentRef: ref },
                  signal: requestSignal(active.signal),
                }),
              )
            ).data
          : (
              await unwrap(
                sdk.getWorkflow({
                  path: { workflowRef: ref },
                  signal: requestSignal(active.signal),
                }),
              )
            ).data;
      if (active.signal.aborted) return;
      if (item.ref !== ref || item.projectRef !== project)
        throw new Error("Prompt scope target mismatch");
      selected.value = item;
    } catch (error) {
      if (!active.signal.aborted) problem.value = asProblem(error);
    } finally {
      if (controller === active) busy.value = false;
    }
  },
  { immediate: true },
);
onBeforeUnmount(() => controller?.abort());
async function load(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  if (!props.projectRef) return { items: [] };
  const options = {
    path: { projectRef: props.projectRef },
    query: { query, pageToken, pageSize: 40 },
    signal: requestSignal(signal),
  };
  const page =
    kind.value === "AGENT"
      ? (await unwrap(sdk.listAgents(options))).data
      : (await unwrap(sdk.listWorkflows(options))).data;
  if (page.items.some((item) => item.projectRef !== props.projectRef))
    throw new Error("Prompt scope catalog mismatch");
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      title: item.name,
      description: item.purpose,
    })),
    nextPageToken: page.nextPageToken,
  };
}
function selectTarget(value: unknown): void {
  if (props.disabled || typeof value !== "string" || !value) return;
  emit("update:modelValue", {
    targetKind: kind.value,
    targetRef: value,
    templateKind: props.modelValue?.templateKind ?? "INSTRUCTIONS",
  });
}
function selectStage(event: Event): void {
  if (
    props.disabled ||
    !props.modelValue ||
    !(event.target instanceof HTMLSelectElement)
  )
    return;
  const value = event.target.value;
  const step = body.value?.steps.find((item) => item.ref === value);
  if (!step || !revisionRef.value) return;
  emit("update:modelValue", {
    ...props.modelValue,
    workflowStageKey: step.ref,
    workflowRevisionRef: revisionRef.value,
    agentRef: step.agentRef,
    expectedContextDigest: undefined,
  });
}
function selectTemplateKind(event: Event): void {
  if (
    props.disabled ||
    !props.modelValue ||
    !(event.target instanceof HTMLSelectElement)
  )
    return;
  const value = event.target.value;
  if (value === "INSTRUCTIONS" || value === "CONTINUATION")
    emit("update:modelValue", { ...props.modelValue, templateKind: value });
}
</script>

<template>
  <section class="stack prompt-scope-fields">
    <h3>{{ t("promptContext.scope") }}</h3>
    <p>{{ t("promptContext.scopeHelp") }}</p>
    <p v-if="!projectRef" class="text-muted">
      {{ t("promptContext.unscoped") }}
    </p>
    <template v-else>
      <label
        >{{ t("integrations.targetType")
        }}<select v-model="kind" :disabled="disabled || busy">
          <option value="AGENT">{{ t("promptContext.AGENT") }}</option>
          <option value="WORKFLOW_STAGE">
            {{ t("promptContext.WORKFLOW_STAGE") }}
          </option>
        </select></label
      >
      <AsyncEntityPicker
        :key="`${projectRef}:${kind}`"
        :model-value="
          modelValue?.targetKind === kind ? modelValue.targetRef : undefined
        "
        :selected="
          selected && modelValue?.targetKind === kind
            ? { ref: selected.ref, title: selected.name }
            : undefined
        "
        :load-page="load"
        :disabled="disabled || busy"
        :clearable="false"
        :trigger-label="t('promptContext.scope')"
        @update:model-value="selectTarget"
      />
      <label v-if="workflow && modelValue?.targetKind === 'WORKFLOW_STAGE'"
        >{{ t("promptContext.stage")
        }}<select
          :value="modelValue.workflowStageKey ?? ''"
          :disabled="disabled || busy"
          @change="selectStage"
        >
          <option value="" disabled>{{ t("promptContext.stage") }}</option>
          <option v-for="item in body?.steps" :key="item.ref" :value="item.ref">
            {{ item.name }}
          </option>
        </select></label
      >
      <label v-if="modelValue"
        >{{ t("promptContext.kind")
        }}<select
          :value="modelValue.templateKind"
          :disabled="disabled || busy"
          @change="selectTemplateKind"
        >
          <option value="INSTRUCTIONS">
            {{ t("promptContext.INSTRUCTIONS") }}
          </option>
          <option value="CONTINUATION">
            {{ t("promptContext.CONTINUATION") }}
          </option>
        </select></label
      >
      <ProblemNotice v-if="problem" :problem="problem" />
      <p v-if="modelValue?.templateKind === 'CONTINUATION'">
        {{ t("promptContext.continuationHint") }}
      </p>
      <PromptTargetPreview
        v-if="target && modelValue?.templateKind !== 'CONTINUATION'"
        :target="target"
        :template="template"
        :disabled="busy"
      />
    </template>
  </section>
</template>
<style scoped>
.prompt-scope-fields {
  min-width: 0;
}
.prompt-scope-fields label {
  display: grid;
  gap: 0.4rem;
}
</style>
