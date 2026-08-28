<script setup lang="ts">
import { computed } from "vue";

const props = withDefaults(
  defineProps<{
    modelValue: string;
    label: string;
    readonly?: boolean;
    invalidLines?: number[];
  }>(),
  { readonly: false, invalidLines: () => [] },
);
const emit = defineEmits<{ "update:modelValue": [value: string] }>();

const lines = computed(() => Math.max(1, props.modelValue.split("\n").length));
const invalid = computed(() => new Set(props.invalidLines));
</script>

<template>
  <section class="toml-editor" :class="{ 'toml-editor--readonly': readonly }">
    <header>
      <strong>{{ label }}</strong>
      <span>TOML</span>
    </header>
    <div class="toml-editor__body">
      <ol class="toml-editor__gutter" aria-hidden="true">
        <li
          v-for="line in lines"
          :key="line"
          :class="{ 'toml-editor__line--invalid': invalid.has(line) }"
        >
          {{ line }}
        </li>
      </ol>
      <textarea
        :value="modelValue"
        :readonly="readonly"
        spellcheck="false"
        autocapitalize="off"
        autocomplete="off"
        @input="
          emit(
            'update:modelValue',
            ($event.target as HTMLTextAreaElement).value,
          )
        "
      />
    </div>
  </section>
</template>

<style scoped>
.toml-editor {
  overflow: hidden;
  border: 1px solid var(--border-strong);
  border-radius: 7px;
  background: var(--surface);
}
.toml-editor > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 40px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
  background: var(--panel);
}
.toml-editor > header span {
  color: var(--text-secondary);
  font-family: var(--font-mono);
  font-size: 0.78rem;
}
.toml-editor__body {
  display: grid;
  min-height: 260px;
  grid-template-columns: 44px minmax(0, 1fr);
  background: #fbfcfe;
}
.toml-editor__gutter {
  padding: 12px 8px;
  margin: 0;
  border-right: 1px solid var(--border);
  color: var(--subtle);
  font-family: var(--font-mono);
  line-height: 1.55;
  list-style: none;
  text-align: right;
  user-select: none;
}
.toml-editor__line--invalid {
  color: var(--danger);
  font-weight: 600;
}
.toml-editor textarea {
  width: 100%;
  min-height: 260px;
  resize: vertical;
  padding: 12px;
  border: 0;
  border-radius: 0;
  outline: 0;
  background: transparent;
  color: var(--text);
  font-family: var(--font-mono);
  line-height: 1.55;
  tab-size: 2;
  white-space: pre;
}
.toml-editor--readonly .toml-editor__body {
  background: var(--panel);
}
</style>
