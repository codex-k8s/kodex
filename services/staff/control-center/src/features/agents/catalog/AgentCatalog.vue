<script setup lang="ts">
import { Search, X } from "@lucide/vue";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import AgentCard from "@/features/agents/catalog/AgentCard.vue";
import AgentTable from "@/features/agents/catalog/AgentTable.vue";
import {
  availableAgentRoles,
  availableAgentStates,
  filterAgentCatalog,
  toAgentCatalogItem,
  type AgentCatalogView,
  type AgentStateFilter,
} from "@/features/agents/catalog/model";
import type { Agent } from "@/shared/api/generated/openapi/types.gen";
import ViewModeToggle from "@/shared/ui/ViewModeToggle.vue";

const props = defineProps<{
  agents: Agent[];
  projectRef: string;
  view: AgentCatalogView;
}>();
const emit = defineEmits<{ "update:view": [view: AgentCatalogView] }>();
const { t } = useI18n();
const query = ref("");
const state = ref<AgentStateFilter>("ALL");
const role = ref("");
const items = computed(() => props.agents.map(toAgentCatalogItem));
const states = computed(() => availableAgentStates(items.value));
const roles = computed(() => availableAgentRoles(items.value));
const visibleItems = computed(() =>
  filterAgentCatalog(items.value, {
    query: query.value,
    role: role.value,
    state: state.value,
  }),
);
const hasFilters = computed(
  () =>
    Boolean(query.value.trim()) || state.value !== "ALL" || Boolean(role.value),
);

function resetFilters(): void {
  query.value = "";
  state.value = "ALL";
  role.value = "";
}
</script>

<template>
  <section class="agent-catalog" :aria-label="t('agents.title')">
    <div class="agent-catalog__toolbar">
      <label class="agent-catalog__search">
        <span class="sr-only">{{ t("agents.catalogSearch") }}</span>
        <Search :size="16" aria-hidden="true" />
        <input
          v-model="query"
          type="search"
          :placeholder="t('agents.catalogSearchPlaceholder')"
        />
        <button
          v-if="query"
          type="button"
          :aria-label="t('agents.catalogClearSearch')"
          :title="t('agents.catalogClearSearch')"
          @click="query = ''"
        >
          <X :size="15" aria-hidden="true" />
        </button>
      </label>

      <label class="agent-catalog__filter">
        <span>{{ t("common.status") }}</span>
        <select v-model="state">
          <option value="ALL">{{ t("common.all") }}</option>
          <option v-for="value in states" :key="value" :value="value">
            {{ t(`states.${value}`) }}
          </option>
        </select>
      </label>

      <label class="agent-catalog__filter">
        <span>{{ t("agents.role") }}</span>
        <select v-model="role">
          <option value="">{{ t("common.all") }}</option>
          <option v-for="value in roles" :key="value" :value="value">
            {{ value }}
          </option>
        </select>
      </label>

      <output class="agent-catalog__count" aria-live="polite">
        {{ visibleItems.length }} / {{ items.length }}
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

    <div v-if="visibleItems.length === 0" class="agent-catalog__empty">
      <p>{{ t("common.empty") }}</p>
      <button
        v-if="hasFilters"
        class="button"
        type="button"
        @click="resetFilters"
      >
        {{ t("agents.catalogResetFilters") }}
      </button>
    </div>

    <template v-else>
      <div class="agent-catalog__mobile-grid">
        <AgentCard
          v-for="item in visibleItems"
          :key="item.ref"
          :item="item"
          :to="`/projects/${projectRef}/agents/${item.ref}`"
        />
      </div>
      <div class="agent-catalog__desktop-view">
        <div v-if="view === 'grid'" class="agent-catalog__grid">
          <AgentCard
            v-for="item in visibleItems"
            :key="item.ref"
            :item="item"
            :to="`/projects/${projectRef}/agents/${item.ref}`"
          />
        </div>
        <AgentTable v-else :items="visibleItems" :project-ref="projectRef" />
      </div>
    </template>
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
  grid-template-columns:
    minmax(260px, 1fr) minmax(145px, 180px) minmax(160px, 210px)
    auto auto;
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
.agent-catalog__filter {
  display: grid;
  min-width: 0;
  gap: 3px;
}
.agent-catalog__filter span {
  color: var(--subtle);
  font-size: 0.7rem;
  font-weight: 500;
}
.agent-catalog__filter select {
  width: 100%;
  min-width: 0;
  height: 36px;
  padding-block: 5px;
  overflow: hidden;
  text-overflow: ellipsis;
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
@media (max-width: 1050px) {
  .agent-catalog__toolbar {
    grid-template-columns:
      minmax(240px, 1fr) repeat(2, minmax(140px, 190px))
      auto;
  }
  .agent-catalog__count {
    display: none;
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
  .agent-catalog__search input,
  .agent-catalog__filter select {
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
