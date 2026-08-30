<script
  setup
  lang="ts"
  generic="
    T extends AsyncEntityPickerItem = AsyncEntityPickerItem,
    S extends T | AsyncEntityOption = T
  "
>
import {
  Check,
  ChevronDown,
  CircleAlert,
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
  type AsyncEntityOption,
  type AsyncEntityOptionPage,
  type AsyncEntityPickerItem,
} from "@/shared/ui/async-entity-picker";
import DismissiblePopover from "@/shared/ui/DismissiblePopover.vue";

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
interface PickerEntry extends AsyncEntityPickerItem {
  source: AsyncEntityPickerItem | AsyncEntityOption;
  meta?: string;
  disabledReason?: string;
}

const props = defineProps<{
  modelValue?: PickerValue;
  loadItems?: AsyncEntityLoader<T>;
  labels?: AsyncEntityPickerLabels;
  multiple?: boolean;
  disabled?: boolean;
  debounceMs?: number;
  selected?: S;
  loadPage?: (
    query: string,
    cursor: string | undefined,
    signal: AbortSignal,
  ) => Promise<AsyncEntityOptionPage>;
  placeholder?: string;
  searchPlaceholder?: string;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: PickerValue];
  select: [item: S];
}>();
defineSlots<{ option?(props: { item: T; selected: boolean }): unknown }>();

const inline = props.loadItems !== undefined;
const pickerId = `async-picker-${useId()}`;
const list = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const open = ref(false);
const activeIndex = ref(-1);

const loader: AsyncEntityLoader<PickerEntry> = async (request) => {
  if (props.loadItems) {
    const page = await props.loadItems(request);
    return {
      items: page.items.map((item) => ({
        ...item,
        id: item.id,
        label: item.label,
        source: item,
      })),
      nextCursor: page.nextCursor,
    };
  }
  if (!props.loadPage) throw new Error("Async entity loader is required");
  const page = await props.loadPage(
    request.query.trim(),
    request.cursor,
    request.signal,
  );
  return {
    items: page.items.map((item) => ({
      id: item.ref,
      label: item.title,
      description: item.description,
      disabled: item.disabled,
      disabledReason: item.disabledReason,
      meta: item.meta,
      source: item,
    })),
    nextCursor: page.nextPageToken,
  };
};

const {
  hasMore,
  initialLoading,
  items,
  loadMore,
  loadMoreError,
  loadingMore,
  phase,
  query,
  refresh,
} = useAsyncEntityCollection(loader, {
  debounceMs: props.debounceMs ?? (inline ? 250 : 300),
  immediate: inline,
});

const copy = computed<AsyncEntityPickerLabels>(
  () =>
    props.labels ?? {
      label: props.placeholder ?? "",
      searchPlaceholder: props.searchPlaceholder ?? "",
      loading: "",
      loadingMore: "",
      empty: "",
      error: "",
      retry: "",
    },
);
const popoverLabel = computed(
  () => props.searchPlaceholder ?? props.placeholder ?? copy.value.label,
);
const selectedIds = computed<readonly string[]>(() => {
  if (Array.isArray(props.modelValue))
    return props.modelValue.filter(
      (id): id is string => typeof id === "string",
    );
  return typeof props.modelValue === "string" ? [props.modelValue] : [];
});
const selectedOption = computed(() => {
  const entry = items.value.find((item) => item.id === props.modelValue);
  if (entry && isOption(entry.source)) return entry.source;
  return props.selected && isOption(props.selected)
    ? props.selected
    : undefined;
});
const activeDescendant = computed(() => {
  const item = items.value[activeIndex.value];
  return item ? `${pickerId}-option-${item.id}` : undefined;
});
const infiniteScrollEnabled = computed(
  () => (inline || open.value) && hasMore.value,
);

watch(items, (nextItems, previousItems) => {
  const previousActiveId = previousItems[activeIndex.value]?.id;
  const preservedIndex = previousActiveId
    ? nextItems.findIndex((item) => item.id === previousActiveId)
    : -1;
  activeIndex.value =
    preservedIndex >= 0
      ? preservedIndex
      : nextItems.findIndex((item) => !item.disabled);
});
useCursorInfiniteScroll({
  root: list,
  sentinel,
  enabled: infiniteScrollEnabled,
  loadMore,
});

function isOption(
  value: AsyncEntityPickerItem | AsyncEntityOption,
): value is AsyncEntityOption {
  return "ref" in value && "title" in value;
}
function isSelected(item: PickerEntry): boolean {
  return selectedIds.value.includes(item.id);
}
function secondaryText(item: PickerEntry): string {
  return [item.description, item.meta, item.disabledReason]
    .filter((value): value is string => Boolean(value))
    .join(" · ");
}
function chooseInline(item: PickerEntry): void {
  if (props.disabled || item.disabled) return;
  if (props.multiple) {
    const selection = new Set(selectedIds.value);
    if (selection.has(item.id)) selection.delete(item.id);
    else selection.add(item.id);
    emit("update:modelValue", [...selection]);
  } else emit("update:modelValue", isSelected(item) ? null : item.id);
  emit("select", item.source as S);
}
function chooseDropdown(item: PickerEntry): void {
  if (props.disabled || item.disabled || !isOption(item.source)) return;
  emit("update:modelValue", item.source.ref);
  emit("select", item.source as S);
  close();
}
function moveActive(direction: 1 | -1): void {
  if (!items.value.length) return;
  let index = activeIndex.value;
  for (let attempts = 0; attempts < items.value.length; attempts += 1) {
    index = (index + direction + items.value.length) % items.value.length;
    if (!items.value[index]?.disabled) {
      activeIndex.value = index;
      document
        .getElementById(activeDescendant.value ?? "")
        ?.scrollIntoView({ block: "nearest" });
      return;
    }
  }
}
function handleListKeydown(event: KeyboardEvent, dropdown: boolean): void {
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    moveActive(event.key === "ArrowDown" ? 1 : -1);
    return;
  }
  if (event.key !== "Enter" || activeIndex.value < 0) return;
  const item = items.value[activeIndex.value];
  if (!item) return;
  event.preventDefault();
  if (dropdown) chooseDropdown(item);
  else chooseInline(item);
}
function handleScroll(event: Event): void {
  const target = event.currentTarget;
  if (target instanceof HTMLElement && hasMore.value && nearScrollEnd(target))
    void loadMore();
}
function close(): void {
  open.value = false;
  activeIndex.value = -1;
}
function handlePopoverOpen(value: boolean): void {
  open.value = value;
  if (value) {
    refresh();
  } else {
    activeIndex.value = -1;
  }
}
</script>

<template>
  <section
    v-if="inline"
    class="async-picker async-picker--inline"
    :aria-label="copy.label"
    :aria-busy="initialLoading || loadingMore"
  >
    <label class="async-picker__search"
      ><Search :size="16" aria-hidden="true" /><span class="sr-only">{{
        copy.label
      }}</span
      ><input
        v-model="query"
        type="search"
        :placeholder="copy.searchPlaceholder"
        :disabled="disabled"
        role="combobox"
        :aria-controls="`${pickerId}-listbox`"
        :aria-activedescendant="activeDescendant"
        aria-expanded="true"
        aria-haspopup="listbox"
        aria-autocomplete="list"
        @keydown="handleListKeydown($event, false)"
    /></label>
    <div
      :id="`${pickerId}-listbox`"
      ref="list"
      class="async-picker__options"
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
        />{{ copy.loading }}
      </div>
      <div
        v-else-if="phase === 'error'"
        class="async-picker__state async-picker__state--error"
        role="alert"
      >
        <CircleAlert :size="20" aria-hidden="true" /><span>{{
          copy.error
        }}</span
        ><button type="button" class="async-picker__retry" @click="refresh">
          <RefreshCw :size="15" aria-hidden="true" />{{ copy.retry }}
        </button>
      </div>
      <div
        v-else-if="phase === 'empty'"
        class="async-picker__state"
        role="status"
      >
        <Search :size="20" aria-hidden="true" />{{ copy.empty }}
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
            'async-picker__option--selected': isSelected(item),
          }"
          role="option"
          :aria-selected="isSelected(item)"
          :disabled="disabled || item.disabled"
          @mouseenter="activeIndex = index"
          @click="chooseInline(item)"
        >
          <slot
            name="option"
            :item="item.source as T"
            :selected="isSelected(item)"
            ><span
              class="async-picker__option-copy"
              :class="{
                'async-picker__option-copy--single': !secondaryText(item),
              }"
              ><strong>{{ item.label }}</strong
              ><small v-if="secondaryText(item)">{{
                secondaryText(item)
              }}</small></span
            ></slot
          ><Check v-if="isSelected(item)" :size="17" aria-hidden="true" />
        </button>
        <div ref="sentinel" class="async-picker__sentinel" aria-hidden="true" />
        <div
          v-if="loadingMore"
          class="async-picker__state async-picker__state--more"
          role="status"
        >
          <LoaderCircle
            class="async-picker__spin"
            :size="16"
            aria-hidden="true"
          />{{ copy.loadingMore }}
        </div>
        <div
          v-else-if="loadMoreError"
          class="async-picker__more async-picker__more--error"
          role="alert"
        >
          <CircleAlert :size="16" aria-hidden="true" />
          <span>{{ copy.error }}</span>
          <button type="button" class="async-picker__retry" @click="loadMore">
            <RefreshCw :size="15" aria-hidden="true" />
            {{ copy.retry }}
          </button>
        </div>
      </template>
    </div>
  </section>
  <div v-else class="async-picker">
    <DismissiblePopover
      :open="open"
      :ariaLabel="popoverLabel"
      role="dialog"
      placement="bottom-start"
      width="lg"
      block
      contained
      @update:open="handlePopoverOpen"
    >
      <template #trigger="{ toggle, attrs }">
        <button
          v-bind="attrs"
          class="async-picker__trigger"
          type="button"
          :disabled="disabled"
          @click="toggle"
          @keydown.down.prevent="handlePopoverOpen(true)"
        >
          <span v-if="selectedOption" class="async-picker__selection"
            ><strong>{{ selectedOption.title }}</strong
            ><small v-if="selectedOption.description">{{
              selectedOption.description
            }}</small></span
          ><span v-else class="async-picker__placeholder">{{
            placeholder
          }}</span
          ><ChevronDown :size="17" aria-hidden="true" />
        </button>
      </template>
      <section
        class="async-picker__popover"
        @keydown="handleListKeydown($event, true)"
      >
        <label class="async-picker__search"
          ><Search :size="16" aria-hidden="true" /><span class="sr-only">{{
            searchPlaceholder
          }}</span
          ><input
            v-model="query"
            type="search"
            :placeholder="searchPlaceholder"
            role="combobox"
            :aria-controls="`${pickerId}-listbox`"
            :aria-activedescendant="activeDescendant"
            aria-autocomplete="list"
            aria-expanded="true"
            aria-haspopup="listbox"
        /></label>
        <div
          :id="`${pickerId}-listbox`"
          ref="list"
          class="async-picker__options"
          role="listbox"
          :aria-busy="initialLoading || loadingMore"
          @scroll.passive="handleScroll"
        >
          <div
            v-if="phase === 'initial-loading'"
            class="async-picker__state"
            role="status"
          >
            <LoaderCircle
              class="async-picker__spin"
              :size="18"
              aria-hidden="true"
            />{{ $t("common.loading") }}
          </div>
          <div
            v-else-if="phase === 'error'"
            class="async-picker__state async-picker__state--error"
            role="alert"
          >
            <span>{{ $t("errors.default") }}</span
            ><button class="button" type="button" @click="refresh">
              {{ $t("common.retry") }}
            </button>
          </div>
          <div
            v-else-if="phase === 'empty'"
            class="async-picker__state"
            role="status"
          >
            {{ $t("common.empty") }}
          </div>
          <template v-else>
            <button
              v-for="(item, index) in items"
              :id="`${pickerId}-option-${item.id}`"
              :key="item.id"
              class="async-picker__option"
              :class="{
                'async-picker__option--active': index === activeIndex,
                'async-picker__option--selected': item.id === modelValue,
              }"
              type="button"
              role="option"
              :aria-selected="item.id === modelValue"
              :disabled="item.disabled"
              :title="item.disabledReason"
              @mouseenter="activeIndex = index"
              @click="chooseDropdown(item)"
            >
              <span
                class="async-picker__option-copy"
                :class="{
                  'async-picker__option-copy--single': !secondaryText(item),
                }"
                ><strong>{{ item.label }}</strong
                ><small v-if="secondaryText(item)">{{
                  secondaryText(item)
                }}</small></span
              >
            </button>
            <div
              ref="sentinel"
              class="async-picker__sentinel"
              aria-hidden="true"
            />
            <div
              v-if="loadingMore"
              class="async-picker__state async-picker__state--more"
              role="status"
            >
              <LoaderCircle
                class="async-picker__spin"
                :size="17"
                aria-hidden="true"
              />{{ $t("common.loading") }}
            </div>
            <div
              v-else-if="loadMoreError"
              class="async-picker__more async-picker__more--error"
              role="alert"
            >
              <CircleAlert :size="16" aria-hidden="true" />
              <span>{{ $t("common.error") }}</span>
              <button
                type="button"
                class="async-picker__retry"
                @click="loadMore"
              >
                <RefreshCw :size="15" aria-hidden="true" />
                {{ $t("common.retry") }}
              </button>
            </div>
          </template>
        </div>
        <footer class="async-picker__footer">
          {{ $t("runtime.pickerShown", { count: items.length })
          }}<span v-if="hasMore">{{ $t("runtime.pickerScroll") }}</span>
        </footer>
      </section>
    </DismissiblePopover>
  </div>
</template>

<style scoped>
.async-picker {
  width: 100%;
  min-width: 0;
  max-width: 100%;
}
.async-picker--inline {
  display: flex;
  min-height: 220px;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.async-picker__trigger {
  display: flex;
  width: 100%;
  min-height: 48px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 12px;
  border: 1px solid var(--border-strong);
  border-radius: 7px;
  background: var(--surface);
  color: var(--text);
  text-align: left;
  cursor: pointer;
}
.async-picker__selection,
.async-picker__option-copy {
  display: grid;
  min-width: 0;
  flex: 1;
  gap: 3px;
}
.async-picker__selection strong,
.async-picker__selection small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.async-picker__selection small,
.async-picker__placeholder,
.async-picker__footer,
.async-picker__option small {
  color: var(--text-secondary);
}
.async-picker__popover {
  display: flex;
  width: 100%;
  min-height: 0;
  max-height: inherit;
  flex-direction: column;
  overflow: hidden;
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
.async-picker__search input {
  width: 100%;
  min-width: 0;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--text);
}
.async-picker__options {
  min-height: 0;
  flex: 1;
  overflow-y: auto;
  overscroll-behavior: contain;
}
.async-picker--inline .async-picker__options {
  min-height: 170px;
}
.async-picker__option {
  display: flex;
  width: 100%;
  min-height: 54px;
  align-items: center;
  gap: 10px;
  padding: 9px 12px;
  border: 0;
  border-bottom: 1px solid var(--hairline);
  background: transparent;
  color: var(--text);
  text-align: left;
  cursor: pointer;
  overflow: hidden;
}
.async-picker__option:hover,
.async-picker__option--active {
  background: var(--panel);
}
.async-picker__option--selected {
  box-shadow: inset 3px 0 var(--accent);
  background: var(--accent-soft);
}
.async-picker__option:disabled {
  cursor: not-allowed;
  opacity: 0.58;
}
.async-picker__option-copy strong {
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 1;
  overflow-wrap: anywhere;
}
.async-picker__option-copy small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.async-picker__option-copy--single strong {
  -webkit-line-clamp: 2;
}
.async-picker__state {
  display: flex;
  min-height: 110px;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 10px;
  padding: 16px;
  color: var(--text-secondary);
  text-align: center;
}
.async-picker__state--more {
  min-height: 40px;
  flex-direction: row;
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
.async-picker__footer {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 12px;
  border-top: 1px solid var(--border);
  font-size: 0.8rem;
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
.async-picker__more--error {
  flex-wrap: wrap;
  padding: 6px 12px;
  color: var(--danger);
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
</style>
