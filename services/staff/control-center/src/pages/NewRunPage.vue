<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
const platform = usePlatformStore();
const route = useRoute();
const router = useRouter();
const projectRef = computed(() => String(route.params.projectRef));
const project = computed(() => platform.projects[projectRef.value]);
const canLaunch = computed(() =>
  project.value?.nextActions.includes("CREATE_RUN"),
);
const busy = ref(false);
const problem = ref<AppProblem>();
const form = reactive({
  targetType: "AGENT" as "AGENT" | "WORKFLOW",
  targetRef: "",
  title: "",
  task: "",
});
const targets = computed(() =>
  form.targetType === "AGENT"
    ? Object.values(platform.agents).filter(
        (i) => i.projectRef === projectRef.value && i.enabled && !i.system,
      )
    : Object.values(platform.workflows).filter(
        (i) => i.projectRef === projectRef.value && i.state === "PUBLISHED",
      ),
);
async function submit() {
  if (!canLaunch.value || busy.value || !form.targetRef) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const run = await platform.launch({
      projectRef: projectRef.value,
      ...form,
    });
    await router.push(`/runs/${run.ref}`);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
async function load(): Promise<void> {
  await Promise.all([
    platform.loadAgents(projectRef.value),
    platform.loadWorkflows(projectRef.value),
    platform.loadArtifacts(projectRef.value),
    platform.loadProject(projectRef.value),
  ]);
}
onMounted(() => void load());
</script>
<template>
  <PageFrame :title="$t('runs.new')" :subtitle="$t('runs.subtitle')">
    <AsyncState
      :loading="
        platform.loading.project ||
        platform.loading.agents ||
        platform.loading.workflows
      "
      :problem="
        platform.problems.project ??
        platform.problems.agents ??
        platform.problems.workflows
      "
      @retry="load"
    >
      <form v-if="canLaunch" class="new-run-layout" @submit.prevent="submit">
        <section class="panel form-grid">
          <label class="field"
            ><span>{{ $t("runs.targetType") }}</span
            ><select v-model="form.targetType" @change="form.targetRef = ''">
              <option value="AGENT">{{ $t("runs.agent") }}</option>
              <option value="WORKFLOW">{{ $t("runs.workflow") }}</option>
            </select></label
          ><label class="field"
            ><span>{{ $t("common.target") }}</span
            ><select v-model="form.targetRef" required>
              <option value="" disabled>{{ $t("common.noData") }}</option>
              <option
                v-for="target in targets"
                :key="target.ref"
                :value="target.ref"
              >
                {{ target.name }}
              </option>
            </select></label
          ><label class="field field--wide"
            ><span>{{ $t("runs.runTitle") }}</span
            ><input v-model.trim="form.title" required maxlength="160" /></label
          ><label class="field field--wide"
            ><span>{{ $t("runs.task") }}</span
            ><textarea v-model.trim="form.task" required maxlength="8000" />
          </label>
          <details class="field--wide">
            <summary>{{ $t("common.advanced") }}</summary>
            <p>{{ $t("integrations.masked") }}</p>
          </details>
          <ProblemNotice
            v-if="problem"
            class="field--wide"
            :problem="problem"
            compact
          />
          <div class="field--wide form-actions">
            <RouterLink class="button" :to="`/projects/${projectRef}`">{{
              $t("common.cancel")
            }}</RouterLink
            ><button
              class="button button--primary button--large"
              type="submit"
              :disabled="busy || !form.targetRef"
            >
              {{ $t("common.launch") }}
            </button>
          </div>
        </section>
      </form>
      <section v-else class="empty-state" role="status">
        <h2>{{ $t("common.forbidden") }}</h2>
        <p>{{ $t("common.forbiddenText") }}</p>
      </section>
    </AsyncState>
  </PageFrame>
</template>
<style scoped>
.new-run-layout {
  max-width: 820px;
}
.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 9px;
  margin-top: 8px;
}
</style>
