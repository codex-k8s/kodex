<script setup lang="ts">
import { Search, X } from "@lucide/vue";
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";

import AgentTable from "@/features/agents/catalog/AgentTable.vue";
import { toAgentCatalogItem } from "@/features/agents/catalog/model";
import type { Agent } from "@/shared/api/generated/openapi/types.gen";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";

const props = defineProps<{
  agents: Agent[];
  projectRef: string;
  query: string;
  pageSize: number;
  hasMore: boolean;
  loadingMore: boolean;
}>();
const emit = defineEmits<{
  "update:query": [query: string];
  "update:pageSize": [pageSize: number];
  "load-more": [];
}>();
const { t } = useI18n();
const sentinel = ref<HTMLElement>();
const catalogRoot = ref<HTMLElement>();
const items = computed(() =>
  props.agents
    .map(toAgentCatalogItem)
    .sort((left, right) =>
      left.name.localeCompare(right.name, "ru-RU", { sensitivity: "base" }),
    ),
);
const adaptivePageSize = useAdaptiveCursorPageSize({
  container: catalogRoot,
  itemSelector: ".agent-table tbody tr",
  itemCount: () => items.value.length,
  estimatedItemHeight: 64,
  estimatedColumns: 1,
});
let observer: IntersectionObserver | undefined;

function updateQuery(event: Event): void {
  const target = event.currentTarget;
  if (target instanceof HTMLInputElement) emit("update:query", target.value);
}

function requestNextPage(): void {
  if (props.hasMore && !props.loadingMore) emit("load-more");
}

function bindObserver(): void {
  observer?.disconnect();
  observer = undefined;
  if (!props.hasMore || !sentinel.value) return;
  observer = new IntersectionObserver((entries) => {
    if (entries.some((entry) => entry.isIntersecting)) requestNextPage();
  });
  observer.observe(sentinel.value);
}

onMounted(() => bindObserver());
watch(
  () => [props.hasMore, props.loadingMore, sentinel.value] as const,
  () => void nextTick(bindObserver),
);
watch(adaptivePageSize, (value) => {
  if (value !== props.pageSize) emit("update:pageSize", value);
});
onBeforeUnmount(() => observer?.disconnect());
</script>

<template>
  <section
    ref="catalogRoot"
    class="agent-catalog"
    :aria-label="t('agents.title')"
  >
    <div class="agent-catalog__toolbar">
      <label class="agent-catalog__search">
        <span class="sr-only">{{ t("agents.catalogSearch") }}</span>
        <Search :size="16" aria-hidden="true" />
        <input
          id="agent-catalog-search"
          name="agent-catalog-search"
          :value="query"
          type="search"
          :placeholder="t('agents.catalogSearchPlaceholder')"
          @input="updateQuery"
        />
        <button
          v-if="query"
          type="button"
          :aria-label="t('agents.catalogClearSearch')"
          :title="t('agents.catalogClearSearch')"
          @click="emit('update:query', '')"
        >
          <X :size="15" aria-hidden="true" />
        </button>
      </label>

      <output class="agent-catalog__count" aria-live="polite">
        {{ t("agents.catalogLoaded", { count: items.length }) }}
      </output>
    </div>

    <div v-if="items.length === 0" class="agent-catalog__empty">
      <p>{{ t(query.trim() ? "agents.catalogNoResults" : "common.empty") }}</p>
    </div>

    <AgentTable v-else :items="items" :project-ref="projectRef" />

    <div
      v-if="hasMore || loadingMore"
      ref="sentinel"
      class="agent-catalog__pagination"
      aria-live="polite"
    >
      <span v-if="loadingMore">{{ t("agents.catalogLoadingMore") }}</span>
    </div>
  </section>
</template>

<style scoped>
.agent-catalog {
  display: grid;
  min-width: 0;
  gap: 14px;
}
.agent-catalog__toolbar {
  display: grid;
  grid-template-columns: minmax(260px, 1fr) auto;
  align-items: end;
  min-height: 48px;
  gap: 8px;
}
.agent-catalog__search {
  position: relative;
  display: flex;
  align-items: center;
  min-width: 0;
  font-weight: 400;
}
.agent-catalog__search > svg {
  position: absolute;
  left: 11px;
  color: var(--subtle);
  pointer-events: none;
}
.agent-catalog__search input {
  width: 100%;
  min-width: 0;
  height: var(--control-height);
  padding: 6px 38px 6px 34px;
}
.agent-catalog__search button {
  position: absolute;
  right: 3px;
  display: grid;
  width: 30px;
  height: 30px;
  padding: 0;
  border: 0;
  place-items: center;
  color: var(--muted);
  background: transparent;
  cursor: pointer;
}
.agent-catalog__count {
  min-width: 58px;
  padding-bottom: 8px;
  color: var(--subtle);
  font-family: var(--font-mono);
  font-size: 0.74rem;
  text-align: right;
  white-space: nowrap;
}
.agent-catalog__empty {
  display: grid;
  min-height: 180px;
  place-items: center;
  align-content: center;
  gap: 10px;
  border: 1px dashed var(--border-strong);
  border-radius: 8px;
  color: var(--muted);
  background: var(--panel);
}
.agent-catalog__empty p {
  margin: 0;
}
.agent-catalog__pagination {
  display: flex;
  min-height: 52px;
  align-items: center;
  justify-content: center;
  color: var(--muted);
  font-size: 0.82rem;
}
@media (max-width: 1050px) {
  .agent-catalog__toolbar {
    grid-template-columns: minmax(240px, 1fr) auto;
  }
}
@media (max-width: 760px) {
  .agent-catalog__toolbar {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: end;
  }
  .agent-catalog__search {
    grid-column: 1 / -1;
  }
  .agent-catalog__search input {
    height: 42px;
  }
  .agent-catalog__count {
    display: none;
  }
}
</style>
