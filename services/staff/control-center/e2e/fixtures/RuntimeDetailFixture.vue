<script setup lang="ts">
import { provide, ref } from "vue";
import AgentRuntimePanel from "../../src/features/agents/detail/AgentRuntimePanel.vue";
import RuntimeRevisionDiffPanel from "../../src/features/runs/RuntimeRevisionDiffPanel.vue";
import { voiceContextKey } from "../../src/shared/ui/voice-input";
import AgentInstructionsPanel from "../../src/features/agents/detail/AgentInstructionsPanel.vue";
import AssistantPlanEditor from "../../src/features/assistant/components/AssistantPlanEditor.vue";
import type { AssistantPlan } from "../../src/shared/api/generated/openapi/types.gen";
const version = ref(1);
const analogs = ref(false);
const editorBusy = ref(false);
const instructions = ref("Исходные инструкции");
const plan: AssistantPlan = {
  ref: "plan_synthetic",
  version: 1,
  revision: 1,
  state: "DRAFT",
  conversationRef: "conversation_synthetic",
  operations: [],
  auditSummary: "Описание плана",
  applied: false,
  contentDigest: "a".repeat(64),
  validationProblems: [],
  nextActions: [],
};
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
    <button class="button" @click="analogs = true">Проверить редакторы</button>
    <section v-if="analogs" data-testid="analog-editors">
      <label
        ><input v-model="editorBusy" type="checkbox" /> Выполняется
        сохранение</label
      >
      <AgentInstructionsPanel
        v-model="instructions"
        project-ref="project_synthetic"
        agent-ref="agent_synthetic"
        state="DRAFT"
        :validation-messages="[]"
        :can-edit="true"
        :can-validate="true"
        :can-publish="true"
        :busy="editorBusy"
        :dirty="true"
      />
      <AssistantPlanEditor :plan="plan" :busy="editorBusy" />
    </section>
  </main>
</template>
<style scoped>
.runtime-fixture {
  max-width: 1300px;
  padding: 16px;
  margin: auto;
}
</style>
