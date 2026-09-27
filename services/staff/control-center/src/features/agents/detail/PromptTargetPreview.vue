<script setup lang="ts">
import { Check, Copy } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";
import type { PromptTemplatePreview } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import type { TemplateVariablePickerItem } from "./model";
import { templateVariableInsertion } from "./model";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
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
const sectionCopyProblem = ref<AppProblem>();
const copiedSectionIndex = ref<number | null>(null);
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
  sectionCopyProblem.value = undefined;
  copiedSectionIndex.value = null;
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
async function copySection(content: string, index: number): Promise<void> {
  copiedSectionIndex.value = null;
  sectionCopyProblem.value = undefined;
  try {
    await navigator.clipboard.writeText(content);
    copiedSectionIndex.value = index;
  } catch (error) {
    sectionCopyProblem.value = asProblem(error);
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
          {{
            t(
              preview.fullMaterializedPrompt
                ? "promptContext.previewFullHint"
                : "promptContext.previewHint",
            )
          }}
        </p>
        <p
          v-if="copiedSectionIndex !== null"
          class="prompt-target-preview__copied"
          role="status"
        >
          {{
            t("promptContext.sectionCopied", {
              number: copiedSectionIndex + 1,
            })
          }}
        </p>
        <ProblemNotice
          v-if="sectionCopyProblem"
          :problem="sectionCopyProblem"
          compact
        />
        <section class="prompt-target-preview__sections">
          <h3>{{ t("promptContext.previewSections") }}</h3>
          <ol>
            <li v-for="(section, index) in preview.sections" :key="index">
              <button
                type="button"
                class="prompt-target-preview__section"
                :aria-label="
                  t('promptContext.copySection', { number: index + 1 })
                "
                :title="t('promptContext.copySection', { number: index + 1 })"
                @click="copySection(section.content, index)"
              >
                <span class="prompt-target-preview__section-head">
                  <span class="prompt-target-preview__section-number">{{
                    index + 1
                  }}</span>
                  <strong>
                    {{ t(`promptDetails.${section.source}`)
                    }}<template v-if="section.slot">
                      · {{ t(`promptDetails.slots.${section.slot}`) }}</template
                    >
                  </strong>
                  <Check v-if="copiedSectionIndex === index" :size="15" />
                  <Copy v-else :size="15" />
                </span>
                <span class="prompt-target-preview__section-content">{{
                  section.content
                }}</span>
              </button>
            </li>
          </ol>
        </section>
        <details class="prompt-target-preview__details">
          <summary>{{ t("promptContext.previewDetails") }}</summary>
          <div class="prompt-target-preview__details-body">
            <PromptContextDetails :preview="preview" />
          </div>
        </details>
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
  gap: 12px;
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
.prompt-target-preview__sections {
  min-width: 0;
}
.prompt-target-preview__sections h3 {
  margin: 0 0 8px;
  font-size: 0.95rem;
}
.prompt-target-preview__sections ol {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.prompt-target-preview__sections li {
  min-width: 0;
}
.prompt-target-preview__section {
  display: block;
  width: 100%;
  min-width: 0;
  min-height: 72px;
  padding: 9px 11px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
  color: var(--text);
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.prompt-target-preview__section:hover {
  border-color: var(--accent-strong);
  background: var(--accent-soft);
}
.prompt-target-preview__section:focus-visible {
  outline: 2px solid var(--accent-strong);
  outline-offset: 2px;
}
.prompt-target-preview__section-head {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  margin-bottom: 5px;
  font-size: 0.82rem;
}
.prompt-target-preview__section-head strong {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.prompt-target-preview__section-head svg {
  flex: 0 0 auto;
  color: var(--muted);
}
.prompt-target-preview__section-number {
  display: grid;
  width: 21px;
  height: 21px;
  flex: 0 0 21px;
  place-items: center;
  border-radius: 50%;
  color: var(--accent-strong);
  background: var(--accent-soft);
  font-size: 0.75rem;
  font-weight: 700;
}
.prompt-target-preview__section-content {
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  color: var(--muted);
  font-size: 0.78rem;
  line-height: 1.35;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.prompt-target-preview__details,
.prompt-target-preview__raw {
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.prompt-target-preview__details summary,
.prompt-target-preview__raw summary {
  cursor: pointer;
  font-weight: 600;
}
.prompt-target-preview__details-body {
  padding-top: 12px;
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
  .prompt-target-preview__sections ol {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
