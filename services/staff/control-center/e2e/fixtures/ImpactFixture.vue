<script setup lang="ts">
import { ref } from "vue";
import EnvironmentImpactDialog from "../../src/features/runtime/EnvironmentImpactDialog.vue";
import SecretImpactDialog from "../../src/features/runtime/SecretImpactDialog.vue";
import ConfigurationEditor from "../../src/features/managed-configurations/ConfigurationEditor.vue";
import RuntimeSecretDraftDialog from "../../src/features/runtime-secrets/RuntimeSecretDraftDialog.vue";
import type { RuntimeSecret } from "../../src/features/runtime-secrets/model";
const kind = new URLSearchParams(window.location.search).get("kind");
const open = ref(true);
const initialDraftRef =
  new URLSearchParams(window.location.search).get("draftRef") ?? undefined;
const initialPlanRef =
  new URLSearchParams(window.location.search).get("planRef") ?? undefined;
const published = ref<RuntimeSecret>();
const rotationSecret: RuntimeSecret = {
  ref: "secret_rotation",
  projectRef: "project_rotation",
  version: 7,
  name: "ROTATION_SYNTHETIC",
  description: "",
  valueType: "JSON",
  currentRevision: 3,
  state: "ACTIVE",
  nextActions: ["ROTATE"],
  createdAt: "2026-09-05T00:00:00Z",
  updatedAt: "2026-09-05T00:00:00Z",
};
function saveRefs(draftRef: string, planRef?: string): void {
  const url = new URL(window.location.href);
  url.searchParams.set("draftRef", draftRef);
  if (planRef) url.searchParams.set("planRef", planRef);
  window.history.replaceState(null, "", url);
}
</script>
<template>
  <main class="impact-fixture">
    <EnvironmentImpactDialog
      v-if="open && kind === 'environment'"
      environment-ref="environment"
      version-ref="target"
      @close="open = false"
    />
    <SecretImpactDialog
      v-else-if="open && kind === 'secret'"
      secret-ref="secret"
      :revision="7"
      @close="open = false"
    />
    <ConfigurationEditor
      v-else-if="kind === 'git'"
      kind="ROLE_IMAGE"
      configuration-ref="configuration"
    />
    <ConfigurationEditor
      v-else-if="kind === 'managed'"
      kind="PROMPT_TEMPLATE"
      configuration-ref="configuration"
    />
    <RuntimeSecretDraftDialog
      v-else-if="open && kind === 'rotation'"
      project-ref="project_rotation"
      :secret="rotationSecret"
      :initial-draft-ref="initialDraftRef"
      :initial-plan-ref="initialPlanRef"
      @saved="saveRefs($event.ref)"
      @plan-prepared="saveRefs"
      @published="published = $event"
      @close="open = false"
    />
    <output v-if="published" data-testid="rotation-published"
      >{{ published.ref }} · {{ published.currentRevision }}</output
    >
  </main>
</template>
<style scoped>
.impact-fixture {
  max-width: 1000px;
  margin: 0 auto;
  padding: 16px;
}
</style>
