<script setup lang="ts">
import { onBeforeUnmount, ref } from "vue";

import { canConfigureCredential } from "@/features/integrations/connection-setup";
import { loadExactIntegrationDefinition } from "@/features/integrations/definition-lookup";
import IntegrationCredentialField from "@/features/integrations/ui/IntegrationCredentialField.vue";
import { usePlatformStore } from "@/features/platform/store";
import { requestSignal } from "@/shared/api/client";
import { getIntegrationConnection } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  IntegrationConnection,
  IntegrationDefinition,
} from "@/shared/api/generated/openapi/types.gen";
import { idempotencyKey } from "@/shared/api/mutation";
import { asProblem, unwrap, type AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const props = defineProps<{ connectionRef: string }>();
const emit = defineEmits<{ close: []; configured: [] }>();
const platform = usePlatformStore();
const controller = new AbortController();
const connection = ref<IntegrationConnection>();
const definition = ref<IntegrationDefinition>();
const credentialValue = ref("");
const invalid = ref(false);
const loading = ref(true);
const busy = ref(false);
const attempted = ref(false);
const problem = ref<AppProblem>();

async function readConnection(): Promise<IntegrationConnection> {
  const result = await unwrap(
    getIntegrationConnection({
      path: { connectionRef: props.connectionRef },
      signal: requestSignal(controller.signal),
      cache: "no-store",
    }),
  );
  if (result.data.ref !== props.connectionRef)
    throw new Error("Integration connection readback mismatch");
  return result.data;
}

void (async () => {
  try {
    const current = await readConnection();
    const exactDefinition = await loadExactIntegrationDefinition(
      current.definitionKey,
      controller.signal,
    );
    if (controller.signal.aborted) return;
    if (!canConfigureCredential(exactDefinition, current))
      throw new Error("Integration credential configuration unavailable");
    connection.value = current;
    definition.value = exactDefinition;
  } catch (error) {
    if (!controller.signal.aborted) problem.value = asProblem(error);
  } finally {
    loading.value = false;
  }
})();

async function submit(): Promise<void> {
  if (
    busy.value ||
    loading.value ||
    attempted.value ||
    !connection.value ||
    !definition.value
  )
    return;
  if (!credentialValue.value.trim()) {
    invalid.value = true;
    return;
  }
  busy.value = true;
  attempted.value = true;
  problem.value = undefined;
  const oneTimeValue = credentialValue.value;
  credentialValue.value = "";
  try {
    const fresh = await readConnection();
    if (
      fresh.version !== connection.value.version ||
      fresh.definitionKey !== definition.value.key ||
      !canConfigureCredential(definition.value, fresh)
    )
      throw new Error("Integration credential target changed");
    const updated = await platform.configureConnectionCredential(
      { ref: fresh.ref, version: fresh.version },
      oneTimeValue,
      idempotencyKey(),
    );
    if (updated.ref !== fresh.ref || !updated.credentialsConfigured)
      throw new Error("Integration credential readback mismatch");
    emit("configured");
    emit("close");
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

onBeforeUnmount(() => {
  controller.abort();
  credentialValue.value = "";
});
</script>

<template>
  <ModalDialog
    :title="
      $t('integrations.configureCredentialNamed', {
        name: connection?.name ?? '',
      })
    "
    :busy="busy"
    size="lg"
    @close="emit('close')"
  >
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <form
      v-else-if="connection && definition"
      id="assistant-integration-credential-form"
      class="assistant-integration-credential-form"
      @submit.prevent="submit"
    >
      <p>{{ connection.name }}</p>
      <p>{{ $t("integrations.metadataAlreadyCreated") }}</p>
      <IntegrationCredentialField
        :model-value="credentialValue"
        :credential-secret-key="definition.credentialSecretKey"
        :invalid="invalid"
        :disabled="busy || attempted"
        @update:model-value="
          credentialValue = $event;
          invalid = false;
        "
      />
    </form>
    <ProblemNotice v-if="problem" :problem="problem" compact />
    <p v-if="attempted && problem">
      {{ $t("integrations.metadataPreserved") }}
    </p>
    <template #actions>
      <button
        class="button"
        type="button"
        :disabled="busy"
        @click="emit('close')"
      >
        {{ $t("common.cancel") }}
      </button>
      <button
        v-if="connection && definition"
        class="button button--primary"
        type="submit"
        form="assistant-integration-credential-form"
        :disabled="busy || attempted"
      >
        {{ $t("integrations.connect") }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.assistant-integration-credential-form {
  display: grid;
  gap: 12px;
}
.assistant-integration-credential-form p {
  margin: 0;
}
</style>
