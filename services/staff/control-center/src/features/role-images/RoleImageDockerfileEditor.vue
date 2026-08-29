<script setup lang="ts">
import { FileCode2, LockKeyhole, ShieldAlert } from "@lucide/vue";
import { computed, ref } from "vue";

import { tokenizeDockerfileLine } from "@/features/role-images/model";

const props = withDefaults(
  defineProps<{
    modelValue: string;
    label: string;
    readonly?: boolean;
    validationMessages?: readonly string[];
  }>(),
  {
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
  lines.value.map((line) => tokenizeDockerfileLine(line)),
);

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
  <div class="dockerfile-editor" :class="{ 'is-readonly': readonly }">
    <header>
      <FileCode2 :size="17" aria-hidden="true" />
      <strong>{{ label }}</strong>
      <code>Dockerfile</code>
      <span />
      <LockKeyhole v-if="readonly" :size="15" aria-hidden="true" />
    </header>
    <div class="dockerfile-editor__viewport">
      <pre
        ref="gutter"
        class="dockerfile-editor__gutter"
        aria-hidden="true"
      ><span
        v-for="(_, index) in lines"
        :key="index"
      >{{ index + 1 }}</span></pre>
      <div class="dockerfile-editor__stack">
        <pre
          ref="highlight"
          class="dockerfile-editor__highlight"
          aria-hidden="true"
        ><code><span
          v-for="(line, lineIndex) in highlightedLines"
          :key="lineIndex"
        ><span
          v-for="(token, tokenIndex) in line"
          :key="tokenIndex"
          :class="`token--${token.tone}`"
        >{{ token.text }}</span>{{ lineIndex < highlightedLines.length - 1 ? "\n" : "" }}</span></code></pre>
        <textarea
          class="dockerfile-editor__input"
          :value="modelValue"
          :readonly="readonly"
          :aria-label="label"
          :aria-invalid="validationMessages.length > 0 || undefined"
          spellcheck="false"
          @input="update"
          @scroll="syncScroll"
        />
      </div>
    </div>
    <footer aria-live="polite">
      <span class="mono">{{ lines.length }} · {{ modelValue.length }}</span>
      <span v-if="validationMessages.length" class="editor-errors">
        <ShieldAlert :size="14" aria-hidden="true" />
        {{ validationMessages.join(" · ") }}
      </span>
    </footer>
  </div>
</template>

<style scoped>
.dockerfile-editor {
  overflow: hidden;
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  background: var(--panel);
}
.dockerfile-editor > header,
.dockerfile-editor > footer {
  display: flex;
  min-height: 38px;
  align-items: center;
  gap: 8px;
  padding: 7px 12px;
  color: var(--muted);
  font-size: 0.8rem;
}
.dockerfile-editor > header {
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.dockerfile-editor > header strong {
  color: var(--text);
}
.dockerfile-editor > header code {
  color: var(--accent-strong);
}
.dockerfile-editor > header > span {
  flex: 1;
}
.dockerfile-editor__viewport {
  display: grid;
  grid-template-columns: 48px minmax(0, 1fr);
  min-height: 440px;
  background: color-mix(in srgb, var(--panel) 84%, var(--canvas));
}
.dockerfile-editor__gutter,
.dockerfile-editor__highlight,
.dockerfile-editor__input {
  padding-block: 15px;
  margin: 0;
  border: 0;
  border-radius: 0;
  font-family: var(--font-mono);
  font-size: 12.5px;
  font-variant-ligatures: none;
  line-height: 1.6;
  white-space: pre;
  overflow-wrap: normal;
}
.dockerfile-editor__gutter {
  display: flex;
  overflow: hidden;
  flex-direction: column;
  padding-inline: 8px;
  border-right: 1px solid var(--border);
  color: var(--subtle);
  text-align: right;
  user-select: none;
}
.dockerfile-editor__stack {
  position: relative;
  min-width: 0;
  min-height: 440px;
  overflow: hidden;
}
.dockerfile-editor__highlight,
.dockerfile-editor__input {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  padding-inline: 15px;
  overflow: auto;
}
.dockerfile-editor__highlight {
  pointer-events: none;
  color: var(--text);
}
.dockerfile-editor__input {
  z-index: 1;
  resize: none;
  outline: none;
  background: transparent;
  caret-color: var(--text);
  color: transparent;
  -webkit-text-fill-color: transparent;
}
.dockerfile-editor__input:focus-visible {
  box-shadow: inset 0 0 0 2px color-mix(in srgb, var(--accent) 42%, transparent);
}
.dockerfile-editor__input::selection {
  background: color-mix(in srgb, var(--accent) 28%, transparent);
}
.token--comment {
  color: var(--subtle);
  font-style: italic;
}
.token--instruction {
  color: var(--accent-strong);
  font-weight: 700;
}
.token--argument {
  color: var(--text);
}
.token--variable {
  color: var(--warning);
  font-weight: 600;
}
.dockerfile-editor > footer {
  justify-content: space-between;
  border-top: 1px solid var(--border);
  background: var(--surface);
}
.editor-errors {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--danger);
}
.is-readonly .dockerfile-editor__input {
  cursor: not-allowed;
}
@media (max-width: 640px) {
  .dockerfile-editor__viewport {
    grid-template-columns: 36px minmax(0, 1fr);
  }
  .dockerfile-editor__viewport,
  .dockerfile-editor__stack {
    min-height: 340px;
  }
}
</style>
