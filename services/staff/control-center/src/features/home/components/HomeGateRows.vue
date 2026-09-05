<script setup lang="ts">
import { useServerMessage } from "@/shared/ui/server-message";
import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";
import SafeSummary from "@/shared/ui/SafeSummary.vue";
defineProps<{ items: OwnerGate[]; more?: string; loading: boolean }>();
const emit = defineEmits<{ more: [] }>();
function scroll(event: Event): void {
  const element = event.currentTarget as HTMLElement;
  if (element.scrollTop + element.clientHeight >= element.scrollHeight - 80)
    emit("more");
}
const serverMessage = useServerMessage();
</script>
<template>
  <div class="home-gate-rows" @scroll="scroll">
    <RouterLink
      v-for="gate in items"
      :key="gate.ref"
      :to="{
        path: '/decisions',
        query: { gateRef: gate.ref, projectRef: gate.projectRef },
      }"
      class="home-gate-row"
    >
      <strong>{{ serverMessage(gate.title) }}</strong>
      <SafeSummary :content="gate.contextSummary" />
      <small>{{ gate.requestedBy.displayName }}</small>
    </RouterLink>
    <button
      v-if="more"
      type="button"
      class="button"
      :disabled="loading"
      @click="emit('more')"
    >
      {{ $t("common.loadMore") }}
    </button>
  </div>
</template>
<style scoped>
.home-gate-rows {
  max-height: 552px;
  overflow: auto;
}
.home-gate-row {
  display: grid;
  gap: 4px;
  height: 92px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--hairline);
  color: inherit;
  text-decoration: none;
}
.home-gate-row:hover {
  background: var(--panel);
}
.home-gate-row strong,
.home-gate-row small {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.home-gate-row :deep(.safe-summary) {
  margin: 0;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 1;
  -webkit-box-orient: vertical;
}
</style>
