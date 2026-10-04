<script setup lang="ts">
import { client } from "../../src/shared/api/generated/openapi/client.gen";
import { usePlatformStore } from "../../src/features/platform/store";
import type { BootstrapState } from "../../src/shared/api/generated/openapi/types.gen";
usePlatformStore().bootstrap = {
  organizationRef: "org_fixture",
  platformRole: "OWNER",
} as BootstrapState;
import AssistantEnvironmentSettingsPanel from "../../src/features/assistant/components/AssistantEnvironmentSettingsPanel.vue";
import { createRuntimeResourceCatalogs } from "../../src/features/runtime/resource-catalog-api";
client.setConfig({ baseUrl: location.origin });
const scope = { kind: "ORGANIZATION", organizationRef: "org_fixture" } as const;
const catalogs = createRuntimeResourceCatalogs(scope.organizationRef);
</script>
<template>
  <main class="assistant-environment-fixture">
    <h1>{{ $t("assistant.resources.environment") }}</h1>
    <AssistantEnvironmentSettingsPanel
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
