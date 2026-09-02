<script setup lang="ts">
import { Check, ChevronDown, LoaderCircle, Search, X } from "@lucide/vue";
import { computed, ref, watch } from "vue";

import {
  nearScrollEnd,
  useAsyncEntityCollection,
  type AsyncEntityPickerItem,
} from "@/shared/ui/async-entity-picker";
import DismissiblePopover from "@/shared/ui/DismissiblePopover.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

import { loadProviderAccount, loadProviderAccounts } from "./api";
import {
  isRuntimeEligible,
  normalizeProviderAccountCandidates,
  toggleProviderAccountCandidate,
  type ProviderAccount,
  type ProviderAccountCandidate,
  type ProviderDefinitionKey,
  type ProviderPolicyMode,
} from "./model";

interface AccountOption extends AsyncEntityPickerItem {
  account: ProviderAccount;
}

const props = defineProps<{
  modelValue: ProviderAccountCandidate[];
  definitionKey: string;
  policyMode: ProviderPolicyMode;
  disabled?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: ProviderAccountCandidate[]];
  "eligibility-state-change": [state: ProviderAccountEligibilityState];
}>();

type ProviderAccountEligibilityState = "CONNECTING" | "READY" | "UNAVAILABLE";

const open = ref(false);
const resolved = ref<Record<string, ProviderAccount>>({});
const resolvingSelection = ref(false);

const definitionKey = computed(() =>
  props.definitionKey === "openai-codex"
    ? (props.definitionKey as ProviderDefinitionKey)
    : undefined,
);
const selectedRefs = computed(() =>
  props.modelValue.map((item) => item.accountRef),
);
const selectedAccounts = computed(() =>
  props.modelValue.map((candidate) => ({
    candidate,
    account: resolved.value[candidate.accountRef],
  })),
);
const selectionEligible = computed(
  () =>
    props.modelValue.length > 0 &&
    selectedAccounts.value.every(
      ({ account }) =>
        account !== undefined &&
        account.definitionKey === definitionKey.value &&
        isRuntimeEligible(account),
    ),
);
const eligibilityState = computed<ProviderAccountEligibilityState>(() => {
  if (resolvingSelection.value) return "CONNECTING";
  return selectionEligible.value ? "READY" : "UNAVAILABLE";
});

const { hasMore, items, loadMore, loadingMore, phase, query, refresh } =
  useAsyncEntityCollection<AccountOption>(
    async ({ cursor, query: search, signal }) => {
      if (!definitionKey.value) return { items: [], nextCursor: null };
      const page = await loadProviderAccounts(
        search,
        cursor,
        signal,
        definitionKey.value,
      );
      const matching = page.items.filter(
        (account) => account.definitionKey === definitionKey.value,
      );
      for (const account of matching)
        resolved.value = { ...resolved.value, [account.ref]: account };
      return {
        items: matching.map((account) => ({
          id: account.ref,
          label: account.name,
          description: account.externalAccountMasked,
          disabled: !isRuntimeEligible(account),
          account,
        })),
        nextCursor: page.nextPageToken || null,
      };
    },
    { immediate: false, debounceMs: 250 },
  );

watch(
  () => ({ definitionKey: definitionKey.value, refs: [...selectedRefs.value] }),
  async (selection, _previous, onCleanup) => {
    const controller = new AbortController();
    onCleanup(() => {
      controller.abort();
    });
    const missing = selection.refs.filter(
      (ref) => resolved.value[ref]?.definitionKey !== selection.definitionKey,
    );
    if (!selection.definitionKey || !missing.length) {
      resolvingSelection.value = false;
      return;
    }
    resolvingSelection.value = true;
    const results = await Promise.allSettled(
      missing.map((ref) => loadProviderAccount(ref, controller.signal)),
    );
    if (controller.signal.aborted) return;
    const additions: Record<string, ProviderAccount> = {};
    for (const result of results)
      if (result.status === "fulfilled")
        additions[result.value.ref] = result.value;
    resolved.value = { ...resolved.value, ...additions };
    resolvingSelection.value = false;
  },
  { immediate: true },
);
watch(
  () => props.policyMode,
  (mode) => {
    const normalized = normalizeProviderAccountCandidates(
      props.modelValue,
      mode,
    );
    if (JSON.stringify(normalized) !== JSON.stringify(props.modelValue))
      emit("update:modelValue", normalized);
  },
);
watch(eligibilityState, (state) => emit("eligibility-state-change", state), {
  immediate: true,
});
watch(definitionKey, () => {
  if (open.value) refresh();
});

function setOpen(value: boolean): void {
  open.value = value;
  if (value) refresh();
}

function selected(ref: string): boolean {
  return selectedRefs.value.includes(ref);
}

function toggle(account: ProviderAccount): void {
  if (props.disabled || !isRuntimeEligible(account)) return;
  emit(
    "update:modelValue",
    toggleProviderAccountCandidate(
      props.modelValue,
      account.ref,
      props.policyMode,
    ),
  );
}

function remove(accountRef: string): void {
  emit(
    "update:modelValue",
    props.modelValue.filter((item) => item.accountRef !== accountRef),
  );
}

function changeWeight(accountRef: string, event: Event): void {
  const target = event.currentTarget;
  if (!(target instanceof HTMLInputElement)) return;
  const weight = Number(target.value);
  if (!Number.isSafeInteger(weight) || weight < 1 || weight > 10_000) return;
  emit(
    "update:modelValue",
    props.modelValue.map((item) =>
      item.accountRef === accountRef ? { ...item, weight } : item,
    ),
  );
}

function handleScroll(event: Event): void {
  const target = event.currentTarget;
  if (target instanceof HTMLElement && hasMore.value && nearScrollEnd(target))
    void loadMore();
}
</script>

<template>
  <div class="provider-selector">
    <DismissiblePopover
      :open="open"
      :ariaLabel="$t('providers.selectorLabel')"
      placement="bottom-start"
      width="lg"
      block
      contained
      @update:open="setOpen"
    >
      <template #trigger="{ toggle, attrs }">
        <button
          v-bind="attrs"
          class="provider-selector__trigger"
          type="button"
          :disabled="disabled || !definitionKey"
          @click="toggle"
        >
          <span>
            <strong>{{ $t("providers.selectorLabel") }}</strong>
            <small>{{
              $t("providers.selectedCount", { count: modelValue.length })
            }}</small>
          </span>
          <ChevronDown :size="17" aria-hidden="true" />
        </button>
      </template>
      <section class="provider-selector__popover">
        <label class="provider-selector__search">
          <Search :size="16" aria-hidden="true" />
          <span class="sr-only">{{ $t("providers.search") }}</span>
          <input
            v-model="query"
            type="search"
            :placeholder="$t('providers.searchPlaceholder')"
          />
        </label>
        <div
          class="provider-selector__options"
          role="listbox"
          aria-multiselectable="true"
          @scroll.passive="handleScroll"
        >
          <p v-if="phase === 'initial-loading'" role="status">
            <LoaderCircle class="spin" :size="17" aria-hidden="true" />
            {{ $t("common.loading") }}
          </p>
          <p v-else-if="phase === 'error'" role="alert">
            {{ $t("errors.default") }}
          </p>
          <p v-else-if="phase === 'empty'" role="status">
            {{ $t("providers.noEligibleAccounts") }}
          </p>
          <button
            v-for="item in items"
            v-else
            :key="item.id"
            class="provider-selector__option"
            type="button"
            role="option"
            :aria-selected="selected(item.id)"
            :disabled="item.disabled"
            @click="toggle(item.account)"
          >
            <span>
              <strong>{{ item.label }}</strong>
              <small>{{
                item.description || $t("providers.externalAccountPending")
              }}</small>
            </span>
            <StatusBadge
              :state="item.account.ready ? item.account.state : 'UNAVAILABLE'"
            />
            <Check v-if="selected(item.id)" :size="17" aria-hidden="true" />
          </button>
          <p v-if="loadingMore" role="status">
            <LoaderCircle class="spin" :size="16" aria-hidden="true" />
            {{ $t("common.loading") }}
          </p>
        </div>
        <RouterLink
          class="provider-selector__manage"
          to="/administration/providers"
        >
          {{ $t("providers.manageAccounts") }}
        </RouterLink>
      </section>
    </DismissiblePopover>

    <div v-if="selectedAccounts.length" class="provider-selector__selected">
      <article
        v-for="item in selectedAccounts"
        :key="item.candidate.accountRef"
        class="provider-selector__selected-row"
      >
        <span>
          <strong>{{
            item.account?.name ?? $t("providers.accountUnavailable")
          }}</strong>
          <small>{{
            item.account?.externalAccountMasked ||
            $t("providers.accountUnavailableHelp")
          }}</small>
        </span>
        <label v-if="policyMode === 'WEIGHTED'">
          <span>{{ $t("runtime.weight") }}</span>
          <input
            type="number"
            min="1"
            max="10000"
            :value="item.candidate.weight"
            :disabled="disabled"
            @change="changeWeight(item.candidate.accountRef, $event)"
          />
        </label>
        <button
          class="icon-button icon-button--danger"
          type="button"
          :disabled="disabled"
          :aria-label="
            $t('providers.removeSelection', { name: item.account?.name ?? '' })
          "
          @click="remove(item.candidate.accountRef)"
        >
          <X :size="16" aria-hidden="true" />
        </button>
      </article>
    </div>
  </div>
</template>

<style scoped>
.provider-selector {
  display: grid;
  gap: 10px;
}
.provider-selector__trigger {
  display: flex;
  width: 100%;
  min-height: 54px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 9px 12px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--panel);
  color: var(--text);
  text-align: left;
}
.provider-selector__trigger span,
.provider-selector__option span,
.provider-selector__selected-row > span {
  display: grid;
  min-width: 0;
  gap: 3px;
}
.provider-selector small {
  color: var(--muted);
}
.provider-selector__popover {
  display: grid;
  min-width: min(430px, calc(100vw - 32px));
  gap: 8px;
}
.provider-selector__search {
  display: flex;
  align-items: center;
  gap: 8px;
}
.provider-selector__search input {
  min-width: 0;
  flex: 1;
}
.provider-selector__options {
  display: grid;
  max-height: 310px;
  gap: 4px;
  overflow-y: auto;
}
.provider-selector__options > p {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0;
  padding: 16px 10px;
  color: var(--muted);
}
.provider-selector__option {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 10px;
  padding: 10px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: var(--text);
  text-align: left;
}
.provider-selector__option:hover:not(:disabled) {
  border-color: var(--border);
  background: var(--surface);
}
.provider-selector__manage {
  justify-self: end;
  font-size: 0.82rem;
}
.provider-selector__selected {
  display: grid;
  gap: 6px;
}
.provider-selector__selected-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
}
.provider-selector__selected-row label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.76rem;
}
.provider-selector__selected-row input {
  width: 76px;
}
.spin {
  animation: spin 0.9s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
@media (max-width: 560px) {
  .provider-selector__selected-row {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .provider-selector__selected-row label {
    grid-column: 1 / -1;
    grid-row: 2;
  }
}
</style>
