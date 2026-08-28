<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";

import { usePlatformStore } from "@/features/platform/store";
import { useRuntimeStore } from "@/features/runtime/store";
import TomlEditor from "@/features/runtime/components/TomlEditor.vue";
import type {
  Agent,
  AgentRuntimeConfigurationInput,
  ProviderAccountCandidate,
  RuntimeEnvironmentSet,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOption } from "@/shared/ui/async-entity-picker";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  agent: Agent;
  projectRef: string;
  canEdit: boolean;
}>();
const platform = usePlatformStore();
const runtime = useRuntimeStore();
const view = computed(() => runtime.agentViews[props.agent.ref]);
const versions = computed(() => runtime.agentVersions[props.agent.ref] ?? []);
const problem = ref<AppProblem>();
const busy = ref(false);
const overlayContent = ref("");
const selectedEnvironment = ref("");
const selectedEnvironmentOption = ref<AsyncEntityOption>();
const form = reactive<AgentRuntimeConfigurationInput>({
  runtimeProfileRef: "",
  model: "",
  providerPolicyMode: "FIXED",
  providerAccounts: [],
});

const runtimes = computed(() =>
  Object.values(platform.runtimes)
    .filter((item) => item.ready)
    .sort((left, right) => left.name.localeCompare(right.name)),
);
const selectedRuntime = computed(() =>
  runtimes.value.find((item) => item.ref === form.runtimeProfileRef),
);
const provider = computed(
  () => selectedRuntime.value?.provider ?? view.value?.configuration.provider,
);
const invalidLines = computed(() => {
  if (view.value?.draftOverlay?.state !== "INVALID") return [];
  return Array.from(
    { length: Math.max(1, overlayContent.value.split("\n").length) },
    (_, index) => index + 1,
  );
});
const runtimeChanged = computed(() => {
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
const overlayChanged = computed(
  () =>
    overlayContent.value !==
    (view.value?.draftOverlay?.content ?? view.value?.publishedOverlay.content),
);

function accountLabel(index: number): string {
  return `#${String(index + 1)}`;
}

function environmentOption(value: RuntimeEnvironmentSet): AsyncEntityOption {
  return {
    ref: value.ref,
    title: value.name,
    description: value.description,
    meta: `rev ${String(value.currentVersion.revision)}`,
  };
}

function sync(): void {
  const value = view.value;
  if (!value) return;
  form.runtimeProfileRef = value.configuration.runtimeProfileRef;
  form.model = value.configuration.model;
  form.providerPolicyMode = value.configuration.providerPolicy.mode;
  form.providerAccounts =
    value.configuration.providerPolicy.accountCandidates.map((candidate) => ({
      ...candidate,
    }));
  overlayContent.value =
    value.draftOverlay?.content ?? value.publishedOverlay.content;
  selectedEnvironment.value = value.environment.ref;
  selectedEnvironmentOption.value = { ...environmentOption(value.environment) };
}

function selectRuntime(event: Event): void {
  const ref = (event.target as HTMLSelectElement).value;
  form.runtimeProfileRef = ref;
  const selected = runtimes.value.find((item) => item.ref === ref);
  if (selected) form.model = selected.model;
}

function changeWeight(candidate: ProviderAccountCandidate, event: Event): void {
  const value = Number((event.target as HTMLInputElement).value);
  candidate.weight = Number.isInteger(value)
    ? Math.min(10000, Math.max(1, value))
    : 1;
}

async function load(): Promise<void> {
  await Promise.all([
    runtime.loadAgentRuntime(props.agent.ref),
    runtime.loadAgentVersions(props.agent.ref),
    platform.loadRuntimes(),
  ]);
  sync();
}

async function execute(action: () => Promise<unknown>): Promise<void> {
  busy.value = true;
  problem.value = undefined;
  try {
    await action();
    sync();
    await runtime.loadAgentVersions(props.agent.ref);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function saveRuntime(): Promise<void> {
  const current = view.value;
  if (!current || !props.canEdit) return;
  await execute(() =>
    runtime.saveAgentRuntime(
      props.agent.ref,
      {
        runtimeProfileRef: form.runtimeProfileRef,
        model: form.model.trim(),
        providerPolicyMode: form.providerPolicyMode,
        providerAccounts: form.providerAccounts.map((item) => ({ ...item })),
      },
      current.agentVersion,
    ),
  );
}

async function saveOverlay(): Promise<void> {
  const current = view.value;
  if (!current || !props.canEdit) return;
  await execute(() =>
    runtime.saveOverlayDraft(
      props.agent.ref,
      overlayContent.value,
      current.agentVersion,
    ),
  );
}

async function overlayAction(action: "VALIDATE" | "PUBLISH"): Promise<void> {
  const current = view.value;
  if (!current || !props.canEdit) return;
  await execute(() =>
    runtime.changeOverlay(props.agent.ref, action, current.agentVersion),
  );
}

async function bindSelectedEnvironment(): Promise<void> {
  const current = view.value;
  if (
    !current ||
    !props.canEdit ||
    selectedEnvironment.value === current.environment.ref
  )
    return;
  await execute(() =>
    runtime.bindEnvironment(
      props.agent.ref,
      selectedEnvironment.value,
      current.agentVersion,
    ),
  );
}

async function loadEnvironmentPage(
  query: string,
  cursor?: string,
): Promise<{ items: AsyncEntityOption[]; nextPageToken?: string }> {
  const page = await runtime.searchEnvironmentPage(
    props.projectRef,
    query,
    cursor,
  );
  return {
    items: page.items.map(environmentOption),
    ...(page.nextPageToken ? { nextPageToken: page.nextPageToken } : {}),
  };
}

function setEnvironment(option: AsyncEntityOption): void {
  selectedEnvironmentOption.value = { ...option };
}

watch(view, sync);
onMounted(() => void load());
</script>

<template>
  <section class="runtime-workspace">
    <header class="runtime-workspace__header">
      <div>
        <h2>{{ $t("runtime.title") }}</h2>
        <p>{{ $t("runtime.appliesNextTurn") }}</p>
      </div>
      <StatusBadge
        v-if="view"
        :state="view.draftOverlay?.state ?? view.publishedOverlay.state"
      />
    </header>
    <AsyncState
      :loading="runtime.loading[`agent:${agent.ref}`]"
      :problem="runtime.problems[`agent:${agent.ref}`]"
      @retry="load"
    >
      <div v-if="view" class="runtime-layout">
        <div class="runtime-layout__main">
          <article class="panel runtime-settings">
            <div class="section-header">
              <h3>{{ $t("runtime.modelAndExecution") }}</h3>
              <span class="secondary-text">{{
                $t("runtime.nextTurnHint")
              }}</span>
            </div>
            <label class="runtime-row">
              <span
                ><strong>{{ $t("runtime.provider") }}</strong
                ><small>{{ $t("runtime.providerHelp") }}</small></span
              >
              <input :value="provider" readonly />
            </label>
            <label class="runtime-row">
              <span
                ><strong>{{ $t("runtime.profile") }}</strong
                ><small>{{ $t("runtime.profileHelp") }}</small></span
              >
              <select
                :value="form.runtimeProfileRef"
                :disabled="busy || !canEdit"
                @change="selectRuntime"
              >
                <option
                  v-for="item in runtimes"
                  :key="item.ref"
                  :value="item.ref"
                >
                  {{ item.name }} · {{ item.revision }}
                </option>
              </select>
            </label>
            <label class="runtime-row">
              <span
                ><strong>{{ $t("runtime.model") }}</strong
                ><small>{{ $t("runtime.modelHelp") }}</small></span
              >
              <input
                v-model.trim="form.model"
                required
                maxlength="160"
                :disabled="busy || !canEdit"
              />
            </label>
            <label class="runtime-row">
              <span
                ><strong>{{ $t("runtime.accountPolicy") }}</strong
                ><small>{{ $t("runtime.accountPolicyHelp") }}</small></span
              >
              <select
                v-model="form.providerPolicyMode"
                :disabled="busy || !canEdit"
              >
                <option value="FIXED">{{ $t("runtime.policy.FIXED") }}</option>
                <option value="LEAST_USED">
                  {{ $t("runtime.policy.LEAST_USED") }}
                </option>
                <option value="WEIGHTED">
                  {{ $t("runtime.policy.WEIGHTED") }}
                </option>
              </select>
            </label>
            <section class="account-list" :aria-label="$t('runtime.accounts')">
              <div
                v-for="(candidate, index) in form.providerAccounts"
                :key="candidate.accountRef"
                class="account-row"
              >
                <span
                  ><strong>{{
                    $t("runtime.account", { account: accountLabel(index) })
                  }}</strong
                  ><small>{{ $t("runtime.authorizedAccount") }}</small></span
                >
                <label v-if="form.providerPolicyMode === 'WEIGHTED'">
                  <span>{{ $t("runtime.weight") }}</span>
                  <input
                    type="number"
                    min="1"
                    max="10000"
                    :value="candidate.weight"
                    :disabled="busy || !canEdit"
                    @input="changeWeight(candidate, $event)"
                  />
                </label>
              </div>
            </section>
            <div class="runtime-blocker" role="note">
              <strong>{{ $t("runtime.accountCatalogUnavailable") }}</strong>
              <p>{{ $t("runtime.accountCatalogBlocker") }}</p>
            </div>
            <div class="runtime-row runtime-row--picker">
              <span
                ><strong>{{ $t("runtime.environment") }}</strong
                ><small>{{ $t("runtime.environmentHelp") }}</small></span
              >
              <div>
                <AsyncEntityPicker
                  v-model="selectedEnvironment"
                  :selected="selectedEnvironmentOption"
                  :load-page="loadEnvironmentPage"
                  :placeholder="$t('runtime.chooseEnvironment')"
                  :search-placeholder="$t('runtime.searchEnvironment')"
                  :disabled="busy || !canEdit"
                  @select="setEnvironment"
                />
                <button
                  class="button"
                  type="button"
                  :disabled="
                    busy ||
                    !canEdit ||
                    selectedEnvironment === view.environment.ref
                  "
                  @click="bindSelectedEnvironment"
                >
                  {{ $t("runtime.bindEnvironment") }}
                </button>
              </div>
            </div>
            <div class="inline-actions runtime-settings__actions">
              <button
                class="button button--primary"
                type="button"
                :disabled="
                  busy ||
                  !canEdit ||
                  !runtimeChanged ||
                  !form.runtimeProfileRef ||
                  !form.model ||
                  !form.providerAccounts.length
                "
                @click="saveRuntime"
              >
                {{ $t("runtime.publishConfiguration") }}
              </button>
              <button
                class="button"
                type="button"
                :disabled="busy || !runtimeChanged"
                @click="sync"
              >
                {{ $t("common.cancel") }}
              </button>
            </div>
          </article>

          <article class="panel overlay-panel">
            <div class="section-header">
              <div>
                <h3>{{ $t("runtime.overlay") }}</h3>
                <p>{{ $t("runtime.overlayHelp") }}</p>
              </div>
              <StatusBadge
                :state="view.draftOverlay?.state ?? view.publishedOverlay.state"
              />
            </div>
            <TomlEditor
              v-model="overlayContent"
              :label="$t('runtime.overlayEditor')"
              :readonly="!canEdit"
              :invalid-lines="invalidLines"
            />
            <ul
              v-if="view.draftOverlay?.validationMessages.length"
              class="validation-list"
              role="alert"
            >
              <li
                v-for="message in view.draftOverlay.validationMessages"
                :key="message"
              >
                {{ message }}
              </li>
            </ul>
            <div class="inline-actions">
              <button
                class="button"
                type="button"
                :disabled="busy || !canEdit || !overlayChanged"
                @click="saveOverlay"
              >
                {{ $t("runtime.saveDraft") }}
              </button>
              <button
                class="button"
                type="button"
                :disabled="busy || !canEdit || !view.draftOverlay"
                @click="overlayAction('VALIDATE')"
              >
                {{ $t("runtime.validate") }}
              </button>
              <button
                class="button button--primary"
                type="button"
                :disabled="
                  busy || !canEdit || view.draftOverlay?.state !== 'VALID'
                "
                @click="overlayAction('PUBLISH')"
              >
                {{ $t("runtime.publishOverlay") }}
              </button>
            </div>
          </article>
        </div>

        <aside class="runtime-layout__side">
          <article class="panel effective-panel">
            <div class="section-header">
              <h3>{{ $t("runtime.effectiveConfig") }}</h3>
              <span class="tag">RuntimeRevision</span>
            </div>
            <p class="runtime-lock-note">{{ $t("runtime.effectiveHelp") }}</p>
            <TomlEditor
              :model-value="view.safeEffectiveConfig"
              :label="$t('runtime.safeReadback')"
              readonly
            />
          </article>
          <article class="panel history-panel">
            <div class="section-header">
              <h3>{{ $t("runtime.history") }}</h3>
              <span>{{ versions.length }}</span>
            </div>
            <div v-if="versions.length" class="runtime-history">
              <div v-for="item in versions" :key="item.ref">
                <strong>{{
                  $t("common.version", { version: item.version })
                }}</strong>
                <small>{{ new Date(item.createdAt).toLocaleString() }}</small>
                <span>{{ item.provider }} · {{ item.model }}</span>
              </div>
            </div>
            <p v-else>{{ $t("common.empty") }}</p>
            <div class="runtime-blocker" role="note">
              <strong>{{ $t("runtime.overlayRollbackUnavailable") }}</strong>
              <p>{{ $t("runtime.overlayRollbackBlocker") }}</p>
            </div>
          </article>
        </aside>
      </div>
      <ProblemNotice v-if="problem" :problem="problem" />
      <section
        v-if="problem?.kind === 'conflict'"
        class="runtime-conflict"
        role="status"
      >
        <p>{{ $t("runtime.conflictHelp") }}</p>
        <button class="button" type="button" @click="load">
          {{ $t("runtime.reload") }}
        </button>
      </section>
    </AsyncState>
  </section>
</template>

<style scoped>
.runtime-workspace {
  display: grid;
  gap: 14px;
}
.runtime-workspace__header,
.section-header,
.runtime-row,
.account-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.runtime-workspace__header p,
.section-header p {
  margin-bottom: 0;
  color: var(--text-secondary);
}
.runtime-layout {
  display: grid;
  grid-template-columns: minmax(0, 1.55fr) minmax(320px, 0.85fr);
  gap: 16px;
}
.runtime-layout__main,
.runtime-layout__side {
  display: grid;
  align-content: start;
  gap: 16px;
}
.runtime-settings {
  display: grid;
  gap: 0;
}
.runtime-row {
  display: grid;
  grid-template-columns: minmax(180px, 0.72fr) minmax(240px, 1.15fr);
  align-items: center;
  padding: 12px 0;
  border-top: 1px solid var(--hairline);
}
.runtime-row > span,
.runtime-row > span > * {
  display: block;
}
.runtime-row small,
.account-row small,
.runtime-history small {
  color: var(--text-secondary);
}
.runtime-row input,
.runtime-row select {
  width: 100%;
}
.runtime-row--picker > div {
  display: grid;
  gap: 8px;
}
.account-list,
.runtime-history {
  display: grid;
  gap: 8px;
  padding: 10px 0;
}
.account-row {
  align-items: center;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 7px;
}
.account-row > span,
.account-row > span > * {
  display: block;
}
.account-row label {
  display: flex;
  align-items: center;
  gap: 8px;
}
.account-row input {
  width: 88px;
}
.runtime-blocker,
.runtime-lock-note,
.runtime-conflict {
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--panel);
}
.runtime-blocker p,
.runtime-lock-note,
.runtime-conflict p {
  margin: 4px 0 0;
  color: var(--text-secondary);
}
.runtime-settings__actions {
  justify-content: flex-end;
}
.overlay-panel,
.effective-panel,
.history-panel {
  display: grid;
  gap: 12px;
}
.effective-panel :deep(.toml-editor__body) {
  min-height: 430px;
}
.effective-panel :deep(textarea) {
  min-height: 430px;
}
.runtime-history > div {
  display: grid;
  gap: 3px;
  padding: 10px 0;
  border-bottom: 1px solid var(--hairline);
}
.tag {
  padding: 4px 7px;
  border-radius: 5px;
  background: var(--accent-soft);
  color: var(--accent-strong);
  font-family: var(--font-mono);
  font-size: 0.75rem;
}
.validation-list {
  padding: 10px 10px 10px 28px;
  border: 1px solid var(--danger);
  border-radius: 7px;
  background: var(--danger-soft);
  color: var(--danger);
}
@media (max-width: 1040px) {
  .runtime-layout {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 700px) {
  .runtime-row {
    grid-template-columns: 1fr;
    gap: 8px;
  }
}
</style>
