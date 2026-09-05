<script setup lang="ts">
import {
  Layers3,
  Plus,
  Power,
  PowerOff,
  Search,
  ShieldCheck,
  Trash2,
  Maximize2,
  ExternalLink,
} from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { useRuntimeStore } from "@/features/runtime/store";
import {
  compactIdentifier,
  hasEnvironmentAction,
} from "@/features/runtime/environment-capabilities";
import type { RuntimeEnvironmentSet } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { invalidSearchResult } from "@/shared/api/search-result";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const route = useRoute();
const router = useRouter();
const registry = ref<HTMLElement>();
const { t } = useI18n();
const runtime = useRuntimeStore();
const projectRef = computed(() => String(route.params.projectRef));
const query = ref("");
const items = ref<RuntimeEnvironmentSet[]>([]);
const cursor = ref<string>();
const loading = ref(false);
const loadingMore = ref(false);
const expanded = ref(false);
const problem = ref<AppProblem>();
const selectedRef = ref("");
const selected = computed(() =>
  items.value.find((item) => item.ref === selectedRef.value),
);
const selectedReadiness = computed(() =>
  selected.value ? runtime.environmentReadiness[selected.value.ref] : undefined,
);
const selectedAgents = computed(() =>
  selected.value ? (runtime.environmentAgents[selected.value.ref] ?? []) : [],
);
const actionRef = ref("");
const deleteTarget = ref<RuntimeEnvironmentSet>();
let generation = 0;
let listController: AbortController | undefined;
let inspectorController: AbortController | undefined;
const visitedCursors = new Set<string>();
let debounceTimer: ReturnType<typeof setTimeout> | undefined;

async function load(reset = true): Promise<void> {
  if (!reset && (!cursor.value || loadingMore.value)) return;
  const current = ++generation;
  listController?.abort();
  const controller = new AbortController();
  listController = controller;
  const requestedCursor = reset ? undefined : cursor.value;
  if (reset) {
    visitedCursors.clear();
    selectedRef.value = "";
    loading.value = true;
    cursor.value = undefined;
  } else {
    loadingMore.value = true;
  }
  problem.value = undefined;
  try {
    const page = await runtime.searchEnvironmentPage(
      projectRef.value,
      query.value,
      requestedCursor,
      controller.signal,
    );
    if (generation !== current || controller.signal.aborted) return;
    if (
      !Array.isArray(page.items) ||
      page.items.some((item) => item.projectRef !== projectRef.value) ||
      new Set(page.items.map((item) => item.ref)).size !== page.items.length ||
      (page.nextPageToken &&
        (page.nextPageToken === requestedCursor ||
          visitedCursors.has(page.nextPageToken))) ||
      (!reset &&
        page.items.some((item) =>
          items.value.some((existing) => existing.ref === item.ref),
        ))
    )
      throw invalidSearchResult();
    if (requestedCursor) visitedCursors.add(requestedCursor);
    if (reset) items.value = page.items;
    else {
      const merged = new Map(items.value.map((item) => [item.ref, item]));
      for (const item of page.items) merged.set(item.ref, item);
      items.value = [...merged.values()];
    }
    cursor.value = page.nextPageToken;
    if (
      !selectedRef.value ||
      !items.value.some((item) => item.ref === selectedRef.value)
    )
      selectedRef.value = "";
  } catch (error) {
    if (generation === current && !controller.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (generation === current) {
      loading.value = false;
      loadingMore.value = false;
    }
  }
}

function replaceItem(value: RuntimeEnvironmentSet): void {
  const index = items.value.findIndex((item) => item.ref === value.ref);
  if (index >= 0) items.value[index] = value;
  else items.value.push(value);
}

async function loadOperationalState(
  environmentRef: string,
  signal: AbortSignal,
): Promise<void> {
  await Promise.all([
    runtime.loadEnvironmentReadiness(environmentRef, signal),
    runtime.loadEnvironmentAgents(environmentRef, true, signal),
  ]);
}

async function setEnabled(
  environment: RuntimeEnvironmentSet,
  enabled: boolean,
): Promise<void> {
  const action = enabled ? "ENABLE" : "DISABLE";
  if (!hasEnvironmentAction(environment, action)) return;
  actionRef.value = environment.ref;
  problem.value = undefined;
  try {
    const saved = await runtime.setEnvironmentEnabled(environment, enabled);
    replaceItem(saved);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    actionRef.value = "";
  }
}

async function remove(environment: RuntimeEnvironmentSet): Promise<void> {
  if (!hasEnvironmentAction(environment, "DELETE")) return;
  actionRef.value = environment.ref;
  problem.value = undefined;
  try {
    const saved = await runtime.removeEnvironment(environment);
    replaceItem(saved);
    selectedRef.value = "";
    deleteTarget.value = undefined;
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    actionRef.value = "";
  }
}

function onScroll(event: Event): void {
  const element = event.currentTarget as HTMLElement;
  if (
    cursor.value &&
    element.scrollTop + element.clientHeight >= element.scrollHeight - 80
  )
    void load(false);
}

watch(query, () => {
  generation += 1;
  listController?.abort();
  selectedRef.value = "";
  if (debounceTimer) clearTimeout(debounceTimer);
  debounceTimer = setTimeout(() => void load(), 500);
});
watch(projectRef, () => {
  if (debounceTimer) clearTimeout(debounceTimer);
  void load();
});
watch(
  () => selected.value?.ref,
  (value, _previous, onCleanup) => {
    const controller = new AbortController();
    inspectorController = controller;
    onCleanup(() => controller.abort());
    if (value) void loadOperationalState(value, controller.signal);
  },
  { immediate: true },
);
function dismissInspector(event: PointerEvent): void {
  if (event.target instanceof Node && !registry.value?.contains(event.target))
    selectedRef.value = "";
}
function toggleInspector(environmentRef: string): void {
  selectedRef.value =
    selectedRef.value === environmentRef ? "" : environmentRef;
}
function clickRow(
  event: MouseEvent,
  environmentRef: string,
  open = false,
): void {
  if (
    event.target instanceof Element &&
    event.target.closest("a, button, input, select, textarea")
  )
    return;
  if (open) openEditor(environmentRef);
  else toggleInspector(environmentRef);
}
function openEditor(environmentRef: string): void {
  void router.push(
    `/projects/${encodeURIComponent(projectRef.value)}/environments/${encodeURIComponent(environmentRef)}`,
  );
}
onMounted(() => {
  void load();
  document.addEventListener("pointerdown", dismissInspector);
});
onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", dismissInspector);
  generation += 1;
  listController?.abort();
  if (debounceTimer) clearTimeout(debounceTimer);
});
</script>

<template>
  <PageFrame :title="$t('runtime.environmentsTitle')">
    <template #actions>
      <RouterLink
        class="button button--primary"
        :to="`/projects/${encodeURIComponent(projectRef)}/environments/new`"
      >
        <Plus :size="16" aria-hidden="true" />
        {{ $t("runtime.newEnvironment") }}
      </RouterLink>
    </template>
    <component
      :is="expanded ? ModalDialog : 'section'"
      :class="expanded ? undefined : 'environment-registry'"
      :title="expanded ? $t('runtime.environmentsTitle') : undefined"
      size="full"
      @close="expanded = false"
    >
      <header class="environment-toolbar">
        <label>
          <Search :size="16" aria-hidden="true" />
          <span class="sr-only">{{ $t("runtime.searchEnvironment") }}</span>
          <input
            v-model="query"
            type="search"
            :placeholder="$t('runtime.searchEnvironment')"
          />
        </label>
        <span>{{ $t("runtime.pickerShown", { count: items.length }) }}</span>
        <button
          v-if="!expanded"
          class="icon-button"
          :title="$t('catalog.expand')"
          :aria-label="$t('catalog.expand')"
          @click="expanded = true"
        >
          <Maximize2 :size="16" />
        </button>
      </header>
      <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
      <div
        ref="registry"
        class="environment-registry__content"
        :class="{ 'environment-registry__content--selected': selected }"
        @keydown.esc="selectedRef = ''"
      >
        <div
          class="environment-table-wrap"
          :class="{ 'environment-table-wrap--expanded': expanded }"
          :aria-busy="loading || loadingMore"
          @scroll="onScroll"
        >
          <div v-if="loading" class="environment-state" role="status">
            {{ $t("common.loading") }}
          </div>
          <div v-else-if="!items.length" class="environment-state">
            <Layers3 :size="28" aria-hidden="true" />
            <strong>{{ $t("runtime.environmentsEmpty") }}</strong>
          </div>
          <table v-else class="environment-table">
            <thead>
              <tr>
                <th>{{ $t("common.name") }}</th>
                <th>{{ $t("runtime.revision") }}</th>
                <th>{{ $t("runtime.versionDigest") }}</th>
                <th>{{ $t("runtime.exactImage") }}</th>
                <th>{{ $t("runtime.verifiedTools") }}</th>
                <th>{{ $t("runtime.variables") }}</th>
                <th>{{ $t("runtime.secretDescriptors") }}</th>
                <th>{{ $t("common.status") }}</th>
                <th>
                  <span class="sr-only">{{ $t("common.actions") }}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="environment in items"
                :key="environment.ref"
                @click="clickRow($event, environment.ref)"
                @dblclick="clickRow($event, environment.ref, true)"
                :class="{
                  'environment-table__row--selected':
                    environment.ref === selected?.ref,
                }"
              >
                <td>
                  <button
                    class="environment-name"
                    type="button"
                    @click="toggleInspector(environment.ref)"
                    @dblclick="openEditor(environment.ref)"
                  >
                    <strong>{{ environment.name }}</strong>
                    <small>{{ environment.description }}</small>
                  </button>
                </td>
                <td>rev {{ environment.currentVersion.revision }}</td>
                <td>
                  <code>{{
                    compactIdentifier(environment.currentVersion.digest)
                  }}</code>
                </td>
                <td>
                  <code>{{
                    compactIdentifier(environment.currentVersion.image.digest)
                  }}</code>
                </td>
                <td>{{ environment.currentVersion.tools.length }}</td>
                <td>{{ environment.currentVersion.values.length }}</td>
                <td>
                  {{ environment.currentVersion.secretDescriptors.length }}
                </td>
                <td><StatusBadge :state="environment.state" /></td>
                <td>
                  <div class="environment-row-actions">
                    <RouterLink
                      class="icon-button"
                      :title="$t('runtime.openEditor')"
                      :aria-label="$t('runtime.openEditor')"
                      :to="`/projects/${encodeURIComponent(projectRef)}/environments/${encodeURIComponent(environment.ref)}`"
                    >
                      <ExternalLink :size="16" />
                    </RouterLink>
                    <button
                      v-if="hasEnvironmentAction(environment, 'DISABLE')"
                      class="icon-button"
                      type="button"
                      :disabled="actionRef === environment.ref"
                      :title="$t('common.disable')"
                      :aria-label="$t('common.disable')"
                      @click="setEnabled(environment, false)"
                    >
                      <Power :size="16" aria-hidden="true" />
                    </button>
                    <button
                      v-if="hasEnvironmentAction(environment, 'ENABLE')"
                      class="icon-button"
                      type="button"
                      :disabled="actionRef === environment.ref"
                      :title="$t('common.enable')"
                      :aria-label="$t('common.enable')"
                      @click="setEnabled(environment, true)"
                    >
                      <PowerOff :size="16" aria-hidden="true" />
                    </button>
                    <button
                      v-if="hasEnvironmentAction(environment, 'DELETE')"
                      class="icon-button icon-button--danger"
                      type="button"
                      :disabled="actionRef === environment.ref"
                      :title="$t('common.delete')"
                      :aria-label="$t('common.delete')"
                      @click="deleteTarget = environment"
                    >
                      <Trash2 :size="16" aria-hidden="true" />
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
          <p v-if="loadingMore" class="environment-loading" role="status">
            {{ $t("common.loading") }}
          </p>
          <button
            v-else-if="cursor"
            class="button environment-loading"
            type="button"
            @click="load(false)"
          >
            {{ $t("roleImages.loadMore") }}
          </button>
        </div>
        <aside v-if="selected" class="environment-inspector">
          <div class="section-header">
            <div>
              <h2>{{ selected.name }}</h2>
              <p>{{ selected.description }}</p>
            </div>
            <StatusBadge :state="selected.state" />
          </div>
          <dl>
            <div>
              <dt>{{ $t("runtime.revision") }}</dt>
              <dd>rev {{ selected.currentVersion.revision }}</dd>
            </div>
            <div>
              <dt>{{ $t("runtime.versionDigest") }}</dt>
              <dd>
                <code>{{
                  compactIdentifier(selected.currentVersion.digest)
                }}</code>
              </dd>
            </div>
            <div>
              <dt>{{ $t("runtime.variables") }}</dt>
              <dd>{{ selected.currentVersion.values.length }}</dd>
            </div>
            <div>
              <dt>{{ $t("runtime.secretDescriptors") }}</dt>
              <dd>{{ selected.currentVersion.secretDescriptors.length }}</dd>
            </div>
            <div>
              <dt>{{ $t("runtime.updatedAt") }}</dt>
              <dd>{{ new Date(selected.updatedAt).toLocaleString() }}</dd>
            </div>
            <div>
              <dt>{{ $t("runtime.exactImage") }}</dt>
              <dd>
                <code>{{
                  compactIdentifier(selected.currentVersion.image.digest)
                }}</code>
              </dd>
            </div>
          </dl>
          <section>
            <h3>{{ $t("runtime.verifiedTools") }}</h3>
            <div v-if="selected.currentVersion.tools.length" class="chip-list">
              <span
                v-for="tool in selected.currentVersion.tools"
                :key="tool.command"
                :title="tool.description"
              >
                {{ tool.name }} · <code>{{ tool.command }}</code>
              </span>
            </div>
            <p v-else>{{ $t("common.empty") }}</p>
          </section>
          <section>
            <h3>{{ $t("runtime.variableNames") }}</h3>
            <div v-if="selected.currentVersion.values.length" class="chip-list">
              <span
                v-for="item in selected.currentVersion.values"
                :key="item.name"
              >
                {{ item.name }}
              </span>
            </div>
            <p v-else>{{ $t("common.empty") }}</p>
          </section>
          <section class="environment-policy-readback">
            <h3>
              <ShieldCheck :size="17" aria-hidden="true" />
              {{ $t("runtime.effectivePolicyPreview") }}
            </h3>
            <dl>
              <div>
                <dt>{{ $t("runtime.resources") }}</dt>
                <dd>
                  {{
                    selected.currentVersion.policy.resources.cpuRequestMilli
                  }}/{{
                    selected.currentVersion.policy.resources.cpuLimitMilli
                  }}m CPU ·
                  {{
                    selected.currentVersion.policy.resources.memoryRequestMib
                  }}/{{
                    selected.currentVersion.policy.resources.memoryLimitMib
                  }}
                  MiB
                </dd>
              </div>
              <div>
                <dt>{{ $t("runtime.ephemeralVolumes") }}</dt>
                <dd>{{ selected.currentVersion.policy.volumes.length }}</dd>
              </div>
              <div>
                <dt>{{ $t("runtime.networkPolicy") }}</dt>
                <dd>
                  {{ selected.currentVersion.policy.network.egress.length }} ·
                  {{ $t("runtime.denyByDefault") }}
                </dd>
              </div>
              <div>
                <dt>{{ $t("runtime.kubernetesRbac") }}</dt>
                <dd>
                  {{ selected.currentVersion.policy.kubernetesAccess.kind }}
                </dd>
              </div>
            </dl>
          </section>
          <section class="environment-lifecycle">
            <h3>{{ $t("runtime.readiness") }}</h3>
            <p
              v-if="runtime.loading[`environment-readiness:${selected.ref}`]"
              role="status"
            >
              {{ $t("common.loading") }}
            </p>
            <ProblemNotice
              v-if="runtime.problems[`environment-readiness:${selected.ref}`]"
              :problem="
                runtime.problems[`environment-readiness:${selected.ref}`]
              "
              @retry="
                runtime.loadEnvironmentReadiness(
                  selected.ref,
                  inspectorController?.signal,
                )
              "
            />
            <dl>
              <div>
                <dt>{{ $t("common.status") }}</dt>
                <dd>
                  <StatusBadge
                    v-if="selectedReadiness"
                    :state="
                      selectedReadiness?.ready ? 'READY' : 'NEEDS_ATTENTION'
                    "
                  />
                  <span v-else>{{
                    $t("runtime.readinessState.UNAVAILABLE")
                  }}</span>
                </dd>
              </div>
              <div>
                <dt>{{ $t("agents.title") }}</dt>
                <dd>{{ selectedAgents.length }}</dd>
              </div>
            </dl>
            <p v-if="selectedReadiness?.blockers.length" class="secondary-text">
              {{ $t("runtime.readinessState.NEEDS_ATTENTION") }} ·
              {{ selectedReadiness.blockers.length }}
            </p>
            <div v-if="selectedAgents.length" class="chip-list">
              <span v-for="agent in selectedAgents" :key="agent.ref">
                {{ agent.name }}
              </span>
            </div>
            <ProblemNotice
              v-if="runtime.problems[`environment-agents:${selected.ref}`]"
              :problem="runtime.problems[`environment-agents:${selected.ref}`]"
              @retry="
                runtime.loadEnvironmentAgents(
                  selected.ref,
                  true,
                  inspectorController?.signal,
                )
              "
            />
            <button
              v-if="runtime.environmentAgentCursors[selected.ref]"
              class="button"
              type="button"
              :disabled="runtime.loading[`environment-agents:${selected.ref}`]"
              @click="
                runtime.loadEnvironmentAgents(
                  selected.ref,
                  false,
                  inspectorController?.signal,
                )
              "
            >
              {{ $t("roleImages.loadMore") }}
            </button>
          </section>
          <section>
            <h3>{{ $t("runtime.secretDescriptorNames") }}</h3>
            <div
              v-if="selected.currentVersion.secretDescriptors.length"
              class="chip-list"
            >
              <span
                v-for="item in selected.currentVersion.secretDescriptors"
                :key="item.name"
              >
                {{ item.name }}
              </span>
            </div>
            <p v-else>{{ $t("common.empty") }}</p>
          </section>
          <RouterLink
            class="button button--primary"
            :to="`/projects/${encodeURIComponent(projectRef)}/environments/${encodeURIComponent(selected.ref)}`"
          >
            {{ $t("runtime.openEditor") }}
          </RouterLink>
        </aside>
      </div>
    </component>
  </PageFrame>
  <ModalDialog
    v-if="deleteTarget"
    :title="`${t('common.delete')} «${deleteTarget.name}»?`"
    :busy="actionRef === deleteTarget.ref"
    size="md"
    @close="deleteTarget = undefined"
  >
    <div class="environment-delete-confirmation">
      <Trash2 :size="24" aria-hidden="true" />
      <div>
        <strong>{{ deleteTarget.name }}</strong>
        <p>{{ deleteTarget.description }}</p>
        <StatusBadge :state="deleteTarget.state" />
      </div>
    </div>
    <template #actions>
      <button
        class="button"
        type="button"
        :disabled="actionRef === deleteTarget.ref"
        @click="deleteTarget = undefined"
      >
        {{ t("common.cancel") }}
      </button>
      <button
        class="button button--danger"
        type="button"
        :disabled="actionRef === deleteTarget.ref"
        @click="remove(deleteTarget)"
      >
        <Trash2 :size="16" aria-hidden="true" />
        {{ t("common.delete") }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.environment-registry {
  padding: 0;
  overflow: hidden;
}
.environment-delete-confirmation {
  display: grid;
  grid-template-columns: 32px minmax(0, 1fr);
  align-items: start;
  gap: 12px;
}
.environment-delete-confirmation > svg {
  color: var(--danger);
}
.environment-delete-confirmation p {
  margin: 5px 0 10px;
  color: var(--text-secondary);
}
.environment-toolbar {
  display: flex;
  min-height: 58px;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
}
.environment-toolbar label {
  display: flex;
  width: min(460px, 100%);
  align-items: center;
  gap: 8px;
}
.environment-toolbar input {
  width: 100%;
}
.environment-toolbar > span {
  color: var(--text-secondary);
}
.environment-table-wrap {
  max-height: 526px;
  overflow: auto;
}
.environment-table-wrap--expanded {
  max-height: calc(100dvh - 220px);
}
.environment-table tbody tr {
  height: 80px;
}
.environment-registry__content {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
}
.environment-registry__content--selected {
  grid-template-columns: minmax(0, 1fr) minmax(300px, 0.38fr);
}
.environment-table {
  width: 100%;
  min-width: 1120px;
  border-collapse: collapse;
}
.environment-table th,
.environment-table td {
  padding: 12px 14px;
  border-bottom: 1px solid var(--hairline);
  text-align: left;
  vertical-align: middle;
}
.environment-table th {
  position: sticky;
  z-index: 1;
  top: 0;
  background: var(--panel);
  color: var(--text-secondary);
  font-size: 0.78rem;
  font-weight: 500;
}
.environment-table td:first-child {
  min-width: 280px;
}
.environment-table td:first-child > * {
  display: block;
}
.environment-table small {
  max-width: 520px;
  margin-top: 3px;
  overflow: hidden;
  color: var(--text-secondary);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.environment-table code,
.environment-inspector code {
  font-family: var(--font-mono);
  font-size: 0.76rem;
}
.environment-name {
  display: grid;
  width: 100%;
  gap: 3px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--text);
  text-align: left;
  cursor: pointer;
}
.environment-row-actions {
  display: flex;
  align-items: center;
  gap: 6px;
}
.environment-table__row--selected {
  box-shadow: inset 3px 0 var(--accent);
  background: var(--accent-soft);
}
.environment-inspector {
  display: grid;
  align-content: start;
  gap: 18px;
  padding: 16px;
  border-left: 1px solid var(--border);
  background: var(--surface);
}
.environment-inspector .section-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.environment-inspector .section-header p {
  margin-bottom: 0;
  color: var(--text-secondary);
}
.environment-inspector dl {
  margin: 0;
}
.environment-lifecycle h3 {
  margin-bottom: 8px;
}
.environment-lifecycle dl > div {
  align-items: center;
}
.environment-inspector dl > div {
  display: grid;
  grid-template-columns: minmax(100px, 0.55fr) minmax(0, 1fr);
  gap: 8px;
  padding: 9px 0;
  border-bottom: 1px solid var(--hairline);
}
.environment-inspector dt {
  color: var(--text-secondary);
}
.environment-inspector dd {
  margin: 0;
}
.chip-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.chip-list span {
  padding: 4px 7px;
  border-radius: 5px;
  background: var(--panel);
  font-family: var(--font-mono);
  font-size: 0.76rem;
}
.environment-state {
  display: grid;
  min-height: 360px;
  place-items: center;
  align-content: center;
  gap: 8px;
  padding: 24px;
  text-align: center;
}
.environment-state p {
  max-width: 500px;
  color: var(--text-secondary);
}
.environment-loading {
  padding: 12px;
  text-align: center;
}
.environment-loading--hint {
  color: var(--text-secondary);
}
.environment-policy-readback {
  padding: 12px 0;
  border-top: 1px solid var(--hairline);
}
.environment-policy-readback h3 {
  display: flex;
  align-items: center;
  gap: 7px;
}
.environment-policy-readback h3 svg {
  color: var(--accent-strong);
}
.environment-policy-readback dd {
  max-width: 210px;
  overflow-wrap: anywhere;
  text-align: right;
}
@media (max-width: 780px) {
  .environment-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .environment-table-wrap {
    overflow-x: auto;
  }
  .environment-registry__content {
    grid-template-columns: 1fr;
  }
  .environment-inspector {
    border-top: 1px solid var(--border);
    border-left: 0;
  }
  .environment-table {
    min-width: 760px;
  }
}
</style>
