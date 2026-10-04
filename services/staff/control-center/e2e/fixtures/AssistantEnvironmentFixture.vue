<script setup lang="ts">
import { client } from "../../src/shared/api/generated/openapi/client.gen";
import { usePlatformStore } from "../../src/features/platform/store";
import type { BootstrapState } from "../../src/shared/api/generated/openapi/types.gen";
usePlatformStore().bootstrap = {
  organizationRef: "org_fixture",
  platformRole: "OWNER",
  assistant: { ref: "agt_system_fixture" },
} as BootstrapState;
import AssistantEnvironmentSettingsPanel from "../../src/features/assistant/components/AssistantEnvironmentSettingsPanel.vue";
import { createRuntimeResourceCatalogs } from "../../src/features/runtime/resource-catalog-api";
import AssistantPlanEditor from "../../src/features/assistant/components/AssistantPlanEditor.vue";
import AssistantEnvironmentDraftCard from "../../src/features/assistant/components/AssistantEnvironmentDraftCard.vue";
import AssistantInstructionDraftCard from "../../src/features/assistant/components/AssistantInstructionDraftCard.vue";
import AssistantAgentEnvironmentBindingCard from "../../src/features/assistant/components/AssistantAgentEnvironmentBindingCard.vue";
import { projectHelperPlan } from "./assistant-project-helper";
import { ref } from "vue";
import type { AssistantPlanOperationInput } from "../../src/shared/api/generated/openapi/types.gen";
client.setConfig({ baseUrl: location.origin });
const scope = { kind: "ORGANIZATION", organizationRef: "org_fixture" } as const;
const catalogs = createRuntimeResourceCatalogs(scope.organizationRef);
const query = new URLSearchParams(location.search);
const helperKind = query.get("helper");
const helperPlan = helperKind
  ? projectHelperPlan(
      helperKind,
      query.get("state") === "APPLIED",
      query.get("tamper") ?? undefined,
      query.get("source") === "CONTEXT" ? "prj_source_context" : undefined,
    )
  : undefined;
const saved = ref<AssistantPlanOperationInput[]>([]);
</script>
<template>
  <main class="assistant-environment-fixture">
    <h1>{{ $t("assistant.resources.environment") }}</h1>
    <template v-if="helperPlan">
      <AssistantPlanEditor
        :plan="helperPlan"
        @save="(_summary, operations) => (saved = operations)"
      />
      <AssistantEnvironmentDraftCard
        v-if="helperKind === 'ENV'"
        :plan="helperPlan"
        operation-ref="operation_project_helper"
      />
      <AssistantInstructionDraftCard
        v-if="helperKind === 'INSTR'"
        :plan="helperPlan"
        operation-ref="operation_project_helper"
      />
      <AssistantAgentEnvironmentBindingCard
        v-if="helperKind === 'BIND'"
        :plan="helperPlan"
        operation-ref="operation_project_helper"
      />
      <output hidden data-testid="helper-saved">{{
        JSON.stringify(saved)
      }}</output>
      <output hidden data-testid="helper-plan-project">{{
        helperPlan.projectRef ?? "SYSTEM_WITHOUT_PROJECT"
      }}</output>
    </template>
    <AssistantEnvironmentSettingsPanel
      v-else
      agent-ref="agent_fixture"
      :can-edit="true"
      :resource-scope="scope"
      :image-catalog="catalogs.images"
      :secret-catalog="catalogs.secrets"
    />
  </main>
</template>
<style scoped>
.assistant-environment-fixture {
  max-width: 1120px;
  margin: 20px auto;
  padding: 12px;
  min-width: 0;
}
</style>
