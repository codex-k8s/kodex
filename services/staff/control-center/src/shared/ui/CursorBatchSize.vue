<script setup lang="ts">
import { computed, useId } from "vue";

type CursorBatchSize = 10 | 20 | 50;

const props = defineProps<{
  modelValue: CursorBatchSize;
  loaded?: number;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: CursorBatchSize];
}>();

const selectId = useId();
const value = computed({
  get: () => props.modelValue,
  set: (next: number) => {
    if (next === 10 || next === 20 || next === 50)
      emit("update:modelValue", next);
  },
});
</script>

<template>
  <div class="cursor-batch-size">
    <label :for="selectId">
      <span>{{ $t("common.rowsPerLoad") }}</span>
      <select :id="selectId" v-model.number="value">
        <option :value="10">10</option>
        <option :value="20">20</option>
        <option :value="50">50</option>
      </select>
    </label>
    <span v-if="typeof loaded === 'number'" class="cursor-batch-size__loaded">
      {{ $t("common.loadedRows", { count: loaded }) }}
    </span>
  </div>
</template>

<style scoped>
.cursor-batch-size,
.cursor-batch-size label {
  display: flex;
  align-items: center;
  gap: 8px;
}
.cursor-batch-size {
  color: var(--muted);
  font-size: 0.82rem;
}
.cursor-batch-size label span {
  white-space: nowrap;
}
.cursor-batch-size select {
  min-width: 72px;
  min-height: 36px;
  padding: 6px 28px 6px 10px;
}
.cursor-batch-size__loaded {
  white-space: nowrap;
}
</style>
