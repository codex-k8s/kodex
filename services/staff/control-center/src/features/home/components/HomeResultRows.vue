<script setup lang="ts">
import type { HomeResultItem } from "../result-catalog";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useServerMessage } from "@/shared/ui/server-message";
defineProps<{ items: HomeResultItem[]; more?: string; loading: boolean }>();
const emit = defineEmits<{ more: []; open: [item: HomeResultItem] }>();
const serverMessage = useServerMessage();
function scroll(event: Event) {
  const element = event.currentTarget as HTMLElement;
  if (element.scrollTop + element.clientHeight >= element.scrollHeight - 80)
    emit("more");
}
</script>
<template>
  <div class="home-result-rows" @scroll="scroll">
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
      <small>{{ item.description }}</small>
      <StatusBadge :state="item.state" />
    </div>
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
}
.home-result-row small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
