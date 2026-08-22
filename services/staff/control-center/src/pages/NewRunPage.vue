<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
const platform = usePlatformStore();
const route = useRoute();
const router = useRouter();
const projectRef = computed(() => String(route.params.projectRef));
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
onMounted(
  () =>
    void Promise.all([
      platform.loadAgents(projectRef.value),
      platform.loadWorkflows(projectRef.value),
      platform.loadArtifacts(projectRef.value),
    ]),
);
</script>
<template>
  <PageFrame :title="$t('runs.new')" :subtitle="$t('runs.subtitle')"
    ><form class="new-run-layout" @submit.prevent="submit">
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
    </form></PageFrame
  >
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
