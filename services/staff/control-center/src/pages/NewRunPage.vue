<script setup lang="ts">
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import PromptTargetPreview from "@/features/agents/detail/PromptTargetPreview.vue";
import type { PromptTarget } from "@/features/agents/detail/prompt-context";
import {
  Bot,
  Check,
  ChevronDown,
  Files,
  FolderKanban,
  History,
  Play,
  Workflow as WorkflowIcon,
} from "@lucide/vue";
import { computed, nextTick, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import {
  createArtifactPickerLoader,
  createSessionPickerLoader,
} from "@/features/new-run/api";
import { loadAttachmentEligibility } from "@/features/new-run/attachment-eligibility";
import NewRunFilePicker, {
  type NewRunFilePickerLabels,
} from "@/features/new-run/components/NewRunFilePicker.vue";
import NewRunSessionPicker, {
  type NewRunSessionPickerLabels,
} from "@/features/new-run/components/NewRunSessionPicker.vue";
import NewRunSessionPolicy from "@/features/new-run/components/NewRunSessionPolicy.vue";
import {
  formatTimestamp,
  type NewRunTargetType,
} from "@/features/new-run/model";
import { usePlatformStore } from "@/features/platform/store";
import { useRealtimeStore } from "@/features/realtime/store";
import {
  createExecutionTargetPickerLoader,
  isEligibleAgent,
  isEligibleWorkflow,
  selectedExecutionTargetOption,
  type ExecutionTargetPickerOption,
} from "@/shared/api/execution-target-picker";
import type {
  Agent,
  Artifact,
  AttachmentSetPurpose,
  Run,
  RunAttachmentEligibility,
  Workflow,
  WorkflowInputField,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { runPath } from "@/shared/routes";
import AsyncState from "@/shared/ui/AsyncState.vue";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import AttachmentComposer from "@/shared/ui/AttachmentComposer.vue";
import type {
  AttachmentComposerHandle,
  AttachmentComposerState,
  ExistingAttachmentSelection,
} from "@/shared/ui/attachment-composer";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const platform = usePlatformStore();
const realtime = useRealtimeStore();
const route = useRoute();
const router = useRouter();
const { locale, t } = useI18n();

const projectRef = computed(() => String(route.params.projectRef));
const project = computed(() => platform.projects[projectRef.value]);
const canLaunch = computed(() =>
  project.value?.nextActions.includes("CREATE_RUN"),
);
const busy = ref(false);
const problem = ref<AppProblem>();
const sessionMode = ref<"NEW" | "CONTINUE">("NEW");
const selectedArtifactItems = ref<Artifact[]>([]);
const selectedSession = ref<Run>();
const attachmentComposer = ref<AttachmentComposerHandle>();
const attachmentState = ref<AttachmentComposerState>({
  count: 0,
  uploadedCount: 0,
  totalBytes: 0,
  busy: false,
  hasErrors: false,
  overLimit: false,
  ready: true,
});
const filePickerOpen = ref(false);
const sessionPickerOpen = ref(false);
const selectedTargetValue = ref<Agent | Workflow>();
const inputValues = reactive<Record<string, string | number>>({});
const booleanInputValues = reactive<Record<string, boolean>>({});
const form = reactive({
  targetType: "WORKFLOW" as NewRunTargetType,
  targetRef: "",
  title: "",
  task: "",
  sessionRef: "",
});
const continuationPreviewTarget = ref<PromptTarget>();
const continuationPreview = ref<{ refresh: () => Promise<void> }>();
const continuationPreviewBusy = ref(false);
const continuationPreviewRequested = ref(false);
const continuationPreviewChecked = ref(false);
const continuationPreviewStale = computed(
  () =>
    sessionMode.value === "CONTINUE" &&
    continuationPreviewRequested.value &&
    !continuationPreviewChecked.value,
);
function isContinuationMode(): boolean {
  return sessionMode.value === "CONTINUE";
}
const continuationPreviewIdentity = computed(() =>
  JSON.stringify({
    project: projectRef.value,
    mode: sessionMode.value,
    run: selectedSession.value?.ref,
    session: selectedSession.value?.sessionRef,
    task: form.task,
    target: [
      form.targetType,
      form.targetRef,
      selectedTargetValue.value?.version,
    ],
    attachments: attachmentState.value,
    selected: selectedArtifactItems.value.map((item) => item.ref),
  }),
);
watch(
  continuationPreviewIdentity,
  () => {
    continuationPreviewTarget.value = undefined;
    continuationPreviewChecked.value = false;
  },
  { flush: "sync" },
);
async function previewContinuation(): Promise<void> {
  const selected = selectedSession.value;
  if (
    !selected ||
    sessionMode.value !== "CONTINUE" ||
    !canSubmit.value ||
    busy.value ||
    continuationPreviewBusy.value
  )
    return;
  continuationPreviewBusy.value = true;
  continuationPreviewRequested.value = true;
  problem.value = undefined;
  try {
    const task = form.task.trim();
    const attachmentSetRef = await attachmentComposer.value?.finalize();
    if (
      selectedSession.value !== selected ||
      !isContinuationMode() ||
      form.task.trim() !== task
    )
      return;
    continuationPreviewTarget.value = {
      projectRef: projectRef.value,
      targetKind: "SESSION_CONTINUATION",
      targetRef: selected.sessionRef,
      context: { task, ...(attachmentSetRef ? { attachmentSetRef } : {}) },
    };
    await nextTick();
    await continuationPreview.value?.refresh();
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    continuationPreviewBusy.value = false;
  }
}

const targetLoader = computed(() =>
  createExecutionTargetPickerLoader(projectRef.value, form.targetType),
);
const selectedTarget = computed(() => selectedTargetValue.value);
const selectedTargetOption = computed(() =>
  selectedExecutionTargetOption(form.targetType, selectedTarget.value),
);
const selectedWorkflow = computed(() =>
  form.targetType === "WORKFLOW"
    ? (selectedTarget.value as Workflow | undefined)
    : undefined,
);
const attachmentSelections = computed<ExistingAttachmentSelection[]>(() =>
  selectedArtifactItems.value.map((artifact) => ({
    ref: artifact.ref,
    name: artifact.fileName,
    mediaType: artifact.mediaType,
    size: artifact.sizeBytes,
  })),
);
const attachmentPurpose = computed<AttachmentSetPurpose>(() => {
  if (sessionMode.value === "CONTINUE") return "SESSION_TURN";
  return form.targetType === "WORKFLOW" ? "WORKFLOW_INPUT" : "RUN_INPUT";
});
const attachmentEligibility = ref<RunAttachmentEligibility>();
const attachmentEligibilityBusy = ref(false);
const attachmentEligibilityProblem = ref<AppProblem>();
const attachmentEligibilityReload = ref(0);
const targetSupportsFiles = computed(
  () => attachmentEligibility.value?.eligible === true,
);
watch(
  () =>
    [
      projectRef.value,
      form.targetType,
      form.targetRef,
      selectedTarget.value?.version,
      sessionMode.value,
      selectedSession.value?.ref,
      attachmentEligibilityReload.value,
    ] as const,
  async ([project, targetType, targetRef, , mode, runRef], _, onCleanup) => {
    const controller = new AbortController();
    onCleanup(() => controller.abort());
    attachmentEligibility.value = undefined;
    attachmentEligibilityProblem.value = undefined;
    attachmentEligibilityBusy.value = false;
    if (
      !targetRef ||
      selectedTarget.value?.ref !== targetRef ||
      selectedTarget.value.projectRef !== project ||
      (mode === "CONTINUE" && !runRef)
    )
      return;
    attachmentEligibilityBusy.value = true;
    try {
      const result = await loadAttachmentEligibility(
        {
          projectRef: project,
          targetType,
          targetRef,
          ...(mode === "CONTINUE" ? { runRef } : {}),
        },
        controller.signal,
      );
      if (!controller.signal.aborted) attachmentEligibility.value = result;
    } catch (error) {
      if (!controller.signal.aborted)
        attachmentEligibilityProblem.value = asProblem(error);
    } finally {
      if (!controller.signal.aborted) attachmentEligibilityBusy.value = false;
    }
  },
  { immediate: true, flush: "sync" },
);
const workflowInputValid = computed(() => {
  if (sessionMode.value !== "NEW") return true;
  for (const field of selectedWorkflow.value?.inputFields ?? []) {
    if (!field.required || field.valueType === "BOOLEAN") continue;
    const value = inputValues[field.key];
    if (value === undefined || String(value).trim() === "") return false;
  }
  return true;
});
const canSubmit = computed(
  () =>
    Boolean(canLaunch.value) &&
    realtime.platformState.state === "live" &&
    Boolean(selectedTarget.value) &&
    (sessionMode.value === "CONTINUE" || Boolean(form.title.trim())) &&
    Boolean(form.task.trim()) &&
    workflowInputValid.value &&
    attachmentState.value.ready &&
    (attachmentState.value.count === 0 || targetSupportsFiles.value) &&
    (sessionMode.value === "NEW" || Boolean(form.sessionRef)),
);

const artifactLoader = computed(() =>
  createArtifactPickerLoader(projectRef.value),
);
const sessionLoader = computed(() =>
  createSessionPickerLoader({
    projectRef: projectRef.value,
    targetRef: form.targetRef,
    targetType: form.targetType,
  }),
);

function text(key: string, values?: Record<string, unknown>): string {
  return values ? t(key, values) : t(key);
}

const sessionPolicyLabels = computed(() => ({
  legend: text("runs.sessionPolicy"),
  newTitle: text("runs.newSession"),
  newDescription: text("runs.newRun.session.newDescription"),
  continueTitle: text("runs.continueSession"),
  continueDescription: text("runs.newRun.session.continueDescription"),
}));
const filePickerLabels = computed<NewRunFilePickerLabels>(() => ({
  title: text("runs.newRun.files.pickerTitle"),
  subtitle: text("runs.newRun.files.pickerSubtitle", {
    project: project.value?.name ?? "",
  }),
  close: text("common.close"),
  cancel: text("common.cancel"),
  confirm: text("runs.newRun.files.confirm"),
  selected: text("common.selected"),
  selectedCount: (count) => text("runs.newRun.files.selectedCount", { count }),
  remove: (name) => text("runs.newRun.files.remove", { name }),
  viewMode: text("runs.newRun.files.viewMode"),
  listView: text("runs.newRun.files.listView"),
  gridView: text("runs.newRun.files.gridView"),
  unavailable: text("runs.newRun.files.unavailable"),
  revision: (revision) => text("common.version", { version: revision }),
  scanStates: {
    PENDING: text("states.PENDING"),
    SCANNING: text("states.SCANNING"),
    CLEAN: text("states.CLEAN"),
    QUARANTINED: text("states.QUARANTINED"),
    FAILED: text("states.FAILED"),
  },
  picker: {
    label: text("runs.newRun.files.pickerLabel"),
    searchPlaceholder: text("runs.newRun.files.searchPlaceholder"),
    loading: text("runs.newRun.files.loading"),
    loadingMore: text("runs.newRun.files.loadingMore"),
    empty: text("runs.newRun.files.empty"),
    error: text("runs.newRun.files.error"),
    retry: text("common.retry"),
  },
}));
const sessionPickerLabels = computed<NewRunSessionPickerLabels>(() => ({
  title: text("runs.newRun.session.pickerTitle"),
  subtitle: text("runs.newRun.session.pickerSubtitle"),
  close: text("common.close"),
  states: {
    QUEUED: text("states.QUEUED"),
    RUNNING: text("states.RUNNING"),
    WAITING_HUMAN: text("states.WAITING_HUMAN"),
    CANCELLING: text("states.CANCELLING"),
    SUCCEEDED: text("states.SUCCEEDED"),
    FAILED: text("states.FAILED"),
    CANCELLED: text("states.CANCELLED"),
  },
  picker: {
    label: text("runs.newRun.session.pickerLabel"),
    searchPlaceholder: text("runs.newRun.session.searchPlaceholder"),
    loading: text("runs.newRun.session.loading"),
    loadingMore: text("runs.newRun.session.loadingMore"),
    empty: text("runs.newRun.session.empty"),
    error: text("runs.newRun.session.error"),
    retry: text("common.retry"),
  },
}));

function resetTargetContext(): void {
  for (const key of Object.keys(inputValues))
    Reflect.deleteProperty(inputValues, key);
  for (const key of Object.keys(booleanInputValues))
    Reflect.deleteProperty(booleanInputValues, key);
  for (const field of selectedWorkflow.value?.inputFields ?? []) {
    if (field.valueType === "BOOLEAN") booleanInputValues[field.key] = false;
  }
  selectedArtifactItems.value = [];
  attachmentComposer.value?.clear();
  selectedSession.value = undefined;
  sessionMode.value = "NEW";
  form.sessionRef = "";
  filePickerOpen.value = false;
  sessionPickerOpen.value = false;
}

function resetProjectForm(): void {
  form.targetType = "WORKFLOW";
  form.targetRef = "";
  selectedTargetValue.value = undefined;
  form.title = "";
  form.task = "";
  resetTargetContext();
}

function selectTargetType(targetType: NewRunTargetType): void {
  if (form.targetType === targetType) return;
  form.targetType = targetType;
  form.targetRef = "";
  selectedTargetValue.value = undefined;
}

function selectTarget(option: ExecutionTargetPickerOption): void {
  if (
    option.targetType !== form.targetType ||
    option.target.projectRef !== projectRef.value
  )
    return;
  selectedTargetValue.value = option.target;
  form.targetRef = option.ref;
}

function setSessionMode(mode: "NEW" | "CONTINUE"): void {
  sessionMode.value = mode;
  if (mode === "NEW") {
    selectedSession.value = undefined;
    form.sessionRef = "";
    sessionPickerOpen.value = false;
    return;
  }
  sessionPickerOpen.value = true;
}

function selectSession(run: Run): void {
  selectedSession.value = run;
  form.sessionRef = run.sessionRef;
  sessionPickerOpen.value = false;
}

function clearSession(): void {
  selectedSession.value = undefined;
  form.sessionRef = "";
}

function confirmArtifacts(artifacts: Artifact[]): void {
  selectedArtifactItems.value = artifacts;
  filePickerOpen.value = false;
}

function inputComponentType(field: WorkflowInputField): string {
  if (field.valueType === "NUMBER") return "number";
  if (field.valueType === "DATE") return "date";
  return "text";
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
  if (
    !canSubmit.value ||
    continuationPreviewStale.value ||
    busy.value ||
    !selectedTarget.value
  )
    return;
  busy.value = true;
  problem.value = undefined;
  try {
    const attachmentSetRef = await attachmentComposer.value?.finalize();
    const run =
      sessionMode.value === "CONTINUE" && selectedSession.value
        ? await platform.continueSession(selectedSession.value.sessionRef, {
            runRef: selectedSession.value.ref,
            task: form.task.trim(),
            ...(attachmentSetRef ? { attachmentSetRef } : {}),
          })
        : await platform.launch({
            projectRef: projectRef.value,
            targetRef: form.targetRef,
            targetType: form.targetType,
            title: form.title.trim(),
            task: form.task.trim(),
            ...(selectedWorkflow.value ? { input: workflowInput() } : {}),
            ...(attachmentSetRef ? { attachmentSetRef } : {}),
          });
    await router.push(runPath(run.ref, projectRef.value));
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

let loadGeneration = 0;
async function load(): Promise<void> {
  const generation = ++loadGeneration;
  resetProjectForm();
  await platform.loadProject(projectRef.value);
  if (generation !== loadGeneration) return;

  const requestedType = route.query.targetType;
  const requestedRef = route.query.targetRef;
  if (requestedType === "AGENT" || requestedType === "WORKFLOW") {
    form.targetType = requestedType;
  }
  if (typeof requestedRef === "string") {
    if (form.targetType === "AGENT") {
      await platform.loadAgent(requestedRef);
      const agent = platform.agents[requestedRef];
      if (agent?.projectRef === projectRef.value && isEligibleAgent(agent)) {
        selectedTargetValue.value = agent;
        form.targetRef = agent.ref;
      }
    } else {
      await platform.loadWorkflow(requestedRef);
      const workflow = platform.workflows[requestedRef];
      if (
        workflow?.projectRef === projectRef.value &&
        isEligibleWorkflow(workflow)
      ) {
        selectedTargetValue.value = workflow;
        form.targetRef = workflow.ref;
      }
    }
  }
  if (generation !== loadGeneration) return;
  resetTargetContext();
}

watch(
  () => [form.targetType, form.targetRef],
  () => resetTargetContext(),
);
watch(
  projectRef,
  () => {
    void load();
  },
  { immediate: true },
);
watch(
  () => sessionPickerOpen.value,
  async (open) => {
    if (open) return;
    await nextTick();
    document
      .querySelector<HTMLElement>("#new-run-session-picker-trigger")
      ?.focus();
  },
);
</script>

<template>
  <PageFrame :title="$t('runs.new')" :subtitle="$t('runs.newRun.subtitle')">
    <AsyncState
      :loading="platform.loading.project && !project"
      :problem="project ? undefined : platform.problems.project"
      @retry="load"
    >
      <form v-if="canLaunch" class="new-run-layout" @submit.prevent="submit">
        <div class="new-run-main">
          <div
            class="project-context"
            role="group"
            :aria-label="$t('runs.newRun.projectContextLabel')"
          >
            <FolderKanban :size="18" aria-hidden="true" />
            <span class="project-context__name">
              {{ $t("app.project") }}:
              <strong>{{ project?.name }}</strong>
            </span>
            <span class="project-context__hint">
              {{ $t("runs.newRun.projectContextPreserved") }}
            </span>
            <RouterLink class="project-context__change" to="/projects">
              {{ $t("runs.newRun.changeProject") }}
            </RouterLink>
          </div>

          <section
            class="panel new-run-section"
            aria-labelledby="new-run-target-title"
          >
            <header class="section-heading section-heading--split">
              <div>
                <h2 id="new-run-target-title">{{ $t("runs.targetType") }}</h2>
                <p>{{ $t("runs.newRun.targetHint") }}</p>
              </div>
              <fieldset class="target-type-switch">
                <legend class="sr-only">
                  {{ $t("runs.newRun.targetTypeLabel") }}
                </legend>
                <label>
                  <input
                    type="radio"
                    name="new-run-target-type"
                    value="AGENT"
                    :checked="form.targetType === 'AGENT'"
                    @change="selectTargetType('AGENT')"
                  />
                  <Bot :size="16" aria-hidden="true" />
                  <span>{{ $t("runs.agent") }}</span>
                </label>
                <label>
                  <input
                    type="radio"
                    name="new-run-target-type"
                    value="WORKFLOW"
                    :checked="form.targetType === 'WORKFLOW'"
                    @change="selectTargetType('WORKFLOW')"
                  />
                  <WorkflowIcon :size="16" aria-hidden="true" />
                  <span>{{ $t("runs.workflow") }}</span>
                </label>
              </fieldset>
            </header>

            <div class="form-grid">
              <div class="field field--wide">
                <span>{{ $t("common.target") }}</span>
                <AsyncEntityPicker
                  :key="`${projectRef}:${form.targetType}`"
                  v-model="form.targetRef"
                  :load-page="targetLoader"
                  :selected="selectedTargetOption"
                  :trigger-label="$t('common.target')"
                  :placeholder="$t('runs.chooseTarget')"
                  :search-placeholder="$t('runs.chooseTarget')"
                  @select="selectTarget"
                />
                <small v-if="selectedWorkflow">
                  {{
                    $t("common.version", { version: selectedWorkflow.version })
                  }}
                  · {{ $t("workflows.coordinator") }}:
                  {{
                    platform.agents[selectedWorkflow.coordinatorAgentRef ?? ""]
                      ?.name ?? $t("common.noData")
                  }}
                </small>
                <small
                  v-else-if="selectedTarget && form.targetType === 'AGENT'"
                >
                  {{ selectedTarget.purpose }}
                </small>
              </div>

              <label v-if="sessionMode === 'NEW'" class="field">
                <span>
                  {{ $t("runs.runTitle") }}
                  <span class="required-mark" aria-hidden="true">*</span>
                </span>
                <input
                  v-model="form.title"
                  required
                  maxlength="240"
                  :placeholder="$t('runs.newRun.titlePlaceholder')"
                />
                <small>{{ $t("runs.newRun.titleRequiredHint") }}</small>
              </label>

              <label class="field">
                <span>
                  {{ $t("runs.task") }}
                  <span class="required-mark" aria-hidden="true">*</span>
                </span>
                <VoiceTextarea
                  v-model="form.task"
                  required
                  maxlength="32768"
                  rows="4"
                  :placeholder="$t('runs.newRun.taskPlaceholder')"
                />
                <small>{{ $t("runs.newRun.taskHint") }}</small>
              </label>
            </div>
          </section>

          <section
            v-if="sessionMode === 'NEW' && selectedWorkflow?.inputFields.length"
            class="panel new-run-section form-grid"
            aria-labelledby="new-run-workflow-input-title"
          >
            <header class="section-heading field--wide">
              <h2 id="new-run-workflow-input-title">
                {{ $t("runs.workflowInput") }}
              </h2>
              <p>{{ $t("runs.workflowInputHint") }}</p>
            </header>
            <template
              v-for="field in selectedWorkflow.inputFields"
              :key="field.key"
            >
              <label
                v-if="field.valueType === 'LONG_TEXT'"
                class="field field--wide"
              >
                <span>{{ field.label }}</span>
                <VoiceTextarea
                  v-model="inputValues[field.key]"
                  :required="field.required"
                  maxlength="32768"
                  :aria-describedby="
                    field.description ? `hint-${field.key}` : undefined
                  "
                />
                <small v-if="field.description" :id="`hint-${field.key}`">
                  {{ field.description }}
                </small>
              </label>
              <label v-else-if="field.valueType === 'SELECT'" class="field">
                <span>{{ field.label }}</span>
                <select
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
                  </option>
                </select>
                <small v-if="field.description">{{ field.description }}</small>
              </label>
              <label
                v-else-if="field.valueType === 'BOOLEAN'"
                class="workflow-checkbox"
              >
                <input
                  v-model="booleanInputValues[field.key]"
                  type="checkbox"
                />
                <span>
                  <strong>{{ field.label }}</strong>
                  <small v-if="field.description">{{
                    field.description
                  }}</small>
                </span>
              </label>
              <label v-else class="field">
                <span>{{ field.label }}</span>
                <input
                  v-model="inputValues[field.key]"
                  :type="inputComponentType(field)"
                  :required="field.required"
                  :maxlength="field.valueType === 'TEXT' ? 4000 : undefined"
                />
                <small v-if="field.description">{{ field.description }}</small>
              </label>
            </template>
          </section>

          <div class="new-run-two-column">
            <section
              class="panel new-run-section"
              aria-labelledby="new-run-session-title"
            >
              <header class="section-heading">
                <h2 id="new-run-session-title">
                  {{ $t("runs.sessionPolicy") }}
                </h2>
              </header>
              <NewRunSessionPolicy
                :model-value="sessionMode"
                :continue-disabled="!selectedTarget"
                :labels="sessionPolicyLabels"
                @update:model-value="setSessionMode"
              />
              <div v-if="sessionMode === 'CONTINUE'" class="session-selection">
                <span class="session-selection__label">{{
                  $t("runs.previousWork")
                }}</span>
                <button
                  id="new-run-session-picker-trigger"
                  class="entity-picker-trigger"
                  type="button"
                  aria-haspopup="dialog"
                  :aria-expanded="sessionPickerOpen"
                  @click="sessionPickerOpen = true"
                >
                  <History :size="18" aria-hidden="true" />
                  <span>
                    <strong>
                      {{ selectedSession?.title ?? $t("runs.chooseSession") }}
                    </strong>
                    <small v-if="selectedSession">
                      {{ selectedSession.target.displayName }} ·
                      {{ formatTimestamp(selectedSession.createdAt, locale) }}
                    </small>
                    <small v-else>{{
                      $t("runs.newRun.session.searchHint")
                    }}</small>
                  </span>
                  <ChevronDown :size="17" aria-hidden="true" />
                </button>
              </div>
            </section>

            <section
              class="panel new-run-section"
              aria-labelledby="new-run-notifications-title"
            >
              <header class="section-heading">
                <h2 id="new-run-notifications-title">
                  {{ $t("runs.notifications") }}
                </h2>
              </header>
              <label class="notification-choice">
                <input
                  type="radio"
                  name="new-run-notification"
                  value="CONTROL_CENTER"
                  checked
                />
                <span class="notification-choice__mark" aria-hidden="true">
                  <Check :size="14" />
                </span>
                <span>
                  <strong>{{ $t("runs.controlCenterOnly") }}</strong>
                  <small>{{ $t("runs.optionalChannelsHint") }}</small>
                </span>
              </label>
            </section>
          </div>

          <section
            class="panel new-run-section"
            aria-labelledby="new-run-files-title"
          >
            <header class="section-heading section-heading--split">
              <div>
                <h2 id="new-run-files-title">
                  {{ $t("runs.inputFiles") }}
                  <span class="count-badge">{{ attachmentState.count }}</span>
                </h2>
                <p>
                  {{
                    attachmentEligibilityBusy
                      ? $t("common.loading")
                      : attachmentEligibility && !targetSupportsFiles
                        ? $t(
                            "runs.attachmentEligibility." +
                              attachmentEligibility.reason,
                          )
                        : $t("runs.inputFilesHint")
                  }}
                </p>
              </div>
              <button
                class="button"
                type="button"
                :disabled="!targetSupportsFiles"
                @click="filePickerOpen = true"
              >
                <Files :size="17" aria-hidden="true" />
                {{ $t("runs.newRun.files.choose") }}
              </button>
            </header>
            <ProblemNotice
              v-if="attachmentEligibilityProblem"
              :problem="attachmentEligibilityProblem"
              @retry="attachmentEligibilityReload++"
            />

            <AttachmentComposer
              ref="attachmentComposer"
              :purpose="attachmentPurpose"
              :project-ref="projectRef"
              :external-selections="attachmentSelections"
              :disabled="busy || !targetSupportsFiles"
              @change="attachmentState = $event"
            />

            <div
              v-if="attachmentState.count === 0"
              class="selected-files-empty"
            >
              <Files :size="22" aria-hidden="true" />
              <span>{{ $t("runs.noInputFiles") }}</span>
            </div>
            <RouterLink
              class="files-manage-link"
              :to="`/projects/${projectRef}/files`"
            >
              {{ $t("runs.manageFiles") }}
            </RouterLink>
          </section>

          <section v-if="sessionMode === 'CONTINUE'" class="panel stack">
            <button
              v-if="!continuationPreviewTarget"
              type="button"
              class="button"
              :disabled="!canSubmit || busy || continuationPreviewBusy"
              @click="previewContinuation"
            >
              {{ $t("promptContext.continuation") }}
            </button>
            <PromptTargetPreview
              v-if="continuationPreviewTarget"
              ref="continuationPreview"
              :target="continuationPreviewTarget"
              :disabled="busy"
              @checked="continuationPreviewChecked = $event"
            />
          </section>
          <ProblemNotice v-if="problem" :problem="problem" compact />
        </div>

        <aside
          class="panel launch-summary"
          aria-labelledby="new-run-summary-title"
        >
          <header class="section-heading">
            <h2 id="new-run-summary-title">{{ $t("runs.launchSummary") }}</h2>
          </header>
          <dl>
            <div>
              <dt>{{ $t("app.project") }}</dt>
              <dd>{{ project?.name ?? $t("common.noData") }}</dd>
            </div>
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
            <div v-if="sessionMode === 'NEW'">
              <dt>{{ $t("runs.runTitle") }}</dt>
              <dd>{{ form.title || $t("common.noData") }}</dd>
            </div>
            <div>
              <dt>{{ $t("runs.inputFiles") }}</dt>
              <dd>{{ attachmentState.count }}</dd>
            </div>
            <div>
              <dt>{{ $t("runs.sessionPolicy") }}</dt>
              <dd>
                <template v-if="sessionMode === 'NEW'">
                  {{ $t("runs.newSession") }}
                </template>
                <template v-else>
                  {{ selectedSession?.title ?? $t("runs.chooseSession") }}
                </template>
              </dd>
            </div>
            <div>
              <dt>{{ $t("runs.notifications") }}</dt>
              <dd>{{ $t("runs.controlCenterOnly") }}</dd>
            </div>
          </dl>
          <p class="launch-summary__authority">
            {{ $t("runs.newRun.authorityHint") }}
          </p>
          <div class="launch-summary__actions">
            <button
              class="button button--primary button--large"
              type="submit"
              :disabled="busy || !canSubmit || continuationPreviewStale"
            >
              <Play :size="17" aria-hidden="true" />
              {{ busy ? $t("common.loading") : $t("common.launch") }}
            </button>
            <RouterLink
              class="button button--large"
              :to="`/projects/${projectRef}`"
            >
              {{ $t("common.cancel") }}
            </RouterLink>
          </div>
        </aside>
      </form>

      <section v-else class="empty-state" role="status">
        <h2>{{ $t("common.forbidden") }}</h2>
        <p>{{ $t("common.forbiddenText") }}</p>
      </section>
    </AsyncState>

    <NewRunFilePicker
      v-if="filePickerOpen"
      :open="filePickerOpen"
      :selected-artifacts="selectedArtifactItems"
      :load-items="artifactLoader"
      :labels="filePickerLabels"
      :locale="locale"
      :disabled="!targetSupportsFiles"
      @close="filePickerOpen = false"
      @confirm="confirmArtifacts"
    />
    <NewRunSessionPicker
      v-if="sessionPickerOpen"
      :open="sessionPickerOpen"
      :selected-session-ref="form.sessionRef"
      :load-items="sessionLoader"
      :labels="sessionPickerLabels"
      :locale="locale"
      @close="sessionPickerOpen = false"
      @select="selectSession"
      @clear="clearSession"
    />
  </PageFrame>
</template>

<style scoped>
.new-run-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 380px;
  gap: 16px;
  align-items: start;
}
.new-run-main {
  display: grid;
  min-width: 0;
  gap: 14px;
}
.project-context {
  display: flex;
  min-height: 42px;
  align-items: center;
  gap: 9px;
  padding: 7px 12px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--panel);
  font-size: 13px;
}
.project-context > svg {
  flex: 0 0 auto;
  color: var(--text-secondary);
}
.project-context__name {
  min-width: 0;
}
.project-context__hint {
  color: var(--text-secondary);
}
.project-context__change {
  margin-left: auto;
  color: var(--accent);
  font-weight: 600;
}
.new-run-section {
  display: grid;
  gap: 14px;
}
.section-heading {
  min-width: 0;
}
.section-heading--split {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.section-heading h2 {
  display: flex;
  align-items: center;
  gap: 7px;
  margin: 0;
  font-size: 15px;
}
.section-heading p {
  margin: 4px 0 0;
  color: var(--text-secondary);
  font-size: 12px;
}
.target-type-switch {
  display: inline-grid;
  grid-template-columns: repeat(2, minmax(130px, 1fr));
  flex: 0 0 auto;
  margin: 0;
  padding: 0;
  overflow: hidden;
  border: 1px solid var(--border-strong);
  border-radius: 7px;
}
.target-type-switch label {
  position: relative;
  display: inline-flex;
  min-height: 36px;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 5px 10px;
  border-right: 1px solid var(--border);
  color: var(--text-secondary);
  cursor: pointer;
}
.target-type-switch label:last-child {
  border-right: 0;
}
.target-type-switch label:has(input:checked) {
  color: var(--accent);
  background: var(--accent-soft);
}
.target-type-switch label:focus-within {
  outline: 3px solid rgba(27, 111, 196, 0.45);
  outline-offset: -3px;
}
.target-type-switch input {
  position: absolute;
  width: 1px;
  height: 1px;
  min-height: 1px;
  opacity: 0;
}
.required-mark {
  color: var(--danger);
}
.new-run-two-column {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
.session-selection {
  display: grid;
  gap: 6px;
}
.session-selection__label {
  font-size: 13px;
  font-weight: 600;
}
.entity-picker-trigger {
  display: flex;
  width: 100%;
  min-height: 56px;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid var(--border-strong);
  border-radius: 7px;
  background: var(--surface);
  color: var(--text);
  text-align: left;
  cursor: pointer;
}
.entity-picker-trigger > svg {
  flex: 0 0 auto;
  color: var(--text-secondary);
}
.entity-picker-trigger > span {
  display: grid;
  min-width: 0;
  flex: 1;
  gap: 3px;
}
.entity-picker-trigger strong,
.entity-picker-trigger small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.entity-picker-trigger small {
  color: var(--text-secondary);
  font-weight: 400;
}
.notification-choice {
  position: relative;
  display: flex;
  min-height: 64px;
  align-items: center;
  gap: 11px;
  padding: 10px 12px;
  border: 1px solid var(--accent);
  border-radius: 7px;
  background: var(--accent-soft);
  cursor: pointer;
}
.notification-choice:focus-within {
  outline: 3px solid rgba(27, 111, 196, 0.45);
  outline-offset: 2px;
}
.notification-choice input {
  position: absolute;
  width: 1px;
  height: 1px;
  min-height: 1px;
  opacity: 0;
}
.notification-choice__mark {
  display: grid;
  width: 20px;
  height: 20px;
  flex: 0 0 auto;
  place-items: center;
  border-radius: 50%;
  color: white;
  background: var(--accent);
}
.notification-choice > span:last-child {
  display: grid;
  gap: 3px;
}
.notification-choice small {
  color: var(--text-secondary);
  font-weight: 400;
  line-height: 1.4;
}
.workflow-checkbox {
  display: flex;
  min-height: 48px;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 7px;
}
.workflow-checkbox input {
  width: 20px;
  min-height: 20px;
  flex: 0 0 auto;
}
.workflow-checkbox > span {
  display: grid;
  gap: 2px;
}
.workflow-checkbox small {
  color: var(--text-secondary);
  font-weight: 400;
}
.selected-files {
  display: grid;
}
.selected-file {
  display: flex;
  min-height: 58px;
  align-items: center;
  gap: 11px;
  padding: 8px 0;
  border-top: 1px solid var(--hairline);
}
.selected-file:first-child {
  border-top: 0;
}
.selected-file__copy {
  display: grid;
  min-width: 0;
  flex: 1;
  gap: 3px;
}
.selected-file__copy strong,
.selected-file__copy small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.selected-file__copy small {
  color: var(--text-secondary);
}
.selected-file__ready {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--success);
  font-size: 12px;
  font-weight: 600;
}
.icon-action {
  display: grid;
  width: 34px;
  height: 34px;
  flex: 0 0 auto;
  place-items: center;
  padding: 0;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}
.icon-action:hover {
  border-color: var(--border);
  background: var(--panel);
  color: var(--text);
}
.selected-files-empty {
  display: flex;
  min-height: 76px;
  align-items: center;
  justify-content: center;
  gap: 9px;
  border: 1px dashed var(--border-strong);
  border-radius: 7px;
  color: var(--text-secondary);
}
.files-manage-link {
  width: fit-content;
  color: var(--accent);
  font-size: 13px;
  font-weight: 600;
}
.launch-summary {
  position: sticky;
  top: 18px;
  display: grid;
  gap: 12px;
}
.launch-summary dl,
.launch-summary dl div {
  display: grid;
  margin: 0;
}
.launch-summary dl div {
  grid-template-columns: 112px minmax(0, 1fr);
  gap: 10px;
  padding: 9px 0;
  border-top: 1px solid var(--hairline);
}
.launch-summary dt {
  color: var(--text-secondary);
}
.launch-summary dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}
.launch-summary__authority {
  margin: 0;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.5;
}
.launch-summary__actions {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.launch-summary__actions .button {
  width: 100%;
  min-width: 0;
  min-height: 46px;
  padding-inline: 10px;
}
@media (max-width: 1100px) {
  .new-run-layout {
    grid-template-columns: minmax(0, 1fr) 330px;
  }
  .new-run-two-column {
    grid-template-columns: 1fr;
  }
  .section-heading--split {
    align-items: flex-start;
  }
}
@media (max-width: 900px) {
  .new-run-layout {
    grid-template-columns: 1fr;
  }
  .launch-summary {
    position: static;
  }
  .project-context__hint {
    display: none;
  }
}
@media (max-width: 760px) {
  .new-run-main {
    gap: 12px;
  }
  .new-run-section {
    padding: 14px;
  }
  .project-context {
    min-height: 44px;
  }
  .project-context__name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .section-heading--split {
    align-items: stretch;
    flex-direction: column;
  }
  .section-heading--split > .button {
    min-height: 44px;
  }
  .target-type-switch {
    width: 100%;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .target-type-switch label {
    min-width: 0;
    min-height: 44px;
  }
  .selected-file {
    align-items: flex-start;
  }
  .selected-file__ready {
    display: none;
  }
  .icon-action {
    width: 44px;
    height: 44px;
  }
  .launch-summary {
    padding-bottom: 0;
  }
  .launch-summary__actions {
    position: sticky;
    z-index: 3;
    bottom: 0;
    margin: 0 -16px;
    padding: 10px 16px 14px;
    border-top: 1px solid var(--border);
    background: var(--surface);
  }
  .launch-summary__actions .button {
    min-height: 48px;
  }
}
@media (max-width: 420px) {
  .project-context {
    gap: 7px;
  }
  .project-context__name {
    font-size: 12px;
  }
  .target-type-switch label {
    padding-inline: 6px;
    font-size: 12px;
  }
  .launch-summary dl div {
    grid-template-columns: 96px minmax(0, 1fr);
  }
}
</style>
