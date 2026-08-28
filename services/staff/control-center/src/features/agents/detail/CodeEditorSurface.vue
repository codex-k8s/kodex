<script setup lang="ts">
import { FileCode2, LockKeyhole, ShieldAlert } from "@lucide/vue";
import { computed, ref } from "vue";

import { tokenizeCodeLine } from "@/features/agents/detail/model";

const props = withDefaults(
  defineProps<{
    modelValue: string;
    language: "markdown" | "toml";
    label: string;
    placeholder?: string;
    readonly?: boolean;
    validationMessages?: readonly string[];
    minLines?: number;
  }>(),
  {
    minLines: 12,
    placeholder: "",
    readonly: false,
    validationMessages: () => [],
  },
);

const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const highlight = ref<HTMLElement>();
const gutter = ref<HTMLElement>();
const lines = computed(() =>
  props.modelValue.replace(/\r\n?/g, "\n").split("\n"),
);
const highlightedLines = computed(() =>
  lines.value.map((line) => tokenizeCodeLine(line, props.language)),
);
const editorStyle = computed<Record<string, string>>(() => ({
  "--editor-lines": String(Math.max(props.minLines, lines.value.length)),
}));

function update(event: Event): void {
  const target = event.currentTarget;
  if (target instanceof HTMLTextAreaElement)
    emit("update:modelValue", target.value);
}

function syncScroll(event: Event): void {
  const target = event.currentTarget;
  if (!(target instanceof HTMLTextAreaElement)) return;
  if (highlight.value) {
    highlight.value.scrollLeft = target.scrollLeft;
    highlight.value.scrollTop = target.scrollTop;
  }
  if (gutter.value) gutter.value.scrollTop = target.scrollTop;
}
</script>

<template>
  <div
    class="code-editor"
    :class="{ 'code-editor--readonly': readonly }"
    :style="editorStyle"
  >
    <div class="code-editor__bar">
      <FileCode2 :size="16" aria-hidden="true" />
      <strong>{{ label }}</strong>
      <code>{{ language === "toml" ? "TOML" : "Markdown" }}</code>
      <span class="code-editor__spacer" />
      <LockKeyhole v-if="readonly" :size="15" aria-hidden="true" />
    </div>
    <div class="code-editor__viewport">
      <pre ref="gutter" class="code-editor__gutter" aria-hidden="true"><span
        v-for="(_, index) in lines"
        :key="index"
      >{{ index + 1 }}</span></pre>
      <div class="code-editor__stack">
        <pre
          ref="highlight"
          class="code-editor__highlight"
          aria-hidden="true"
        ><code><span
          v-for="(line, lineIndex) in highlightedLines"
          :key="lineIndex"
          class="code-editor__line"
        ><span
          v-for="(token, tokenIndex) in line"
          :key="tokenIndex"
          :class="`code-editor__token--${token.tone}`"
        >{{ token.text }}</span>{{ lineIndex < highlightedLines.length - 1 ? "\n" : "" }}</span></code></pre>
        <textarea
          class="code-editor__input"
          :value="modelValue"
          :readonly="readonly"
          :placeholder="placeholder"
          :aria-label="label"
          :aria-invalid="validationMessages.length > 0 || undefined"
          spellcheck="false"
          @input="update"
          @scroll="syncScroll"
        />
      </div>
    </div>
    <div class="code-editor__foot" aria-live="polite">
      <span class="mono">{{ lines.length }} · {{ modelValue.length }}</span>
      <span v-if="validationMessages.length" class="code-editor__validation">
        <ShieldAlert :size="14" aria-hidden="true" />
        {{ validationMessages.join(" · ") }}
      </span>
      <span v-else class="code-editor__spacer" />
    </div>
  </div>
</template>

<style scoped>
.code-editor {
  overflow: hidden;
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  background: var(--panel);
}
.code-editor__bar,
.code-editor__foot {
  display: flex;
  min-height: 36px;
  align-items: center;
  gap: 8px;
  padding: 7px 11px;
  color: var(--muted);
  font-size: 0.78rem;
}
.code-editor__bar {
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.code-editor__bar strong {
  color: var(--text);
}
.code-editor__bar code {
  color: var(--accent-strong);
  font-family: var(--font-mono);
}
.code-editor__spacer {
  flex: 1;
}
.code-editor__viewport {
  display: grid;
  grid-template-columns: 44px minmax(0, 1fr);
  height: clamp(240px, calc(var(--editor-lines) * 1.55em + 28px), 440px);
  min-height: 240px;
  background: color-mix(in srgb, var(--panel) 84%, var(--canvas));
}
.code-editor__gutter,
.code-editor__highlight,
.code-editor__input {
  padding-block: 14px;
  margin: 0;
  border: 0;
  border-radius: 0;
  font-family: var(--font-mono);
  font-size: 12.5px;
  font-variant-ligatures: none;
  line-height: 1.55;
  white-space: pre;
  overflow-wrap: normal;
}
.code-editor__gutter {
  display: flex;
  overflow: hidden;
  flex-direction: column;
  padding-inline: 8px;
  border-right: 1px solid var(--border);
  color: var(--subtle);
  text-align: right;
  user-select: none;
}
.code-editor__stack {
  position: relative;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}
.code-editor__highlight,
.code-editor__input {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  padding-inline: 14px;
  overflow: auto;
}
.code-editor__highlight {
  pointer-events: none;
  color: var(--text);
}
.code-editor__input {
  z-index: 1;
  resize: none;
  outline: none;
  background: transparent;
  caret-color: var(--text);
  color: transparent;
  -webkit-text-fill-color: transparent;
}
.code-editor__input::placeholder {
  color: var(--subtle);
  -webkit-text-fill-color: var(--subtle);
}
.code-editor__input:focus-visible {
  box-shadow: inset 0 0 0 2px color-mix(in srgb, var(--accent) 42%, transparent);
}
.code-editor__input::selection {
  background: color-mix(in srgb, var(--accent) 28%, transparent);
}
.code-editor__token--comment {
  color: var(--subtle);
  font-style: italic;
}
.code-editor__token--keyword,
.code-editor__token--section {
  color: var(--accent-strong);
  font-weight: 600;
}
.code-editor__token--string {
  color: var(--success);
}
.code-editor__token--number {
  color: var(--warning);
}
.code-editor__token--variable {
  color: var(--danger);
  background: var(--danger-soft);
}
.code-editor__token--strong {
  color: var(--text);
  font-weight: 700;
}
.code-editor__foot {
  min-height: 34px;
  border-top: 1px solid var(--border);
  background: var(--surface);
}
.code-editor__validation {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 5px;
  color: var(--danger);
  overflow-wrap: anywhere;
}
.code-editor--readonly .code-editor__input {
  cursor: not-allowed;
}
@media (max-width: 640px) {
  .code-editor__viewport {
    grid-template-columns: 34px minmax(0, 1fr);
  }
  .code-editor__gutter,
  .code-editor__highlight,
  .code-editor__input {
    font-size: 12px;
  }
}
</style>
