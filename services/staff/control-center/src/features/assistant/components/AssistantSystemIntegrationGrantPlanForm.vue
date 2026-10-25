<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import { usePlatformStore } from "@/features/platform/store";
import {
  operationInputs,
  operationParameter,
  type EditablePlanOperation,
} from "../model";
import {
  systemIntegrationGrantPlanCandidate,
  systemIntegrationGrantAppliedCandidate,
  systemIntegrationGrantPlanOwner,
} from "../system-integration-grant-plan";
import {
  readSystemGrantCandidates,
  readSystemGrantOwner,
  validSystemGrantSelection,
} from "../system-integration-grants";
import {
  getIntegrationConnection,
  getSystemAssistant,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  IntegrationConnection,
  SystemAssistantIntegrationGrantCandidate,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import { unwrap } from "@/shared/api/problem";
import { useIntegrationGrantPlanPolicy } from "../use-integration-grant-plan-policy";
import AssistantIntegrationGrantPolicyFields from "./AssistantIntegrationGrantPolicyFields.vue";

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
const id = `system-grant-plan-${useId()}`;
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
    systemIntegrationGrantPlanOwner(
      input.value,
      platform.bootstrap?.organizationRef,
    ),
);
const { policy, paths, fields, changePolicy, togglePath } =
  useIntegrationGrantPlanPolicy(candidate, value, changed);
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
      !systemIntegrationGrantPlanOwner(
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
      const [connectionResponse, assistantResponse] = await Promise.all([
        unwrap(
          getIntegrationConnection({
            path: {
              connectionRef: operation.parameters.connectionRef as string,
            },
            signal: requestSignal(signal),
            cache: "no-store",
          }),
        ),
        unwrap(
          getSystemAssistant({
            signal: requestSignal(signal),
            cache: "no-store",
          }),
        ),
      ]);
      signal.throwIfAborted();
      const selected = connectionResponse.data;
      const assistant = assistantResponse.data;
      if (
        selected.ref !== operation.parameters.connectionRef ||
        (props.appliedGrantRef
          ? selected.version <
            (operation.expectedVersion ?? Number.POSITIVE_INFINITY)
          : selected.version !== operation.expectedVersion) ||
        assistant.ref !== operation.parameters.systemAssistantRef
      )
        throw new Error("System assistant grant plan readback mismatch");
      const owner = await readSystemGrantOwner(
        platform.bootstrap?.organizationRef ?? "",
        assistant,
        signal,
      );
      const seen = new Set<string>();
      let cursor: string | undefined;
      for (let pageCount = 0; pageCount < 10; pageCount++) {
        const page = await readSystemGrantCandidates(
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
              ? systemIntegrationGrantAppliedCandidate(
                  operation,
                  page,
                  found,
                  props.appliedGrantRef,
                )
              : systemIntegrationGrantPlanCandidate(operation, page, found))
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
  <div class="system-grant-plan">
    <p>{{ $t("assistant.planEditor.systemGrantFixedTarget") }}</p>
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <p v-if="!ownerValid || failed" class="field-error" role="alert">
      {{ $t("assistant.planEditor.grantStale") }}
    </p>
    <template v-if="connection && candidate">
      <AssistantIntegrationGrantPolicyFields
        :id="id"
        :candidate="candidate"
        :applied-grant-ref="appliedGrantRef"
        :disabled="disabled || !ownerValid"
        :enabled="value('enabled') === true"
        :fields="fields"
        :connection-name="connection.name"
        recipient-scope="SYSTEM"
        @enabled="changed('enabled', $event)"
        @policy="changePolicy"
        @path="togglePath"
      />
    </template>
  </div>
</template>
<style scoped>
.system-grant-plan {
  display: grid;
  gap: 10px;
  min-width: 0;
}
</style>
