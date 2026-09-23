<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { assistantIntegrationConnectionTarget } from "@/features/assistant/model";
import { requestSignal } from "@/shared/api/client";
import { getIntegrationConnection } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AssistantPlan,
  IntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const emit = defineEmits<{ navigate: [] }>();
const target = computed(() =>
  assistantIntegrationConnectionTarget(props.plan, props.operationRef),
);
const connection = ref<IntegrationConnection>();
const loading = ref(false);
const problem = ref(false);
const needsCredential = computed(() =>
  connection.value?.nextActions.includes("CONFIGURE_CREDENTIAL"),
);
const destination = computed(() => ({
  name: "integrations",
  ...(needsCredential.value && target.value
    ? { query: { assistantCredentialRef: target.value.connectionRef } }
    : {}),
}));
let refresh: (() => Promise<void>) | undefined;

watch(
  target,
  (value, _previous, onCleanup) => {
    connection.value = undefined;
    loading.value = false;
    problem.value = false;
    if (!value) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    let attempts = 0;
    onCleanup(() => {
      controller.abort();
      if (timer) clearTimeout(timer);
      refresh = undefined;
    });
    refresh = async () => {
      if (controller.signal.aborted) return;
      loading.value = true;
      try {
        const next = (
          await unwrap(
            getIntegrationConnection({
              path: { connectionRef: value.connectionRef },
              signal: requestSignal(controller.signal),
            }),
          )
        ).data;
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (controller.signal.aborted) return;
        if (next.ref !== value.connectionRef)
          throw new Error("Integration connection readback mismatch");
        connection.value = next;
        problem.value = false;
      } catch {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) problem.value = true;
      } finally {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) {
          loading.value = false;
          attempts += 1;
          if (
            attempts < 120 &&
            (problem.value || connection.value?.state === "TESTING")
          )
            timer = setTimeout(() => void refresh?.(), 5000);
        }
      }
    };
    void refresh();
  },
  { immediate: true },
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
        @click="refresh?.()"
      >
        {{ $t("common.refresh") }}
      </button>
      <RouterLink
        v-if="connection"
        class="button button--primary"
        :to="destination"
        @click="emit('navigate')"
      >
        {{
          $t(
            needsCredential
              ? "assistant.connection.openCredential"
              : "assistant.connection.open",
          )
        }}
      </RouterLink>
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
