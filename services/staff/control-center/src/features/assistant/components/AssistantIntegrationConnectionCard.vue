<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { assistantIntegrationConnectionTarget } from "@/features/assistant/model";
import { watchAssistantReadback } from "@/features/assistant/readback-lifecycle";
import { usePlatformStore } from "@/features/platform/store";
import { requestSignal } from "@/shared/api/client";
import { getIntegrationConnection } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AssistantPlan,
  IntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  plan: AssistantPlan;
  operationRef: string;
  refreshToken?: number;
}>();
const emit = defineEmits<{
  prepareCredential: [connectionRef: string];
}>();
const platform = usePlatformStore();
const target = computed(() =>
  assistantIntegrationConnectionTarget(props.plan, props.operationRef),
);
const readback = ref<IntegrationConnection>();
const connection = computed(() => {
  const exact = target.value;
  return exact
    ? (platform.connections[exact.connectionRef] ?? readback.value)
    : undefined;
});
const loading = ref(false);
const problem = ref(false);
const needsCredential = computed(
  () =>
    !connection.value?.credentialsConfigured &&
    connection.value?.nextActions.includes("CONFIGURE_CREDENTIAL"),
);
const destination = computed(() => ({
  name: "integrations",
  query: { assistantForm: "1", connectionRef: connection.value?.ref },
}));
const refresh = watchAssistantReadback(target, {
  loading,
  problem,
  reset: () => {
    readback.value = undefined;
  },
  read: async (value, signal) =>
    (
      await unwrap(
        getIntegrationConnection({
          path: { connectionRef: value.connectionRef },
          signal: requestSignal(signal),
        }),
      )
    ).data,
  apply: (value, next) => {
    if (next.ref !== value.connectionRef)
      throw new Error("Integration connection readback mismatch");
    readback.value = next;
    platform.connections[next.ref] = next;
  },
  restore: (value) => {
    const cached = platform.connections[value.connectionRef];
    if (cached) readback.value = cached;
    return Boolean(cached);
  },
});
watch(
  () => props.refreshToken,
  () => void refresh(),
);
</script>

<template>
  <section v-if="target" class="assistant-connection-card" aria-live="polite">
    <header>
      <strong>{{ $t("assistant.connection.title") }}</strong>
      <StatusBadge v-if="connection" :state="connection.state" />
    </header>
    <p v-if="loading && !connection">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="assistant-connection-card__problem" role="alert">
      {{ $t("assistant.connection.loadFailed") }}
    </p>
    <template v-if="connection">
      <p>{{ connection.name }}</p>
      <p v-if="needsCredential">
        {{ $t("assistant.connection.credentialNeeded") }}
      </p>
      <p v-else-if="connection.state === 'TESTING'">
        {{ $t("assistant.connection.testing") }}
      </p>
      <p v-else-if="connection.state === 'CONNECTED'">
        {{ $t("assistant.connection.connected") }}
      </p>
      <p v-else>{{ $t("assistant.connection.nextSteps") }}</p>
    </template>
    <div class="assistant-connection-card__actions">
      <button
        class="button"
        type="button"
        :disabled="loading"
        @click="refresh()"
      >
        {{ $t("common.refresh") }}
      </button>
      <button
        v-if="connection && needsCredential"
        class="button button--primary"
        type="button"
        :disabled="loading"
        @click="emit('prepareCredential', connection.ref)"
      >
        {{ $t("assistant.connection.openCredential") }}
      </button>
      <RouterLink
        v-else-if="connection"
        class="button button--primary"
        :to="destination"
        >{{ $t("assistant.connection.open") }}</RouterLink
      >
    </div>
  </section>
</template>

<style scoped>
.assistant-connection-card {
  display: grid;
  gap: 8px;
  min-width: 0;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.assistant-connection-card header,
.assistant-connection-card__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.assistant-connection-card p {
  margin: 0;
  overflow-wrap: anywhere;
}
.assistant-connection-card__problem {
  color: var(--danger);
}
</style>
