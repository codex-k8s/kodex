<script setup lang="ts" generic="T extends AsyncEntityPickerItem">
import {
  CircleAlert,
  Check,
  LoaderCircle,
  RefreshCw,
  Search,
} from "@lucide/vue";
import { computed, ref, useId, watch } from "vue";

import {
  nearScrollEnd,
  useAsyncEntityCollection,
  useCursorInfiniteScroll,
  type AsyncEntityLoader,
  type AsyncEntityPickerItem,
} from "@/shared/ui/async-entity-picker";

export interface AsyncEntityPickerLabels {
  label: string;
  searchPlaceholder: string;
  loading: string;
  loadingMore: string;
  empty: string;
  error: string;
  retry: string;
}

type PickerValue = string | null | readonly string[];

const props = withDefaults(
  defineProps<{
    modelValue: PickerValue;
    loadItems: AsyncEntityLoader<T>;
    labels: AsyncEntityPickerLabels;
    multiple?: boolean;
    disabled?: boolean;
    debounceMs?: number;
  }>(),
  {
    debounceMs: 250,
    disabled: false,
    multiple: false,
  },
);
const emit = defineEmits<{
  "update:modelValue": [value: PickerValue];
  select: [item: T];
}>();

const list = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const activeIndex = ref(-1);
const pickerId = `async-picker-${useId()}`;
const {
  hasMore,
  initialLoading,
  items,
  loadMore,
  loadingMore,
  phase,
  query,
  refresh,
} = useAsyncEntityCollection(props.loadItems, {
  debounceMs: props.debounceMs,
});

const selectedIds = computed<readonly string[]>(() => {
  if (Array.isArray(props.modelValue))
    return props.modelValue.filter(
      (identifier): identifier is string => typeof identifier === "string",
    );
  return typeof props.modelValue === "string" ? [props.modelValue] : [];
});
const activeDescendant = computed(() => {
  const item = items.value[activeIndex.value];
  return item ? `${pickerId}-option-${item.id}` : undefined;
});

watch(items, (nextItems) => {
  activeIndex.value = nextItems.findIndex((item) => !item.disabled);
});

useCursorInfiniteScroll({
  root: list,
  sentinel,
  enabled: hasMore,
  loadMore,
});

function selected(item: T): boolean {
  return selectedIds.value.includes(item.id);
}

function select(item: T): void {
  if (props.disabled || item.disabled) return;
  if (props.multiple) {
    const selection = new Set(selectedIds.value);
    if (selection.has(item.id)) selection.delete(item.id);
    else selection.add(item.id);
    emit("update:modelValue", [...selection]);
  } else {
    emit("update:modelValue", selected(item) ? null : item.id);
  }
  emit("select", item);
}

function moveActive(direction: 1 | -1): void {
  if (items.value.length === 0) return;
  let index = activeIndex.value;
  for (let attempts = 0; attempts < items.value.length; attempts += 1) {
    index = (index + direction + items.value.length) % items.value.length;
    if (!items.value[index]?.disabled) {
      activeIndex.value = index;
      document.getElementById(activeDescendant.value ?? "")?.scrollIntoView({
        block: "nearest",
      });
      return;
    }
  }
}

function handleSearchKeydown(event: KeyboardEvent): void {
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    moveActive(event.key === "ArrowDown" ? 1 : -1);
    return;
  }
  if (event.key !== "Enter" || activeIndex.value < 0) return;
  const item = items.value[activeIndex.value];
  if (!item) return;
  event.preventDefault();
  select(item);
}

function handleScroll(event: Event): void {
  const target = event.currentTarget;
  if (target instanceof HTMLElement && hasMore.value && nearScrollEnd(target))
    void loadMore();
}
</script>

<template>
  <section
    class="async-picker"
    :aria-label="labels.label"
    :aria-busy="initialLoading || loadingMore"
  >
    <label class="async-picker__search">
      <Search :size="16" aria-hidden="true" />
      <span class="sr-only">{{ labels.label }}</span>
      <input
        v-model="query"
        type="search"
        :placeholder="labels.searchPlaceholder"
        :disabled="disabled"
        :aria-controls="`${pickerId}-listbox`"
        :aria-activedescendant="activeDescendant"
        aria-autocomplete="list"
        @keydown="handleSearchKeydown"
      />
    </label>

    <div
      :id="`${pickerId}-listbox`"
      ref="list"
      class="async-picker__list"
      role="listbox"
      :aria-multiselectable="multiple || undefined"
      @scroll.passive="handleScroll"
    >
      <div
        v-if="phase === 'initial-loading'"
        class="async-picker__state"
        role="status"
      >
        <LoaderCircle
          class="async-picker__spin"
          :size="20"
          aria-hidden="true"
        />
        <span>{{ labels.loading }}</span>
      </div>

      <div
        v-else-if="phase === 'error'"
        class="async-picker__state async-picker__state--error"
        role="alert"
      >
        <CircleAlert :size="20" aria-hidden="true" />
        <span>{{ labels.error }}</span>
        <button type="button" class="async-picker__retry" @click="refresh">
          <RefreshCw :size="15" aria-hidden="true" />
          {{ labels.retry }}
        </button>
      </div>

      <div
        v-else-if="phase === 'empty'"
        class="async-picker__state"
        role="status"
      >
        <Search :size="20" aria-hidden="true" />
        <span>{{ labels.empty }}</span>
      </div>

      <template v-else>
        <button
          v-for="(item, index) in items"
          :id="`${pickerId}-option-${item.id}`"
          :key="item.id"
          type="button"
          class="async-picker__option"
          :class="{
            'async-picker__option--active': index === activeIndex,
            'async-picker__option--selected': selected(item),
          }"
          role="option"
          :aria-selected="selected(item)"
          :disabled="disabled || item.disabled"
          @mouseenter="activeIndex = index"
          @click="select(item)"
        >
          <slot name="option" :item="item" :selected="selected(item)">
            <span class="async-picker__option-copy">
              <strong>{{ item.label }}</strong>
              <span v-if="item.description">{{ item.description }}</span>
            </span>
          </slot>
          <Check
            v-if="selected(item)"
            class="async-picker__check"
            :size="17"
            aria-hidden="true"
          />
        </button>
        <div ref="sentinel" class="async-picker__sentinel" aria-hidden="true" />
        <div v-if="loadingMore" class="async-picker__more" role="status">
          <LoaderCircle
            class="async-picker__spin"
            :size="16"
            aria-hidden="true"
          />
          {{ labels.loadingMore }}
        </div>
      </template>
    </div>
  </section>
</template>

<style scoped>
.async-picker {
  display: flex;
  min-width: 0;
  min-height: 220px;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.async-picker__search {
  display: flex;
  min-height: 42px;
  align-items: center;
  gap: 8px;
  padding: 0 12px;
  border-bottom: 1px solid var(--border);
  color: var(--muted);
}
.async-picker__search:focus-within {
  box-shadow: inset 0 0 0 2px color-mix(in srgb, var(--accent) 35%, transparent);
}
.async-picker__search input {
  width: 100%;
  min-width: 0;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--text);
}
.async-picker__list {
  min-height: 0;
  flex: 1;
  overflow-y: auto;
  overscroll-behavior: contain;
}
.async-picker__option {
  display: flex;
  width: 100%;
  min-height: 52px;
  align-items: center;
  gap: 10px;
  padding: 9px 12px;
  border: 0;
  border-bottom: 1px solid var(--hairline);
  background: transparent;
  color: var(--text);
  text-align: left;
  cursor: pointer;
}
.async-picker__option:hover,
.async-picker__option--active {
  background: var(--panel);
}
.async-picker__option--selected {
  background: var(--accent-soft);
}
.async-picker__option:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}
.async-picker__option-copy {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 3px;
}
.async-picker__option-copy strong,
.async-picker__option-copy span {
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow-wrap: anywhere;
}
.async-picker__option-copy span {
  color: var(--text-secondary);
  font-size: 12px;
}
.async-picker__check {
  flex: 0 0 auto;
  color: var(--accent);
}
.async-picker__state {
  display: flex;
  min-height: 170px;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 10px;
  padding: 24px;
  color: var(--text-secondary);
  text-align: center;
}
.async-picker__state--error {
  color: var(--danger);
}
.async-picker__retry {
  display: inline-flex;
  min-height: 32px;
  align-items: center;
  gap: 6px;
  padding: 0 10px;
  border: 1px solid var(--border-strong);
  border-radius: 6px;
  background: var(--surface);
  color: var(--text);
  cursor: pointer;
}
.async-picker__sentinel {
  height: 1px;
}
.async-picker__more {
  display: flex;
  min-height: 40px;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--text-secondary);
  font-size: 12px;
}
.async-picker__spin {
  animation: async-picker-spin 0.9s linear infinite;
}
@keyframes async-picker-spin {
  to {
    transform: rotate(360deg);
  }
}
@media (prefers-reduced-motion: reduce) {
  .async-picker__spin {
    animation: none;
  }
}
@media (max-width: 760px) {
  .async-picker__search,
  .async-picker__option {
    min-height: 48px;
  }
}
</style>
