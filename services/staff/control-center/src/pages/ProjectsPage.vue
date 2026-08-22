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
const dialog = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const form = reactive({ name: "", purpose: "", language: "ru" as "ru" | "en" });
const filtered = computed(() => {
  const query =
    typeof route.query.q === "string" ? route.query.q.toLowerCase() : "";
  return platform.projectList.filter(
    (item) =>
      !query || `${item.name} ${item.purpose}`.toLowerCase().includes(query),
  );
});

async function submit(): Promise<void> {
  busy.value = true;
  problem.value = undefined;
  try {
    const project = await platform.saveProject(form);
    dialog.value = false;
    await router.push(`/projects/${project.ref}`);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

onMounted(() => void platform.loadProjects());
</script>

<template>
  <PageFrame :title="$t('projects.title')" :subtitle="$t('projects.subtitle')">
    <template #actions
      ><button
        class="button button--primary"
        type="button"
        @click="dialog = true"
      >
        {{ $t("projects.new") }}
      </button></template
    >
    <AsyncState
      :loading="platform.loading.projects"
      :problem="platform.problems.projects"
      :empty="filtered.length === 0"
      :empty-title="$t('projects.emptyTitle')"
      :empty-text="$t('projects.emptyText')"
      @retry="platform.loadProjects()"
    >
      <template #empty-action
        ><button
          class="button button--primary"
          type="button"
          @click="dialog = true"
        >
          {{ $t("projects.new") }}
        </button></template
      >
      <div class="card-grid">
        <RouterLink
          v-for="project in filtered"
          :key="project.ref"
          :to="`/projects/${project.ref}`"
          class="project-card card"
        >
          <div class="project-card__header">
            <h2>{{ project.name }}</h2>
            <StatusBadge :state="project.lifecycle" />
          </div>
          <p>{{ project.purpose }}</p>
          <dl>
            <div>
              <dt>{{ $t("project.agents") }}</dt>
              <dd>{{ project.agentCount }}</dd>
            </div>
            <div>
              <dt>{{ $t("project.workflows") }}</dt>
              <dd>{{ project.workflowCount }}</dd>
            </div>
            <div>
              <dt>{{ $t("project.activeRuns") }}</dt>
              <dd>{{ project.activeRunCount }}</dd>
            </div>
          </dl>
        </RouterLink>
      </div>
    </AsyncState>
    <ModalDialog
      v-if="dialog"
      :title="$t('projects.new')"
      :busy="busy"
      @close="dialog = false"
    >
      <form id="project-form" class="form-grid" @submit.prevent="submit">
        <label class="field field--wide"
          ><span>{{ $t("common.name") }}</span
          ><input v-model.trim="form.name" required maxlength="120" autofocus
        /></label>
        <label class="field field--wide"
          ><span>{{ $t("common.purpose") }}</span
          ><textarea v-model.trim="form.purpose" required maxlength="1000" />
        </label>
        <label class="field"
          ><span>{{ $t("projects.language") }}</span
          ><select v-model="form.language">
            <option value="ru">{{ $t("common.russian") }}</option>
            <option value="en">{{ $t("common.english") }}</option>
          </select></label
        >
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
          form="project-form"
          :disabled="busy"
          type="submit"
        >
          {{ $t("common.create") }}
        </button></template
      >
    </ModalDialog>
  </PageFrame>
</template>

<style scoped>
.project-card {
  color: inherit;
  text-decoration: none;
}
.project-card:hover {
  border-color: #b7c3cf;
}
.project-card__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.project-card p {
  min-height: 48px;
  color: var(--muted);
}
.project-card dl {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 8px;
  margin: 18px 0 0;
}
.project-card dl div {
  display: grid;
  gap: 3px;
}
.project-card dt {
  color: var(--subtle);
  font-size: 0.78rem;
}
.project-card dd {
  margin: 0;
  font-weight: 600;
}
</style>
