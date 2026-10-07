<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  onScopeDispose,
  ref,
  useId,
  watch,
} from "vue";
import { usePlatformStore } from "@/features/platform/store";
import { ChevronDown } from "@lucide/vue";
import {
  operationInputs,
  operationParameter,
  type EditablePlanOperation,
} from "../model";
import {
  projectIntegrationGrantPlanCandidate,
  projectIntegrationGrantAppliedCandidate,
  projectIntegrationGrantPlanOwner,
} from "../project-integration-grant-plan";
import { validSystemGrantSelection } from "../system-integration-grants";
import {
  projectGrantBatchContext,
  type ProjectGrantBatchContext,
} from "../project-grant-batch-context";
import {
  createProjectIntegrationGrantReadBundle,
  type ProjectIntegrationGrantReadBundle,
} from "../project-integration-grant-read-bundle";
import type {
  IntegrationConnection,
  SystemAssistantIntegrationGrantCandidate,
  SystemAssistantIntegrationGrantInput,
} from "@/shared/api/generated/openapi/types.gen";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import { approvalScopeOptions } from "@/features/integrations/approval-scope-options";
import { allowedIntegrationApprovalPolicies } from "@/features/integrations/ui/model";

const props = defineProps<{
  operation: EditablePlanOperation;
  disabled: boolean;
  appliedGrantRef?: string;
  readBundle?: ProjectIntegrationGrantReadBundle;
  compact?: boolean;
  sharedContext?: boolean;
}>();
const emit = defineEmits<{
  valid: [value: boolean];
  dirty: [];
  expanded: [value: boolean];
  context: [value: ProjectGrantBatchContext | undefined];
  parameter: [key: string, value: string | boolean | string[]];
}>();
const platform = usePlatformStore();
const id = `project-grant-plan-${useId()}`;
const ownerLifetime = ownerRequestSignal();
const connection = ref<IntegrationConnection>();
const candidate = ref<SystemAssistantIntegrationGrantCandidate>();
const loading = ref(false);
const failed = ref(false);
const expanded = ref(false);
watch(expanded, (value) => emit("expanded", value));
const localReadBundle = createProjectIntegrationGrantReadBundle();
onScopeDispose(() => localReadBundle.close());
function value(key: string): unknown {
  try {
    return operationParameter(props.operation, key);
  } catch {
    return undefined;
  }
}
const input = computed(() => {
  try {
    return operationInputs([props.operation])[0];
  } catch {
    return undefined;
  }
});
const ownerValid = computed(
  () =>
    !ownerLifetime.aborted &&
    !!input.value &&
    projectIntegrationGrantPlanOwner(
      input.value,
      platform.bootstrap?.organizationRef,
    ),
);
const policy = computed(() =>
  allowedIntegrationApprovalPolicies(candidate.value?.capability).find(
    (item) => item === value("approvalPolicy"),
  ),
);
const paths = computed<string[]>(() => {
  const selected = value("approvalScopePaths");
  return Array.isArray(selected) &&
    selected.every((path) => typeof path === "string")
    ? selected
    : [];
});
const allowedPolicies = computed(() =>
  allowedIntegrationApprovalPolicies(candidate.value?.capability),
);
const allowedPaths = computed(() =>
  approvalScopeOptions(candidate.value?.capability.inputSchema).filter(
    (path) => path.length <= 160,
  ),
);
const valid = computed(
  () =>
    ownerValid.value &&
    !loading.value &&
    !failed.value &&
    validSystemGrantSelection(candidate.value, policy.value, paths.value),
);
watch(valid, (selected) => emit("valid", selected), { immediate: true });
const batchContext = computed(() =>
  valid.value && input.value
    ? projectGrantBatchContext(
        input.value,
        platform.bootstrap?.organizationRef,
        connection.value?.name,
      )
    : undefined,
);
watch(batchContext, (context) => emit("context", context), {
  immediate: true,
  flush: "sync",
});
watch(
  [
    () =>
      JSON.stringify([
        platform.bootstrap?.organizationRef,
        input.value?.target,
        input.value?.expectedVersion,
        input.value?.before,
        props.appliedGrantRef,
      ]),
    () => props.readBundle,
  ],
  async (_key, _previous, onCleanup) => {
    connection.value = undefined;
    candidate.value = undefined;
    failed.value = false;
    loading.value = false;
    const operation = input.value;
    if (
      !operation ||
      !projectIntegrationGrantPlanOwner(
        operation,
        platform.bootstrap?.organizationRef,
      )
    )
      return;
    const controller = new AbortController();
    onCleanup(() => controller.abort());
    const signal = AbortSignal.any([controller.signal, ownerLifetime]);
    loading.value = true;
    try {
      const snapshot = await (props.readBundle ?? localReadBundle).read(
        operation,
        platform.bootstrap?.organizationRef,
        signal,
        Boolean(props.appliedGrantRef),
      );
      signal.throwIfAborted();
      const found = snapshot.page.items.find(
        (item) => item.capability.key === operation.parameters.capabilityKey,
      );
      if (
        !found ||
        !(props.appliedGrantRef
          ? projectIntegrationGrantAppliedCandidate(
              operation,
              snapshot.page,
              found,
              props.appliedGrantRef,
            )
          : projectIntegrationGrantPlanCandidate(
              operation,
              snapshot.page,
              found,
            ))
      )
        throw new Error("Project assistant grant plan pins changed");
      connection.value = snapshot.connection;
      candidate.value = found;
    } catch {
      if (!signal.aborted) failed.value = true;
    } finally {
      if (!signal.aborted) loading.value = false;
    }
  },
  { immediate: true },
);
function changed(key: string, selected: string | boolean | string[]): void {
  emit("parameter", key, selected);
  emit("dirty");
}
function changePolicy(selected: string): void {
  if (
    !allowedPolicies.value.includes(
      selected as SystemAssistantIntegrationGrantInput["approvalPolicy"],
    )
  )
    return;
  changed("approvalScopePaths", []);
  changed("approvalPolicy", selected);
}
function togglePath(path: string, checked: boolean): void {
  if (!allowedPaths.value.includes(path)) return;
  changed(
    "approvalScopePaths",
    checked
      ? [...new Set([...paths.value, path])].sort()
      : paths.value.filter((item) => item !== path),
  );
}
function ownerReset(): void {
  connection.value = undefined;
  candidate.value = undefined;
  loading.value = false;
  emit("valid", false);
}
ownerLifetime.addEventListener("abort", ownerReset, { once: true });
onBeforeUnmount(() => ownerLifetime.removeEventListener("abort", ownerReset));
</script>
<template>
  <div class="project-grant-plan">
    <p v-if="!compact">
      {{ $t("assistant.planEditor.projectGrantFixedTarget") }}
    </p>
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <p v-if="!ownerValid || failed" class="field-error" role="alert">
      {{ $t("assistant.planEditor.grantStale") }}
    </p>
    <button
      v-if="compact && connection && candidate"
      class="button button--ghost project-grant-plan__summary"
      :class="{
        'project-grant-plan__summary--shared': sharedContext && batchContext,
      }"
      type="button"
      :aria-expanded="expanded"
      :aria-controls="`${id}-fields`"
      @click="expanded = !expanded"
    >
      <ChevronDown :size="16" aria-hidden="true" />
      <strong v-if="!sharedContext || !batchContext">{{
        $t("assistant.settings.projectScope")
      }}</strong>
      <span class="project-grant-plan__capability"
        ><template v-if="!sharedContext || !batchContext"
          >{{ connection.name }} · </template
        >{{ candidate.capability.name }}</span
      >
      <span
        >{{
          $t(
            value("enabled") === true
              ? "assistant.planEditor.grantEnableShort"
              : "assistant.planEditor.grantDisableShort",
          )
        }}
        ·
        {{
          policy
            ? $t(`integrations.approvalPolicies.${policy}`)
            : $t("integrations.chooseApprovalPolicy")
        }}</span
      >
    </button>
    <div
      v-if="connection && candidate"
      v-show="!compact || expanded"
      :id="`${id}-fields`"
      class="project-grant-plan__fields"
    >
      <dl>
        <dt>{{ $t("assistant.planEditor.grantConnection") }}</dt>
        <dd>{{ connection.name }}</dd>
        <dt>{{ $t("assistant.planEditor.grantRecipient") }}</dt>
        <dd>{{ $t("assistant.settings.projectScope") }}</dd>
        <dt>{{ $t("assistant.planEditor.grantCapability") }}</dt>
        <dd>{{ candidate.capability.name }}</dd>
      </dl>
      <p>
        {{ candidate.capability.description }} ·
        {{ $t(`integrations.risk.${candidate.capability.risk}`) }}
      </p>
      <p v-if="appliedGrantRef" role="status">
        {{ $t("assistant.planEditor.systemGrantApplied") }} ·
        {{
          $t(`integrations.approvalPolicies.${candidate.currentApprovalPolicy}`)
        }}
      </p>
      <template v-else>
        <label
          ><input
            type="checkbox"
            :checked="value('enabled') === true"
            :disabled="disabled || !ownerValid || !candidate.grantable"
            @change="
              changed('enabled', ($event.target as HTMLInputElement).checked)
            "
          />{{ $t("assistant.planEditor.grantEnable") }}</label
        >
        <label class="field"
          ><span>{{ $t("integrations.approvalPolicy") }}</span
          ><select
            :id="`${id}-policy`"
            :value="policy ?? ''"
            :disabled="disabled || !ownerValid"
            @change="changePolicy(($event.target as HTMLSelectElement).value)"
          >
            <option disabled value="">
              {{ $t("integrations.chooseApprovalPolicy") }}
            </option>
            <option
              v-for="option in allowedPolicies"
              :key="option"
              :value="option"
            >
              {{ $t(`integrations.approvalPolicies.${option}`) }}
            </option>
          </select></label
        >
        <fieldset v-if="policy === 'HUMAN_SCOPED'">
          <legend>{{ $t("integrations.approvalScopeTitle") }}</legend>
          <p>{{ $t("integrations.approvalScopeHelp") }}</p>
          <label v-for="path in allowedPaths" :key="path"
            ><input
              type="checkbox"
              :checked="paths.includes(path)"
              :disabled="
                disabled ||
                !ownerValid ||
                (!paths.includes(path) && paths.length >= 16)
              "
              @change="
                togglePath(path, ($event.target as HTMLInputElement).checked)
              "
            /><code>{{ path }}</code></label
          >
        </fieldset>
      </template>
    </div>
  </div>
</template>
<style scoped>
.project-grant-plan {
  display: grid;
  gap: 10px;
  min-width: 0;
}
.project-grant-plan__fields {
  display: grid;
  gap: 10px;
  min-width: 0;
}
.project-grant-plan__summary {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-start;
  gap: 4px 12px;
  height: auto;
  min-height: 32px;
  max-width: 100%;
  text-align: left;
  white-space: normal;
  overflow-wrap: anywhere;
}
.project-grant-plan__summary span,
.project-grant-plan__summary strong {
  min-width: 0;
}
.project-grant-plan__summary svg {
  flex-shrink: 0;
}
.project-grant-plan__summary--shared {
  display: grid;
  grid-template-columns: 16px minmax(0, 1fr);
  gap: 2px 8px;
  padding: 4px 6px;
  line-height: 1.3;
}
.project-grant-plan__summary--shared svg {
  grid-column: 1;
  grid-row: 1 / 3;
  align-self: center;
}
.project-grant-plan__summary--shared span {
  grid-column: 2;
}
.project-grant-plan__summary--shared .project-grant-plan__capability {
  font-weight: 600;
}
.project-grant-plan__summary[aria-expanded="true"] svg {
  transform: rotate(180deg);
}
.project-grant-plan fieldset {
  display: grid;
  gap: 8px;
  min-width: 0;
}
.project-grant-plan label {
  display: flex;
  align-items: center;
  gap: 8px;
}
.project-grant-plan .field {
  display: grid;
}
.project-grant-plan code {
  overflow-wrap: anywhere;
}
.project-grant-plan dd {
  margin: 0 0 8px;
}
</style>
