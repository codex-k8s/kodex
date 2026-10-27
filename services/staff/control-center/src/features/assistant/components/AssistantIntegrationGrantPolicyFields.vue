<script setup lang="ts">
import type { SystemAssistantIntegrationGrantCandidate } from "@/shared/api/generated/openapi/types.gen";

import type { IntegrationGrantPlanPolicyFields } from "../use-integration-grant-plan-policy";

// Общие поля не читают API и не назначают полномочия. Owner и readonly состояние
// проверяет родитель; события проходят его прежний parameter/dirty обработчик.
defineProps<{
  id: string;
  candidate: SystemAssistantIntegrationGrantCandidate;
  appliedGrantRef?: string;
  disabled: boolean;
  enabled: boolean;
  fields: IntegrationGrantPlanPolicyFields;
  connectionName: string;
  recipientScope: "SYSTEM" | "PROJECT";
}>();
defineEmits<{
  enabled: [value: boolean];
  policy: [value: string];
  path: [path: string, checked: boolean];
}>();
</script>
<template>
  <dl>
    <dt>{{ $t("assistant.planEditor.grantConnection") }}</dt>
    <dd>{{ connectionName }}</dd>
    <dt>{{ $t("assistant.planEditor.grantRecipient") }}</dt>
    <dd>
      {{
        $t(
          recipientScope === "PROJECT"
            ? "assistant.settings.projectScope"
            : "assistant.settings.systemScope",
        )
      }}
    </dd>
    <dt>{{ $t("assistant.planEditor.grantCapability") }}</dt>
    <dd>{{ candidate.capability.name }}</dd>
  </dl>

  <p>
    {{ candidate.capability.description }} ·
    {{ $t(`integrations.risk.${candidate.capability.risk}`) }}
  </p>
  <p v-if="appliedGrantRef" role="status">
    {{ $t("assistant.planEditor.systemGrantApplied") }} ·
    {{ $t(`integrations.approvalPolicies.${candidate.currentApprovalPolicy}`) }}
  </p>
  <template v-else>
    <label
      ><input
        type="checkbox"
        :checked="enabled"
        :disabled="disabled || !candidate.grantable"
        @change="$emit('enabled', ($event.target as HTMLInputElement).checked)"
      />{{ $t("assistant.planEditor.grantEnable") }}</label
    >
    <label class="field"
      ><span>{{ $t("integrations.approvalPolicy") }}</span
      ><select
        :id="`${id}-policy`"
        :value="fields.policy ?? ''"
        :disabled="disabled"
        @change="$emit('policy', ($event.target as HTMLSelectElement).value)"
      >
        <option disabled value="">
          {{ $t("integrations.chooseApprovalPolicy") }}
        </option>
        <option
          v-for="option in fields.allowedPolicies"
          :key="option"
          :value="option"
        >
          {{ $t(`integrations.approvalPolicies.${option}`) }}
        </option>
      </select></label
    >
    <fieldset v-if="fields.policy === 'HUMAN_SCOPED'">
      <legend>{{ $t("integrations.approvalScopeTitle") }}</legend>
      <p>{{ $t("integrations.approvalScopeHelp") }}</p>
      <label v-for="path in fields.allowedPaths" :key="path"
        ><input
          type="checkbox"
          :checked="fields.paths.includes(path)"
          :disabled="
            disabled ||
            (!fields.paths.includes(path) && fields.paths.length >= 16)
          "
          @change="
            $emit('path', path, ($event.target as HTMLInputElement).checked)
          "
        /><code>{{ path }}</code></label
      >
    </fieldset>
  </template>
</template>
<style scoped>
fieldset {
  display: grid;
  gap: 8px;
  min-width: 0;
}
label {
  display: flex;
  align-items: center;
  gap: 8px;
}
.field {
  display: grid;
}
code {
  overflow-wrap: anywhere;
}
dd {
  margin: 0 0 8px;
}
</style>
