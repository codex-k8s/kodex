<script setup lang="ts">
import { computed, reactive, ref } from "vue";
import Editor from "../../src/features/workflows/WorkflowResultContractEditor.vue";
import AttemptResults from "../../src/features/runs/WorkflowAttemptResults.vue";
import { validWorkflowDAG } from "../../src/features/workflows/result-contract";
const disabled = ref(false);
const steps = reactive([
  {
    ref: "code",
    name: "Разработка",
    agentRef: "developer",
    dependsOn: [] as string[],
    requiredCapabilityKeys: [],
    resultRequirements: [
      { key: "pr", kind: "PULL_REQUEST", repository: "codex-k8s/test-repo" },
    ] as Record<string, unknown>[],
  },
  {
    ref: "review",
    name: "Ревью",
    agentRef: "reviewer",
    dependsOn: ["code"],
    requiredCapabilityKeys: [],
    resultRequirements: [
      { key: "review", kind: "REVIEW", subjectStepKey: "code" },
    ] as Record<string, unknown>[],
  },
]);
const valid = computed(() =>
  validWorkflowDAG(steps, 2, "Журнал обращений", "ref"),
);
</script>
<template>
  <main>
    <label><input v-model="disabled" type="checkbox" /> Только чтение</label>
    <section
      v-for="(step, index) in steps"
      :key="index"
      :aria-label="step.name"
    >
      <h2>{{ step.name }}</h2>
      <Editor
        :step-key="step.ref"
        :dependencies="step.dependsOn"
        :requirements="step.resultRequirements"
        :predecessors="
          steps
            .slice(0, index)
            .map((prior) => ({ key: prior.ref, name: prior.name }))
        "
        :disabled="disabled"
        @key="step.ref = $event"
        @dependencies="step.dependsOn = $event"
        @requirements="step.resultRequirements = $event"
      />
    </section>
    <button type="button" :disabled="disabled || !valid">
      Сохранить процесс
    </button>
    <pre data-test-draft>{{ JSON.stringify(steps) }}</pre>
    <AttemptResults
      :attempts="[
        {
          attempt: 1,
          runRef: 'run_fixture123',
          accepted: false,
          reason: 'MISSING_RESULT',
          results: [],
        },
      ]"
    />
  </main>
</template>
<style scoped>
main {
  padding: 16px;
  max-width: 900px;
  margin: auto;
  display: grid;
  gap: 16px;
}
section {
  min-width: 0;
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
