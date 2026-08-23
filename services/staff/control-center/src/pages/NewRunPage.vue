<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import { usePlatformStore } from "@/features/platform/store";
import type {
  Run,
  WorkflowInputField,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const platform = usePlatformStore();
const route = useRoute();
const router = useRouter();
const { locale } = useI18n();
const projectRef = computed(() => String(route.params.projectRef));
const project = computed(() => platform.projects[projectRef.value]);
const canLaunch = computed(() =>
  project.value?.nextActions.includes("CREATE_RUN"),
);
const busy = ref(false);
const problem = ref<AppProblem>();
const sessionMode = ref<"NEW" | "CONTINUE">("NEW");
const selectedArtifacts = ref<string[]>([]);
const inputValues = reactive<Record<string, string | number>>({});
const booleanInputValues = reactive<Record<string, boolean>>({});
const form = reactive({
  targetType: "AGENT" as "AGENT" | "WORKFLOW",
  targetRef: "",
  title: "",
  task: "",
  sessionRef: "",
});

const agents = computed(() =>
  Object.values(platform.agents).filter(
    (agent) =>
      agent.projectRef === projectRef.value &&
      agent.enabled &&
      !agent.system &&
      agent.nextActions.includes("LAUNCH"),
  ),
);
const workflows = computed(() =>
  Object.values(platform.workflows).filter(
    (workflow) =>
      workflow.projectRef === projectRef.value &&
      workflow.state === "PUBLISHED" &&
      workflow.nextActions.includes("LAUNCH"),
  ),
);
const targets = computed(() =>
  form.targetType === "AGENT" ? agents.value : workflows.value,
);
const selectedTarget = computed(() =>
  targets.value.find((target) => target.ref === form.targetRef),
);
const selectedWorkflow = computed(() =>
  form.targetType === "WORKFLOW"
    ? platform.workflows[form.targetRef]
    : undefined,
);
const availableArtifacts = computed(() =>
  Object.values(platform.artifacts)
    .filter(
      (artifact) =>
        artifact.projectRef === projectRef.value &&
        artifact.scanState === "CLEAN",
    )
    .sort((left, right) => right.createdAt.localeCompare(left.createdAt)),
);
const resumableRuns = computed(() => {
  const sessions = new Set<string>();
  return Object.values(platform.runs)
    .filter(
      (run) =>
        run.projectRef === projectRef.value &&
        run.target.type === form.targetType &&
        run.target.ref === form.targetRef &&
        ["SUCCEEDED", "FAILED", "CANCELLED"].includes(run.state),
    )
    .sort((left, right) => right.createdAt.localeCompare(left.createdAt))
    .filter((run) => {
      if (sessions.has(run.sessionRef)) return false;
      sessions.add(run.sessionRef);
      return true;
    });
});

function resetTargetContext(): void {
  for (const key of Object.keys(inputValues))
    Reflect.deleteProperty(inputValues, key);
  for (const key of Object.keys(booleanInputValues))
    Reflect.deleteProperty(booleanInputValues, key);
  for (const field of selectedWorkflow.value?.inputFields ?? []) {
    if (field.valueType === "BOOLEAN") booleanInputValues[field.key] = false;
  }
  selectedArtifacts.value = [];
  sessionMode.value = "NEW";
  form.sessionRef = "";
}

function inputComponentType(field: WorkflowInputField): string {
  if (field.valueType === "NUMBER") return "number";
  if (field.valueType === "DATE") return "date";
  return "text";
}

function formatRun(run: Run): string {
  return `${run.title} · ${new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(run.createdAt))}`;
}

function workflowInput(): Record<string, string | number | boolean> {
  const result: Record<string, string | number | boolean> = {};
  for (const field of selectedWorkflow.value?.inputFields ?? []) {
    if (field.valueType === "BOOLEAN") {
      result[field.key] = booleanInputValues[field.key] ?? false;
      continue;
    }
    const value = inputValues[field.key];
    if (value !== undefined && value !== "") result[field.key] = value;
  }
  return result;
}

async function submit(): Promise<void> {
  if (!canLaunch.value || busy.value || !selectedTarget.value) return;
  if (sessionMode.value === "CONTINUE" && !form.sessionRef) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const run = await platform.launch({
      projectRef: projectRef.value,
      targetRef: form.targetRef,
      targetType: form.targetType,
      title: form.title,
      task: form.task,
      ...(selectedWorkflow.value ? { input: workflowInput() } : {}),
      artifactRefs: [...selectedArtifacts.value],
      ...(sessionMode.value === "CONTINUE"
        ? { sessionRef: form.sessionRef }
        : {}),
    });
    await router.push(`/runs/${run.ref}`);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function load(): Promise<void> {
  await Promise.all([
    platform.loadAgents(projectRef.value),
    platform.loadWorkflows(projectRef.value),
    platform.loadArtifacts(projectRef.value),
    platform.loadRuns(projectRef.value),
    platform.loadProject(projectRef.value),
  ]);
  const requestedType = route.query.targetType;
  const requestedRef = route.query.targetRef;
  if (
    (requestedType === "AGENT" || requestedType === "WORKFLOW") &&
    typeof requestedRef === "string"
  ) {
    form.targetType = requestedType;
    if (targets.value.some((target) => target.ref === requestedRef))
      form.targetRef = requestedRef;
  }
  resetTargetContext();
}

watch(
  () => [form.targetType, form.targetRef],
  () => resetTargetContext(),
);
watch(resumableRuns, (runs) => {
  if (!runs.some((run) => run.sessionRef === form.sessionRef)) {
    form.sessionRef = "";
    if (sessionMode.value === "CONTINUE" && runs.length === 0)
      sessionMode.value = "NEW";
  }
});
onMounted(() => void load());
</script>

<template>
  <PageFrame :title="$t('runs.new')" :subtitle="$t('runs.subtitle')">
    <AsyncState
      :loading="
        platform.loading.project ||
        platform.loading.agents ||
        platform.loading.workflows ||
        platform.loading.artifacts ||
        platform.loading.runs
      "
      :problem="
        platform.problems.project ??
        platform.problems.agents ??
        platform.problems.workflows ??
        platform.problems.artifacts ??
        platform.problems.runs
      "
      @retry="load"
    >
      <form v-if="canLaunch" class="new-run-layout" @submit.prevent="submit">
        <div class="new-run-main">
          <section class="panel form-grid">
            <label class="field"
              ><span>{{ $t("runs.targetType") }}</span
              ><select v-model="form.targetType" @change="form.targetRef = ''">
                <option value="AGENT">{{ $t("runs.agent") }}</option>
                <option value="WORKFLOW">{{ $t("runs.workflow") }}</option>
              </select></label
            ><label class="field"
              ><span>{{ $t("common.target") }}</span
              ><select v-model="form.targetRef" required>
                <option value="" disabled>{{ $t("runs.chooseTarget") }}</option>
                <option
                  v-for="target in targets"
                  :key="target.ref"
                  :value="target.ref"
                >
                  {{ target.name }}
                </option>
              </select></label
            ><label class="field field--wide"
              ><span>{{ $t("runs.runTitle") }}</span
              ><input
                v-model.trim="form.title"
                required
                maxlength="240" /></label
            ><label class="field field--wide"
              ><span>{{ $t("runs.task") }}</span
              ><textarea v-model.trim="form.task" required maxlength="32768" />
            </label>
          </section>

          <section
            v-if="selectedWorkflow?.inputFields.length"
            class="panel form-grid"
          >
            <header class="section-heading field--wide">
              <h2>{{ $t("runs.workflowInput") }}</h2>
              <p>{{ $t("runs.workflowInputHint") }}</p>
            </header>
            <template
              v-for="field in selectedWorkflow.inputFields"
              :key="field.key"
            >
              <label
                v-if="field.valueType === 'LONG_TEXT'"
                class="field field--wide"
                ><span>{{ field.label }}</span
                ><textarea
                  v-model="inputValues[field.key]"
                  :required="field.required"
                  maxlength="32768"
                  :aria-describedby="
                    field.description ? `hint-${field.key}` : undefined
                  "
                /><small v-if="field.description" :id="`hint-${field.key}`">{{
                  field.description
                }}</small></label
              ><label v-else-if="field.valueType === 'SELECT'" class="field"
                ><span>{{ field.label }}</span
                ><select
                  v-model="inputValues[field.key]"
                  :required="field.required"
                >
                  <option value="" :disabled="field.required">
                    {{ $t("common.noData") }}
                  </option>
                  <option
                    v-for="option in field.options"
                    :key="option"
                    :value="option"
                  >
                    {{ option }}
                  </option></select
                ><small v-if="field.description">{{
                  field.description
                }}</small></label
              ><label
                v-else-if="field.valueType === 'BOOLEAN'"
                class="check-field run-checkbox"
                ><input
                  v-model="booleanInputValues[field.key]"
                  type="checkbox"
                />{{ field.label
                }}<small v-if="field.description">{{
                  field.description
                }}</small></label
              ><label v-else-if="field.valueType === 'NUMBER'" class="field"
                ><span>{{ field.label }}</span
                ><input
                  v-model.number="inputValues[field.key]"
                  type="number"
                  :required="field.required"
                /><small v-if="field.description">{{
                  field.description
                }}</small></label
              ><label v-else class="field"
                ><span>{{ field.label }}</span
                ><input
                  v-model="inputValues[field.key]"
                  :type="inputComponentType(field)"
                  :required="field.required"
                  :maxlength="field.valueType === 'TEXT' ? 4000 : undefined"
                /><small v-if="field.description">{{
                  field.description
                }}</small></label
              ></template
            >
          </section>

          <section class="panel">
            <header class="section-heading">
              <h2>{{ $t("runs.inputFiles") }}</h2>
              <p>{{ $t("runs.inputFilesHint") }}</p>
            </header>
            <div v-if="availableArtifacts.length" class="file-options">
              <label
                v-for="artifact in availableArtifacts"
                :key="artifact.ref"
                class="file-option"
              >
                <input
                  v-model="selectedArtifacts"
                  type="checkbox"
                  :value="artifact.ref"
                  :disabled="
                    selectedArtifacts.length >= 50 &&
                    !selectedArtifacts.includes(artifact.ref)
                  "
                />
                <span>
                  <strong>{{ artifact.fileName }}</strong>
                  <small>{{ $t("runs.fileReady") }}</small>
                </span>
              </label>
            </div>
            <p v-else class="secondary-copy">{{ $t("runs.noInputFiles") }}</p>
            <RouterLink class="button" :to="`/projects/${projectRef}/files`">
              {{ $t("runs.manageFiles") }}
            </RouterLink>
          </section>

          <section class="run-policies">
            <fieldset class="panel choice-field">
              <legend>{{ $t("runs.sessionPolicy") }}</legend>
              <label class="check-field"
                ><input v-model="sessionMode" type="radio" value="NEW" />{{
                  $t("runs.newSession")
                }}</label
              ><label
                class="check-field"
                :class="{ disabled: !resumableRuns.length }"
                ><input
                  v-model="sessionMode"
                  type="radio"
                  value="CONTINUE"
                  :disabled="!resumableRuns.length"
                />{{ $t("runs.continueSession") }}</label
              ><label v-if="sessionMode === 'CONTINUE'" class="field"
                ><span>{{ $t("runs.previousWork") }}</span
                ><select v-model="form.sessionRef" required>
                  <option value="" disabled>
                    {{ $t("runs.chooseSession") }}
                  </option>
                  <option
                    v-for="run in resumableRuns"
                    :key="run.sessionRef"
                    :value="run.sessionRef"
                  >
                    {{ formatRun(run) }}
                  </option>
                </select></label
              >
            </fieldset>
            <section class="panel policy-summary">
              <h2>{{ $t("runs.notifications") }}</h2>
              <strong>{{ $t("runs.controlCenterOnly") }}</strong>
              <p>{{ $t("runs.optionalChannelsHint") }}</p>
            </section>
          </section>

          <ProblemNotice v-if="problem" :problem="problem" compact />
        </div>

        <aside class="panel launch-summary">
          <h2>{{ $t("runs.launchSummary") }}</h2>
          <dl>
            <div>
              <dt>{{ $t("common.target") }}</dt>
              <dd>{{ selectedTarget?.name ?? $t("common.noData") }}</dd>
            </div>
            <div v-if="selectedWorkflow">
              <dt>{{ $t("workflows.coordinator") }}</dt>
              <dd>
                {{
                  platform.agents[selectedWorkflow.coordinatorAgentRef ?? ""]
                    ?.name ?? $t("common.noData")
                }}
              </dd>
            </div>
            <div>
              <dt>{{ $t("runs.inputFiles") }}</dt>
              <dd>{{ selectedArtifacts.length }}</dd>
            </div>
            <div>
              <dt>{{ $t("runs.sessionPolicy") }}</dt>
              <dd>
                {{
                  sessionMode === "NEW"
                    ? $t("runs.newSession")
                    : $t("runs.continueSession")
                }}
              </dd>
            </div>
          </dl>
          <button
            class="button button--primary button--large"
            type="submit"
            :disabled="busy || !selectedTarget"
          >
            {{ busy ? $t("common.loading") : $t("common.launch") }}
          </button>
          <RouterLink class="button" :to="`/projects/${projectRef}`">{{
            $t("common.cancel")
          }}</RouterLink>
        </aside>
      </form>
      <section v-else class="empty-state" role="status">
        <h2>{{ $t("common.forbidden") }}</h2>
        <p>{{ $t("common.forbiddenText") }}</p>
      </section>
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.new-run-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 340px;
  gap: 18px;
  align-items: start;
}
.new-run-main,
.file-options,
.launch-summary,
.policy-summary {
  display: grid;
  gap: 14px;
}
.section-heading h2,
.policy-summary h2,
.launch-summary h2 {
  margin: 0;
  font-size: 15px;
}
.section-heading p,
.policy-summary p,
.secondary-copy,
.field small,
.file-option small {
  margin: 3px 0 0;
  color: var(--muted);
}
.file-options {
  margin: 12px 0;
}
.file-option {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 9px;
}
.file-option input,
.run-checkbox input {
  width: 20px;
  min-height: 20px;
}
.file-option span,
.file-option small {
  display: block;
}
.run-policies {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
.choice-field {
  display: grid;
  gap: 10px;
  margin: 0;
}
.choice-field legend {
  padding: 0 6px;
  font-weight: 600;
}
.disabled {
  opacity: 0.55;
}
.launch-summary {
  position: sticky;
  top: 18px;
}
.launch-summary dl,
.launch-summary dl div {
  display: grid;
  gap: 8px;
  margin: 0;
}
.launch-summary dl div {
  grid-template-columns: 110px 1fr;
  padding: 9px 0;
  border-top: 1px solid var(--border);
}
.launch-summary dt {
  color: var(--muted);
}
.launch-summary dd {
  margin: 0;
}
@media (max-width: 950px) {
  .new-run-layout,
  .run-policies {
    grid-template-columns: 1fr;
  }
  .launch-summary {
    position: static;
  }
}
</style>
