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
const list = computed(() =>
  Object.values(platform.agents).filter(
    (item) => item.projectRef === projectRef.value && !item.system,
  ),
);
const dialog = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const form = reactive({
  name: "",
  purpose: "",
  roleDescription: "",
  initialInstructions: "",
});

async function submit(): Promise<void> {
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
onMounted(() => void platform.loadAgents(projectRef.value));
</script>

<template>
  <PageFrame :title="$t('agents.title')" :subtitle="$t('agents.subtitle')">
    <template #actions
      ><button
        class="button button--primary"
        type="button"
        @click="dialog = true"
      >
        {{ $t("agents.new") }}
      </button></template
    >
    <AsyncState
      :loading="platform.loading.agents"
      :problem="platform.problems.agents"
      :empty="list.length === 0"
      :empty-title="$t('agents.emptyTitle')"
      @retry="platform.loadAgents(projectRef)"
    >
      <template #empty-action
        ><button
          class="button button--primary"
          type="button"
          @click="dialog = true"
        >
          {{ $t("agents.new") }}
        </button></template
      >
      <div class="entity-list">
        <RouterLink
          v-for="agent in list"
          :key="agent.ref"
          :to="`/projects/${projectRef}/agents/${agent.ref}`"
          class="entity-row"
          ><div>
            <h3>{{ agent.name }}</h3>
            <p>{{ agent.purpose }}</p>
          </div>
          <StatusBadge :state="agent.state" /><span>{{
            agent.currentActivity ?? agent.roleDescription
          }}</span></RouterLink
        >
      </div>
    </AsyncState>
    <ModalDialog
      v-if="dialog"
      :title="$t('agents.new')"
      :busy="busy"
      @close="dialog = false"
      ><form id="agent-form" class="form-grid" @submit.prevent="submit">
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
          ><textarea
            v-model.trim="form.roleDescription"
            required
            maxlength="1000"
          /></label
        ><label class="field field--wide"
          ><span>{{ $t("agents.instructions") }}</span
          ><textarea
            v-model.trim="form.initialInstructions"
            required
            maxlength="65536"
          /></label
        ><ProblemNotice
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
          :disabled="busy"
        >
          {{ $t("common.create") }}
        </button></template
      ></ModalDialog
    >
  </PageFrame>
</template>
