<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";
import { LoaderCircle, Save } from "@lucide/vue";
import { requestSignal } from "@/shared/api/client";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { setProviderAccountConcurrency } from "./api";
import { accountAllows, type ProviderAccount } from "./model";

const props = defineProps<{ account: ProviderAccount }>();
const emit = defineEmits<{ saved: [account: ProviderAccount] }>();
const { t } = useI18n();
const inputId = useId();
const limit = ref(props.account.maximumConcurrentExecutions);
const busy = ref(false);
const problem = ref<AppProblem>();
let controller = new AbortController();
const canEdit = computed(() => accountAllows(props.account, "EDIT"));
const valid = computed(
  () => Number.isInteger(limit.value) && limit.value >= 1 && limit.value <= 256,
);
watch([() => props.account.ref, () => props.account.version, canEdit], () => {
  controller.abort();
  controller = new AbortController();
  busy.value = false;
  limit.value = props.account.maximumConcurrentExecutions;
  problem.value = undefined;
});
onBeforeUnmount(() => controller.abort());

async function save(): Promise<void> {
  if (!valid.value || !canEdit.value || busy.value) return;
  const account = { ...props.account };
  const signal = AbortSignal.any([controller.signal, requestSignal()]);
  busy.value = true;
  problem.value = undefined;
  try {
    const updated = await setProviderAccountConcurrency(
      account,
      limit.value,
      signal,
    );
    if (
      !signal.aborted &&
      props.account.ref === account.ref &&
      props.account.version === account.version &&
      accountAllows(props.account, "EDIT")
    )
      emit("saved", updated);
  } catch (error) {
    if (!signal.aborted) problem.value = asProblem(error);
  } finally {
    if (!signal.aborted) busy.value = false;
  }
}
</script>

<template>
  <form class="provider-concurrency" @submit.prevent="save">
    <p>{{ t("providers.concurrencyHint") }}</p>
    <div class="provider-concurrency__controls">
      <label class="field" :for="inputId">
        <span class="provider-concurrency__label">
          <span>{{ t("providers.concurrencyLimit") }}</span>
          <output :for="inputId">{{ limit }}</output>
        </span>
        <input
          :id="inputId"
          v-model.number="limit"
          type="range"
          min="1"
          max="256"
          step="1"
          :disabled="busy || !canEdit"
        />
        <span class="provider-concurrency__bounds" aria-hidden="true">
          <span>1</span><span>256</span>
        </span>
      </label>
      <button
        class="button button--primary"
        type="submit"
        :disabled="
          busy ||
          !canEdit ||
          !valid ||
          limit === account.maximumConcurrentExecutions
        "
      >
        <LoaderCircle v-if="busy" :size="16" class="spin" aria-hidden="true" />
        <Save v-else :size="16" aria-hidden="true" />
        {{ t("common.save") }}
      </button>
    </div>
    <p class="provider-concurrency__hint">
      {{ t("providers.concurrencyActiveHint") }}
    </p>
    <ProblemNotice v-if="problem" :problem="problem" />
  </form>
</template>

<style scoped>
.provider-concurrency {
  display: grid;
  gap: 16px;
}
.provider-concurrency p {
  margin: 0;
}
.provider-concurrency__controls {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}
.provider-concurrency__controls .field {
  flex: 1 1 240px;
  min-width: 0;
  max-width: 100%;
}
.provider-concurrency__label,
.provider-concurrency__bounds {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.provider-concurrency__label output {
  min-width: 40px;
  padding: 2px 8px;
  border-radius: 6px;
  color: var(--accent-strong);
  background: var(--accent-soft);
  text-align: center;
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}
.provider-concurrency__controls input[type="range"] {
  width: 100%;
  height: 32px;
  min-height: 32px;
  padding: 0;
  border: 0;
  box-shadow: none;
  background: transparent;
  accent-color: var(--accent);
  cursor: pointer;
}
.provider-concurrency__controls input[type="range"]:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.provider-concurrency__bounds {
  font-size: 11px;
  color: var(--muted);
}
.provider-concurrency__hint {
  color: var(--muted);
  font-size: 12px;
}
</style>
