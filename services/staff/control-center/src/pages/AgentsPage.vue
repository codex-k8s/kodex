<script setup lang="ts">
import { Plus, Sparkles } from "@lucide/vue";
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import AgentCatalog from "@/features/agents/catalog/AgentCatalog.vue";
import { openAssistantWorkspace } from "@/features/assistant/events";
import { useAgentCatalogStore } from "@/features/agents/catalog/store";
import { usePlatformStore } from "@/features/platform/store";
import { useRealtimeStore } from "@/features/realtime/store";
import AgentFormFields from "@/features/platform/AgentFormFields.vue";
import {
  isAgentDraftComplete,
  resolveAgentRuntimeRef,
} from "@/features/platform/agent-form";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const platform = usePlatformStore();
const realtime = useRealtimeStore();
const catalog = useAgentCatalogStore();
const route = useRoute();
const router = useRouter();
const projectRef = computed(() => String(route.params.projectRef));
const project = computed(() => platform.projects[projectRef.value]);
const canCreate = computed(() =>
  project.value?.nextActions.includes("CREATE_AGENT"),
);
const list = computed(() => catalog.items.filter((item) => !item.system));
const runtimes = computed(() =>
  Object.values(platform.runtimes).filter((item) => item.ready),
);
const catalogQuery = ref("");
const pageSize = ref(20);
const dialog = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const form = reactive({
  name: "",
  purpose: "",
  roleDescription: "",
  initialInstructions: "",
  runtimeRef: "",
});
const formReady = computed(
  () =>
    isAgentDraftComplete(form) &&
    runtimes.value.some((runtime) => runtime.ref === form.runtimeRef),
);
let searchTimer: number | undefined;

async function openDialog(): Promise<void> {
  if (!canCreate.value) return;
  if (runtimes.value.length === 0) await platform.loadRuntimes();
  form.runtimeRef ||= runtimes.value[0]?.ref ?? "";
  dialog.value = true;
}

async function submit(): Promise<void> {
  if (!canCreate.value) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const agent = await platform.saveAgent(projectRef.value, form);
    dialog.value = false;
    await router.push(`/projects/${projectRef.value}/agents/${agent.ref}`);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
function applyRealtimeCatalog(): void {
  if (catalogQuery.value.trim()) return;
  const snapshot = platform.realtimeSnapshot("AGENT", projectRef.value);
  if (!snapshot) {
    catalog.prepareRefresh();
    return;
  }
  catalog.applySnapshot(
    projectRef.value,
    Object.values(platform.agents).filter(
      (agent) => agent.projectRef === projectRef.value,
    ),
    snapshot.nextPageToken,
    pageSize.value,
  );
  if (route.query.create === "1") void openDialog();
}

watch(
  runtimes,
  (available) => {
    form.runtimeRef = resolveAgentRuntimeRef(
      form.runtimeRef,
      available.map((runtime) => runtime.ref),
    );
  },
  { immediate: true },
);

watch(catalogQuery, (value) => {
  if (searchTimer !== undefined) window.clearTimeout(searchTimer);
  if (!value.trim()) {
    applyRealtimeCatalog();
    return;
  }
  searchTimer = window.setTimeout(() => {
    void catalog.load(projectRef.value, value, false, pageSize.value);
  }, 500);
});

watch(
  () => [
    projectRef.value,
    platform.realtimeSnapshot("AGENT", projectRef.value)?.nextPageToken ?? "",
    Object.values(platform.agents)
      .filter((agent) => agent.projectRef === projectRef.value)
      .map((agent) => `${agent.ref}:${String(agent.version)}`)
      .sort()
      .join("|"),
  ],
  () => applyRealtimeCatalog(),
  { immediate: true, flush: "sync" },
);

function retryCatalog(): void {
  if (catalogQuery.value.trim())
    void catalog.load(
      projectRef.value,
      catalogQuery.value,
      false,
      pageSize.value,
    );
  else realtime.refreshSession();
}

onBeforeUnmount(() => {
  if (searchTimer !== undefined) window.clearTimeout(searchTimer);
  catalog.clear();
});
</script>

<template>
  <PageFrame :title="$t('agents.title')" :subtitle="$t('agents.subtitle')">
    <template #actions>
      <button
        v-if="canCreate"
        class="button"
        type="button"
        @click="openAssistantWorkspace"
      >
        <Sparkles :size="17" aria-hidden="true" />
        {{ $t("agents.createWithAssistant") }}
      </button>
      <button
        v-if="canCreate"
        class="button button--primary"
        type="button"
        @click="openDialog"
      >
        <Plus :size="17" aria-hidden="true" />
        {{ $t("agents.new") }}
      </button>
    </template>
    <AsyncState
      :loading="catalog.loading && list.length === 0"
      :problem="catalog.problem"
      :empty="list.length === 0 && !catalogQuery.trim()"
      :empty-title="$t('agents.emptyTitle')"
      @retry="retryCatalog"
    >
      <template #empty-action
        ><button
          v-if="canCreate"
          class="button button--primary"
          type="button"
          @click="openDialog"
        >
          <Plus :size="17" aria-hidden="true" />
          {{ $t("agents.new") }}
        </button></template
      >
      <AgentCatalog
        v-model:query="catalogQuery"
        v-model:page-size="pageSize"
        :agents="list"
        :project-ref="projectRef"
        :has-more="catalog.hasMore"
        :loading-more="catalog.loadingMore"
        @load-more="catalog.loadMore(pageSize)"
      />
    </AsyncState>
    <ModalDialog
      v-if="dialog"
      :title="$t('agents.new')"
      :busy="busy"
      @close="dialog = false"
      ><form
        id="agent-form"
        class="form-grid"
        :inert="busy"
        @submit.prevent="submit"
      >
        <AgentFormFields
          v-model:name="form.name"
          v-model:purpose="form.purpose"
          v-model:role-description="form.roleDescription"
          v-model:initial-instructions="form.initialInstructions"
          v-model:runtime-ref="form.runtimeRef"
          :runtimes="runtimes"
          :runtime-problem="platform.problems.runtimes"
          :disabled="busy"
        />
        <ProblemNotice
          v-if="problem"
          class="field--wide"
          :problem="problem"
          compact
        />
      </form>
      <template #actions
        ><button
          class="button"
          type="button"
          :disabled="busy"
          @click="dialog = false"
        >
          {{ $t("common.cancel") }}</button
        ><button
          class="button button--primary"
          form="agent-form"
          type="submit"
          :disabled="busy || !formReady"
        >
          <Plus :size="17" aria-hidden="true" />
          {{ $t("common.create") }}
        </button></template
      ></ModalDialog
    >
  </PageFrame>
</template>
