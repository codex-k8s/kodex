<script setup lang="ts">
import { Braces, LoaderCircle, Plus, RefreshCw, Search } from "@lucide/vue";
import { computed, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";

import { createTemplateVariableLoader } from "@/features/agents/detail/api";
import { agentDetailCopy } from "@/features/agents/detail/copy";
import type { TemplateVariablePickerItem } from "@/features/agents/detail/model";
import {
  nearScrollEnd,
  useAsyncEntityCollection,
  type AsyncEntityLoader,
} from "@/shared/ui/async-entity-picker";

const props = defineProps<{
  projectRef: string;
  agentRef?: string;
  runtimeRevisionRef?: string;
  disabled: boolean;
  loadItems?: AsyncEntityLoader<TemplateVariablePickerItem>;
  contextKey?: string;
}>();
const emit = defineEmits<{ select: [item: TemplateVariablePickerItem] }>();
const { locale, t } = useI18n();
const copy = computed(() => agentDetailCopy(locale.value).instructions);
const listboxId = `template-variable-catalog-${useId()}`;
const activeScope = ref("ALL");
const loader: ReturnType<typeof createTemplateVariableLoader> = (request) =>
  props.loadItems
    ? props.loadItems(request)
    : createTemplateVariableLoader(props.projectRef, {
        agentRef: props.agentRef,
        runtimeRevisionRef: props.runtimeRevisionRef,
      })(request);
const {
  hasMore,
  items,
  loadMore,
  loadMoreError,
  loadingMore,
  phase,
  query,
  refresh,
} = useAsyncEntityCollection(loader, { debounceMs: 500 });
watch(
  () => [
    props.projectRef,
    props.agentRef,
    props.runtimeRevisionRef,
    props.contextKey,
  ],
  () => refresh(),
  { flush: "sync" },
);

const scopeOrder = [
  "SYSTEM",
  "USER",
  "ORGANIZATION",
  "PROJECT",
  "AGENT",
  "ENVIRONMENT",
  "RUNTIME",
  "TOOLS",
  "INPUT_FILES",
  "SESSION_FILES",
  "RUN_FILES",
  "WORKFLOW_FILES",
  "PROJECT_FILES",
] as const;

const scopes = computed(() => {
  const present = new Set(items.value.map((item) => item.scope));
  const ordered = scopeOrder.filter((scope) => present.has(scope));
  const additional = [...present]
    .filter((scope) => !scopeOrder.some((known) => known === scope))
    .sort();
  return [...ordered, ...additional];
});
const visibleItems = computed(() =>
  activeScope.value === "ALL"
    ? items.value
    : items.value.filter((item) => item.scope === activeScope.value),
);
const groups = computed(() =>
  scopes.value
    .map((scope) => ({
      scope,
      items: visibleItems.value.filter((item) => item.scope === scope),
    }))
    .filter((group) => group.items.length > 0),
);

watch(scopes, (values) => {
  if (activeScope.value !== "ALL" && !values.includes(activeScope.value))
    activeScope.value = "ALL";
});

function handleScroll(event: Event): void {
  const target = event.currentTarget;
  if (target instanceof HTMLElement && hasMore.value && nearScrollEnd(target))
    void loadMore();
}
</script>

<template>
  <section
    class="variable-catalog"
    :aria-label="copy.variables"
    :aria-busy="phase === 'initial-loading' || loadingMore"
  >
    <div class="variable-catalog__toolbar">
      <label class="variable-catalog__search">
        <Search :size="15" aria-hidden="true" />
        <span class="sr-only">{{ copy.variableSearch }}</span>
        <input
          v-model="query"
          type="search"
          :placeholder="copy.variableSearch"
          :disabled="disabled"
          role="combobox"
          :aria-controls="listboxId"
          aria-expanded="true"
          aria-haspopup="listbox"
          aria-autocomplete="list"
        />
      </label>
      <label class="variable-catalog__scope">
        <span class="sr-only">{{ copy.variableScope }}</span>
        <select v-model="activeScope" :disabled="disabled">
          <option value="ALL">{{ copy.allScopes }}</option>
          <option v-for="scope in scopes" :key="scope" :value="scope">
            {{ scope }}
          </option>
        </select>
      </label>
    </div>

    <div class="variable-catalog__summary" aria-live="polite">
      <span>{{ copy.loadedVariables }}: {{ items.length }}</span>
      <span>{{ copy.visibleScopes }}: {{ groups.length }}</span>
    </div>

    <div
      :id="listboxId"
      class="variable-catalog__list"
      role="listbox"
      @scroll.passive="handleScroll"
    >
      <div
        v-if="phase === 'initial-loading'"
        class="variable-catalog__state"
        role="status"
      >
        <LoaderCircle class="spin" :size="18" aria-hidden="true" />
        {{ t("common.loading") }}
      </div>
      <div
        v-else-if="phase === 'error'"
        class="variable-catalog__state variable-catalog__state--error"
        role="alert"
      >
        <span>{{ t("errors.default") }}</span>
        <button class="button" type="button" @click="refresh">
          <RefreshCw :size="15" aria-hidden="true" />
          {{ t("common.retry") }}
        </button>
      </div>
      <div
        v-else-if="groups.length === 0"
        class="variable-catalog__state"
        role="status"
      >
        <Search :size="18" aria-hidden="true" />
        {{ t("common.empty") }}
      </div>
      <template v-else>
        <section
          v-for="group in groups"
          :key="group.scope"
          class="variable-catalog__group"
          role="group"
          :aria-label="group.scope"
        >
          <h4>
            <Braces :size="14" aria-hidden="true" />
            {{ group.scope }}
            <span>{{ group.items.length }}</span>
          </h4>
          <button
            v-for="item in group.items"
            :key="item.id"
            class="variable-catalog__option"
            type="button"
            role="option"
            aria-selected="false"
            :disabled="disabled || item.disabled"
            @click="emit('select', item)"
          >
            <span class="variable-catalog__option-copy">
              <span class="variable-catalog__option-title">
                <code>{{ item.variable.name }}</code>
                <span>{{ item.variable.valueType }}</span>
              </span>
              <small>{{ item.variable.description }}</small>
              <small v-if="item.disabled" class="variable-catalog__reason">{{
                $t(`templateAvailability.${item.variable.reason}`)
              }}</small>
              <code
                v-if="item.variable.example"
                class="variable-catalog__example"
              >
                {{ item.variable.example }}
              </code>
              <span
                v-if="item.variable.itemFields.length"
                class="variable-catalog__fields"
              >
                <code
                  v-for="field in item.variable.itemFields"
                  :key="field.name"
                >
                  {{ field.name }}: {{ field.valueType }}
                </code>
              </span>
            </span>
            <Plus :size="16" :aria-label="copy.insertVariable" />
          </button>
        </section>
        <div
          v-if="loadingMore"
          class="variable-catalog__state variable-catalog__state--more"
          role="status"
        >
          <LoaderCircle class="spin" :size="16" aria-hidden="true" />
          {{ t("common.loading") }}
        </div>
        <div
          v-else-if="loadMoreError"
          class="variable-catalog__state variable-catalog__state--error"
          role="alert"
        >
          <span>{{ t("errors.default") }}</span>
          <button class="button" type="button" @click="loadMore">
            <RefreshCw :size="15" aria-hidden="true" />
            {{ t("common.retry") }}
          </button>
        </div>
      </template>
    </div>
  </section>
</template>

<style scoped>
.variable-catalog {
  display: grid;
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.variable-catalog__toolbar {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(116px, 0.42fr);
  border-bottom: 1px solid var(--border);
}
.variable-catalog__search {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 7px;
  padding-inline: 10px;
  color: var(--muted);
}
.variable-catalog__search input,
.variable-catalog__scope select {
  width: 100%;
  min-width: 0;
  min-height: 40px;
  border: 0;
  outline: 0;
  background: transparent;
}
.variable-catalog__scope {
  border-left: 1px solid var(--border);
}
.variable-catalog__scope select {
  padding-inline: 8px;
}
.variable-catalog__summary {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  padding: 7px 10px;
  border-bottom: 1px solid var(--hairline);
  color: var(--subtle);
  font-size: 0.7rem;
}
.variable-catalog__list {
  min-height: 250px;
  max-height: 430px;
  overflow-y: auto;
  overscroll-behavior: contain;
}
.variable-catalog__group h4 {
  position: sticky;
  z-index: 1;
  top: 0;
  display: flex;
  min-height: 31px;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  margin: 0;
  border-block: 1px solid var(--hairline);
  color: var(--muted);
  background: var(--panel);
  font-size: 0.72rem;
}
.variable-catalog__group:first-child h4 {
  border-top: 0;
}
.variable-catalog__group h4 span {
  margin-left: auto;
  font-family: var(--font-mono);
}
.variable-catalog__option {
  display: flex;
  width: 100%;
  min-height: 74px;
  align-items: flex-start;
  gap: 8px;
  padding: 9px 10px;
  border: 0;
  border-bottom: 1px solid var(--hairline);
  color: var(--text);
  background: var(--surface);
  text-align: left;
  cursor: pointer;
}
.variable-catalog__option:hover,
.variable-catalog__option:focus-visible {
  background: var(--accent-soft);
}
.variable-catalog__option > svg {
  flex: 0 0 auto;
  margin-top: 2px;
  color: var(--accent-strong);
}
.variable-catalog__option-copy {
  display: grid;
  min-width: 0;
  flex: 1;
  gap: 4px;
}
.variable-catalog__option-title {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 7px;
}
.variable-catalog__option-title code {
  min-width: 0;
  overflow: hidden;
  color: var(--accent-strong);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.variable-catalog__option-title span {
  flex: 0 0 auto;
  color: var(--subtle);
  font-size: 0.66rem;
}
.variable-catalog__option small {
  display: -webkit-box;
  overflow: hidden;
  color: var(--muted);
  line-height: 1.35;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}
.variable-catalog__example {
  overflow: hidden;
  color: var(--subtle);
  font-size: 0.66rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.variable-catalog__fields {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.variable-catalog__fields code {
  padding: 2px 4px;
  border-radius: 4px;
  background: var(--panel);
  font-size: 0.62rem;
}
.variable-catalog__state {
  display: flex;
  min-height: 120px;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 16px;
  color: var(--muted);
  text-align: center;
}
.variable-catalog__state--more {
  min-height: 48px;
}
.variable-catalog__state--error {
  color: var(--danger);
}
@media (max-width: 640px) {
  .variable-catalog__toolbar {
    grid-template-columns: 1fr;
  }
  .variable-catalog__scope {
    border-top: 1px solid var(--border);
    border-left: 0;
  }
}
</style>
