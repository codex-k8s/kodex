<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import type { WorkflowStepInput } from "@/shared/api/generated/openapi/types.gen";
import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
const platform = usePlatformStore();
const route = useRoute();
const router = useRouter();
const projectRef = computed(() => String(route.params.projectRef));
const workflowRef = computed(() => String(route.params.workflowRef));
const workflow = computed(() => platform.workflows[workflowRef.value]);
const agentList = computed(() =>
  Object.values(platform.agents).filter(
    (i) => i.projectRef === projectRef.value && !i.system,
  ),
);
const busy = ref(false);
const problem = ref<AppProblem>();
const task = ref("");
const form = reactive({
  name: "",
  purpose: "",
  coordinatorAgentRef: "",
  maxConcurrency: 2,
  timeoutSeconds: 7200,
  completionCriteria: "",
  steps: [] as WorkflowStepInput[],
});
function addStep() {
  form.steps.push({
    position: form.steps.length + 1,
    name: "",
    purpose: "",
    agentRef: "",
    parallel: false,
    parallelGroup: 0,
    timeoutSeconds: 1800,
    expectedResult: "",
    humanGate: false,
    gateDecisions: ["APPROVE", "REJECT", "REQUEST_CHANGES"],
    requiredCapabilityKeys: [],
  });
}
function removeStep(index: number) {
  form.steps.splice(index, 1);
  form.steps.forEach((step, position) => {
    step.position = position + 1;
  });
}
async function load() {
  await Promise.all([
    platform.loadWorkflow(workflowRef.value),
    platform.loadAgents(projectRef.value),
  ]);
  if (workflow.value) {
    Object.assign(form, {
      name: workflow.value.name,
      purpose: workflow.value.purpose,
      coordinatorAgentRef: workflow.value.coordinatorAgentRef ?? "",
      maxConcurrency: workflow.value.maxConcurrency ?? 1,
      timeoutSeconds: workflow.value.timeoutSeconds ?? 7200,
      completionCriteria: workflow.value.completionCriteria ?? "",
      steps: workflow.value.steps.map((step) => ({
        position: step.position,
        name: step.name,
        purpose: step.purpose,
        agentRef: step.agentRef ?? "",
        parallel: step.parallel,
        parallelGroup: step.parallelGroup,
        humanGate: step.humanGate,
        timeoutSeconds: step.timeoutSeconds,
        expectedResult: step.expectedResult,
        gateDecisions: [...step.gateDecisions],
        requiredCapabilityKeys: [...step.requiredCapabilityKeys],
      })),
    });
  }
}
async function save() {
  if (!workflow.value) return;
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.saveWorkflow(projectRef.value, form, workflow.value);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
async function command(action: "VALIDATE" | "PUBLISH" | "ARCHIVE") {
  if (!workflow.value) return;
  busy.value = true;
  try {
    await platform.changeWorkflow(workflow.value, action);
    await load();
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
async function launch() {
  if (!workflow.value || !task.value.trim()) return;
  busy.value = true;
  try {
    const run = await platform.launch({
      projectRef: projectRef.value,
      targetRef: workflow.value.ref,
      targetType: "WORKFLOW",
      title: task.value.trim().slice(0, 160),
      task: task.value.trim(),
    });
    await router.push(`/runs/${run.ref}`);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
onMounted(() => void load());
</script>
<template>
  <PageFrame
    :title="workflow?.name ?? $t('workflows.title')"
    :subtitle="workflow?.purpose"
    ><template #actions
      ><StatusBadge v-if="workflow" :state="workflow.state" /><button
        v-if="workflow?.nextActions.includes('VALIDATE')"
        class="button"
        type="button"
        :disabled="busy"
        @click="command('VALIDATE')"
      >
        {{ $t("workflows.validate") }}</button
      ><button
        v-if="workflow?.nextActions.includes('PUBLISH')"
        class="button button--primary"
        type="button"
        :disabled="busy"
        @click="command('PUBLISH')"
      >
        {{ $t("workflows.publish") }}
      </button></template
    ><AsyncState
      :loading="platform.loading.workflow"
      :problem="platform.problems.workflow"
      @retry="load"
      ><div v-if="workflow" class="workflow-layout">
        <section class="workflow-editor">
          <div class="panel form-grid">
            <label class="field"
              ><span>{{ $t("common.name") }}</span
              ><input v-model.trim="form.name" required /></label
            ><label class="field"
              ><span>{{ $t("workflows.coordinator") }}</span
              ><select v-model="form.coordinatorAgentRef" required>
                <option
                  v-for="agent in agentList"
                  :key="agent.ref"
                  :value="agent.ref"
                >
                  {{ agent.name }}
                </option>
              </select></label
            ><label class="field field--wide"
              ><span>{{ $t("common.purpose") }}</span
              ><textarea v-model.trim="form.purpose" /></label
            ><label class="field"
              ><span>{{ $t("workflows.timeout") }}</span
              ><input
                v-model.number="form.timeoutSeconds"
                type="number"
                min="1"
                max="604800" /></label
            ><label class="field"
              ><span>{{ $t("workflows.completion") }}</span
              ><input v-model.trim="form.completionCriteria"
            /></label>
          </div>
          <div class="section-header">
            <h2>{{ $t("workflows.steps") }}</h2>
            <button class="button" type="button" @click="addStep">
              {{ $t("common.create") }}
            </button>
          </div>
          <article
            v-for="(step, index) in form.steps"
            :key="index"
            class="workflow-step panel"
          >
            <span class="step-number">{{ index + 1 }}</span>
            <div class="form-grid">
              <label class="field"
                ><span>{{ $t("workflows.stepName") }}</span
                ><input v-model.trim="step.name" required /></label
              ><label class="field"
                ><span>{{ $t("workflows.stepAgent") }}</span
                ><select v-model="step.agentRef" required>
                  <option
                    v-for="agent in agentList"
                    :key="agent.ref"
                    :value="agent.ref"
                  >
                    {{ agent.name }}
                  </option>
                </select></label
              ><label class="field field--wide"
                ><span>{{ $t("common.purpose") }}</span
                ><textarea v-model.trim="step.purpose" required /></label
              ><label class="check-field"
                ><input v-model="step.parallel" type="checkbox" />{{
                  $t("workflows.parallel")
                }}</label
              ><label class="check-field"
                ><input v-model="step.humanGate" type="checkbox" />{{
                  $t("workflows.humanGate")
                }}</label
              >
            </div>
            <button
              class="icon-button"
              type="button"
              :aria-label="$t('common.delete')"
              @click="removeStep(index)"
            >
              ×
            </button>
          </article>
          <ProblemNotice v-if="problem" :problem="problem" compact /><button
            class="button button--primary"
            type="button"
            :disabled="busy || !workflow.nextActions.includes('EDIT')"
            @click="save"
          >
            {{ $t("common.save") }}
          </button>
        </section>
        <aside class="panel launch-panel">
          <h2>{{ $t("runs.new") }}</h2>
          <label class="field"
            ><span>{{ $t("runs.task") }}</span
            ><textarea v-model="task" maxlength="8000" /></label
          ><button
            class="button button--primary"
            type="button"
            :disabled="
              busy || !task.trim() || !workflow.nextActions.includes('LAUNCH')
            "
            @click="launch"
          >
            {{ $t("common.launch") }}
          </button>
        </aside>
      </div></AsyncState
    ></PageFrame
  >
</template>
<style scoped>
.workflow-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  gap: 18px;
}
.workflow-editor {
  display: grid;
  gap: 14px;
}
.workflow-step {
  position: relative;
  display: grid;
  grid-template-columns: 36px 1fr 40px;
  gap: 12px;
}
.step-number {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  color: white;
  background: var(--accent);
}
.check-field {
  display: flex;
  align-items: center;
  gap: 8px;
}
.check-field input {
  width: 20px;
  min-height: 20px;
}
.launch-panel {
  display: grid;
  align-content: start;
  gap: 12px;
}
@media (max-width: 950px) {
  .workflow-layout {
    grid-template-columns: 1fr;
  }
}
</style>
