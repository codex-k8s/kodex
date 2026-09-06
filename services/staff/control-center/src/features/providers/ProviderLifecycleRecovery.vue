<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type { ProviderAccount } from "./model";
import {
  readProviderLifecycleAttempt,
  type ProviderLifecycleAttempt,
} from "./lifecycle-attempt";
import {
  retryProviderLifecycle,
  type ProviderLifecycleResult,
} from "./lifecycle";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
const props = defineProps<{ account: ProviderAccount; problem?: unknown }>();
const emit = defineEmits<{
  recovered: [result: ProviderLifecycleResult];
  pending: [value: boolean];
}>();
const { t } = useI18n();
const attempt = ref<ProviderLifecycleAttempt>();
const recoveryProblem = ref<AppProblem>();
const busy = ref(false);
let controller = new AbortController();
function read(): void {
  controller.abort();
  controller = new AbortController();
  attempt.value = undefined;
  recoveryProblem.value = undefined;
  busy.value = false;
  try {
    attempt.value = readProviderLifecycleAttempt(
      props.account.ref,
      window.sessionStorage,
    );
  } catch (error) {
    recoveryProblem.value = asProblem(error);
  }
  emit("pending", !!attempt.value || !!recoveryProblem.value);
}
async function retry(): Promise<void> {
  const original = attempt.value;
  if (!original || busy.value) return;
  const signal = controller.signal;
  busy.value = true;
  recoveryProblem.value = undefined;
  try {
    const result = await retryProviderLifecycle(
      original,
      window.sessionStorage,
      signal,
    );
    if (signal.aborted) return;
    attempt.value = undefined;
    emit("pending", false);
    emit("recovered", result);
  } catch (error) {
    if (!signal.aborted) recoveryProblem.value = asProblem(error);
  } finally {
    if (!signal.aborted) busy.value = false;
  }
}
watch(() => [props.account.ref, props.problem], read, { immediate: true });
onBeforeUnmount(() => controller.abort());
</script>
<template>
  <section v-if="attempt || recoveryProblem" class="provider-recovery">
    <p v-if="attempt" role="status">{{ t("providerLifecycle.pending") }}</p>
    <template v-if="attempt">
      <strong>{{ t(`providerLifecycle.actions.${attempt.action}`) }}</strong>
      <span>{{
        t("providerLifecycle.originalVersion", { version: attempt.version })
      }}</span>
      <button type="button" class="button" :disabled="busy" @click="retry">
        {{ t("providerLifecycle.retry") }}
      </button>
    </template>
    <ProblemNotice v-if="recoveryProblem" :problem="recoveryProblem" />
  </section>
</template>
<style scoped>
.provider-recovery {
  display: grid;
  gap: 8px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  min-width: 0;
}
.provider-recovery .button {
  white-space: normal;
}
</style>
