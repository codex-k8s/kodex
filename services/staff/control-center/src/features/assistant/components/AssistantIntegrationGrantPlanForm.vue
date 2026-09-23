<script setup lang="ts">
import { computed, ref, watch } from "vue";

import {
  operationParameter,
  type EditablePlanOperation,
} from "@/features/assistant/model";
import { capabilityCandidates } from "@/features/integrations/grant-candidates";
import { requestSignal } from "@/shared/api/client";
import {
  getAgent,
  getIntegrationConnection,
  getWorkflow,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Agent,
  IntegrationConnection,
  IntegrationGrantCapabilityCandidate,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";

const props = defineProps<{
  operation: EditablePlanOperation;
  projectRef?: string;
  disabled: boolean;
}>();
const emit = defineEmits<{
  valid: [value: boolean];
  dirty: [];
  parameter: [key: string, value: string | boolean];
}>();
const connection = ref<IntegrationConnection>();
const recipient = ref<Agent | Workflow>();
const candidate = ref<IntegrationGrantCapabilityCandidate>();
const loading = ref(false);
const problem = ref(false);
const candidateProblem = ref(false);

function parameter(key: string): unknown {
  try {
    return operationParameter(props.operation, key);
  } catch {
    return undefined;
  }
}
function stringParameter(key: string): string {
  const value = parameter(key);
  return typeof value === "string" ? value : "";
}
const connectionRef = computed(() => stringParameter("connectionRef"));
const agentRef = computed(() => stringParameter("agentRef"));
const workflowRef = computed(() => stringParameter("workflowRef"));
const recipientKind = computed<"AGENT" | "WORKFLOW" | undefined>(() =>
  Boolean(agentRef.value) === Boolean(workflowRef.value)
    ? undefined
    : agentRef.value
      ? "AGENT"
      : "WORKFLOW",
);
const recipientRef = computed(() => agentRef.value || workflowRef.value);
const capabilityKey = computed(() => stringParameter("capabilityKey"));
const enabled = computed(() => parameter("enabled"));
const selectedCapability = computed(() =>
  connection.value?.capabilities.find(
    (item) => item.key === capabilityKey.value,
  ),
);
const versionMatches = computed(
  () =>
    connection.value?.version === props.operation.value.expectedVersion &&
    connection.value?.version === parameter("expectedVersion") &&
    (props.operation.value.target.version === undefined ||
      connection.value?.version === props.operation.value.target.version),
);
const existingGrant = computed(() =>
  connection.value?.grants.find(
    (item) =>
      item.capabilityKey === capabilityKey.value &&
      item.agentRef === (agentRef.value || undefined) &&
      item.workflowRef === (workflowRef.value || undefined) &&
      item.enabled,
  ),
);
const targetMatches = computed(() =>
  Boolean(
    props.projectRef &&
    recipientKind.value &&
    recipient.value?.ref === recipientRef.value &&
    recipient.value.projectRef === props.projectRef &&
    connection.value?.ref === connectionRef.value &&
    props.operation.value.target.ref === connectionRef.value &&
    props.operation.value.target.kind === "INTEGRATION_CONNECTION" &&
    versionMatches.value,
  ),
);
const valid = computed(() =>
  Boolean(
    targetMatches.value &&
    selectedCapability.value &&
    (enabled.value === true
      ? candidate.value?.capability.key === capabilityKey.value &&
        candidate.value.grantable &&
        candidate.value.pins.connectionVersion === connection.value?.version
      : enabled.value === false && existingGrant.value),
  ),
);
watch(valid, (value) => emit("valid", value), { immediate: true });

watch(
  [() => props.projectRef, connectionRef, recipientKind, recipientRef] as const,
  ([projectRef, connRef, kind, targetRef], _previous, onCleanup) => {
    connection.value = undefined;
    recipient.value = undefined;
    loading.value = false;
    problem.value = false;
    if (!projectRef || !connRef || !kind || !targetRef) return;
    const controller = new AbortController();
    onCleanup(() => controller.abort());
    loading.value = true;
    const targetRequest =
      kind === "AGENT"
        ? unwrap(
            getAgent({
              path: { agentRef: targetRef },
              signal: requestSignal(controller.signal),
            }),
          )
        : unwrap(
            getWorkflow({
              path: { workflowRef: targetRef },
              signal: requestSignal(controller.signal),
            }),
          );
    void Promise.all([
      unwrap(
        getIntegrationConnection({
          path: { connectionRef: connRef },
          signal: requestSignal(controller.signal),
        }),
      ),
      targetRequest,
    ])
      .then(([connectionResponse, targetResponse]) => {
        if (controller.signal.aborted) return;
        const nextConnection = connectionResponse.data;
        const nextRecipient = targetResponse.data;
        if (
          nextConnection.ref !== connRef ||
          nextRecipient.ref !== targetRef ||
          nextRecipient.projectRef !== projectRef
        )
          throw new Error("Assistant integration grant readback mismatch");
        connection.value = nextConnection;
        recipient.value = nextRecipient;
      })
      .catch(() => {
        if (!controller.signal.aborted) problem.value = true;
      })
      .finally(() => {
        if (!controller.signal.aborted) loading.value = false;
      });
  },
  { immediate: true },
);

watch(
  [
    () => props.projectRef,
    connection,
    recipientKind,
    recipientRef,
    capabilityKey,
    enabled,
  ] as const,
  (
    [projectRef, currentConnection, kind, targetRef, key, isEnabled],
    _previous,
    onCleanup,
  ) => {
    candidate.value = undefined;
    candidateProblem.value = false;
    if (
      !projectRef ||
      !currentConnection ||
      !kind ||
      !targetRef ||
      !key ||
      isEnabled !== true
    )
      return;
    const controller = new AbortController();
    onCleanup(() => controller.abort());
    void (async () => {
      try {
        const load = capabilityCandidates({
          connectionRef: currentConnection.ref,
          projectRef,
          recipientKind: kind,
          recipientRef: targetRef,
        });
        const seen = new Set<string>();
        let cursor: string | undefined;
        for (let pageCount = 0; pageCount < 10; pageCount++) {
          const page = await load(key, cursor, controller.signal);
          if (controller.signal.aborted) return;
          if (page.pins.connectionVersion !== currentConnection.version)
            throw new Error("Integration grant candidate version changed");
          const found = page.items.find((item) => item.capability.key === key);
          if (found) {
            candidate.value = found;
            return;
          }
          if (!page.nextPageToken) return;
          if (seen.has(page.nextPageToken))
            throw new Error("Repeated integration grant candidate cursor");
          seen.add(page.nextPageToken);
          cursor = page.nextPageToken;
        }
      } catch {
        if (!controller.signal.aborted) candidateProblem.value = true;
      }
    })();
  },
  { immediate: true },
);

function changed(key: string, value: string | boolean): void {
  emit("parameter", key, value);
  emit("dirty");
}
</script>

<template>
  <div class="assistant-grant-form">
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="field-error" role="alert">
      {{ $t("assistant.planEditor.grantLoadFailed") }}
    </p>
    <template v-if="connection && recipient">
      <p>
        {{ $t("assistant.planEditor.grantConnection") }}:
        <strong>{{ connection.name }}</strong>
      </p>
      <p>
        {{ $t("assistant.planEditor.grantRecipient") }}:
        <strong>{{ recipient.name }}</strong>
      </p>
      <p v-if="!versionMatches" class="field-error" role="alert">
        {{ $t("assistant.planEditor.grantStale") }}
      </p>
      <label class="field">
        <span>{{ $t("assistant.planEditor.grantCapability") }}</span>
        <select
          :value="capabilityKey"
          :disabled="disabled || !versionMatches"
          @change="
            changed('capabilityKey', ($event.target as HTMLSelectElement).value)
          "
        >
          <option value="">
            {{ $t("assistant.planEditor.capabilityChoose") }}
          </option>
          <option
            v-for="item in connection.capabilities"
            :key="item.key"
            :value="item.key"
          >
            {{ item.name }}
          </option>
        </select>
      </label>
      <p v-if="selectedCapability">
        {{ selectedCapability.description }} ·
        {{ $t(`integrations.risk.${selectedCapability.risk}`) }}
      </p>
      <p v-else-if="capabilityKey" class="field-error" role="alert">
        {{ $t("assistant.planEditor.capabilityUnknown") }}
      </p>
      <label class="assistant-grant-form__enabled">
        <input
          type="checkbox"
          :checked="enabled === true"
          :disabled="disabled || !versionMatches || !selectedCapability"
          @change="
            changed('enabled', ($event.target as HTMLInputElement).checked)
          "
        />
        {{ $t("assistant.planEditor.grantEnable") }}
      </label>
      <p v-if="candidateProblem" class="field-error" role="alert">
        {{ $t("assistant.planEditor.grantCandidateFailed") }}
      </p>
      <p
        v-else-if="enabled === true && candidate && !candidate.grantable"
        class="field-error"
        role="alert"
      >
        {{ $t("assistant.planEditor.grantUnavailable") }}
      </p>
      <p
        v-else-if="enabled === false && !existingGrant"
        class="field-error"
        role="alert"
      >
        {{ $t("assistant.planEditor.grantNothingToRevoke") }}
      </p>
      <p>{{ $t("assistant.planEditor.grantFixedTarget") }}</p>
    </template>
  </div>
</template>

<style scoped>
.assistant-grant-form {
  display: grid;
  gap: 10px;
  min-width: 0;
}
.assistant-grant-form p {
  margin: 0;
}
.assistant-grant-form__enabled {
  display: flex;
  gap: 8px;
  align-items: center;
}
</style>
