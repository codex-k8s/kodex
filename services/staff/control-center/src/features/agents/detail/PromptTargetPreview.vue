<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type { PromptTemplatePreview } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import PromptContextDetails from "./PromptContextDetails.vue";
import TemplateVariableCatalog from "./TemplateVariableCatalog.vue";
import {
  createPromptVariableLoader,
  previewContextPrompt,
  type PromptTarget,
} from "./prompt-context";

const props = defineProps<{
  target?: PromptTarget;
  disabled?: boolean;
  template?: string;
  disabledReason?: string;
}>();
const { t } = useI18n();
const emit = defineEmits<{ checked: [value: boolean] }>();
const full = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const preview = ref<PromptTemplatePreview>();
const contextKey = computed(() => JSON.stringify(props.target ?? {}));
const loader = computed(
  () => props.target && createPromptVariableLoader(props.target),
);
let controller: AbortController | undefined;
function invalidate(): void {
  emit("checked", false);
  controller?.abort();
  controller = undefined;
  busy.value = false;
  preview.value = undefined;
  problem.value = undefined;
}
watch(
  () => [contextKey.value, props.template, props.disabled, full.value],
  invalidate,
  { flush: "sync" },
);
onBeforeUnmount(invalidate);
async function refresh(): Promise<void> {
  if (!props.target || props.disabled || busy.value) return;
  invalidate();
  const active = new AbortController();
  controller = active;
  busy.value = true;
  try {
    const result = await previewContextPrompt(
      props.target,
      props.template ?? "",
      active.signal,
      full.value,
    );
    if (controller === active && !active.signal.aborted) {
      preview.value = result;
      emit("checked", true);
    }
  } catch (error) {
    if (controller === active && !active.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (controller === active) {
      busy.value = false;
      controller = undefined;
    }
  }
}
defineExpose({ refresh });
</script>

<template>
  <section class="prompt-target-preview stack">
    <div class="toolbar">
      <button
        type="button"
        class="button button--secondary"
        :disabled="!target || disabled || busy"
        @click="refresh"
      >
        {{ t("promptContext.preview") }}
      </button>
      <StatusBadge
        v-if="preview"
        :state="preview.complete ? 'AVAILABLE' : 'DRAFT'"
      />
    </div>
    <p v-if="disabledReason && (!target || disabled)" class="text-muted">
      {{ disabledReason }}
    </p>
    <label class="checkbox-label">
      <input
        v-model="full"
        type="checkbox"
        :disabled="disabled || busy || !target"
      />
      <span>{{ t("promptContext.full") }}</span>
    </label>
    <ProblemNotice v-if="problem" :problem="problem" />
    <template v-if="preview">
      <SafeMarkdown
        :content="preview.fullMaterializedPrompt ?? preview.safePreview"
      />
      <PromptContextDetails :preview="preview" />
    </template>
    <TemplateVariableCatalog
      v-if="target?.projectRef && loader && !disabled"
      :project-ref="target.projectRef"
      :load-items="loader"
      :context-key="contextKey"
      :disabled="true"
    />
  </section>
</template>

<style scoped>
.prompt-target-preview {
  min-width: 0;
  overflow-wrap: anywhere;
}
</style>
