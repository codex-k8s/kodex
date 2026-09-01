<script setup lang="ts">
defineProps<{
  modelValue: "NEW" | "CONTINUE";
  continueDisabled?: boolean;
  labels: {
    legend: string;
    newTitle: string;
    newDescription: string;
    continueTitle: string;
    continueDescription: string;
  };
}>();
const emit = defineEmits<{
  "update:modelValue": [value: "NEW" | "CONTINUE"];
}>();
</script>

<template>
  <fieldset class="session-policy">
    <legend class="sr-only">{{ labels.legend }}</legend>
    <label class="session-policy__option">
      <input
        type="radio"
        name="new-run-session-policy"
        value="NEW"
        :checked="modelValue === 'NEW'"
        @change="emit('update:modelValue', 'NEW')"
      />
      <span class="session-policy__dot" aria-hidden="true" />
      <span class="session-policy__copy">
        <strong>{{ labels.newTitle }}</strong>
        <span>{{ labels.newDescription }}</span>
      </span>
    </label>
    <label
      class="session-policy__option"
      :class="{ 'session-policy__option--disabled': continueDisabled }"
    >
      <input
        type="radio"
        name="new-run-session-policy"
        value="CONTINUE"
        :checked="modelValue === 'CONTINUE'"
        :disabled="continueDisabled"
        @change="emit('update:modelValue', 'CONTINUE')"
      />
      <span class="session-policy__dot" aria-hidden="true" />
      <span class="session-policy__copy">
        <strong>{{ labels.continueTitle }}</strong>
        <span>{{ labels.continueDescription }}</span>
      </span>
    </label>
  </fieldset>
</template>

<style scoped>
.session-policy {
  display: grid;
  gap: 8px;
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}
.session-policy__option {
  position: relative;
  display: flex;
  min-height: 64px;
  align-items: center;
  gap: 11px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--surface);
  cursor: pointer;
}
.session-policy__option:hover {
  border-color: var(--border-strong);
}
.session-policy__option:focus-within {
  outline: 3px solid rgba(27, 111, 196, 0.45);
  outline-offset: 2px;
}
.session-policy__option:has(input:checked) {
  border-color: var(--accent);
  background: var(--accent-soft);
}
.session-policy__option input {
  position: absolute;
  width: 1px;
  height: 1px;
  min-height: 1px;
  margin: 0;
  opacity: 0;
}
.session-policy__dot {
  width: 18px;
  height: 18px;
  flex: 0 0 18px;
  border: 2px solid var(--border-strong);
  border-radius: 50%;
  background: var(--surface);
  box-shadow: inset 0 0 0 4px var(--surface);
}
.session-policy__option:has(input:checked) .session-policy__dot {
  border-color: var(--accent);
  background: var(--accent);
}
.session-policy__copy {
  display: grid;
  min-width: 0;
  gap: 3px;
}
.session-policy__copy strong {
  font-size: 13px;
}
.session-policy__copy span {
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 400;
  line-height: 1.4;
}
.session-policy__option--disabled {
  cursor: not-allowed;
  opacity: 0.55;
}
</style>
