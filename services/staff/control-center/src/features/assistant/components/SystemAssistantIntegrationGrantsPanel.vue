<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";
import { usePlatformStore } from "@/features/platform/store";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import type {
  IntegrationConnection,
  SystemAssistant,
  SystemAssistantIntegrationGrantCandidate,
  SystemAssistantIntegrationGrantInput,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import { allowedIntegrationApprovalPolicies } from "@/features/integrations/ui/model";
import { approvalScopeOptions } from "@/features/integrations/approval-scope-options";
import {
  readSystemGrantConnections,
  readSystemGrantCandidates,
  saveSystemGrant,
  selectedSystemGrantPolicy,
  validSystemGrantSelection,
} from "../system-integration-grants";

const props = defineProps<{ assistant: SystemAssistant; canEdit: boolean }>();
const platform = usePlatformStore();
const { t } = useI18n();
const fieldPrefix = useId();
const ownerLifetime = ownerRequestSignal();
const owner = computed(() => ({
  organizationRef: platform.bootstrap?.organizationRef ?? "",
  assistantRef: props.assistant.ref,
  assistantVersion: props.assistant.version,
}));
const ownerKey = computed(() => JSON.stringify(owner.value));
const connection = ref<IntegrationConnection>();
const candidate = ref<SystemAssistantIntegrationGrantCandidate>();
const policy = ref<SystemAssistantIntegrationGrantInput["approvalPolicy"]>();
const paths = ref<string[]>([]);
const enabled = ref(true);
const busy = ref(false);
const problem = ref<AppProblem>();
const success = ref(false);
const connectionRows = new Map<string, IntegrationConnection>();
const candidateRows = new Map<
  string,
  SystemAssistantIntegrationGrantCandidate
>();
let controller = new AbortController();
let generation = 0;
const connectionOption = computed<AsyncEntityOption | undefined>(
  () =>
    connection.value && {
      ref: connection.value.ref,
      title: connection.value.name,
    },
);
const capabilityOption = computed<AsyncEntityOption | undefined>(
  () =>
    candidate.value && {
      ref: candidate.value.capability.key,
      title: candidate.value.capability.name,
    },
);
const capabilityKey = computed(() =>
  JSON.stringify([
    ownerKey.value,
    connection.value?.ref,
    connection.value?.version,
  ]),
);
const allowedPolicies = computed(() =>
  allowedIntegrationApprovalPolicies(candidate.value?.capability),
);
const allowedPaths = computed(() =>
  approvalScopeOptions(candidate.value?.capability.inputSchema).filter(
    (path) => path.length <= 160,
  ),
);
const canManage = computed(
  () =>
    !ownerLifetime.aborted &&
    props.canEdit &&
    candidate.value?.grantable === true,
);
const canSave = computed(
  () =>
    !busy.value &&
    canManage.value &&
    validSystemGrantSelection(candidate.value, policy.value, paths.value),
);

function clearCapability(): void {
  candidate.value = undefined;
  policy.value = undefined;
  paths.value = [];
  enabled.value = true;
  success.value = false;
  problem.value = undefined;
}
function chooseConnection(option: AsyncEntityOption): void {
  const selected = connectionRows.get(option.ref);
  controller.abort();
  controller = new AbortController();
  generation++;
  connection.value = selected;
  candidateRows.clear();
  clearCapability();
  busy.value = false;
}
function clearConnection(): void {
  controller.abort();
  controller = new AbortController();
  generation++;
  connection.value = undefined;
  candidateRows.clear();
  clearCapability();
  busy.value = false;
}
function chooseCapability(option: AsyncEntityOption): void {
  clearCapability();
  const selected = candidateRows.get(option.ref);
  if (!selected) return;
  candidate.value = selected;
  policy.value = selectedSystemGrantPolicy(selected);
  paths.value =
    policy.value === "HUMAN_SCOPED"
      ? [...selected.currentApprovalScopePaths]
      : [];
  enabled.value = selected.currentGrantRef
    ? selected.currentGrantEnabled
    : true;
}
function changePolicy(value: string): void {
  policy.value = allowedPolicies.value.find((item) => item === value);
  paths.value = [];
}
function togglePath(path: string, checked: boolean): void {
  if (!allowedPaths.value.includes(path)) return;
  paths.value = checked
    ? [...new Set([...paths.value, path])].sort()
    : paths.value.filter((item) => item !== path);
}
async function loadConnections(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize?: number,
): Promise<AsyncEntityOptionPage> {
  const current = generation;
  const combined = AbortSignal.any([signal, controller.signal, ownerLifetime]);
  const page = await readSystemGrantConnections(
    query,
    cursor,
    combined,
    pageSize,
  );
  combined.throwIfAborted();
  if (current !== generation)
    throw new Error("System assistant grant selection changed");
  const refs = new Set<string>();
  for (const item of page.items) {
    if (
      typeof item.ref !== "string" ||
      !item.ref ||
      typeof item.name !== "string" ||
      refs.has(item.ref) ||
      !Number.isSafeInteger(item.version) ||
      item.version < 1
    )
      throw new Error("Invalid system assistant connection catalog");
    refs.add(item.ref);
    connectionRows.set(item.ref, item);
  }
  return {
    items: page.items.map((item) => ({ ref: item.ref, title: item.name })),
    nextPageToken: page.nextPageToken,
  };
}
async function loadCapabilities(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize?: number,
): Promise<AsyncEntityOptionPage> {
  const selected = connection.value;
  if (!selected) return { items: [], nextPageToken: "", total: 0 };
  const current = generation;
  const combined = AbortSignal.any([signal, controller.signal, ownerLifetime]);
  const page = await readSystemGrantCandidates(
    owner.value,
    selected,
    query,
    cursor,
    combined,
    pageSize,
  );
  combined.throwIfAborted();
  if (current !== generation || selected !== connection.value)
    throw new Error("System assistant grant selection changed");
  for (const item of page.items) candidateRows.set(item.capability.key, item);
  return {
    items: page.items.map((item) => ({
      ref: item.capability.key,
      title: item.capability.name,
      description: item.capability.description,
      meta: item.currentGrantEnabled
        ? t("assistant.settings.integrationGranted")
        : t("assistant.settings.integrationNotGranted"),
      disabled: !item.grantable,
      disabledReason: item.grantable
        ? undefined
        : t(`integrationCandidates.${item.reason}`),
    })),
    nextPageToken: page.nextPageToken,
    total: page.total,
  };
}
async function save(): Promise<void> {
  const selected = connection.value;
  const item = candidate.value;
  const selectedPolicy = policy.value;
  if (!canSave.value || !selected || !item || !selectedPolicy) return;
  const current = generation;
  const signal = AbortSignal.any([controller.signal, ownerLifetime]);
  busy.value = true;
  problem.value = undefined;
  success.value = false;
  try {
    const result = await saveSystemGrant(
      selected,
      {
        connectionRef: selected.ref,
        capabilityKey: item.capability.key,
        enabled: enabled.value,
        approvalPolicy: selectedPolicy,
        ...(selectedPolicy === "HUMAN_SCOPED"
          ? { approvalScopePaths: [...paths.value] }
          : {}),
      },
      signal,
    );
    if (current !== generation || signal.aborted) return;
    connectionRows.set(result.ref, result);
    connection.value = result;
    candidateRows.clear();
    clearCapability();
    success.value = true;
  } catch (error) {
    if (current === generation && !signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (current === generation) busy.value = false;
  }
}
watch(ownerKey, () => {
  controller.abort();
  controller = new AbortController();
  generation++;
  connection.value = undefined;
  connectionRows.clear();
  candidateRows.clear();
  clearCapability();
  busy.value = false;
});
function resetOwnerSelection(): void {
  clearConnection();
  connectionRows.clear();
}
ownerLifetime.addEventListener("abort", resetOwnerSelection, { once: true });
onBeforeUnmount(() => {
  controller.abort();
  generation++;
  ownerLifetime.removeEventListener("abort", resetOwnerSelection);
});
</script>
<template>
  <section class="system-integration-grants">
    <p>{{ t("assistant.settings.integrationsHelp") }}</p>
    <label
      ><span>{{ t("integrations.connections") }}</span>
      <AsyncEntityPicker
        :key="ownerKey"
        :model-value="connection?.ref ?? ''"
        :selected="connectionOption"
        :load-page="loadConnections"
        :context-key="ownerKey"
        :disabled="busy || !owner.organizationRef"
        :trigger-label="t('integrations.connections')"
        :placeholder="t('integrations.connections')"
        @select="chooseConnection"
        @update:model-value="
          (value) => {
            if (!value) clearConnection();
          }
        "
      />
    </label>
    <label v-if="connection"
      ><span>{{ t("integrations.capability") }}</span>
      <AsyncEntityPicker
        :key="capabilityKey"
        :model-value="candidate?.capability.key ?? ''"
        :selected="capabilityOption"
        :load-page="loadCapabilities"
        :context-key="capabilityKey"
        :disabled="busy"
        :trigger-label="t('integrations.capability')"
        :placeholder="t('integrations.capability')"
        @select="chooseCapability"
        @update:model-value="
          (value) => {
            if (!value) clearCapability();
          }
        "
      />
    </label>
    <template v-if="candidate">
      <p>{{ candidate.capability.description }}</p>
      <template v-if="canManage">
        <label
          ><input v-model="enabled" type="checkbox" :disabled="busy" />{{
            t("assistant.settings.integrationEnabled")
          }}</label
        >
        <label
          ><span>{{ t("integrations.approvalPolicy") }}</span>
          <select
            :id="`${fieldPrefix}-policy`"
            :value="policy ?? ''"
            :disabled="busy || !allowedPolicies.length"
            @change="changePolicy(($event.target as HTMLSelectElement).value)"
          >
            <option v-if="!policy" disabled value="">
              {{ t("integrations.chooseApprovalPolicy") }}
            </option>
            <option
              v-for="option in allowedPolicies"
              :key="option"
              :value="option"
            >
              {{ t(`integrations.approvalPolicies.${option}`) }}
            </option>
          </select>
          <small>{{ t("integrations.approvalPolicySelectionHelp") }}</small>
        </label>
        <fieldset v-if="policy === 'HUMAN_SCOPED'">
          <legend>{{ t("integrations.approvalScopeTitle") }}</legend>
          <p>{{ t("integrations.approvalScopeHelp") }}</p>
          <p v-if="!allowedPaths.length">
            {{ t("integrations.approvalScopeUnavailable") }}
          </p>
          <label v-for="path in allowedPaths" :key="path"
            ><input
              type="checkbox"
              :value="path"
              :checked="paths.includes(path)"
              :disabled="busy || (!paths.includes(path) && paths.length >= 16)"
              @change="
                togglePath(path, ($event.target as HTMLInputElement).checked)
              "
            /><code>{{ path }}</code></label
          >
        </fieldset>
        <button
          class="button button--primary"
          type="button"
          :disabled="!canSave"
          @click="save"
        >
          {{ t("common.save") }}
        </button>
      </template>
      <p v-else>{{ t("assistant.settings.integrationsReadOnly") }}</p>
    </template>
    <ProblemNotice v-if="problem" :problem="problem" compact />
    <p v-if="success" role="status">
      {{ t("assistant.settings.integrationSaved") }}
    </p>
  </section>
</template>
<style scoped>
.system-integration-grants {
  display: grid;
  gap: 16px;
}
.system-integration-grants label {
  display: grid;
  gap: 8px;
}
.system-integration-grants fieldset {
  display: grid;
  gap: 8px;
  min-width: 0;
}
.system-integration-grants fieldset label {
  display: flex;
  align-items: center;
  gap: 8px;
}
.system-integration-grants code {
  overflow-wrap: anywhere;
}
</style>
