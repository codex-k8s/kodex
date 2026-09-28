<script setup lang="ts">
import { ref } from "vue";
import { useServerMessage } from "@/shared/ui/server-message";
import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";
import SafeSummary from "@/shared/ui/SafeSummary.vue";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";
const props = defineProps<{
  items: OwnerGate[];
  more?: string;
  loading: boolean;
}>();
const emit = defineEmits<{ more: [pageSize: number] }>();
const root = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const pageSize = useAdaptiveCursorPageSize({
  container: root,
  itemSelector: ".home-gate-row",
  itemCount: () => props.items.length,
  estimatedViewportHeight: 552,
  estimatedItemHeight: 92,
  minimum: 6,
  maximum: 100,
});
useCursorInfiniteScroll({
  root,
  sentinel,
  enabled: () => Boolean(props.more) && !props.loading,
  loadMore: () => emit("more", pageSize.value),
});
const serverMessage = useServerMessage();
</script>
<template>
  <div ref="root" class="home-gate-rows">
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
    <div
      v-if="more"
      ref="sentinel"
      class="home-gate-rows__sentinel"
      role="status"
    >
      <span v-if="loading">{{ $t("common.loading") }}</span>
    </div>
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
.home-gate-rows__sentinel {
  min-height: 1px;
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
