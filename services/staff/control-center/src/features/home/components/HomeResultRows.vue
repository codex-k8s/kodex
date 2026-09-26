<script setup lang="ts">
import { ref } from "vue";
import { useI18n } from "vue-i18n";
import type { HomeResultItem } from "../result-catalog";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useServerMessage } from "@/shared/ui/server-message";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";
const props = defineProps<{
  items: HomeResultItem[];
  more?: string;
  loading: boolean;
  dashboard?: boolean;
}>();
const emit = defineEmits<{
  more: [pageSize: number];
  open: [item: HomeResultItem];
}>();
const serverMessage = useServerMessage();
const { locale } = useI18n();

function formatDate(value: string): string {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return "";
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(timestamp);
}
const root = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const pageSize = useAdaptiveCursorPageSize({
  container: root,
  itemSelector: ".home-result-row",
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
</script>
<template>
  <div
    ref="root"
    class="home-result-rows"
    :class="{ 'home-result-rows--dashboard': dashboard }"
  >
    <div v-for="item in items" :key="item.ref" class="home-result-row">
      <RouterLink v-if="item.to" :to="item.to">{{
        serverMessage(item.title)
      }}</RouterLink>
      <button
        v-else
        class="button button--ghost"
        type="button"
        @click="emit('open', item)"
      >
        {{ item.title }}
      </button>
      <small v-if="item.artifact" class="home-result-row__source">
        <span>{{ $t(`files.source.${item.artifact.source}`) }}</span>
        <template v-if="formatDate(item.artifact.createdAt)">
          <span aria-hidden="true">·</span>
          <time :datetime="item.artifact.createdAt">{{
            formatDate(item.artifact.createdAt)
          }}</time>
        </template>
      </small>
      <small v-else>{{ item.description }}</small>
      <StatusBadge :state="item.state" />
    </div>
    <div
      v-if="more"
      ref="sentinel"
      class="home-result-rows__sentinel"
      role="status"
    >
      <span v-if="loading">{{ $t("common.loading") }}</span>
    </div>
  </div>
</template>
<style scoped>
.home-result-rows {
  max-height: 552px;
  overflow: auto;
}
.home-result-row {
  height: 92px;
  box-sizing: border-box;
  padding: 10px 16px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 6px;
  border-bottom: 1px solid var(--hairline);
}
.home-result-row > :first-child {
  grid-column: 1 / -1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: inherit;
  text-align: left;
  font-weight: 600;
  justify-content: flex-start;
}
.home-result-row small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.home-result-row__source {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
}
.home-result-row__source > :first-child,
.home-result-row__source time {
  overflow: hidden;
  text-overflow: ellipsis;
}
.home-result-rows__sentinel {
  min-height: 1px;
}
.home-result-rows--dashboard {
  max-height: none;
  overflow: visible;
}
.home-result-rows--dashboard .home-result-row {
  height: auto;
  min-height: 74px;
  align-items: center;
  padding: 12px 16px;
}
.home-result-rows--dashboard .home-result-row > :first-child {
  grid-column: 1;
}
.home-result-rows--dashboard .home-result-row small {
  grid-column: 1;
}
.home-result-rows--dashboard .home-result-row :deep(.status-badge) {
  grid-column: 2;
  grid-row: 1 / 3;
}
</style>
