<script setup lang="ts">
import { ChevronDown, LoaderCircle, Search } from "@lucide/vue";
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";

import { asProblem, type AppProblem } from "@/shared/api/problem";
import type {
  AsyncEntityOption,
  AsyncEntityPage,
} from "@/shared/ui/async-entity-picker";

const props = withDefaults(
  defineProps<{
    modelValue?: string;
    selected?: AsyncEntityOption;
    loadPage: (query: string, cursor?: string) => Promise<AsyncEntityPage>;
    placeholder: string;
    searchPlaceholder: string;
    disabled?: boolean;
  }>(),
  { modelValue: "", selected: undefined, disabled: false },
);
const emit = defineEmits<{
  "update:modelValue": [value: string];
  select: [option: AsyncEntityOption];
}>();

const root = ref<HTMLElement>();
const searchInput = ref<HTMLInputElement>();
const open = ref(false);
const query = ref("");
const items = ref<AsyncEntityOption[]>([]);
const cursor = ref<string>();
const loading = ref(false);
const loadingMore = ref(false);
const problem = ref<AppProblem>();
const activeIndex = ref(-1);
let requestGeneration = 0;
let debounceTimer: ReturnType<typeof setTimeout> | undefined;

const selectedOption = computed(
  () =>
    items.value.find((item) => item.ref === props.modelValue) ?? props.selected,
);
const hasMore = computed(() => Boolean(cursor.value));

function merge(values: AsyncEntityOption[]): void {
  const byRef = new Map(items.value.map((item) => [item.ref, item]));
  for (const item of values) byRef.set(item.ref, item);
  items.value = [...byRef.values()];
}

async function load(reset: boolean): Promise<void> {
  if (props.disabled || (!reset && (!cursor.value || loadingMore.value)))
    return;
  const generation = ++requestGeneration;
  if (reset) {
    loading.value = true;
    items.value = [];
    cursor.value = undefined;
    activeIndex.value = -1;
  } else {
    loadingMore.value = true;
  }
  problem.value = undefined;
  try {
    const page = await props.loadPage(
      query.value.trim(),
      reset ? undefined : cursor.value,
    );
    if (requestGeneration !== generation) return;
    if (reset) items.value = page.items;
    else merge(page.items);
    cursor.value = page.nextPageToken;
  } catch (error) {
    if (requestGeneration === generation) problem.value = asProblem(error);
  } finally {
    if (requestGeneration === generation) {
      loading.value = false;
      loadingMore.value = false;
    }
  }
}

function toggle(): void {
  if (props.disabled) return;
  open.value = !open.value;
  if (open.value) {
    void load(true);
    void nextTick(() => searchInput.value?.focus());
  }
}

function close(): void {
  open.value = false;
  activeIndex.value = -1;
}

function choose(option: AsyncEntityOption): void {
  if (option.disabled) return;
  emit("update:modelValue", option.ref);
  emit("select", option);
  close();
}

function onDocumentPointerDown(event: PointerEvent): void {
  if (!root.value?.contains(event.target as Node)) close();
}

function onScroll(event: Event): void {
  const element = event.currentTarget as HTMLElement;
  if (
    hasMore.value &&
    element.scrollTop + element.clientHeight >= element.scrollHeight - 48
  ) {
    void load(false);
  }
}

function move(delta: number): void {
  if (!items.value.length) return;
  let next = activeIndex.value;
  for (let attempt = 0; attempt < items.value.length; attempt += 1) {
    next = (next + delta + items.value.length) % items.value.length;
    if (!items.value[next]?.disabled) {
      activeIndex.value = next;
      return;
    }
  }
}

function onKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape") {
    close();
    return;
  }
  if (!open.value && ["Enter", " ", "ArrowDown"].includes(event.key)) {
    event.preventDefault();
    toggle();
    return;
  }
  if (!open.value) return;
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    move(event.key === "ArrowDown" ? 1 : -1);
  } else if (event.key === "Enter" && activeIndex.value >= 0) {
    event.preventDefault();
    const option = items.value[activeIndex.value];
    if (option) choose(option);
  }
}

watch(query, () => {
  if (!open.value) return;
  if (debounceTimer) clearTimeout(debounceTimer);
  debounceTimer = setTimeout(() => void load(true), 300);
});

onMounted(() =>
  document.addEventListener("pointerdown", onDocumentPointerDown),
);
onBeforeUnmount(() => {
  requestGeneration += 1;
  if (debounceTimer) clearTimeout(debounceTimer);
  document.removeEventListener("pointerdown", onDocumentPointerDown);
});
</script>

<template>
  <div ref="root" class="async-picker" @keydown="onKeydown">
    <button
      class="async-picker__trigger"
      type="button"
      role="combobox"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :disabled="disabled"
      @click="toggle"
    >
      <span v-if="selectedOption" class="async-picker__selection">
        <strong>{{ selectedOption.title }}</strong>
        <small v-if="selectedOption.description">{{
          selectedOption.description
        }}</small>
      </span>
      <span v-else class="async-picker__placeholder">{{ placeholder }}</span>
      <ChevronDown :size="17" aria-hidden="true" />
    </button>
    <section v-if="open" class="async-picker__popover">
      <label class="async-picker__search">
        <Search :size="16" aria-hidden="true" />
        <span class="sr-only">{{ searchPlaceholder }}</span>
        <input
          ref="searchInput"
          v-model="query"
          type="search"
          :placeholder="searchPlaceholder"
        />
      </label>
      <div
        class="async-picker__options"
        role="listbox"
        :aria-busy="loading || loadingMore"
        @scroll="onScroll"
      >
        <div v-if="loading" class="async-picker__state" role="status">
          <LoaderCircle class="spin" :size="18" aria-hidden="true" />
          {{ $t("common.loading") }}
        </div>
        <div v-else-if="problem" class="async-picker__state" role="alert">
          <span>{{ problem.title ?? $t("errors.default") }}</span>
          <button
            v-if="problem.retryable"
            class="button"
            type="button"
            @click="load(true)"
          >
            {{ $t("common.retry") }}
          </button>
        </div>
        <div v-else-if="!items.length" class="async-picker__state">
          {{ $t("common.empty") }}
        </div>
        <button
          v-for="(option, index) in items"
          v-else
          :key="option.ref"
          class="async-picker__option"
          :class="{
            'async-picker__option--active': index === activeIndex,
            'async-picker__option--selected': option.ref === modelValue,
          }"
          type="button"
          role="option"
          :aria-selected="option.ref === modelValue"
          :aria-disabled="option.disabled"
          :disabled="option.disabled"
          :title="option.disabledReason"
          @mouseenter="activeIndex = index"
          @click="choose(option)"
        >
          <span>
            <strong>{{ option.title }}</strong>
            <small v-if="option.description">{{ option.description }}</small>
          </span>
          <small v-if="option.meta">{{ option.meta }}</small>
          <small v-if="option.disabledReason" class="async-picker__reason">{{
            option.disabledReason
          }}</small>
        </button>
        <div v-if="loadingMore" class="async-picker__state" role="status">
          <LoaderCircle class="spin" :size="17" aria-hidden="true" />
          {{ $t("common.loading") }}
        </div>
      </div>
      <footer class="async-picker__footer">
        {{ $t("runtime.pickerShown", { count: items.length }) }}
        <span v-if="hasMore">{{ $t("runtime.pickerScroll") }}</span>
      </footer>
    </section>
  </div>
</template>

<style scoped>
.async-picker {
  position: relative;
  min-width: 0;
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
.async-picker__selection {
  display: grid;
  min-width: 0;
  gap: 2px;
}
.async-picker__selection strong,
.async-picker__selection small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.async-picker__selection small,
.async-picker__placeholder,
.async-picker__footer {
  color: var(--text-secondary);
}
.async-picker__popover {
  position: absolute;
  z-index: 60;
  top: calc(100% + 6px);
  left: 0;
  width: min(560px, calc(100vw - 40px));
  overflow: hidden;
  border: 1px solid var(--border-strong);
  border-radius: 8px;
  background: var(--surface);
  box-shadow: 0 18px 40px rgba(16, 22, 30, 0.18);
}
.async-picker__search {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px;
  border-bottom: 1px solid var(--border);
}
.async-picker__search input {
  width: 100%;
  border: 0;
  outline: 0;
}
.async-picker__options {
  max-height: 340px;
  overflow: auto;
}
.async-picker__option {
  display: grid;
  width: 100%;
  min-height: 62px;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 0;
  border-bottom: 1px solid var(--hairline);
  background: transparent;
  color: var(--text);
  text-align: left;
  cursor: pointer;
}
.async-picker__option > span {
  display: grid;
  min-width: 0;
  gap: 3px;
}
.async-picker__option small {
  color: var(--text-secondary);
}
.async-picker__option--active,
.async-picker__option:hover {
  background: var(--panel);
}
.async-picker__option--selected {
  box-shadow: inset 3px 0 var(--accent);
  background: var(--accent-soft);
}
.async-picker__option:disabled {
  cursor: not-allowed;
  opacity: 0.62;
}
.async-picker__reason {
  grid-column: 1 / -1;
  color: var(--warning) !important;
}
.async-picker__state {
  display: flex;
  min-height: 96px;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 16px;
  color: var(--text-secondary);
}
.async-picker__footer {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 12px;
  border-top: 1px solid var(--border);
  font-size: 0.8rem;
}
.spin {
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
