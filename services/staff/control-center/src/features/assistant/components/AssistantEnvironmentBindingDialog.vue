<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute } from "vue-router";

import { loadAgentCatalogPage } from "@/features/agents/catalog/api";
import {
  bindRuntimeEnvironment,
  loadAgentRuntime,
} from "@/features/agents/detail/runtime-api";
import { requestSignal } from "@/shared/api/client";
import {
  getAgent,
  getRuntimeEnvironmentSet,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Agent,
  AgentRuntimeConfigurationView,
  RuntimeEnvironmentSet,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem, unwrap } from "@/shared/api/problem";
import { readWithRetry } from "@/shared/api/read-retry";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const props = defineProps<{ projectRef: string; environmentRef: string }>();
const emit = defineEmits<{ close: []; bound: [agentName: string] }>();
const route = useRoute();
const controller = new AbortController();
const environment = ref<RuntimeEnvironmentSet>();
const selectedAgent = ref<Agent>();
const current = ref<AgentRuntimeConfigurationView>();
const loading = ref(true);
const loadingAgent = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const unavailable = ref(false);
const agentUnavailable = ref(false);
let selectionGeneration = 0;

function readEnvironment(): Promise<RuntimeEnvironmentSet> {
  return readWithRetry(
    async () =>
      (
        await unwrap(
          getRuntimeEnvironmentSet({
            path: { environmentRef: props.environmentRef },
            signal: requestSignal(controller.signal),
          }),
        )
      ).data,
    undefined,
    controller.signal,
  );
}

async function loadEnvironment(): Promise<void> {
  loading.value = true;
  problem.value = undefined;
  unavailable.value = false;
  if (route.params.projectRef !== props.projectRef) {
    unavailable.value = true;
    loading.value = false;
    return;
  }
  try {
    const value = await readEnvironment();
    if (controller.signal.aborted) return;
    if (
      value.ref !== props.environmentRef ||
      value.projectRef !== props.projectRef ||
      value.state !== "ACTIVE" ||
      !value.ready
    ) {
      unavailable.value = true;
      return;
    }
    environment.value = value;
  } catch (error) {
    if (!controller.signal.aborted) problem.value = asProblem(error);
  } finally {
    if (!controller.signal.aborted) loading.value = false;
  }
}

async function loadAgents(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  const page = await loadAgentCatalogPage(
    { projectRef: props.projectRef, query, pageToken: cursor, pageSize: 30 },
    signal,
  );
  return {
    items: page.items
      .filter(
        (agent) =>
          agent.projectRef === props.projectRef &&
          !agent.system &&
          agent.state !== "ARCHIVED" &&
          agent.nextActions.includes("EDIT"),
      )
      .map((agent) => ({
        ref: agent.ref,
        title: agent.name,
        description: agent.purpose,
        meta: agent.state,
      })),
    ...(page.nextPageToken ? { nextPageToken: page.nextPageToken } : {}),
  };
}

async function selectAgent(agentRef: string): Promise<void> {
  const generation = ++selectionGeneration;
  selectedAgent.value = undefined;
  current.value = undefined;
  problem.value = undefined;
  agentUnavailable.value = false;
  loadingAgent.value = false;
  if (!agentRef) return;
  loadingAgent.value = true;
  try {
    const agent = await readWithRetry(
      async () =>
        (
          await unwrap(
            getAgent({
              path: { agentRef },
              signal: requestSignal(controller.signal),
            }),
          )
        ).data,
      undefined,
      controller.signal,
    );
    if (controller.signal.aborted || generation !== selectionGeneration) return;
    if (
      agent.ref !== agentRef ||
      agent.projectRef !== props.projectRef ||
      agent.system ||
      agent.state === "ARCHIVED" ||
      !agent.nextActions.includes("EDIT")
    ) {
      agentUnavailable.value = true;
      return;
    }
    const view = await loadAgentRuntime(agentRef, controller.signal);
    // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- выбор может измениться во время await.
    if (controller.signal.aborted || generation !== selectionGeneration) return;
    if (
      view.environment.projectRef &&
      view.environment.projectRef !== props.projectRef
    ) {
      agentUnavailable.value = true;
      return;
    }
    selectedAgent.value = agent;
    current.value = view;
  } catch (error) {
    if (!controller.signal.aborted && generation === selectionGeneration)
      problem.value = asProblem(error);
  } finally {
    if (!controller.signal.aborted && generation === selectionGeneration)
      loadingAgent.value = false;
  }
}

async function bind(): Promise<void> {
  const agent = selectedAgent.value;
  const view = current.value;
  const target = environment.value;
  if (
    !agent ||
    !view ||
    !target ||
    target.projectRef !== props.projectRef ||
    route.params.projectRef !== props.projectRef ||
    !target.ready ||
    (view.environment.projectRef &&
      view.environment.projectRef !== props.projectRef) ||
    view.environment.ref === target.ref ||
    busy.value
  )
    return;
  busy.value = true;
  problem.value = undefined;
  try {
    const fresh = await readEnvironment();
    if (
      fresh.ref !== target.ref ||
      fresh.version !== target.version ||
      fresh.projectRef !== props.projectRef ||
      fresh.state !== "ACTIVE" ||
      !fresh.ready
    ) {
      unavailable.value = true;
      return;
    }
    const result = await bindRuntimeEnvironment(
      agent.ref,
      target.ref,
      view.agentVersion,
    );
    if (
      result.environment.ref !== target.ref ||
      result.environment.projectRef !== props.projectRef ||
      result.environmentBinding.agentRef !== agent.ref ||
      result.environmentBinding.environmentRef !== target.ref
    ) {
      unavailable.value = true;
      return;
    }
    emit("bound", agent.name);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

onMounted(() => void loadEnvironment());
onBeforeUnmount(() => controller.abort());
</script>

<template>
  <ModalDialog
    :title="$t('assistant.environmentDraft.bindTitle')"
    :busy="busy"
    size="lg"
    @close="emit('close')"
  >
    <ProblemNotice v-if="problem" :problem="problem" compact />
    <p v-if="loading">{{ $t("common.loading") }}</p>
    <p v-else-if="unavailable" role="alert">
      {{ $t("assistant.environmentDraft.bindUnavailable") }}
    </p>
    <template v-else-if="environment">
      <p>{{ $t("assistant.environmentDraft.bindExplanation") }}</p>
      <dl class="assistant-environment-binding__preview">
        <div>
          <dt>{{ $t("assistant.environmentDraft.targetEnvironment") }}</dt>
          <dd>{{ environment.name }}</dd>
        </div>
        <div v-if="current">
          <dt>{{ $t("assistant.environmentDraft.currentEnvironment") }}</dt>
          <dd>{{ current.environment.name }}</dd>
        </div>
      </dl>
      <label class="field">
        <span>{{ $t("assistant.environmentDraft.chooseAgent") }}</span>
        <AsyncEntityPicker
          :model-value="selectedAgent?.ref ?? ''"
          :load-page="loadAgents"
          :disabled="busy || loadingAgent"
          :placeholder="$t('assistant.environmentDraft.chooseAgent')"
          :search-placeholder="$t('assistant.environmentDraft.searchAgent')"
          @update:model-value="
            void selectAgent(typeof $event === 'string' ? $event : '')
          "
        />
      </label>
      <p v-if="loadingAgent">{{ $t("common.loading") }}</p>
      <p v-if="agentUnavailable" role="alert">
        {{ $t("assistant.environmentDraft.agentUnavailable") }}
      </p>
    </template>
    <template #actions>
      <button
        class="button"
        type="button"
        :disabled="busy"
        @click="emit('close')"
      >
        {{ $t("common.cancel") }}
      </button>
      <button
        class="button button--primary"
        type="button"
        :disabled="
          busy ||
          loading ||
          loadingAgent ||
          unavailable ||
          !environment ||
          !selectedAgent ||
          !current ||
          current.environment.ref === environment.ref
        "
        @click="bind"
      >
        {{ $t("assistant.environmentDraft.bind") }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.assistant-environment-binding__preview {
  display: grid;
  gap: 8px;
}
.assistant-environment-binding__preview div {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
  gap: 8px;
}
.assistant-environment-binding__preview dd {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
