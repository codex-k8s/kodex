<script setup lang="ts">
import { Layers3, Save, Search } from "@lucide/vue";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import { agentDetailCopy } from "@/features/agents/detail/copy";
import type { ApplyBoundary } from "@/features/agents/detail/model";
import {
  bindRuntimeEnvironment,
  loadAgentRuntime,
  searchRuntimeEnvironments,
} from "@/features/agents/detail/runtime-api";
import type { RuntimeEnvironmentSet } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  agentRef: string;
  projectRef: string;
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
const view = ref<Awaited<ReturnType<typeof loadAgentRuntime>>>();
const selectedEnvironment = ref("");
const selectedCandidate = ref<AsyncEntityOption>();
const busy = ref(false);
const loading = ref(false);
const problem = ref<AppProblem>();
const dirty = computed(
  () =>
    Boolean(selectedEnvironment.value) &&
    selectedEnvironment.value !== view.value?.environment.ref,
);

function notify(state: "APPLIED" | "DRAFT" | "RUNNING" | "FAILED"): void {
  emit("apply-state", state, copy.value.environment.catalog, "next-turn");
}

function environmentOption(value: RuntimeEnvironmentSet): AsyncEntityOption {
  return {
    ref: value.ref,
    title: value.name,
    description: [
      value.description,
      `rev ${String(value.currentVersion.revision)}`,
    ]
      .filter(Boolean)
      .join(" · "),
    meta: value.state,
  };
}

const selectedOption = computed<AsyncEntityOption | undefined>(() => {
  if (selectedCandidate.value?.ref === selectedEnvironment.value)
    return selectedCandidate.value;
  const environment = view.value?.environment;
  return environment?.ref === selectedEnvironment.value
    ? environmentOption(environment)
    : undefined;
});

function sync(): void {
  if (!view.value) return;
  selectedEnvironment.value = view.value.environment.ref;
  selectedCandidate.value = environmentOption(view.value.environment);
  notify("APPLIED");
}

async function load(): Promise<void> {
  loading.value = true;
  problem.value = undefined;
  try {
    view.value = await loadAgentRuntime(props.agentRef);
    sync();
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    loading.value = false;
  }
}

async function loadEnvironmentPage(
  query: string,
  cursor?: string,
): Promise<AsyncEntityOptionPage> {
  const page = await searchRuntimeEnvironments(props.projectRef, query, cursor);
  return {
    items: page.items.map(environmentOption),
    ...(page.nextPageToken ? { nextPageToken: page.nextPageToken } : {}),
  };
}

function select(value: string | null | readonly string[]): void {
  if (typeof value !== "string") return;
  selectedEnvironment.value = value;
  notify(value === view.value?.environment.ref ? "APPLIED" : "DRAFT");
}

function selectOption(value: AsyncEntityOption): void {
  selectedCandidate.value = value;
}

async function bind(): Promise<void> {
  const current = view.value;
  if (!current || !props.canEdit || !dirty.value) return;
  busy.value = true;
  problem.value = undefined;
  notify("RUNNING");
  try {
    view.value = await bindRuntimeEnvironment(
      props.agentRef,
      selectedEnvironment.value,
      current.agentVersion,
    );
    sync();
  } catch (error) {
    problem.value = asProblem(error);
    notify("FAILED");
  } finally {
    busy.value = false;
  }
}

onMounted(() => void load());
</script>

<template>
  <AsyncState :loading="loading" :problem="problem" @retry="load">
    <div v-if="view" class="environment-layout">
      <article class="environment-current panel">
        <div class="environment-current__head">
          <div>
            <h2>{{ copy.environment.current }}</h2>
            <p>{{ view.environment.description }}</p>
          </div>
          <StatusBadge :state="view.environment.state" />
        </div>
        <div class="environment-current__identity">
          <span class="environment-current__icon">
            <Layers3 :size="21" aria-hidden="true" />
          </span>
          <div>
            <h3>{{ view.environment.name }}</h3>
            <code>{{ view.environment.ref }}</code>
          </div>
        </div>
        <dl class="environment-current__meta">
          <div>
            <dt>
              {{ $t("common.version", { version: view.environment.version }) }}
            </dt>
            <dd>rev {{ view.environment.currentVersion.revision }}</dd>
          </div>
          <div>
            <dt>{{ copy.environment.values }}</dt>
            <dd>{{ view.environment.currentVersion.values.length }}</dd>
          </div>
          <div>
            <dt>{{ copy.environment.secrets }}</dt>
            <dd>
              {{ view.environment.currentVersion.secretDescriptors.length }}
            </dd>
          </div>
        </dl>
      </article>

      <article class="environment-catalog panel">
        <div class="environment-catalog__head">
          <div>
            <h2>{{ copy.environment.catalog }}</h2>
            <p>{{ copy.environment.serverSearch }}</p>
          </div>
          <Search :size="19" aria-hidden="true" />
        </div>
        <AsyncEntityPicker
          :model-value="selectedEnvironment"
          :selected="selectedOption"
          :load-page="loadEnvironmentPage"
          :placeholder="copy.environment.choose"
          :search-placeholder="copy.environment.choose"
          :disabled="!canEdit || busy"
          @update:model-value="select"
          @select="selectOption"
        />
        <div class="environment-catalog__selection">
          <div>
            <span>{{ $t("common.selected") }}</span>
            <strong>{{ selectedOption?.title ?? selectedEnvironment }}</strong>
          </div>
          <button
            class="button button--primary"
            type="button"
            :disabled="!canEdit || busy || !dirty"
            @click="bind"
          >
            <Save :size="16" aria-hidden="true" />{{ copy.environment.bind }}
          </button>
        </div>
      </article>
    </div>
  </AsyncState>
</template>

<style scoped>
.environment-layout {
  display: grid;
  grid-template-columns: minmax(300px, 0.75fr) minmax(0, 1.25fr);
  gap: 16px;
  align-items: start;
}
.environment-current,
.environment-catalog {
  display: grid;
  gap: 14px;
}
.environment-current__head,
.environment-catalog__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.environment-current h2,
.environment-current h3,
.environment-current p,
.environment-catalog h2,
.environment-catalog p {
  margin: 0;
}
.environment-current__head p,
.environment-catalog__head p {
  margin-top: 4px;
  color: var(--muted);
  font-size: 0.8rem;
}
.environment-catalog__head > svg {
  color: var(--accent-strong);
}
.environment-current__identity {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 13px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.environment-current__icon {
  display: inline-grid;
  flex: 0 0 auto;
  place-items: center;
  width: 38px;
  height: 38px;
  border: 1px solid color-mix(in srgb, var(--accent) 25%, var(--border));
  border-radius: 7px;
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.environment-current__identity code {
  display: block;
  margin-top: 5px;
  color: var(--muted);
  font-family: var(--font-mono);
  font-size: 0.72rem;
  overflow-wrap: anywhere;
}
.environment-current__meta {
  display: grid;
  gap: 8px;
  margin: 0;
}
.environment-current__meta div {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--hairline);
}
.environment-current__meta dt {
  color: var(--muted);
}
.environment-current__meta dd {
  margin: 0;
  font-family: var(--font-mono);
}
.environment-catalog__selection {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.environment-catalog__selection div {
  display: grid;
  min-width: 0;
  gap: 3px;
}
.environment-catalog__selection span {
  color: var(--subtle);
  font-size: 0.72rem;
}
.environment-catalog__selection strong {
  overflow-wrap: anywhere;
}
@media (max-width: 900px) {
  .environment-layout {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 640px) {
  .environment-catalog__selection {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
