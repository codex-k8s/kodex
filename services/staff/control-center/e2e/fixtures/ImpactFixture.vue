<script setup lang="ts">
import { ref } from "vue";
import EnvironmentImpactDialog from "../../src/features/runtime/EnvironmentImpactDialog.vue";
import SecretImpactDialog from "../../src/features/runtime/SecretImpactDialog.vue";
import ConfigurationEditor from "../../src/features/managed-configurations/ConfigurationEditor.vue";
const kind = new URLSearchParams(window.location.search).get("kind");
const open = ref(true);
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
      v-else-if="kind === 'managed'"
      kind="PROMPT_TEMPLATE"
      configuration-ref="configuration"
    />
  </main>
</template>
<style scoped>
.impact-fixture {
  max-width: 1000px;
  margin: 0 auto;
  padding: 16px;
}
</style>
