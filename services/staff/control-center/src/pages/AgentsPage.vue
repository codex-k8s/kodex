<script setup lang="ts">
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import { Plus } from "@lucide/vue";
import {
  computed,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from "vue";
import { useRoute, useRouter } from "vue-router";

import AgentCatalog from "@/features/agents/catalog/AgentCatalog.vue";
import {
  parseAgentCatalogView,
  type AgentCatalogView,
} from "@/features/agents/catalog/model";
import { useAgentCatalogStore } from "@/features/agents/catalog/store";
import { usePlatformStore } from "@/features/platform/store";
import {
  isAgentDraftComplete,
  resolveAgentRuntimeRef,
} from "@/features/platform/agent-form";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const catalogViewStorageKey = "kodex.agents.catalog.view";

const platform = usePlatformStore();
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
const catalogView = ref<AgentCatalogView>("grid");
const catalogQuery = ref("");
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
const formReady = computed(() => isAgentDraftComplete(form));
let searchTimer: number | undefined;

function openDialog(): void {
  if (!canCreate.value) return;
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
async function load(): Promise<void> {
  await Promise.all([
    platform.loadProject(projectRef.value),
    catalog.load(projectRef.value, catalogQuery.value),
    platform.loadRuntimes(),
  ]);
  if (route.query.create === "1") openDialog();
}

onMounted(() => void load());

onMounted(() => {
  try {
    catalogView.value = parseAgentCatalogView(
      window.localStorage.getItem(catalogViewStorageKey),
    );
  } catch {
    catalogView.value = "grid";
  }
});

watch(catalogView, (value) => {
  try {
    window.localStorage.setItem(catalogViewStorageKey, value);
  } catch {
    // Выбор вида остаётся рабочим в текущей сессии без localStorage.
  }
});

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
  searchTimer = window.setTimeout(() => {
    void catalog.load(projectRef.value, value);
  }, 500);
});

onBeforeUnmount(() => {
  if (searchTimer !== undefined) window.clearTimeout(searchTimer);
  catalog.clear();
});
</script>

<template>
  <PageFrame :title="$t('agents.title')" :subtitle="$t('agents.subtitle')">
    <template #actions
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
    <AsyncState
      :loading="catalog.loading"
      :problem="catalog.problem"
      :empty="list.length === 0"
      :empty-title="$t('agents.emptyTitle')"
      @retry="catalog.load(projectRef, catalogQuery)"
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
        v-model:view="catalogView"
        :agents="list"
        :project-ref="projectRef"
        :has-more="catalog.hasMore"
        :loading-more="catalog.loadingMore"
        @load-more="catalog.loadMore"
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
        <label class="field"
          ><span>{{ $t("common.name") }}</span
          ><input v-model.trim="form.name" required maxlength="120" /></label
        ><label class="field"
          ><span>{{ $t("common.purpose") }}</span
          ><input
            v-model.trim="form.purpose"
            required
            maxlength="1000" /></label
        ><label class="field field--wide"
          ><span>{{ $t("agents.role") }}</span
          ><VoiceTextarea
            v-model.trim="form.roleDescription"
            :disabled="busy"
            required
            maxlength="1000" /></label
        ><label class="field field--wide"
          ><span>{{ $t("agents.instructions") }}</span
          ><VoiceTextarea
            v-model.trim="form.initialInstructions"
            :disabled="busy"
            required
            maxlength="65536"
          />
        </label>
        <details class="field--wide advanced-settings">
          <summary>{{ $t("common.advanced") }}</summary>
          <label class="field"
            ><span>{{ $t("agents.runtime") }}</span
            ><select v-model="form.runtimeRef" required>
              <option
                v-for="runtime in runtimes"
                :key="runtime.ref"
                :value="runtime.ref"
              >
                {{ runtime.name }}
              </option>
            </select>
            <small>{{ $t("agents.runtimeHelp") }}</small></label
          >
          <ProblemNotice
            v-if="platform.problems.runtimes"
            :problem="platform.problems.runtimes"
            compact
          />
        </details>
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

<style scoped>
.advanced-settings {
  display: grid;
  gap: 12px;
}
.advanced-settings summary {
  cursor: pointer;
}
.advanced-settings .field {
  margin-top: 12px;
}
</style>
