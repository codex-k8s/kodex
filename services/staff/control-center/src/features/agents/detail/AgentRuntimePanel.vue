<script setup lang="ts">
import { Save, ShieldCheck } from "@lucide/vue";
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import CodeEditorSurface from "@/features/agents/detail/CodeEditorSurface.vue";
import OverlayHistoryPanel from "./OverlayHistoryPanel.vue";
import { restoreOverlayRevision } from "./overlay-history";
import { agentDetailCopy } from "@/features/agents/detail/copy";
import { ProviderAccountSelector } from "@/features/providers";
import ProviderModelSelector from "@/features/providers/ProviderModelSelector.vue";
import type { ModelSelection } from "@/features/providers/model-catalog";
import {
  pinnedRuntimeInput,
  runtimeCandidates,
  type RuntimeForm,
} from "./runtime-input";
import {
  changeOverlayEffort,
  overlayCompletions,
  overlayEffort,
  overlayHover,
} from "./overlay-editor";
import type {
  CodeEditorCompletionProvider,
  CodeEditorHoverProvider,
} from "./code-editor";
import {
  readyRuntimes,
  runtimeProviders,
  runtimeRefForSelection,
  runtimeSelectionByRef,
  type ApplyBoundary,
} from "@/features/agents/detail/model";
import {
  changeOverlay as changeRuntimeOverlay,
  loadAgentRuntime,
  loadRuntimeCatalog,
  saveAgentRuntime,
  saveOverlayDraft,
} from "@/features/agents/detail/runtime-api";
import type {
  AgentRuntimeConfigurationView,
  RuntimeSelection,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useUnsavedChanges } from "@/shared/ui/unsaved-changes";

const props = defineProps<{
  agentRef: string;
  canEdit: boolean;
}>();
const emit = defineEmits<{
  "apply-state": [
    state: "APPLIED" | "DRAFT" | "RUNNING" | "FAILED",
    scope: string,
    boundary: ApplyBoundary,
  ];
}>();

const { locale, t } = useI18n();
const copy = computed(() => agentDetailCopy(locale.value));
const view = ref<AgentRuntimeConfigurationView>();
const runtimes = ref<RuntimeSelection[]>([]);
const loading = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const overlayContent = ref("");
let generation = 0;
let readController: AbortController | undefined;
const modelAvailable = ref(false);
const modelSelection = ref<ModelSelection>();
const providerAccountEligibility = ref<"CONNECTING" | "READY" | "UNAVAILABLE">(
  "CONNECTING",
);
const form = reactive<RuntimeForm>({
  runtimeProfileRef: "",
  model: "",
  providerPolicyMode: "FIXED",
  providerAccounts: [],
});
const providerPolicyModes = ["FIXED", "LEAST_USED", "WEIGHTED"] as const;
type ProviderPolicyMode = (typeof providerPolicyModes)[number];

function isProviderPolicyMode(value: string): value is ProviderPolicyMode {
  return providerPolicyModes.some((mode) => mode === value);
}

const availableRuntimes = computed(() => readyRuntimes(runtimes.value));
const selectedRuntime = computed(() =>
  runtimeSelectionByRef(runtimes.value, form.runtimeProfileRef),
);
const providers = computed(() => runtimeProviders(availableRuntimes.value));
const selectedProvider = computed(
  () =>
    selectedRuntime.value?.provider ?? view.value?.configuration.provider ?? "",
);
const matchingRuntimes = computed(() =>
  availableRuntimes.value.filter(
    (runtime) => runtime.provider === selectedProvider.value,
  ),
);
const providerOptions = computed<AsyncEntityOption[]>(() =>
  providers.value.map((provider) => ({
    ref: provider,
    title: provider,
    description: `${copy.value.runtime.providerProfiles}: ${String(
      availableRuntimes.value.filter((item) => item.provider === provider)
        .length,
    )}`,
  })),
);
const runtimeOptions = computed<AsyncEntityOption[]>(() =>
  matchingRuntimes.value.map((runtime) => ({
    ref: runtime.ref,
    title: runtime.name,
    description: `${runtime.provider} · ${runtime.model}`,
    meta: runtime.revision,
  })),
);
const selectedProviderOption = computed(() =>
  selectedOrUnavailable(
    selectedProvider.value,
    providerOptions.value,
    copy.value.runtime.unavailableSelection,
  ),
);
const selectedRuntimeOption = computed(() =>
  selectedOrUnavailable(
    form.runtimeProfileRef,
    runtimeOptions.value,
    copy.value.runtime.unavailableSelection,
  ),
);
const runtimeDirty = computed(() => {
  const current = view.value?.configuration;
  if (!current) return false;
  return (
    form.runtimeProfileRef !== current.runtimeProfileRef ||
    form.model !== current.model ||
    form.providerPolicyMode !== current.providerPolicy.mode ||
    JSON.stringify(form.providerAccounts) !==
      JSON.stringify(
        runtimeCandidates(current.providerPolicy.accountCandidates),
      )
  );
});
const overlayDirty = computed(
  () =>
    Boolean(view.value) &&
    overlayContent.value !==
      (view.value?.draftOverlay?.content ??
        view.value?.publishedOverlay.content),
);
const overlayState = computed(
  () =>
    view.value?.draftOverlay?.state ??
    view.value?.publishedOverlay.state ??
    "UNAVAILABLE",
);
const overlayPublicationAvailable = computed(() => {
  const current = view.value;
  const draft = current?.draftOverlay;
  return (
    draft?.state === "VALID" &&
    draft.schemaRevision === current?.overlaySchema.revision &&
    draft.schemaDigest === current?.overlaySchema.digest &&
    !draft.diagnostics?.length
  );
});
const overlayValidation = computed(() =>
  overlayDirty.value || view.value?.draftOverlay?.diagnostics?.length
    ? []
    : (view.value?.draftOverlay?.validationMessages ?? []).map((value) =>
        value === "CONFIG_OVERLAY_INVALID_OR_PROTECTED"
          ? t("runtimeOverlay.invalid")
          : value,
      ),
);
const overlayDiagnostics = computed(() =>
  overlayDirty.value
    ? []
    : (view.value?.draftOverlay?.diagnostics ?? []).map((item) => ({
        line: item.line,
        column: item.column,
        message: `${t(`runtimeOverlay.diagnostics.${item.code}`)}${item.key ? ` · ${item.key}` : ""}${item.line > 0 ? ` · ${t("runtimeOverlay.location", { line: item.line, column: item.column })}` : ""}`,
      })),
);
const effortField = computed(() =>
  view.value?.overlaySchema.fields.find(
    (item) => item.key === "model_reasoning_effort",
  ),
);
const selectedEffort = computed(() =>
  view.value
    ? overlayEffort(overlayContent.value, view.value.overlaySchema)
    : undefined,
);
const effortOptions = computed(() => effortField.value?.allowedValues ?? []);
function chooseEffort(event: Event): void {
  if (
    !props.canEdit ||
    busy.value ||
    !view.value ||
    !(event.target instanceof HTMLSelectElement)
  )
    return;
  try {
    updateOverlay(
      changeOverlayEffort(
        overlayContent.value,
        view.value.overlaySchema,
        event.target.value,
      ),
    );
  } catch {
    problem.value = asProblem(
      new Error(t("runtimeOverlay.repairBeforeEffort")),
    );
  }
}
const overlayCompletionProvider = computed<CodeEditorCompletionProvider>(() => {
  const schema = view.value?.overlaySchema;
  return (query, signal, context) => {
    signal.throwIfAborted();
    return Promise.resolve(
      schema ? overlayCompletions(schema, query, context) : [],
    );
  };
});
const overlayHoverProvider = computed<CodeEditorHoverProvider>(() => {
  const schema = view.value?.overlaySchema;
  return (content, position) =>
    schema ? overlayHover(schema, content, position) : undefined;
});
useUnsavedChanges(
  computed(() => busy.value || runtimeDirty.value || overlayDirty.value),
  () => t("managed.discard"),
);

function notify(state: "APPLIED" | "DRAFT" | "RUNNING" | "FAILED"): void {
  emit("apply-state", state, copy.value.runtime.title, "next-turn");
}

function sync(preserveRuntime = false, preserveOverlay = false): void {
  const value = view.value;
  if (!value) return;
  if (!preserveRuntime) {
    form.runtimeProfileRef = value.configuration.runtimeProfileRef;
    form.model = value.configuration.model;
    form.providerPolicyMode = value.configuration.providerPolicy.mode;
    form.providerAccounts = runtimeCandidates(
      value.configuration.providerPolicy.accountCandidates,
    );
  }
  if (!preserveOverlay)
    overlayContent.value =
      value.draftOverlay?.content ?? value.publishedOverlay.content;
  notify(runtimeDirty.value || overlayDirty.value ? "DRAFT" : "APPLIED");
}

function selectedOrUnavailable(
  ref: string,
  options: readonly AsyncEntityOption[],
  unavailable: string,
): AsyncEntityOption | undefined {
  if (!ref) return undefined;
  return (
    options.find((item) => item.ref === ref) ?? {
      ref,
      title: ref,
      description: unavailable,
      disabled: true,
    }
  );
}

function localOptionPage(
  options: readonly AsyncEntityOption[],
  query: string,
  cursor?: string,
): AsyncEntityOptionPage {
  const normalized = query.trim().toLocaleLowerCase();
  const filtered = normalized
    ? options.filter((item) =>
        [item.title, item.description, item.meta]
          .filter(Boolean)
          .some((value) => value?.toLocaleLowerCase().includes(normalized)),
      )
    : [...options];
  const offset = Number.parseInt(cursor ?? "0", 10);
  const safeOffset = Number.isSafeInteger(offset) && offset >= 0 ? offset : 0;
  const items = filtered.slice(safeOffset, safeOffset + 20);
  const nextOffset = safeOffset + items.length;
  return {
    items,
    ...(nextOffset < filtered.length
      ? { nextPageToken: String(nextOffset) }
      : {}),
  };
}

function loadProviderPage(
  query: string,
  cursor?: string,
): Promise<AsyncEntityOptionPage> {
  return Promise.resolve(localOptionPage(providerOptions.value, query, cursor));
}

function loadRuntimePage(
  query: string,
  cursor?: string,
): Promise<AsyncEntityOptionPage> {
  return Promise.resolve(localOptionPage(runtimeOptions.value, query, cursor));
}

function pickerValue(
  value: string | null | readonly string[],
): string | undefined {
  return typeof value === "string" ? value : undefined;
}

function chooseProvider(value: string | null | readonly string[]): void {
  if (busy.value || !props.canEdit) return;
  const provider = pickerValue(value);
  if (!provider) return;
  const previousProvider = selectedProvider.value;
  const runtimeRef = runtimeRefForSelection(availableRuntimes.value, provider);
  const selected = availableRuntimes.value.find(
    (item) => item.ref === runtimeRef,
  );
  if (!selected) return;
  form.runtimeProfileRef = selected.ref;
  form.model = selected.model;
  if (provider !== previousProvider) form.providerAccounts = [];
  notify(runtimeDirty.value ? "DRAFT" : "APPLIED");
}

function chooseModel(value: string | null | readonly string[]): void {
  if (busy.value || !props.canEdit) return;
  const model = pickerValue(value);
  if (!model || !selectedRuntime.value) return;
  form.model = model;
  notify(runtimeDirty.value ? "DRAFT" : "APPLIED");
}

function chooseRuntime(value: string | null | readonly string[]): void {
  if (busy.value || !props.canEdit) return;
  const runtimeRef = pickerValue(value);
  const selected = availableRuntimes.value.find(
    (item) => item.ref === runtimeRef,
  );
  if (!selected) return;
  form.runtimeProfileRef = selected.ref;
  if (!form.model) form.model = selected.model;
  notify(runtimeDirty.value ? "DRAFT" : "APPLIED");
}

function chooseProviderPolicy(event: Event): void {
  if (busy.value || !props.canEdit) return;
  const target = event.currentTarget;
  const value = target instanceof HTMLSelectElement ? target.value : undefined;
  if (!value || !isProviderPolicyMode(value)) return;
  form.providerPolicyMode = value;
  notify(runtimeDirty.value ? "DRAFT" : "APPLIED");
}

function updateOverlay(value: string): void {
  if (busy.value || !props.canEdit) return;
  overlayContent.value = value;
  notify(overlayDirty.value ? "DRAFT" : "APPLIED");
}

async function load(): Promise<void> {
  if (busy.value) return;
  readController?.abort();
  const controller = new AbortController();
  readController = controller;
  const current = ++generation;
  loading.value = true;
  problem.value = undefined;
  try {
    const [runtimeView, catalog] = await Promise.all([
      loadAgentRuntime(props.agentRef, controller.signal),
      loadRuntimeCatalog(controller.signal),
    ]);
    if (current !== generation || controller.signal.aborted) return;
    view.value = runtimeView;
    runtimes.value = catalog;
    sync();
  } catch (error) {
    if (current === generation && !controller.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (current === generation) loading.value = false;
  }
}

async function execute(
  action: () => Promise<AgentRuntimeConfigurationView>,
  target: "RUNTIME" | "OVERLAY",
): Promise<void> {
  if (busy.value || loading.value || !props.canEdit) return;
  const preserveRuntime = target === "OVERLAY" && runtimeDirty.value;
  const preserveOverlay = target === "RUNTIME" && overlayDirty.value;
  const current = ++generation;
  busy.value = true;
  problem.value = undefined;
  notify("RUNNING");
  try {
    const result = await action();
    if (current !== generation) return;
    view.value = result;
    sync(preserveRuntime, preserveOverlay);
  } catch (error) {
    if (current === generation) {
      problem.value = asProblem(error);
      notify("FAILED");
    }
  } finally {
    if (current === generation) busy.value = false;
  }
}

async function saveRuntime(): Promise<void> {
  const current = view.value;
  if (
    !current ||
    !props.canEdit ||
    !runtimeDirty.value ||
    !modelAvailable.value ||
    providerAccountEligibility.value !== "READY"
  )
    return;
  await execute(
    () =>
      saveAgentRuntime(
        props.agentRef,
        pinnedRuntimeInput(form, modelSelection.value, selectedProvider.value),
        current.agentVersion,
      ),
    "RUNTIME",
  );
}

async function saveOverlay(): Promise<void> {
  const current = view.value;
  if (!current || !props.canEdit || !overlayDirty.value) return;
  await execute(
    () =>
      saveOverlayDraft(
        props.agentRef,
        overlayContent.value,
        current.agentVersion,
      ),
    "OVERLAY",
  );
}

async function changeOverlay(action: "VALIDATE" | "PUBLISH"): Promise<void> {
  const current = view.value;
  if (!current || !props.canEdit || overlayDirty.value) return;
  if (action === "PUBLISH" && !overlayPublicationAvailable.value) return;
  await execute(
    () => changeRuntimeOverlay(props.agentRef, action, current.agentVersion),
    "OVERLAY",
  );
}

async function restoreOverlay(revisionRef: string): Promise<void> {
  const current = view.value;
  if (!current || overlayDirty.value || !props.canEdit) return;
  await execute(
    () =>
      restoreOverlayRevision(props.agentRef, revisionRef, current.agentVersion),
    "OVERLAY",
  );
}

function reset(): void {
  generation++;
  readController?.abort();
  view.value = undefined;
  overlayContent.value = "";
  busy.value = false;
  loading.value = false;
  problem.value = undefined;
  modelAvailable.value = false;
  modelSelection.value = undefined;
}
watch(
  () => props.agentRef,
  () => {
    reset();
    void load();
  },
  { immediate: true },
);
onBeforeUnmount(reset);
</script>

<template>
  <AsyncState :loading="loading" :problem="problem" @retry="load">
    <div v-if="view" class="runtime-layout">
      <article class="runtime-panel panel">
        <div class="runtime-panel__head">
          <div>
            <h2>{{ copy.runtime.title }}</h2>
            <p>{{ copy.runtime.catalogRef }}</p>
          </div>
          <StatusBadge
            :state="selectedRuntime?.ready ? 'READY' : 'UNAVAILABLE'"
          />
        </div>
        <div class="runtime-panel__selectors">
          <label class="field">
            <span>{{ $t("agents.provider") }}</span>
            <AsyncEntityPicker
              :model-value="selectedProvider"
              :selected="selectedProviderOption"
              :load-page="loadProviderPage"
              :placeholder="copy.runtime.chooseProvider"
              :search-placeholder="copy.runtime.searchProvider"
              :disabled="!canEdit || busy || providers.length === 0"
              @update:model-value="chooseProvider"
            />
          </label>
          <label class="field">
            <span>{{ $t("agents.model") }}</span>
            <ProviderModelSelector
              :model-value="form.model"
              :definition-key="selectedProvider"
              :account-refs="
                form.providerAccounts.map((account) => account.accountRef)
              "
              :disabled="!canEdit || busy"
              @update:model-value="chooseModel"
              @availability-change="modelAvailable = $event"
              @selection-change="modelSelection = $event"
            />
            <label class="field">
              <span>{{ $t("runtimeOverlay.effort") }}</span>
              <select
                :aria-label="$t('runtimeOverlay.effort')"
                :value="selectedEffort"
                :disabled="
                  !canEdit ||
                  busy ||
                  selectedEffort === undefined ||
                  !effortField
                "
                @change="chooseEffort"
              >
                <option value="">
                  {{
                    $t("runtimeOverlay.defaultEffort", {
                      value: effortField?.defaultValue || "—",
                    })
                  }}
                </option>
                <option
                  v-if="
                    selectedEffort && !effortOptions.includes(selectedEffort)
                  "
                  :value="selectedEffort"
                  disabled
                >
                  {{ selectedEffort }} · {{ $t("providers.modelUnavailable") }}
                </option>
                <option
                  v-for="effort in effortOptions"
                  :key="effort"
                  :value="effort"
                >
                  {{ effort }}
                </option>
              </select>
              <small>{{
                $t("runtimeOverlay.effortHelp", {
                  model: view.configuration.model,
                })
              }}</small>
              <small>{{ $t("runtimeOverlay.effortCost") }}</small>
              <small v-if="selectedEffort === undefined">{{
                $t("runtimeOverlay.repairBeforeEffort")
              }}</small>
            </label>
          </label>
          <label class="field">
            <span>{{ copy.runtime.profile }}</span>
            <AsyncEntityPicker
              :model-value="form.runtimeProfileRef"
              :selected="selectedRuntimeOption"
              :load-page="loadRuntimePage"
              :placeholder="copy.runtime.chooseProfile"
              :search-placeholder="copy.runtime.searchProfile"
              :disabled="!canEdit || busy || matchingRuntimes.length === 0"
              @update:model-value="chooseRuntime"
            />
          </label>
          <label class="field">
            <span>{{ $t("runtime.accountPolicy") }}</span>
            <select
              :value="form.providerPolicyMode"
              :disabled="!canEdit || busy"
              @change="chooseProviderPolicy"
            >
              <option
                v-for="mode in providerPolicyModes"
                :key="mode"
                :value="mode"
              >
                {{ $t(`runtime.policy.${mode}`) }}
              </option>
            </select>
            <small>{{ $t("runtime.accountPolicyHelp") }}</small>
          </label>
        </div>
        <dl class="runtime-panel__summary">
          <div>
            <dt>
              {{
                $t("common.version", {
                  version: view.configuration.version,
                })
              }}
            </dt>
            <dd class="mono">v{{ view.configuration.version }}</dd>
          </div>
          <div>
            <dt>{{ $t("agents.runtimeRevision") }}</dt>
            <dd class="mono">{{ selectedRuntime?.revision }}</dd>
          </div>
          <div>
            <dt>{{ copy.runtime.accountPolicy }}</dt>
            <dd>{{ form.providerPolicyMode }}</dd>
          </div>
          <div>
            <dt>{{ copy.runtime.accounts }}</dt>
            <dd>{{ form.providerAccounts.length }}</dd>
          </div>
        </dl>
        <section class="runtime-panel__account-capability">
          <div class="runtime-panel__account-heading">
            <div>
              <strong>{{ $t("runtime.accounts") }}</strong>
              <p>{{ $t("runtime.accountCatalogHelp") }}</p>
            </div>
            <StatusBadge :state="providerAccountEligibility" />
          </div>
          <ProviderAccountSelector
            v-model="form.providerAccounts"
            :definition-key="selectedProvider"
            :policy-mode="form.providerPolicyMode"
            :disabled="!canEdit || busy"
            @eligibility-state-change="providerAccountEligibility = $event"
          />
        </section>
        <div v-if="canEdit" class="runtime-panel__actions">
          <span v-if="runtimeDirty">{{ $t("states.DRAFT") }}</span>
          <button
            class="button button--primary"
            type="button"
            :disabled="
              busy ||
              !runtimeDirty ||
              !form.runtimeProfileRef ||
              !form.model ||
              !modelAvailable ||
              !selectedRuntime?.ready ||
              !form.providerAccounts.length ||
              providerAccountEligibility !== 'READY'
            "
            @click="saveRuntime"
          >
            <Save :size="16" aria-hidden="true" />{{ copy.runtime.save }}
          </button>
        </div>
      </article>

      <article class="overlay-panel panel">
        <div class="overlay-panel__head">
          <div>
            <h2>{{ copy.runtime.overlay }}</h2>
            <p>{{ copy.runtime.overlayHelp }}</p>
            <small>
              {{
                $t("agents.revision", {
                  revision:
                    view.draftOverlay?.revision ??
                    view.publishedOverlay.revision,
                })
              }}
              ·
              {{
                view.draftOverlay
                  ? $t("states." + view.draftOverlay.state)
                  : $t("states." + view.publishedOverlay.state)
              }}
            </small>
          </div>
          <StatusBadge :state="overlayDirty ? 'DRAFT' : overlayState" />
        </div>
        <CodeEditorSurface
          :model-value="overlayContent"
          language="toml"
          :label="copy.runtime.overlay"
          :description="copy.runtime.overlayHelp"
          :placeholder="copy.runtime.overlayPlaceholder"
          :readonly="!canEdit || busy"
          :validation-messages="overlayValidation"
          :diagnostics="overlayDiagnostics"
          :completion-provider="overlayCompletionProvider"
          :hover-provider="overlayHoverProvider"
          :min-lines="13"
          @update:model-value="updateOverlay"
        />
        <details class="runtime-overlay-schema">
          <summary>{{ $t("runtimeOverlay.allowedFields") }}</summary>
          <p>
            {{ view.overlaySchema.revision }} · {{ view.overlaySchema.digest }}
          </p>
          <dl v-for="field in view.overlaySchema.fields" :key="field.key">
            <dt>
              <code>{{ field.key }}</code>
            </dt>
            <dd>
              {{ field.description }} · {{ field.valueType }}<br />{{
                field.allowedValues.join(" · ")
              }}<br />{{ field.hover }}
            </dd>
          </dl>
        </details>
        <div v-if="canEdit" class="overlay-panel__actions">
          <button
            class="button"
            type="button"
            :disabled="busy || !overlayDirty"
            @click="saveOverlay"
          >
            <Save :size="16" aria-hidden="true" />{{ $t("runtime.saveDraft") }}
          </button>
          <button
            class="button"
            type="button"
            :disabled="busy || overlayDirty || !view.draftOverlay"
            @click="changeOverlay('VALIDATE')"
          >
            <ShieldCheck :size="16" aria-hidden="true" />{{
              $t("runtime.validate")
            }}
          </button>
          <button
            class="button button--primary"
            type="button"
            :disabled="busy || overlayDirty || !overlayPublicationAvailable"
            @click="changeOverlay('PUBLISH')"
          >
            {{ $t("runtime.publishOverlay") }}
          </button>
        </div>
        <OverlayHistoryPanel
          :agent-ref="agentRef"
          :agent-version="view.agentVersion"
          :disabled="busy || overlayDirty"
          :can-edit="canEdit"
          @restore="restoreOverlay"
        />
        <section class="overlay-panel__effective">
          <div>
            <h3>{{ $t("runtime.effectiveConfig") }}</h3>
            <p>{{ $t("runtime.effectiveHelp") }}</p>
          </div>
          <CodeEditorSurface
            :model-value="view.safeEffectiveConfig"
            language="toml"
            :label="$t('runtime.effectiveConfig')"
            :description="$t('runtime.safeReadback')"
            readonly
            :min-lines="8"
          />
        </section>
      </article>
    </div>
  </AsyncState>
</template>

<style scoped>
.runtime-layout {
  display: grid;
  grid-template-columns: minmax(320px, 0.85fr) minmax(440px, 1.15fr);
  gap: 16px;
  align-items: start;
}
.runtime-panel,
.overlay-panel {
  display: grid;
  gap: 16px;
}
.runtime-panel__head,
.overlay-panel__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.runtime-panel h2,
.overlay-panel h3,
.runtime-panel p,
.overlay-panel h2,
.overlay-panel p {
  margin: 0;
}
.runtime-panel p,
.overlay-panel p {
  margin-top: 4px;
  color: var(--muted);
  font-size: 0.82rem;
}
.overlay-panel__head small {
  display: block;
  margin-top: 6px;
  color: var(--subtle);
  font-family: var(--font-mono);
  font-size: 0.72rem;
}
.runtime-panel__selectors {
  display: grid;
  gap: 12px;
}
.runtime-panel__summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 1px;
  margin: 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--border);
}
.runtime-panel__summary div {
  min-width: 0;
  padding: 10px;
  background: var(--panel);
}
.runtime-panel__summary dt {
  color: var(--subtle);
  font-size: 0.72rem;
}
.runtime-panel__summary dd {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
}
.runtime-panel__account-capability {
  display: grid;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.runtime-panel__account-capability strong {
  font-size: 0.84rem;
}
.runtime-panel__account-capability p {
  margin-top: 4px;
}
.runtime-panel__account-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.runtime-panel__actions,
.overlay-panel__actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.overlay-panel__effective {
  display: grid;
  gap: 10px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
}
.overlay-panel__effective h3,
.overlay-panel__effective p {
  margin: 0;
}
.runtime-panel__actions span {
  margin-right: auto;
  color: var(--warning);
  font-size: 0.78rem;
}
@media (max-width: 1040px) {
  .runtime-layout {
    grid-template-columns: 1fr;
  }
}
.runtime-overlay-schema {
  min-width: 0;
  overflow-wrap: anywhere;
}
.runtime-overlay-schema dd {
  margin-inline-start: 0;
}
@media (max-width: 640px) {
  .runtime-panel__summary {
    grid-template-columns: 1fr;
  }
}
</style>
