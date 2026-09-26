<script setup lang="ts">
import { loadAgentCatalogPage } from "@/features/agents/catalog/api";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import { computed, reactive, ref, useId, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import OrganizationCatalog from "@/features/catalogs/OrganizationCatalog.vue";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
const platform = usePlatformStore();
const route = useRoute();
const router = useRouter();
const fieldPrefix = `workflow-create-${useId()}`;
const projectRef = computed(() => String(route.params.projectRef));
const project = computed(() => platform.projects[projectRef.value]);
const canCreate = computed(() =>
  project.value?.nextActions.includes("CREATE_WORKFLOW"),
);
const dialog = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const selectedCoordinator = ref<AsyncEntityOption>();
const form = reactive({ name: "", purpose: "", coordinatorAgentRef: "" });
const canSubmit = computed(
  () =>
    Boolean(form.name.trim()) &&
    Boolean(form.purpose.trim()) &&
    Boolean(form.coordinatorAgentRef) &&
    !busy.value,
);

async function loadCoordinatorAgents(
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
  pageSize = 40,
): Promise<AsyncEntityOptionPage> {
  const page = await loadAgentCatalogPage(
    { projectRef: projectRef.value, query, pageToken, pageSize },
    signal,
  );
  return {
    items: page.items
      .filter((agent) => !agent.system)
      .map((agent) => ({
        ref: agent.ref,
        title: agent.name,
        description: agent.purpose,
      })),
    nextPageToken: page.nextPageToken,
  };
}

function selectCoordinator(option: AsyncEntityOption): void {
  form.coordinatorAgentRef = option.ref;
  selectedCoordinator.value = option;
}

function clearCoordinator(): void {
  form.coordinatorAgentRef = "";
  selectedCoordinator.value = undefined;
}
async function submit() {
  if (!canCreate.value) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const workflow = await platform.saveWorkflow(projectRef.value, form);
    dialog.value = false;
    await router.push(
      `/projects/${projectRef.value}/workflows/${workflow.ref}`,
    );
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
async function load(): Promise<void> {
  await platform.loadProject(projectRef.value);
  if (route.query.create === "1" && canCreate.value) dialog.value = true;
}

watch(
  projectRef,
  () => {
    dialog.value = false;
    void load();
  },
  { immediate: true },
);
</script>
<template>
  <PageFrame :title="$t('workflows.title')" :subtitle="$t('workflows.subtitle')"
    ><template #actions
      ><button
        v-if="canCreate"
        class="button button--primary"
        type="button"
        @click="dialog = true"
      >
        {{ $t("workflows.new") }}
      </button></template
    ><OrganizationCatalog kind="workflows" :project-ref="projectRef" />
    <ModalDialog
      v-if="dialog"
      :title="$t('workflows.new')"
      :busy="busy"
      @close="dialog = false"
      ><form
        id="workflow-form"
        class="form-grid"
        :inert="busy"
        @submit.prevent="submit"
      >
        <label class="field field--wide"
          ><span>{{ $t("common.name") }}</span
          ><input
            v-model.trim="form.name"
            :id="`${fieldPrefix}-name`"
            :name="`${fieldPrefix}-name`"
            required
            maxlength="160" /></label
        ><label class="field field--wide"
          ><span>{{ $t("common.purpose") }}</span
          ><VoiceTextarea
            v-model.trim="form.purpose"
            :disabled="busy"
            required
            maxlength="1000"
        /></label>
        <div class="field field--wide">
          <span>{{ $t("workflows.coordinator") }}</span>
          <AsyncEntityPicker
            :model-value="form.coordinatorAgentRef || null"
            :selected="selectedCoordinator"
            :load-page="loadCoordinatorAgents"
            :context-key="projectRef"
            :disabled="busy"
            :trigger-label="$t('workflows.coordinator')"
            :placeholder="$t('workflows.selectCoordinator')"
            :search-placeholder="$t('workflows.searchCoordinator')"
            @select="selectCoordinator($event)"
            @update:model-value="$event === null && clearCoordinator()"
          />
        </div>
        <ProblemNotice
          v-if="problem"
          class="field--wide"
          :problem="problem"
          compact
        />
      </form>
      <template #actions
        ><button class="button" type="button" @click="dialog = false">
          {{ $t("common.cancel") }}</button
        ><button
          class="button button--primary"
          form="workflow-form"
          type="submit"
          :disabled="!canSubmit"
        >
          {{ $t("common.create") }}
        </button></template
      ></ModalDialog
    ></PageFrame
  >
</template>
