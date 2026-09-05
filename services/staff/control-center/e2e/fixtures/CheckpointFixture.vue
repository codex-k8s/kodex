<script setup lang="ts">
import { ref } from "vue";
import TemplateVariableCatalog from "../../src/features/agents/detail/TemplateVariableCatalog.vue";
import EmailMailboxCredentialPanel from "../../src/features/integrations/ui/EmailMailboxCredentialPanel.vue";
import type { IntegrationConnection } from "../../src/shared/api/generated/openapi/types.gen";
const agentRef = ref("agent_first");
const selected = ref("");
const connection = ref<IntegrationConnection>({
  ref: "connection_synthetic",
  version: 3,
  definitionKey: "email",
  name: "Email",
  state: "CONNECTED",
  credentialsConfigured: true,
  credentialsHint: "",
  capabilities: [],
  grants: [],
  nextActions: ["CONFIGURE_CREDENTIAL"],
  definitionVersion: "1",
  definitionDigest: "a".repeat(64),
  publicConfiguration: {},
});
</script>
<template>
  <main class="checkpoint-fixture">
    <button class="button" @click="agentRef = 'agent_second'">
      Другой агент
    </button>
    <TemplateVariableCatalog
      project-ref="project_synthetic"
      :agent-ref="agentRef"
      :disabled="false"
      @select="selected = $event.id"
    />
    <output>{{ selected }}</output>
    <EmailMailboxCredentialPanel
      :connection="connection"
      @saved="connection = { ...connection, version: 4 }"
    />
  </main>
</template>
<style scoped>
.checkpoint-fixture {
  max-width: 1000px;
  padding: 16px;
  margin: 0 auto;
}
</style>
