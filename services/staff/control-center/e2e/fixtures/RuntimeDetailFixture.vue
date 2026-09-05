<script setup lang="ts">
import { provide, ref } from "vue";
import AgentRuntimePanel from "../../src/features/agents/detail/AgentRuntimePanel.vue";
import RuntimeRevisionDiffPanel from "../../src/features/runs/RuntimeRevisionDiffPanel.vue";
import { voiceContextKey } from "../../src/shared/ui/voice-input";
const version = ref(1);
provide(voiceContextKey, {
  available: ref(true),
  transcribe: () => Promise.resolve("Synthetic transcript"),
});
</script>
<template>
  <main class="runtime-fixture">
    <AgentRuntimePanel agent-ref="agent_synthetic" :can-edit="true" />
    <button class="button" @click="version++">Новая ревизия</button>
    <RuntimeRevisionDiffPanel
      :run="{ ref: 'run_current', sessionRef: 'session_one', version }"
    />
  </main>
</template>
<style scoped>
.runtime-fixture {
  max-width: 1300px;
  padding: 16px;
  margin: auto;
}
</style>
