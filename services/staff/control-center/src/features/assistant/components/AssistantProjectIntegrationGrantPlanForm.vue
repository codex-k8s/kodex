<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import { usePlatformStore } from "@/features/platform/store";
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
  readProjectGrantCandidates,
  readProjectGrantOwner,
} from "../project-integration-grants";
import { getIntegrationConnection } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  IntegrationConnection,
  SystemAssistantIntegrationGrantCandidate,
  SystemAssistantIntegrationGrantInput,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import { unwrap } from "@/shared/api/problem";
import { approvalScopeOptions } from "@/features/integrations/approval-scope-options";
import { allowedIntegrationApprovalPolicies } from "@/features/integrations/ui/model";

const props = defineProps<{
  operation: EditablePlanOperation;
  disabled: boolean;
  appliedGrantRef?: string;
}>();
const emit = defineEmits<{
  valid: [value: boolean];
  dirty: [];
  parameter: [key: string, value: string | boolean | string[]];
}>();
const platform = usePlatformStore();
const id = `project-grant-plan-${useId()}`;
const ownerLifetime = ownerRequestSignal();
const connection = ref<IntegrationConnection>();
const candidate = ref<SystemAssistantIntegrationGrantCandidate>();
const loading = ref(false);
const failed = ref(false);
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
watch(
  () =>
    JSON.stringify([
      platform.bootstrap?.organizationRef,
      input.value?.target,
      input.value?.expectedVersion,
      input.value?.before,
      props.appliedGrantRef,
    ]),
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
      const connectionResponse = await unwrap(
        getIntegrationConnection({
          path: {
            connectionRef: operation.parameters.connectionRef as string,
          },
          signal: requestSignal(signal),
          cache: "no-store",
        }),
      );
      signal.throwIfAborted();
      const selected = connectionResponse.data;
      if (
        selected.ref !== operation.parameters.connectionRef ||
        (props.appliedGrantRef
          ? selected.version <
            (operation.expectedVersion ?? Number.POSITIVE_INFINITY)
          : selected.version !== operation.expectedVersion)
      )
        throw new Error("System assistant grant plan readback mismatch");
      const owner = await readProjectGrantOwner(
        platform.bootstrap?.organizationRef ?? "",
        operation.parameters.projectRef as string,
        operation.parameters.projectAssistantRef as string,
        signal,
      );
      const seen = new Set<string>();
      let cursor: string | undefined;
      for (let pageCount = 0; pageCount < 10; pageCount++) {
        const page = await readProjectGrantCandidates(
          owner,
          selected,
          operation.parameters.capabilityKey as string,
          cursor,
          signal,
        );
        signal.throwIfAborted();
        const found = page.items.find(
          (item) => item.capability.key === operation.parameters.capabilityKey,
        );
        if (found) {
          if (
            !(props.appliedGrantRef
              ? projectIntegrationGrantAppliedCandidate(
                  operation,
                  page,
                  found,
                  props.appliedGrantRef,
                )
              : projectIntegrationGrantPlanCandidate(operation, page, found))
          )
            throw new Error("System assistant grant plan pins changed");
          connection.value = selected;
          candidate.value = found;
          return;
        }
        if (!page.nextPageToken) break;
        if (seen.has(page.nextPageToken))
          throw new Error("System assistant grant plan cursor repeated");
        seen.add(page.nextPageToken);
        cursor = page.nextPageToken;
      }
      throw new Error("System assistant grant plan capability unavailable");
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
    <p>{{ $t("assistant.planEditor.projectGrantFixedTarget") }}</p>
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <p v-if="!ownerValid || failed" class="field-error" role="alert">
      {{ $t("assistant.planEditor.grantStale") }}
    </p>
    <template v-if="connection && candidate">
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
    </template>
  </div>
</template>
<style scoped>
.project-grant-plan {
  display: grid;
  gap: 10px;
  min-width: 0;
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
