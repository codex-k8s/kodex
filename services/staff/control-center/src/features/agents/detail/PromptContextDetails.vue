<script setup lang="ts">
import { Check, Copy } from "@lucide/vue";
import { onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type { PromptTemplatePreview } from "@/shared/api/generated/openapi/types.gen";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { asProblem, type AppProblem } from "@/shared/api/problem";
const props = defineProps<{ preview: PromptTemplatePreview }>();
const { t } = useI18n();
const copiedSectionIndex = ref<number | null>(null);
const copyingSectionIndex = ref<number | null>(null);
const copyProblem = ref<AppProblem>();
let generation = 0;
function resetCopy(): void {
  generation++;
  copiedSectionIndex.value = null;
  copyingSectionIndex.value = null;
  copyProblem.value = undefined;
}
watch(() => props.preview, resetCopy, { flush: "sync" });
onBeforeUnmount(resetCopy);
function sectionTitle(
  section: PromptTemplatePreview["sections"][number],
): string {
  const source = t(`promptDetails.${section.source}`);
  return section.slot
    ? `${source} · ${t(`promptDetails.slots.${section.slot}`)}`
    : source;
}
function isPlaceholder(content: string): boolean {
  return /^\[[A-Z][A-Z0-9_]*(?:\.[A-Z][A-Z0-9_]*)*\]$/u.test(content.trim());
}
async function copySection(index: number): Promise<void> {
  const section = props.preview.sections[index];
  if (!section || copyingSectionIndex.value !== null) return;
  const current = generation;
  copiedSectionIndex.value = null;
  copyProblem.value = undefined;
  copyingSectionIndex.value = index;
  try {
    await navigator.clipboard.writeText(section.content);
    if (current === generation) copiedSectionIndex.value = index;
  } catch (error) {
    if (current === generation) copyProblem.value = asProblem(error);
  } finally {
    if (current === generation) copyingSectionIndex.value = null;
  }
}
</script>
<template>
  <div class="prompt-context-details">
    <ul v-if="preview.diagnostics.length" role="status">
      <li v-for="(diagnostic, index) in preview.diagnostics" :key="index">
        <code>{{ diagnostic.code }}</code> {{ diagnostic.message }}
        <span v-if="diagnostic.line > 0"
          >({{ diagnostic.line }}:{{ diagnostic.column }})</span
        >
      </li>
    </ul>
    <p v-if="!preview.complete" role="status">
      {{ t("promptDetails.incomplete") }}
    </p>
    <section>
      <h4>{{ t("promptDetails.order") }}</h4>
      <p role="status" class="sr-only">
        {{
          copiedSectionIndex !== null
            ? t("promptContext.sectionCopied", {
                number: copiedSectionIndex + 1,
              })
            : ""
        }}
      </p>
      <ProblemNotice v-if="copyProblem" :problem="copyProblem" compact />
      <ol class="prompt-context-details__sections">
        <li
          v-for="(section, index) in preview.sections"
          :key="index"
          class="prompt-context-details__section"
          :class="{
            'prompt-context-details__section--wide': !isPlaceholder(
              section.content,
            ),
          }"
        >
          <div class="prompt-context-details__section-head">
            <span
              class="prompt-context-details__section-number"
              aria-hidden="true"
              >{{ index + 1 }}</span
            >
            <strong>{{ sectionTitle(section) }}</strong>
            <button
              type="button"
              class="icon-button"
              :disabled="copyingSectionIndex !== null"
              :aria-label="
                t('promptContext.copySection', { number: index + 1 })
              "
              :title="t('promptContext.copySection', { number: index + 1 })"
              @click="copySection(index)"
            >
              <Check
                v-if="copiedSectionIndex === index"
                :size="16"
                aria-hidden="true"
              />
              <Copy v-else :size="16" aria-hidden="true" />
            </button>
          </div>
          <div
            class="prompt-context-details__content"
            tabindex="0"
            role="region"
            :aria-label="sectionTitle(section)"
          >
            <button
              v-if="isPlaceholder(section.content)"
              type="button"
              class="prompt-context-details__placeholder"
              :disabled="copyingSectionIndex !== null"
              :aria-label="`${t('promptContext.copySection', { number: index + 1 })}: ${section.content}`"
              @click="copySection(index)"
            >
              <code>{{ section.content }}</code>
            </button>
            <SafeMarkdown v-else :content="section.content" />
          </div>
        </li>
      </ol>
    </section>
    <details v-if="preview.runtimeDiff">
      <summary>{{ t("promptDetails.changes") }}</summary>
      <p>
        <code>{{ preview.runtimeDiff.previousRevisionRef }}</code> ·
        <code>{{ preview.runtimeDiff.digest }}</code>
      </p>
      <ul>
        <li
          v-for="change in preview.runtimeDiff.changes"
          :key="change.component"
        >
          <strong>{{
            t(`promptDetails.components.${change.component}`)
          }}</strong>
          <dl>
            <template
              v-for="(values, side) in {
                previous: change.previous,
                current: change.current,
              }"
              :key="side"
              ><dt>{{ t(`promptDetails.${side}`) }}</dt>
              <dd v-for="(value, index) in values" :key="index">
                <code
                  >{{ value.ref }} {{ value.version }} {{ value.digest }}
                  {{ value.value }}</code
                >
              </dd></template
            >
          </dl>
        </li>
      </ul>
    </details>
    <details>
      <summary>{{ t("promptDetails.versions") }}</summary>
      <dl>
        <dt>{{ t("promptDetails.context") }}</dt>
        <dd>
          <code>{{ preview.contextPin?.digest }}</code>
        </dd>
        <dt>{{ t("promptDetails.template") }}</dt>
        <dd>
          <code>{{ preview.templateRef }} · {{ preview.templateDigest }}</code>
        </dd>
        <dt>{{ t("promptDetails.service") }}</dt>
        <dd>
          <code
            >{{ preview.serviceTemplateRevision }} ·
            {{ preview.serviceTemplateDigest }} · {{ preview.locale }}</code
          >
        </dd>
        <dt>{{ t("promptDetails.snapshot") }}</dt>
        <dd>
          <code>{{ preview.variableSnapshotDigest }}</code>
        </dd>
        <dt>{{ t("promptDetails.materialization") }}</dt>
        <dd>
          <code>{{ preview.materializationDigest }}</code>
        </dd>
      </dl>
    </details>
  </div>
</template>
<style scoped>
.prompt-context-details {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.prompt-context-details code {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
.prompt-context-details dd {
  margin-inline-start: 0;
}
.prompt-context-details li {
  min-width: 0;
}
.prompt-context-details__sections {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 22rem), 1fr));
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.prompt-context-details__section {
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.prompt-context-details__section--wide {
  grid-column: 1 / -1;
}
.prompt-context-details__section-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
.prompt-context-details__section-head strong {
  min-width: 0;
  flex: 1;
  overflow-wrap: anywhere;
}
.prompt-context-details__section-number {
  flex: none;
  color: var(--muted);
  font-variant-numeric: tabular-nums;
}
.prompt-context-details__content {
  max-height: 14rem;
  overflow: auto;
  overscroll-behavior: contain;
}
.prompt-context-details__placeholder {
  max-width: 100%;
  padding: 6px 8px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--panel);
  color: inherit;
  cursor: pointer;
}
</style>
