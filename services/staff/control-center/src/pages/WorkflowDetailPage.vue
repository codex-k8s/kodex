<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRoute } from "vue-router";
import type {
  WorkflowInputFieldInput,
  WorkflowStepInput,
} from "@/shared/api/generated/openapi/types.gen";
import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
const platform = usePlatformStore();
const route = useRoute();
const projectRef = computed(() => String(route.params.projectRef));
const workflowRef = computed(() => String(route.params.workflowRef));
const workflow = computed(() => platform.workflows[workflowRef.value]);
const canEdit = computed(() => workflow.value?.nextActions.includes("EDIT"));
const canLaunch = computed(() =>
  workflow.value?.nextActions.includes("LAUNCH"),
);
const agentList = computed(() =>
  Object.values(platform.agents).filter(
    (i) => i.projectRef === projectRef.value && !i.system,
  ),
);
const busy = ref(false);
const problem = ref<AppProblem>();
const gateDecisionOptions = [
  "APPROVE",
  "REJECT",
  "REQUEST_CHANGES",
  "CANCEL",
] as const;
const form = reactive({
  name: "",
  purpose: "",
  coordinatorAgentRef: "",
  maxConcurrency: 2,
  timeoutSeconds: 7200,
  completionCriteria: "",
  inputFields: [] as WorkflowInputFieldInput[],
  steps: [] as WorkflowStepInput[],
});
function addInputField() {
  if (!canEdit.value) return;
  form.inputFields.push({
    label: "",
    description: "",
    valueType: "TEXT",
    required: false,
    options: [],
  });
}
function removeInputField(index: number) {
  if (!canEdit.value) return;
  form.inputFields.splice(index, 1);
}
function updateFieldOptions(field: WorkflowInputFieldInput, event: Event) {
  field.options = (event.target as HTMLInputElement).value
    .split("\n")
    .map((item) => item.trim())
    .filter(Boolean);
}
function toggleCapability(step: WorkflowStepInput, key: string) {
  const index = step.requiredCapabilityKeys.indexOf(key);
  if (index >= 0) step.requiredCapabilityKeys.splice(index, 1);
  else step.requiredCapabilityKeys.push(key);
}
function toggleDecision(
  step: WorkflowStepInput,
  decision: WorkflowStepInput["gateDecisions"][number],
) {
  const index = step.gateDecisions.indexOf(decision);
  if (index >= 0) step.gateDecisions.splice(index, 1);
  else step.gateDecisions.push(decision);
}
function addStep() {
  if (!canEdit.value) return;
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
  if (!canEdit.value) return;
  form.steps.splice(index, 1);
  form.steps.forEach((step, position) => {
    step.position = position + 1;
  });
}
async function load() {
  await Promise.all([
    platform.loadWorkflow(workflowRef.value),
    platform.loadAgents(projectRef.value),
    platform.loadCapabilities(),
  ]);
  if (workflow.value) {
    Object.assign(form, {
      name: workflow.value.name,
      purpose: workflow.value.purpose,
      coordinatorAgentRef: workflow.value.coordinatorAgentRef ?? "",
      maxConcurrency: workflow.value.maxConcurrency ?? 1,
      timeoutSeconds: workflow.value.timeoutSeconds ?? 7200,
      completionCriteria: workflow.value.completionCriteria ?? "",
      inputFields: workflow.value.inputFields.map((field) => ({
        key: field.key,
        label: field.label,
        description: field.description,
        valueType: field.valueType,
        required: field.required,
        options: [...field.options],
      })),
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
  if (!workflow.value || !canEdit.value) return;
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
  if (!workflow.value?.nextActions.includes(action)) return;
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
function launchRoute(): string {
  const query = new URLSearchParams({
    targetType: "WORKFLOW",
    targetRef: workflow.value?.ref ?? "",
  });
  return `/projects/${projectRef.value}/runs/new?${query.toString()}`;
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
        <fieldset class="workflow-editor" :disabled="!canEdit">
          <legend class="sr-only">{{ $t("workflows.steps") }}</legend>
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
              ><input v-model.trim="form.completionCriteria" /></label
            ><label class="field"
              ><span>{{ $t("workflows.concurrency") }}</span
              ><input
                v-model.number="form.maxConcurrency"
                type="number"
                min="1"
                max="100"
                required
            /></label>
          </div>
          <section class="editor-section">
            <div class="section-header">
              <div>
                <h2>{{ $t("workflows.inputFields") }}</h2>
                <p>{{ $t("workflows.inputFieldsHint") }}</p>
              </div>
              <button
                v-if="canEdit"
                class="button"
                type="button"
                @click="addInputField"
              >
                {{ $t("workflows.addInputField") }}
              </button>
            </div>
            <div v-if="form.inputFields.length" class="input-field-list">
              <article
                v-for="(field, index) in form.inputFields"
                :key="field.key ?? index"
                class="input-field-card panel form-grid"
              >
                <label class="field"
                  ><span>{{ $t("workflows.inputLabel") }}</span
                  ><input
                    v-model.trim="field.label"
                    required
                    maxlength="160" /></label
                ><label class="field"
                  ><span>{{ $t("workflows.inputType") }}</span
                  ><select v-model="field.valueType">
                    <option value="TEXT">
                      {{ $t("workflows.inputTypes.TEXT") }}
                    </option>
                    <option value="LONG_TEXT">
                      {{ $t("workflows.inputTypes.LONG_TEXT") }}
                    </option>
                    <option value="NUMBER">
                      {{ $t("workflows.inputTypes.NUMBER") }}
                    </option>
                    <option value="BOOLEAN">
                      {{ $t("workflows.inputTypes.BOOLEAN") }}
                    </option>
                    <option value="DATE">
                      {{ $t("workflows.inputTypes.DATE") }}
                    </option>
                    <option value="SELECT">
                      {{ $t("workflows.inputTypes.SELECT") }}
                    </option>
                  </select></label
                ><label class="field field--wide"
                  ><span>{{ $t("workflows.inputDescription") }}</span
                  ><input
                    v-model.trim="field.description"
                    maxlength="500" /></label
                ><label
                  v-if="field.valueType === 'SELECT'"
                  class="field field--wide"
                  ><span>{{ $t("workflows.inputOptions") }}</span
                  ><textarea
                    :value="field.options.join('\n')"
                    required
                    @input="updateFieldOptions(field, $event)"
                  /></label
                ><label class="check-field"
                  ><input v-model="field.required" type="checkbox" />{{
                    $t("workflows.inputRequired")
                  }}</label
                ><button
                  v-if="canEdit"
                  class="button button--danger input-field-remove"
                  type="button"
                  @click="removeInputField(index)"
                >
                  {{ $t("common.delete") }}
                </button>
              </article>
            </div>
            <p v-else class="empty-inline">
              {{ $t("workflows.noInputFields") }}
            </p>
          </section>
          <div class="section-header">
            <h2>{{ $t("workflows.steps") }}</h2>
            <button
              v-if="canEdit"
              class="button"
              type="button"
              @click="addStep"
            >
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
              <details class="field--wide step-advanced">
                <summary>{{ $t("common.advanced") }}</summary>
                <div class="form-grid advanced-grid">
                  <label v-if="step.parallel" class="field"
                    ><span>{{ $t("workflows.parallelGroup") }}</span
                    ><input
                      v-model.number="step.parallelGroup"
                      type="number"
                      min="0"
                      max="50"
                  /></label>
                  <label class="field"
                    ><span>{{ $t("workflows.stepTimeout") }}</span
                    ><input
                      v-model.number="step.timeoutSeconds"
                      type="number"
                      min="1"
                      max="86400"
                      required
                  /></label>
                  <label class="field field--wide"
                    ><span>{{ $t("workflows.expectedResult") }}</span
                    ><textarea
                      v-model.trim="step.expectedResult"
                      maxlength="1000"
                    />
                  </label>
                  <fieldset
                    v-if="step.humanGate"
                    class="choice-field field--wide"
                  >
                    <legend>{{ $t("workflows.gateDecisions") }}</legend>
                    <label
                      v-for="decision in gateDecisionOptions"
                      :key="decision"
                      class="check-field"
                    >
                      <input
                        type="checkbox"
                        :checked="step.gateDecisions.includes(decision)"
                        @change="toggleDecision(step, decision)"
                      />{{ $t(`workflows.gateDecision.${decision}`) }}
                    </label>
                  </fieldset>
                  <fieldset class="choice-field field--wide">
                    <legend>{{ $t("workflows.requiredCapabilities") }}</legend>
                    <label
                      v-for="capability in platform.capabilities"
                      :key="capability.key"
                      class="check-field"
                    >
                      <input
                        type="checkbox"
                        :checked="
                          step.requiredCapabilityKeys.includes(capability.key)
                        "
                        @change="toggleCapability(step, capability.key)"
                      />{{ capability.name }}
                    </label>
                    <span
                      v-if="!platform.capabilities.length"
                      class="secondary-copy"
                      >{{ $t("common.noData") }}</span
                    >
                  </fieldset>
                </div>
              </details>
            </div>
            <button
              v-if="canEdit"
              class="icon-button"
              type="button"
              :aria-label="$t('common.delete')"
              @click="removeStep(index)"
            >
              ×
            </button>
          </article>
          <ProblemNotice v-if="problem" :problem="problem" compact /><button
            v-if="canEdit"
            class="button button--primary"
            type="button"
            :disabled="busy"
            @click="save"
          >
            {{ $t("common.save") }}
          </button>
        </fieldset>
        <aside v-if="canLaunch" class="panel launch-panel">
          <h2>{{ $t("runs.new") }}</h2>
          <p>{{ $t("workflows.launchHint") }}</p>
          <RouterLink class="button button--primary" :to="launchRoute()">
            {{ $t("common.launch") }}
          </RouterLink>
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
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}
.editor-section {
  display: grid;
  gap: 12px;
}
.section-header p,
.launch-panel p,
.empty-inline,
.secondary-copy {
  margin: 4px 0 0;
  color: var(--muted);
}
.input-field-list {
  display: grid;
  gap: 10px;
}
.input-field-card {
  position: relative;
}
.input-field-remove {
  justify-self: end;
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
.step-advanced {
  border-top: 1px solid var(--border);
  padding-top: 10px;
}
.step-advanced summary {
  cursor: pointer;
  color: var(--text-secondary);
}
.advanced-grid {
  margin-top: 12px;
}
.choice-field {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 16px;
  margin: 0;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.choice-field legend {
  padding: 0 6px;
  color: var(--text-secondary);
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
