<script setup lang="ts">
import { computed, onScopeDispose, ref, useId, watch } from "vue";
import { ChevronDown } from "@lucide/vue";

import {
  operationParameter,
  type EditablePlanOperation,
} from "@/features/assistant/model";
import {
  createIntegrationGrantReadBundle,
  type IntegrationGrantReadBundle,
} from "../integration-grant-read-bundle";
import { allowedIntegrationApprovalPolicies } from "@/features/integrations/ui/model";
import {
  approvalScopeOptions,
  validApprovalScopeSelection,
} from "@/features/integrations/approval-scope-options";
import type {
  Agent,
  IntegrationConnection,
  IntegrationGrantCapabilityCandidate,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{
  operation: EditablePlanOperation;
  projectRef?: string;
  disabled: boolean;
  readBundle?: IntegrationGrantReadBundle;
  compact?: boolean;
}>();
const fieldPrefix = `assistant-integration-grant-${useId()}`;
const emit = defineEmits<{
  valid: [value: boolean];
  dirty: [];
  expanded: [value: boolean];
  parameter: [key: string, value: string | boolean | string[]];
}>();
const connection = ref<IntegrationConnection>();
const recipient = ref<Agent | Workflow>();
const candidate = ref<IntegrationGrantCapabilityCandidate>();
const loading = ref(false);
const problem = ref(false);
const candidateProblem = ref(false);
const expanded = ref(false);
watch(expanded, (value) => emit("expanded", value));
const localReadBundle = createIntegrationGrantReadBundle();
onScopeDispose(() => localReadBundle.close());

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
const availableApprovalPolicies = computed(() =>
  allowedIntegrationApprovalPolicies(candidate.value?.capability),
);
const selectedApprovalPolicy = computed(() =>
  availableApprovalPolicies.value.find(
    (policy) => policy === stringParameter("approvalPolicy"),
  ),
);
const selectedCapability = computed(() =>
  connection.value?.capabilities.find(
    (item) => item.key === capabilityKey.value,
  ),
);
const approvalScopePaths = computed(() => {
  const value = parameter("approvalScopePaths");
  return Array.isArray(value) && value.every((path) => typeof path === "string")
    ? value
    : [];
});
const approvalScopeParameterValid = computed(() => {
  const value = parameter("approvalScopePaths");
  return (
    value === undefined ||
    (Array.isArray(value) && value.every((path) => typeof path === "string"))
  );
});
const availableApprovalScopePaths = computed(() =>
  approvalScopeOptions(candidate.value?.capability.inputSchema).filter(
    (path) => path.length <= 160,
  ),
);
const approvalScopeValid = computed(() =>
  selectedApprovalPolicy.value === "HUMAN_SCOPED"
    ? validApprovalScopeSelection(
        approvalScopePaths.value,
        availableApprovalScopePaths.value,
      )
    : approvalScopePaths.value.length === 0,
);
const versionMatches = computed(
  () =>
    connection.value?.version === props.operation.value.expectedVersion &&
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
    selectedApprovalPolicy.value &&
    approvalScopeParameterValid.value &&
    approvalScopeValid.value &&
    (enabled.value === true
      ? candidate.value?.capability.key === capabilityKey.value &&
        candidate.value.grantable &&
        candidate.value.pins.connectionVersion === connection.value?.version
      : enabled.value === false && existingGrant.value),
  ),
);
watch(valid, (value) => emit("valid", value), { immediate: true });

watch(
  [
    () => props.projectRef,
    connectionRef,
    recipientKind,
    recipientRef,
    capabilityKey,
    enabled,
    () => props.operation.value.expectedVersion,
    () => props.readBundle,
  ] as const,
  (
    [projectRef, connRef, kind, targetRef, key, , version],
    _previous,
    onCleanup,
  ) => {
    connection.value = undefined;
    recipient.value = undefined;
    candidate.value = undefined;
    loading.value = false;
    problem.value = false;
    candidateProblem.value = false;
    if (!projectRef || !connRef || !kind || !targetRef || !key || !version)
      return;
    const controller = new AbortController();
    onCleanup(() => controller.abort());
    loading.value = true;
    void (props.readBundle ?? localReadBundle)
      .read(
        {
          projectRef,
          connectionRef: connRef,
          recipientKind: kind,
          recipientRef: targetRef,
          connectionVersion: version,
        },
        controller.signal,
      )
      .then((snapshot) => {
        if (controller.signal.aborted) return;
        connection.value = snapshot.connection;
        recipient.value = snapshot.recipient;
        candidate.value = snapshot.candidates.find(
          (item) => item.capability.key === key,
        );
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

function changed(key: string, value: string | boolean | string[]): void {
  emit("parameter", key, value);
  emit("dirty");
}
function chooseCapability(key: string): void {
  const selected = connection.value?.capabilities.find(
    (item) => item.key === key,
  );
  const allowed = allowedIntegrationApprovalPolicies(selected);
  changed(
    "approvalPolicy",
    selected && allowed.includes(selected.approvalPolicy)
      ? selected.approvalPolicy
      : "",
  );
  changed("approvalScopePaths", []);
  changed("capabilityKey", key);
}
function setEnabled(value: boolean): void {
  changed("enabled", value);
}
function chooseApprovalPolicy(policy: string): void {
  if (!availableApprovalPolicies.value.some((item) => item === policy)) return;
  changed("approvalScopePaths", []);
  changed("approvalPolicy", policy);
}
function toggleApprovalScopePath(path: string, checked: boolean): void {
  if (!availableApprovalScopePaths.value.includes(path)) return;
  changed(
    "approvalScopePaths",
    checked
      ? [...new Set([...approvalScopePaths.value, path])].sort()
      : approvalScopePaths.value.filter((item) => item !== path),
  );
}
</script>

<template>
  <div class="assistant-grant-form">
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="field-error" role="alert">
      {{ $t("assistant.planEditor.grantLoadFailed") }}
    </p>
    <button
      v-if="compact && connection && recipient"
      class="button button--ghost assistant-grant-form__summary"
      type="button"
      :aria-expanded="expanded"
      :aria-controls="`${fieldPrefix}-fields`"
      @click="expanded = !expanded"
    >
      <ChevronDown :size="16" aria-hidden="true" />
      <strong>{{ recipient.name }}</strong>
      <span
        >{{ connection.name }} ·
        {{
          selectedCapability?.name ||
          $t("assistant.planEditor.capabilityUnknown")
        }}</span
      >
      <span
        >{{
          $t(
            enabled === true
              ? "assistant.planEditor.grantEnableShort"
              : "assistant.planEditor.grantDisableShort",
          )
        }}
        ·
        {{
          ["NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"].includes(
            stringParameter("approvalPolicy"),
          )
            ? $t(
                `integrations.approvalPolicies.${stringParameter("approvalPolicy")}`,
              )
            : $t("integrations.chooseApprovalPolicy")
        }}</span
      >
    </button>
    <p
      v-if="compact && !disabled && connection && recipient && !valid"
      class="field-error"
      role="alert"
    >
      {{
        $t(
          versionMatches
            ? "assistant.planEditor.grantUnavailable"
            : "assistant.planEditor.grantStale",
        )
      }}
    </p>
    <div
      v-if="connection && recipient"
      v-show="!compact || expanded"
      :id="`${fieldPrefix}-fields`"
      class="assistant-grant-form__fields"
    >
      <p>
        {{ $t("assistant.planEditor.grantConnection") }}:
        <strong>{{ connection.name }}</strong>
      </p>
      <p>
        {{ $t("assistant.planEditor.grantRecipient") }}:
        <strong>{{ recipient.name }}</strong>
      </p>
      <p v-if="!disabled && !versionMatches" class="field-error" role="alert">
        {{ $t("assistant.planEditor.grantStale") }}
      </p>
      <label class="field">
        <span>{{ $t("assistant.planEditor.grantCapability") }}</span>
        <select
          :value="capabilityKey"
          :id="`${fieldPrefix}-capability`"
          :name="`${fieldPrefix}-capability`"
          :disabled="disabled || !versionMatches"
          @change="chooseCapability(($event.target as HTMLSelectElement).value)"
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
          :id="`${fieldPrefix}-enabled`"
          :name="`${fieldPrefix}-enabled`"
          :checked="enabled === true"
          :disabled="disabled || !versionMatches || !selectedCapability"
          @change="setEnabled(($event.target as HTMLInputElement).checked)"
        />
        {{ $t("assistant.planEditor.grantEnable") }}
      </label>
      <label class="field">
        <span>{{ $t("integrations.approvalPolicy") }}</span>
        <select
          :id="`${fieldPrefix}-policy`"
          :value="selectedApprovalPolicy ?? ''"
          :disabled="
            disabled || !versionMatches || !availableApprovalPolicies.length
          "
          @change="
            chooseApprovalPolicy(($event.target as HTMLSelectElement).value)
          "
        >
          <option value="" disabled>
            {{ $t("integrations.chooseApprovalPolicy") }}
          </option>
          <option
            v-for="policy in availableApprovalPolicies"
            :key="policy"
            :value="policy"
          >
            {{ $t(`integrations.approvalPolicies.${policy}`) }}
          </option>
        </select>
        <small>{{ $t("integrations.approvalPolicySelectionHelp") }}</small>
      </label>
      <fieldset
        v-if="selectedApprovalPolicy === 'HUMAN_SCOPED'"
        class="assistant-grant-form__approval-scope"
      >
        <legend>{{ $t("integrations.approvalScopeTitle") }}</legend>
        <p>{{ $t("integrations.approvalScopeHelp") }}</p>
        <p
          v-if="!availableApprovalScopePaths.length"
          class="field-error"
          role="alert"
        >
          {{ $t("integrations.approvalScopeUnavailable") }}
        </p>
        <label
          v-for="(path, index) in availableApprovalScopePaths"
          :key="path"
          class="assistant-grant-form__scope-option"
        >
          <input
            type="checkbox"
            :id="`${fieldPrefix}-scope-${index}`"
            :name="`${fieldPrefix}-scope-${index}`"
            :checked="approvalScopePaths.includes(path)"
            :disabled="
              disabled ||
              !versionMatches ||
              (!approvalScopePaths.includes(path) &&
                approvalScopePaths.length >= 16)
            "
            @change="
              toggleApprovalScopePath(
                path,
                ($event.target as HTMLInputElement).checked,
              )
            "
          />
          <code>{{ path }}</code>
        </label>
      </fieldset>
      <p v-if="!disabled && candidateProblem" class="field-error" role="alert">
        {{ $t("assistant.planEditor.grantCandidateFailed") }}
      </p>
      <p
        v-else-if="
          !disabled && enabled === true && candidate && !candidate.grantable
        "
        class="field-error"
        role="alert"
      >
        {{ $t("assistant.planEditor.grantUnavailable") }}
      </p>
      <p
        v-else-if="!disabled && enabled === false && !existingGrant"
        class="field-error"
        role="alert"
      >
        {{ $t("assistant.planEditor.grantNothingToRevoke") }}
      </p>
      <p>{{ $t("assistant.planEditor.grantFixedTarget") }}</p>
    </div>
  </div>
</template>

<style scoped>
.assistant-grant-form {
  display: grid;
  gap: 10px;
  min-width: 0;
}
.assistant-grant-form__fields {
  display: grid;
  gap: 10px;
  min-width: 0;
}
.assistant-grant-form__summary {
  height: auto;
  min-height: 32px;
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-start;
  text-align: left;
  gap: 4px 12px;
  overflow-wrap: anywhere;
  white-space: normal;
  max-width: 100%;
}
.assistant-grant-form__summary span,
.assistant-grant-form__summary strong {
  min-width: 0;
}
.assistant-grant-form__summary svg {
  flex-shrink: 0;
}
.assistant-grant-form__summary[aria-expanded="true"] svg {
  transform: rotate(180deg);
}
.assistant-grant-form p {
  margin: 0;
}
.assistant-grant-form__enabled {
  display: flex;
  gap: 8px;
  align-items: center;
}
.assistant-grant-form__approval-scope {
  display: grid;
  gap: 8px;
  min-width: 0;
}
.assistant-grant-form__scope-option {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  min-width: 0;
}
.assistant-grant-form__scope-option code {
  overflow-wrap: anywhere;
}
</style>
