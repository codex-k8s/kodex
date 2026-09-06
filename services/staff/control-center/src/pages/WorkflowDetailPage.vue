<script setup lang="ts">
import { Check, Play, Plus, Save, Trash2, Upload } from "@lucide/vue";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import TemplateSourceField from "@/features/agents/detail/TemplateSourceField.vue";
import PromptTargetPreview from "@/features/agents/detail/PromptTargetPreview.vue";
import {
  workflowEditorInput,
  workflowStagePromptTarget,
} from "@/features/platform/workflow-editor";
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { useI18n } from "vue-i18n";
import { useUnsavedChanges } from "@/shared/ui/unsaved-changes";
import type {
  WorkflowInputFieldInput,
  WorkflowStepInput,
} from "@/shared/api/generated/openapi/types.gen";
import { usePlatformStore } from "@/features/platform/store";
import { workflowLaunchReadiness } from "@/features/platform/workflow-launch";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";
import { loadAgentCatalogPage } from "@/features/agents/catalog/api";
import EffectiveCapabilityCatalog from "@/features/agents/detail/EffectiveCapabilityCatalog.vue";
const platform = usePlatformStore();
const route = useRoute();
const { t } = useI18n();
const projectRef = computed(() => String(route.params.projectRef));
const workflowRef = computed(() => String(route.params.workflowRef));
const workflow = computed(() => platform.workflows[workflowRef.value]);
const canEdit = computed(() => workflow.value?.nextActions.includes("EDIT"));
const launchReadiness = computed(() => workflowLaunchReadiness(workflow.value));
const canLaunch = computed(
  () => launchReadiness.value?.allowedToSubmit === true,
);
const launchExplanation = computed(() =>
  busy.value
    ? t("common.loading")
    : dirty.value
      ? t("runtime.localChanges")
      : !launchReadiness.value
        ? t("workflowLaunch.missing")
        : !canLaunch.value
          ? t(`workflowLaunch.reasons.${launchReadiness.value.reason}`)
          : t(
              `workflowLaunch.operational.${launchReadiness.value.operationalState}`,
            ),
);
const agentList = computed(() =>
  Object.values(platform.agents).filter(
    (i) => i.projectRef === projectRef.value && !i.system,
  ),
);
async function searchAgents(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  const project = projectRef.value;
  const page = await loadAgentCatalogPage(
    { projectRef: project, query, pageToken, pageSize: 40 },
    signal,
  );
  if (page.items.some((agent) => agent.projectRef !== project))
    throw new Error("Workflow agent selector scope mismatch");
  return {
    items: page.items
      .filter((agent) => !agent.system)
      .map((agent) => ({
        ref: agent.ref,
        title: agent.name,
        description: agent.purpose,
        meta: agent.roleDescription,
      })),
    nextPageToken: page.nextPageToken,
  };
}
function selectedAgent(ref: string) {
  const agent = agentList.value.find((item) => item.ref === ref);
  return agent
    ? { ref: agent.ref, title: agent.name, description: agent.purpose }
    : undefined;
}
function selectAgent(value: unknown, step?: WorkflowStepInput): void {
  if (!canEdit.value || busy.value) return;
  const ref = typeof value === "string" ? value : "";
  if (step) step.agentRef = ref;
  else form.coordinatorAgentRef = ref;
}
const busy = ref(false);
const publishedCapabilitiesStep = ref("");
function publishedStep(position: number) {
  return workflow.value?.state === "PUBLISHED"
    ? workflow.value.steps.find((step) => step.position === position)
    : undefined;
}
function promptTarget(position: number) {
  return workflow.value && !dirty.value
    ? workflowStagePromptTarget(workflow.value, position)
    : undefined;
}
function savedPromptTarget(position: number) {
  return workflow.value
    ? workflowStagePromptTarget(workflow.value, position)
    : undefined;
}
const problem = ref<AppProblem>();
let loadGeneration = 0;
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
const savedForm = ref("");
const dirty = computed(
  () => savedForm.value !== "" && JSON.stringify(form) !== savedForm.value,
);
useUnsavedChanges(dirty, () => t("managed.discard"));
const validStepText = computed(() =>
  form.steps.every(
    (step) =>
      !!step.purpose.trim() &&
      !step.purpose.includes("\0") &&
      step.expectedResult.length <= 1000 &&
      !step.expectedResult.includes("\0"),
  ),
);
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
function toggleCapability(
  step: WorkflowStepInput,
  key: string,
  enabled: boolean,
) {
  if (!canEdit.value || busy.value) return;
  const index = step.requiredCapabilityKeys.indexOf(key);
  if (index >= 0 && !enabled) step.requiredCapabilityKeys.splice(index, 1);
  else if (index < 0 && enabled) step.requiredCapabilityKeys.push(key);
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
  const current = ++loadGeneration;
  const project = projectRef.value;
  const target = workflowRef.value;
  problem.value = undefined;
  try {
    await Promise.all([
      platform.loadWorkflow(target),
      platform.loadAgents(project),
      platform.loadCapabilities(),
    ]);
    if (current !== loadGeneration) return;
    if (workflow.value && workflow.value.projectRef !== project)
      throw new Error("Workflow project scope mismatch");
    if (workflow.value) {
      Object.assign(form, workflowEditorInput(workflow.value));
      savedForm.value = JSON.stringify(form);
    }
  } catch (error) {
    if (current === loadGeneration) problem.value = asProblem(error);
  }
}
async function save() {
  if (!workflow.value || !canEdit.value || busy.value || !validStepText.value)
    return;
  const current = loadGeneration;
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.saveWorkflow(projectRef.value, form, workflow.value);
    if (current === loadGeneration) await load();
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
async function command(action: "VALIDATE" | "PUBLISH" | "ARCHIVE") {
  if (
    !workflow.value?.nextActions.includes(action) ||
    busy.value ||
    dirty.value
  )
    return;
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
watch(
  () => [projectRef.value, workflowRef.value],
  () => {
    publishedCapabilitiesStep.value = "";
    void load();
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  loadGeneration += 1;
});
</script>
<template>
  <PageFrame
    :title="workflow?.name ?? $t('workflows.title')"
    :subtitle="workflow?.purpose"
    ><template #actions
      ><StatusBadge
        v-if="workflow"
        :state="dirty ? 'DRAFT' : workflow.state"
        :label="dirty ? $t('runtime.localChanges') : undefined"
      />
      <RouterLink
        v-if="canLaunch && !dirty && !busy"
        class="button button--primary"
        aria-describedby="workflow-launch-explanation"
        :to="launchRoute()"
        ><Play :size="16" />{{ $t("common.launch") }}</RouterLink
      ><button
        v-else-if="workflow"
        class="button button--primary"
        type="button"
        disabled
        :title="launchExplanation"
        aria-describedby="workflow-launch-explanation"
      >
        <Play :size="16" />{{ $t("common.launch") }}</button
      ><button
        v-if="workflow?.nextActions.includes('VALIDATE')"
        class="button"
        type="button"
        :disabled="busy || dirty"
        @click="command('VALIDATE')"
      >
        <Check :size="16" />{{ $t("workflows.validate") }}</button
      ><button
        v-if="workflow?.nextActions.includes('PUBLISH')"
        class="button button--primary"
        type="button"
        :disabled="busy || dirty"
        @click="command('PUBLISH')"
      >
        <Upload :size="16" />{{ $t("workflows.publish") }}
      </button></template
    >
    <p v-if="workflow" id="workflow-launch-explanation" role="status">
      {{ launchExplanation }}
    </p>
    <AsyncState
      :loading="platform.loading.workflow"
      :problem="platform.problems.workflow"
      @retry="load"
      ><div v-if="workflow" class="workflow-layout">
        <ul
          v-if="(workflow.draft ?? workflow).validationMessages.length"
          class="problem"
          role="status"
        >
          <li
            v-for="message in (workflow.draft ?? workflow).validationMessages"
            :key="message"
          >
            {{ message }}
          </li>
        </ul>
        <fieldset
          id="workflow-editor"
          class="workflow-editor"
          :disabled="!canEdit || busy"
        >
          <legend class="sr-only">{{ $t("workflows.steps") }}</legend>
          <div class="form-grid">
            <label class="field"
              ><span>{{ $t("common.name") }}</span
              ><input v-model.trim="form.name" required
            /></label>
            <div class="field">
              <span>{{ $t("workflows.coordinator") }}</span
              ><AsyncEntityPicker
                :model-value="form.coordinatorAgentRef || null"
                :selected="selectedAgent(form.coordinatorAgentRef)"
                :load-page="searchAgents"
                :disabled="!canEdit || busy"
                :trigger-label="$t('workflows.coordinator')"
                @update:model-value="selectAgent($event)"
              />
            </div>
            <label class="field field--wide"
              ><span>{{ $t("common.purpose") }}</span
              ><VoiceTextarea
                v-model.trim="form.purpose"
                :disabled="!canEdit || busy" /></label
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
                  ><VoiceTextarea
                    :disabled="!canEdit || busy"
                    :value="field.options.join('\n')"
                    required
                    @input="updateFieldOptions(field, $event)" /></label
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
              <Plus :size="16" />{{ $t("common.create") }}
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
                ><input v-model.trim="step.name" required
              /></label>
              <div class="field">
                <span>{{ $t("workflows.stepAgent") }}</span
                ><AsyncEntityPicker
                  :model-value="step.agentRef || null"
                  :selected="selectedAgent(step.agentRef ?? '')"
                  :load-page="searchAgents"
                  :disabled="!canEdit || busy"
                  :trigger-label="$t('workflows.stepAgent')"
                  @update:model-value="selectAgent($event, step)"
                />
              </div>
              <div class="field field--wide">
                <span>{{ $t("common.purpose") }}</span
                ><TemplateSourceField
                  v-model="step.purpose"
                  :target="savedPromptTarget(step.position)"
                  :label="$t('common.purpose')"
                  :disabled="!canEdit || busy"
                />
              </div>
              <PromptTargetPreview
                class="field--wide"
                :target="promptTarget(step.position)"
                :disabled="busy || dirty"
                :disabled-reason="$t('promptContext.saveStage')"
              />
              <label class="check-field"
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
                  <div class="field field--wide">
                    <span>{{ $t("workflows.expectedResult") }}</span
                    ><TemplateSourceField
                      :disabled="!canEdit || busy"
                      :target="savedPromptTarget(step.position)"
                      v-model="step.expectedResult"
                      :label="$t('workflows.expectedResult')"
                    />
                    <span
                      :class="{
                        'text-danger': step.expectedResult.length > 1000,
                      }"
                      >{{ step.expectedResult.length }} / 1000</span
                    >
                  </div>
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
                    <EffectiveCapabilityCatalog
                      v-if="step.agentRef"
                      :agent-ref="step.agentRef"
                      :project-ref="projectRef"
                      mode="REQUIREMENTS"
                      :selected-keys="step.requiredCapabilityKeys"
                      :can-manage="Boolean(canEdit)"
                      :busy="busy"
                      @toggle="
                        (key, enabled) => toggleCapability(step, key, enabled)
                      "
                    />
                    <div v-if="publishedStep(step.position)?.agentRef">
                      <button
                        type="button"
                        class="button button--secondary"
                        @click="
                          publishedCapabilitiesStep =
                            publishedCapabilitiesStep ===
                            publishedStep(step.position)!.ref
                              ? ''
                              : publishedStep(step.position)!.ref
                        "
                      >
                        {{ $t("capabilityAuthority.publishedStep") }}
                      </button>
                      <EffectiveCapabilityCatalog
                        v-if="
                          publishedCapabilitiesStep ===
                          publishedStep(step.position)!.ref
                        "
                        :agent-ref="publishedStep(step.position)!.agentRef!"
                        :project-ref="projectRef"
                        :workflow-ref="workflowRef"
                        :step-key="publishedStep(step.position)!.ref"
                        mode="READ"
                      />
                    </div>
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
              <Trash2 :size="16" />
            </button>
          </article>
          <ProblemNotice v-if="problem" :problem="problem" compact /><button
            v-if="canEdit"
            class="button button--primary workflow-save"
            type="button"
            :disabled="busy || !validStepText"
            @click="save"
          >
            <Save :size="16" />{{ $t("common.save") }}
          </button>
        </fieldset>
      </div></AsyncState
    ></PageFrame
  >
</template>
<style scoped>
.text-danger {
  color: var(--danger);
}
.workflow-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
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
  grid-template-columns: 36px minmax(0, 1fr) 40px;
  gap: 12px;
}
.workflow-save {
  justify-self: start;
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
