<script setup lang="ts">
import { ChevronDown, X } from "@lucide/vue";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type {
  AsyncEntityLoadRequest,
  AsyncEntityPickerItem,
  AsyncEntityPage,
} from "@/shared/ui/async-entity-picker";
import DismissiblePopover from "@/shared/ui/DismissiblePopover.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { loadProviderAccount, loadProviderAccounts } from "./api";
import {
  isRuntimeEligible,
  normalizeProviderAccountCandidates,
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
type ProviderAccountEligibilityState = "CONNECTING" | "READY" | "UNAVAILABLE";
const emit = defineEmits<{
  "update:modelValue": [value: ProviderAccountCandidate[]];
  "eligibility-state-change": [state: ProviderAccountEligibilityState];
}>();
const { t } = useI18n();
const route = useRoute();
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
const eligibilityState = computed<ProviderAccountEligibilityState>(() =>
  resolvingSelection.value
    ? "CONNECTING"
    : selectionEligible.value
      ? "READY"
      : "UNAVAILABLE",
);
const labels = computed(() => ({
  label: t("providers.selectorLabel"),
  searchPlaceholder: t("providers.searchPlaceholder"),
  loading: t("common.loading"),
  loadingMore: t("common.loading"),
  empty: t("providers.noEligibleAccounts"),
  error: t("errors.default"),
  retry: t("common.retry"),
}));
async function loadAccounts(
  request: AsyncEntityLoadRequest,
): Promise<AsyncEntityPage<AccountOption>> {
  const key = definitionKey.value;
  if (!key) return { items: [], nextCursor: null };
  const page = await loadProviderAccounts(
    request.query,
    request.cursor,
    request.signal,
    key,
  );
  if (request.signal.aborted) return { items: [], nextCursor: null };
  if (page.items.some((account) => !Object.is(account.definitionKey, key)))
    throw new Error("Provider account catalog scope mismatch");
  for (const account of page.items) resolved.value[account.ref] = account;
  return {
    items: page.items.map((account) => ({
      id: account.ref,
      label: account.name,
      description: account.externalAccountMasked,
      disabled: !isRuntimeEligible(account),
      account,
    })),
    nextCursor: page.nextPageToken || null,
  };
}
watch(
  () => ({
    key: definitionKey.value,
    refs: [...selectedRefs.value],
    opened: open.value,
  }),
  async (selection, _previous, cleanup) => {
    const controller = new AbortController();
    cleanup(() => controller.abort());
    if (!selection.key || !selection.refs.length) {
      resolvingSelection.value = false;
      return;
    }
    resolvingSelection.value = true;
    const results = await Promise.allSettled(
      selection.refs.map((ref) => loadProviderAccount(ref, controller.signal)),
    );
    if (controller.signal.aborted) return;
    const additions = { ...resolved.value };
    for (const ref of selection.refs) Reflect.deleteProperty(additions, ref);
    for (const result of results)
      if (
        result.status === "fulfilled" &&
        Object.is(result.value.definitionKey, selection.key)
      )
        additions[result.value.ref] = result.value;
    resolved.value = additions;
    resolvingSelection.value = false;
  },
  { immediate: true },
);
watch(
  () => props.policyMode,
  (mode) => {
    if (props.disabled) return;
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
watch(
  () => [props.definitionKey, props.disabled, route.fullPath],
  () => {
    open.value = false;
  },
);
function setOpen(value: boolean): void {
  open.value = value && !props.disabled && !!definitionKey.value;
}
function choose(value: string | null | readonly string[]): void {
  if (props.disabled) return;
  const refs = typeof value === "string" ? [value] : (value ?? []);
  const candidates = refs.map(
    (ref) =>
      props.modelValue.find((item) => item.accountRef === ref) ?? {
        accountRef: ref,
        weight: 1,
      },
  );
  if (
    candidates.some((item) => {
      const account = resolved.value[item.accountRef];
      return !account || !isRuntimeEligible(account);
    })
  )
    return;
  emit(
    "update:modelValue",
    normalizeProviderAccountCandidates(candidates, props.policyMode),
  );
  if (props.policyMode === "FIXED") open.value = false;
}
function remove(accountRef: string): void {
  if (!props.disabled)
    emit(
      "update:modelValue",
      props.modelValue.filter((item) => item.accountRef !== accountRef),
    );
}
function changeWeight(accountRef: string, event: Event): void {
  if (props.disabled || !(event.currentTarget instanceof HTMLInputElement))
    return;
  const weight = Number(event.currentTarget.value);
  if (!Number.isSafeInteger(weight) || weight < 1 || weight > 10000) return;
  emit(
    "update:modelValue",
    props.modelValue.map((item) =>
      item.accountRef === accountRef ? { ...item, weight } : item,
    ),
  );
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
          <span
            ><strong>{{ $t("providers.selectorLabel") }}</strong
            ><small>{{
              $t("providers.selectedCount", { count: modelValue.length })
            }}</small></span
          ><ChevronDown :size="17" aria-hidden="true" />
        </button>
      </template>
      <section class="provider-selector__popover">
        <AsyncEntityPicker
          :key="definitionKey"
          :load-items="loadAccounts"
          :labels="labels"
          :model-value="selectedRefs"
          :multiple="policyMode !== 'FIXED'"
          :disabled="disabled"
          @update:model-value="choose"
        >
          <template #option="{ item }">
            <span class="provider-selector__option-copy"
              ><strong>{{ item.label }}</strong
              ><small>{{
                item.description || $t("providers.externalAccountPending")
              }}</small
              ><small
                >OpenAI Codex ·
                {{
                  item.account.authorization
                    ? $t(
                        `providers.methods.${item.account.authorization.method}`,
                      )
                    : $t("common.noData")
                }}</small
              ><small v-if="!item.account.enabled">{{
                $t("states.DISABLED")
              }}</small
              ><small v-else-if="!item.account.ready">{{
                $t("providers.runtimeReadinessUnknown")
              }}</small></span
            >
            <StatusBadge :state="item.account.state" />
          </template>
        </AsyncEntityPicker>
        <RouterLink
          class="provider-selector__manage"
          :to="{
            path: '/administration/providers',
            query: { returnTo: route.fullPath },
          }"
          >{{ $t("providers.manageAccounts") }}</RouterLink
        >
      </section>
    </DismissiblePopover>
    <div v-if="selectedAccounts.length" class="provider-selector__selected">
      <article
        v-for="item in selectedAccounts"
        :key="item.candidate.accountRef"
        class="provider-selector__selected-row"
      >
        <span
          ><strong>{{
            item.account?.name ?? $t("providers.accountUnavailable")
          }}</strong
          ><small>{{
            item.account?.externalAccountMasked ||
            $t("providers.accountUnavailableHelp")
          }}</small></span
        >
        <label v-if="policyMode === 'WEIGHTED'"
          ><span>{{ $t("runtime.weight") }}</span
          ><input
            type="number"
            min="1"
            max="10000"
            :value="item.candidate.weight"
            :disabled="disabled"
            @change="changeWeight(item.candidate.accountRef, $event)"
        /></label>
        <button
          class="icon-button icon-button--danger"
          type="button"
          :disabled="disabled"
          :title="
            $t('providers.removeSelection', { name: item.account?.name ?? '' })
          "
          :aria-label="
            $t('providers.removeSelection', { name: item.account?.name ?? '' })
          "
          @click="remove(item.candidate.accountRef)"
        >
          <X :size="16" />
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
.provider-selector__option-copy {
  display: grid;
  gap: 3px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.provider-selector__option-copy strong,
.provider-selector__option-copy small {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
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
