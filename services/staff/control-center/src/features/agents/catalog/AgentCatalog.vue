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

import AgentCard from "@/features/agents/catalog/AgentCard.vue";
import AgentTable from "@/features/agents/catalog/AgentTable.vue";
import {
  toAgentCatalogItem,
  type AgentCatalogView,
} from "@/features/agents/catalog/model";
import type { Agent } from "@/shared/api/generated/openapi/types.gen";
import ViewModeToggle from "@/shared/ui/ViewModeToggle.vue";

const props = defineProps<{
  agents: Agent[];
  projectRef: string;
  view: AgentCatalogView;
  query: string;
  hasMore: boolean;
  loadingMore: boolean;
}>();
const emit = defineEmits<{
  "update:view": [view: AgentCatalogView];
  "update:query": [query: string];
  "load-more": [];
}>();
const { t } = useI18n();
const sentinel = ref<HTMLElement>();
const items = computed(() =>
  props.agents
    .map(toAgentCatalogItem)
    .sort((left, right) =>
      left.name.localeCompare(right.name, "ru-RU", { sensitivity: "base" }),
    ),
);
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
onBeforeUnmount(() => observer?.disconnect());
</script>

<template>
  <section class="agent-catalog" :aria-label="t('agents.title')">
    <div class="agent-catalog__toolbar">
      <label class="agent-catalog__search">
        <span class="sr-only">{{ t("agents.catalogSearch") }}</span>
        <Search :size="16" aria-hidden="true" />
        <input
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

      <ViewModeToggle
        class="agent-catalog__view"
        :model-value="view"
        :ariaLabel="t('agents.catalogView')"
        :grid-label="t('agents.catalogGrid')"
        :list-label="t('agents.catalogTable')"
        @update:model-value="emit('update:view', $event)"
      />
    </div>

    <div v-if="items.length === 0" class="agent-catalog__empty">
      <p>{{ t("common.empty") }}</p>
    </div>

    <template v-else>
      <div class="agent-catalog__mobile-grid">
        <AgentCard
          v-for="item in items"
          :key="item.ref"
          :item="item"
          :to="`/projects/${projectRef}/agents/${item.ref}`"
        />
      </div>
      <div class="agent-catalog__desktop-view">
        <div v-if="view === 'grid'" class="agent-catalog__grid">
          <AgentCard
            v-for="item in items"
            :key="item.ref"
            :item="item"
            :to="`/projects/${projectRef}/agents/${item.ref}`"
          />
        </div>
        <AgentTable v-else :items="items" :project-ref="projectRef" />
      </div>
    </template>

    <div
      v-if="hasMore || loadingMore"
      ref="sentinel"
      class="agent-catalog__pagination"
      aria-live="polite"
    >
      <span v-if="loadingMore">{{ t("agents.catalogLoadingMore") }}</span>
      <button v-else class="button" type="button" @click="requestNextPage">
        {{ t("agents.catalogLoadMore") }}
      </button>
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
  grid-template-columns: minmax(260px, 1fr) auto auto;
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
  height: 36px;
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
.agent-catalog__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(278px, 100%), 1fr));
  gap: 14px;
}
.agent-catalog__mobile-grid {
  display: none;
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
    grid-template-columns: minmax(240px, 1fr) auto auto;
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
  .agent-catalog__view,
  .agent-catalog__count {
    display: none;
  }
  .agent-catalog__grid {
    grid-template-columns: 1fr;
    gap: 10px;
  }
  .agent-catalog__desktop-view {
    display: none;
  }
  .agent-catalog__mobile-grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 10px;
  }
}
</style>
