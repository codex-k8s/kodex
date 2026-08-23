<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
const platform = usePlatformStore();
const route = useRoute();
const router = useRouter();
const projectRef = computed(() => String(route.params.projectRef));
const project = computed(() => platform.projects[projectRef.value]);
const canCreate = computed(() =>
  project.value?.nextActions.includes("CREATE_WORKFLOW"),
);
const list = computed(() =>
  Object.values(platform.workflows).filter(
    (i) => i.projectRef === projectRef.value,
  ),
);
const agentList = computed(() =>
  Object.values(platform.agents).filter(
    (i) => i.projectRef === projectRef.value && !i.system,
  ),
);
const dialog = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const form = reactive({ name: "", purpose: "", coordinatorAgentRef: "" });
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
  await Promise.all([
    platform.loadWorkflows(projectRef.value),
    platform.loadAgents(projectRef.value),
    platform.loadProject(projectRef.value),
  ]);
  if (
    route.query.create === "1" &&
    canCreate.value &&
    agentList.value.length > 0
  )
    dialog.value = true;
}

onMounted(() => void load());
</script>
<template>
  <PageFrame :title="$t('workflows.title')" :subtitle="$t('workflows.subtitle')"
    ><template #actions
      ><button
        v-if="canCreate"
        class="button button--primary"
        type="button"
        :disabled="!agentList.length"
        @click="dialog = true"
      >
        {{ $t("workflows.new") }}
      </button></template
    ><AsyncState
      :loading="platform.loading.workflows"
      :problem="platform.problems.workflows"
      :empty="list.length === 0"
      :empty-title="$t('workflows.emptyTitle')"
      @retry="platform.loadWorkflows(projectRef)"
      ><template #empty-action
        ><button
          v-if="canCreate"
          class="button button--primary"
          type="button"
          :disabled="!agentList.length"
          @click="dialog = true"
        >
          {{ $t("workflows.new") }}
        </button></template
      >
      <div class="entity-list">
        <RouterLink
          v-for="workflow in list"
          :key="workflow.ref"
          :to="`/projects/${projectRef}/workflows/${workflow.ref}`"
          class="entity-row"
          ><div>
            <h3>{{ workflow.name }}</h3>
            <p>{{ workflow.purpose }}</p>
          </div>
          <StatusBadge :state="workflow.state" /><span
            >{{ workflow.steps.length }} · {{ $t("workflows.steps") }}</span
          ></RouterLink
        >
      </div></AsyncState
    ><ModalDialog
      v-if="dialog"
      :title="$t('workflows.new')"
      :busy="busy"
      @close="dialog = false"
      ><form id="workflow-form" class="form-grid" @submit.prevent="submit">
        <label class="field field--wide"
          ><span>{{ $t("common.name") }}</span
          ><input v-model.trim="form.name" required maxlength="160" /></label
        ><label class="field field--wide"
          ><span>{{ $t("common.purpose") }}</span
          ><textarea
            v-model.trim="form.purpose"
            required
            maxlength="1000"
          /></label
        ><label class="field field--wide"
          ><span>{{ $t("workflows.coordinator") }}</span
          ><select v-model="form.coordinatorAgentRef" required>
            <option value="" disabled>{{ $t("common.noData") }}</option>
            <option
              v-for="agent in agentList"
              :key="agent.ref"
              :value="agent.ref"
            >
              {{ agent.name }}
            </option>
          </select></label
        ><ProblemNotice
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
          :disabled="busy"
        >
          {{ $t("common.create") }}
        </button></template
      ></ModalDialog
    ></PageFrame
  >
</template>
