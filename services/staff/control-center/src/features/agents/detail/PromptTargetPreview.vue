<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";
import type { PromptTemplatePreview } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import type { TemplateVariablePickerItem } from "./model";
import { templateVariableInsertion } from "./model";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import PromptContextDetails from "./PromptContextDetails.vue";
import TemplateVariableCatalog from "./TemplateVariableCatalog.vue";
import {
  createPromptVariableLoader,
  previewContextPrompt,
  type PromptTarget,
} from "./prompt-context";

defineOptions({ inheritAttrs: false });
const props = defineProps<{
  target?: PromptTarget;
  disabled?: boolean;
  template?: string;
  disabledReason?: string;
}>();
const fieldName = `${useId()}-full-prompt`;
const { t } = useI18n();
const emit = defineEmits<{ checked: [value: boolean] }>();
const full = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const copyProblem = ref<AppProblem>();
const copiedVariable = ref("");
const preview = ref<PromptTemplatePreview>();
const previewOpen = ref(false);
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
  previewOpen.value = false;
  problem.value = undefined;
  copyProblem.value = undefined;
  copiedVariable.value = "";
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
      previewOpen.value = true;
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
async function copyVariable(item: TemplateVariablePickerItem): Promise<void> {
  copiedVariable.value = "";
  copyProblem.value = undefined;
  try {
    await navigator.clipboard.writeText(
      templateVariableInsertion(item.variable),
    );
    copiedVariable.value = item.variable.name;
  } catch (error) {
    copyProblem.value = asProblem(error);
  }
}
defineExpose({ refresh });
</script>

<template>
  <section v-bind="$attrs" class="prompt-target-preview stack">
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
        :id="fieldName"
        :name="fieldName"
        type="checkbox"
        :disabled="disabled || busy || !target"
      />
      <span>{{ t("promptContext.full") }}</span>
    </label>
    <ProblemNotice v-if="problem" :problem="problem" />
    <TemplateVariableCatalog
      v-if="target?.projectRef && loader && !disabled"
      :project-ref="target.projectRef"
      :load-items="loader"
      :context-key="contextKey"
      :disabled="false"
      action="copy"
      @select="copyVariable"
    />
    <p
      v-if="copiedVariable"
      class="prompt-target-preview__copied"
      role="status"
    >
      {{ t("promptContext.copiedVariable", { name: copiedVariable }) }}
    </p>
    <ProblemNotice v-if="copyProblem" :problem="copyProblem" compact />
  </section>
  <Teleport to="body">
    <ModalDialog
      v-if="previewOpen && preview"
      :title="t('promptContext.preview')"
      size="xl"
      @close="previewOpen = false"
    >
      <div class="prompt-target-preview__result">
        <div class="prompt-target-preview__meta">
          <StatusBadge :state="preview.complete ? 'AVAILABLE' : 'DRAFT'" />
          <span
            >{{ t("promptContext.previewRevision") }}:
            <code>{{ preview.serviceTemplateRevision }}</code></span
          >
          <span
            >{{ t("promptContext.previewLocale") }}:
            <code>{{ preview.locale }}</code></span
          >
        </div>
        <p class="prompt-target-preview__hint">
          {{ t("promptContext.previewHint") }}
        </p>
        <div class="prompt-target-preview__columns">
          <section class="prompt-target-preview__sections">
            <h3>{{ t("promptContext.previewSections") }}</h3>
            <ol>
              <li v-for="(section, index) in preview.sections" :key="index">
                <div class="prompt-target-preview__section-head">
                  <span class="prompt-target-preview__section-number">{{
                    index + 1
                  }}</span>
                  <strong
                    >{{ t(`promptDetails.${section.source}`)
                    }}<template v-if="section.slot">
                      · {{ t(`promptDetails.slots.${section.slot}`) }}</template
                    ></strong
                  >
                </div>
                <SafeMarkdown :content="section.content" />
              </li>
            </ol>
          </section>
          <aside class="prompt-target-preview__details">
            <PromptContextDetails :preview="preview" />
          </aside>
        </div>
        <details class="prompt-target-preview__raw">
          <summary>{{ t("promptContext.rawPreview") }}</summary>
          <pre>{{ preview.fullMaterializedPrompt ?? preview.safePreview }}</pre>
        </details>
      </div>
    </ModalDialog>
  </Teleport>
</template>

<style scoped>
.prompt-target-preview {
  min-width: 0;
  overflow-wrap: anywhere;
}
.prompt-target-preview__result {
  display: grid;
  min-width: 0;
  gap: 16px;
  overflow-wrap: anywhere;
}
.prompt-target-preview__copied {
  margin: 0;
  color: var(--success);
  font-size: 0.8rem;
}
.prompt-target-preview__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 18px;
  color: var(--muted);
  font-size: 0.82rem;
}
.prompt-target-preview__meta code {
  color: var(--text);
}
.prompt-target-preview__hint {
  margin: 0;
  color: var(--muted);
}
.prompt-target-preview__columns {
  display: grid;
  grid-template-columns: minmax(0, 1.6fr) minmax(280px, 0.8fr);
  align-items: start;
  gap: 16px;
  min-width: 0;
}
.prompt-target-preview__sections,
.prompt-target-preview__details {
  min-width: 0;
}
.prompt-target-preview__sections h3 {
  margin: 0 0 12px;
  font-size: 0.95rem;
}
.prompt-target-preview__sections ol {
  display: grid;
  gap: 10px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.prompt-target-preview__sections li,
.prompt-target-preview__details {
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.prompt-target-preview__section-head {
  display: flex;
  align-items: center;
  gap: 9px;
  margin-bottom: 9px;
}
.prompt-target-preview__section-number {
  display: grid;
  width: 24px;
  height: 24px;
  flex: 0 0 24px;
  place-items: center;
  border-radius: 50%;
  color: var(--accent-strong);
  background: var(--accent-soft);
  font-size: 0.75rem;
  font-weight: 700;
}
.prompt-target-preview__sections :deep(.safe-markdown) {
  overflow-wrap: anywhere;
}
.prompt-target-preview__raw pre {
  max-height: 320px;
  overflow: auto;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
@media (max-width: 800px) {
  .prompt-target-preview__columns {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
