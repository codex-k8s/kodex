<script setup lang="ts">
import { Save, ShieldCheck } from "@lucide/vue";
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import CodeEditorSurface from "@/features/agents/detail/CodeEditorSurface.vue";
import { agentDetailCopy } from "@/features/agents/detail/copy";
import {
  readyRuntimes,
  runtimeModels,
  runtimeProviders,
  runtimeRefForSelection,
  runtimesForSelection,
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
  AgentRuntimeConfigurationInput,
  AgentRuntimeConfigurationView,
  RuntimeSelection,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

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

const { locale } = useI18n();
const copy = computed(() => agentDetailCopy(locale.value));
const view = ref<AgentRuntimeConfigurationView>();
const runtimes = ref<RuntimeSelection[]>([]);
const loading = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const overlayContent = ref("");
const form = reactive<AgentRuntimeConfigurationInput>({
  runtimeProfileRef: "",
  model: "",
  providerPolicyMode: "FIXED",
  providerAccounts: [],
});

const availableRuntimes = computed(() => readyRuntimes(runtimes.value));
const selectedRuntime = computed(
  () =>
    availableRuntimes.value.find(
      (item) => item.ref === form.runtimeProfileRef,
    ) ?? availableRuntimes.value[0],
);
const providers = computed(() => runtimeProviders(availableRuntimes.value));
const models = computed(() =>
  runtimeModels(availableRuntimes.value, selectedRuntime.value?.provider ?? ""),
);
const matchingRuntimes = computed(() =>
  runtimesForSelection(
    availableRuntimes.value,
    selectedRuntime.value?.provider ?? "",
    form.model,
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
      JSON.stringify(current.providerPolicy.accountCandidates)
  );
});
const overlayDirty = computed(
  () =>
    overlayContent.value !==
    (view.value?.draftOverlay?.content ?? view.value?.publishedOverlay.content),
);
const overlayState = computed(
  () =>
    view.value?.draftOverlay?.state ??
    view.value?.publishedOverlay.state ??
    "UNAVAILABLE",
);
const overlayValidation = computed(
  () => view.value?.draftOverlay?.validationMessages ?? [],
);

function notify(state: "APPLIED" | "DRAFT" | "RUNNING" | "FAILED"): void {
  emit("apply-state", state, copy.value.runtime.title, "next-turn");
}

function sync(): void {
  const value = view.value;
  if (!value) return;
  form.runtimeProfileRef = value.configuration.runtimeProfileRef;
  form.model = value.configuration.model;
  form.providerPolicyMode = value.configuration.providerPolicy.mode;
  form.providerAccounts =
    value.configuration.providerPolicy.accountCandidates.map((item) => ({
      ...item,
    }));
  overlayContent.value =
    value.draftOverlay?.content ?? value.publishedOverlay.content;
  notify("APPLIED");
}

function eventValue(event: Event): string | undefined {
  const target = event.currentTarget;
  return target instanceof HTMLSelectElement ? target.value : undefined;
}

function chooseProvider(event: Event): void {
  const provider = eventValue(event);
  if (!provider) return;
  const runtimeRef = runtimeRefForSelection(availableRuntimes.value, provider);
  const selected = availableRuntimes.value.find(
    (item) => item.ref === runtimeRef,
  );
  if (!selected) return;
  form.runtimeProfileRef = selected.ref;
  form.model = selected.model;
  notify(runtimeDirty.value ? "DRAFT" : "APPLIED");
}

function chooseModel(event: Event): void {
  const model = eventValue(event);
  const provider = selectedRuntime.value?.provider;
  if (!model || !provider) return;
  const runtimeRef = runtimeRefForSelection(
    availableRuntimes.value,
    provider,
    model,
  );
  if (!runtimeRef) return;
  form.runtimeProfileRef = runtimeRef;
  form.model = model;
  notify(runtimeDirty.value ? "DRAFT" : "APPLIED");
}

function chooseRuntime(event: Event): void {
  const runtimeRef = eventValue(event);
  const selected = availableRuntimes.value.find(
    (item) => item.ref === runtimeRef,
  );
  if (!selected) return;
  form.runtimeProfileRef = selected.ref;
  form.model = selected.model;
  notify(runtimeDirty.value ? "DRAFT" : "APPLIED");
}

function updateOverlay(value: string): void {
  overlayContent.value = value;
  notify(overlayDirty.value ? "DRAFT" : "APPLIED");
}

async function load(): Promise<void> {
  loading.value = true;
  problem.value = undefined;
  try {
    const [runtimeView, catalog] = await Promise.all([
      loadAgentRuntime(props.agentRef),
      loadRuntimeCatalog(),
    ]);
    view.value = runtimeView;
    runtimes.value = catalog;
    sync();
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    loading.value = false;
  }
}

async function execute(
  action: () => Promise<AgentRuntimeConfigurationView>,
): Promise<void> {
  busy.value = true;
  problem.value = undefined;
  notify("RUNNING");
  try {
    view.value = await action();
    sync();
  } catch (error) {
    problem.value = asProblem(error);
    notify("FAILED");
  } finally {
    busy.value = false;
  }
}

async function saveRuntime(): Promise<void> {
  const current = view.value;
  if (!current || !props.canEdit || !runtimeDirty.value) return;
  await execute(() =>
    saveAgentRuntime(
      props.agentRef,
      {
        runtimeProfileRef: form.runtimeProfileRef,
        model: form.model,
        providerPolicyMode: form.providerPolicyMode,
        providerAccounts: form.providerAccounts.map((item) => ({ ...item })),
      },
      current.agentVersion,
    ),
  );
}

async function saveOverlay(): Promise<void> {
  const current = view.value;
  if (!current || !props.canEdit || !overlayDirty.value) return;
  await execute(() =>
    saveOverlayDraft(
      props.agentRef,
      overlayContent.value,
      current.agentVersion,
    ),
  );
}

async function changeOverlay(action: "VALIDATE" | "PUBLISH"): Promise<void> {
  const current = view.value;
  if (!current || !props.canEdit || overlayDirty.value) return;
  await execute(() =>
    changeRuntimeOverlay(props.agentRef, action, current.agentVersion),
  );
}

onMounted(() => void load());
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
            <select
              :value="selectedRuntime?.provider"
              :disabled="!canEdit || busy || providers.length === 0"
              @change="chooseProvider"
            >
              <option
                v-for="provider in providers"
                :key="provider"
                :value="provider"
              >
                {{ provider }}
              </option>
            </select>
          </label>
          <label class="field">
            <span>{{ $t("agents.model") }}</span>
            <select
              :value="form.model"
              :disabled="!canEdit || busy || models.length === 0"
              @change="chooseModel"
            >
              <option v-for="model in models" :key="model" :value="model">
                {{ model }}
              </option>
            </select>
          </label>
          <label class="field">
            <span>{{ copy.runtime.profile }}</span>
            <select
              :value="form.runtimeProfileRef"
              :disabled="!canEdit || busy || matchingRuntimes.length === 0"
              @change="chooseRuntime"
            >
              <option
                v-for="item in matchingRuntimes"
                :key="item.ref"
                :value="item.ref"
              >
                {{ item.name }} · {{ item.revision }}
              </option>
            </select>
          </label>
        </div>
        <dl class="runtime-panel__summary">
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
        <div v-if="canEdit" class="runtime-panel__actions">
          <span v-if="runtimeDirty">{{ $t("states.DRAFT") }}</span>
          <button
            class="button button--primary"
            type="button"
            :disabled="
              busy || !runtimeDirty || !form.runtimeProfileRef || !form.model
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
          </div>
          <StatusBadge :state="overlayDirty ? 'DRAFT' : overlayState" />
        </div>
        <CodeEditorSurface
          :model-value="overlayContent"
          language="toml"
          :label="copy.runtime.overlay"
          :placeholder="copy.runtime.overlayPlaceholder"
          :readonly="!canEdit"
          :validation-messages="overlayValidation"
          :min-lines="13"
          @update:model-value="updateOverlay"
        />
        <div v-if="canEdit" class="overlay-panel__actions">
          <button
            class="button"
            type="button"
            :disabled="busy || !overlayDirty"
            @click="saveOverlay"
          >
            <Save :size="16" aria-hidden="true" />{{ copy.runtime.saveOverlay }}
          </button>
          <button
            class="button"
            type="button"
            :disabled="busy || overlayDirty || !view.draftOverlay"
            @click="changeOverlay('VALIDATE')"
          >
            <ShieldCheck :size="16" aria-hidden="true" />{{
              $t("agents.validate")
            }}
          </button>
          <button
            class="button button--primary"
            type="button"
            :disabled="
              busy || overlayDirty || view.draftOverlay?.state !== 'VALID'
            "
            @click="changeOverlay('PUBLISH')"
          >
            {{ $t("agents.publish") }}
          </button>
        </div>
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
.runtime-panel__selectors {
  display: grid;
  gap: 12px;
}
.runtime-panel__summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
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
@media (max-width: 640px) {
  .runtime-panel__summary {
    grid-template-columns: 1fr;
  }
}
</style>
