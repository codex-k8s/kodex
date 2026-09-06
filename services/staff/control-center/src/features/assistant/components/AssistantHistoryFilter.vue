<script setup lang="ts">
import type { AssistantConversation } from "@/shared/api/generated/openapi/types.gen";
const props = defineProps<{
  query: string;
  state: AssistantConversation["state"];
  disabled?: boolean;
}>();
const emit = defineEmits<{
  change: [query: string, state: AssistantConversation["state"]];
}>();
function changeState(event: Event): void {
  const value = (event.target as HTMLSelectElement).value;
  if (value === "ACTIVE" || value === "CLOSED" || value === "ARCHIVED")
    emit("change", props.query, value);
}
</script>
<template>
  <div class="assistant-history-filter">
    <input
      :value="query"
      type="search"
      :disabled="disabled"
      :aria-label="$t('assistant.searchHistory')"
      :placeholder="$t('assistant.searchHistory')"
      @input="emit('change', ($event.target as HTMLInputElement).value, state)"
    />
    <select
      :value="state"
      :disabled="disabled"
      :aria-label="$t('assistant.historyState')"
      @change="changeState"
    >
      <option
        v-for="value in ['ACTIVE', 'CLOSED', 'ARCHIVED']"
        :key="value"
        :value="value"
      >
        {{ $t(`states.${value}`) }}
      </option>
    </select>
  </div>
</template>
<style scoped>
.assistant-history-filter {
  display: grid;
  gap: 8px;
  min-width: 0;
}
.assistant-history-filter input,
.assistant-history-filter select {
  width: 100%;
  min-width: 0;
}
</style>
