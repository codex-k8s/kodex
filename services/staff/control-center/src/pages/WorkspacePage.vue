<script setup lang="ts">
import { Pencil, RefreshCw, Trash2 } from "@lucide/vue";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import { useProjectsStore } from "@/features/projects/store";
import WorkspaceResourceLifecycle from "@/features/workspace-resources/WorkspaceResourceLifecycle.vue";
import { useWorkspaceResourcesStore } from "@/features/workspace-resources/store";
import WorkspaceTeamPanel from "@/features/workspace-team/WorkspaceTeamPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const projects = useProjectsStore();
const resources = useWorkspaceResourcesStore();
const projectId = computed(() => String(route.params.projectId));
const project = computed(() =>
  projects.projects.data.find((item) => item.id === projectId.value),
);
const editorOpen = ref(false);
const projectForm = reactive({
  name: "",
  slug: "",
  description: "",
  locale: "ru" as "ru" | "en",
});

watch(
  project,
  (value) => {
    if (!value?.spec.project) return;
    Object.assign(projectForm, {
      name: value.name,
      slug: value.spec.project.slug,
      description: value.spec.project.description,
      locale: value.spec.project.locale,
    });
  },
  { immediate: true },
);

async function load(scope = projectId.value): Promise<void> {
  await Promise.all([projects.load(), resources.load(scope)]);
}

async function updateProject(): Promise<void> {
  if (
    !project.value ||
    !window.confirm(t("workspaces.confirmSave", { name: project.value.name }))
  )
    return;
  const result = await projects.update(
    project.value,
    projectForm.name.trim(),
    projectForm.slug.trim(),
    projectForm.description.trim(),
    projectForm.locale,
  );
  if (result) editorOpen.value = false;
}

async function deleteProject(): Promise<void> {
  if (
    !project.value ||
    !window.confirm(t("workspaces.confirmDelete", { name: project.value.name }))
  )
    return;
  const result = await projects.remove(project.value);
  if (result) await router.push({ name: "workspaces" });
}

watch(
  projectId,
  (scope) => {
    editorOpen.value = false;
    projects.invalidatePending();
    void load(scope);
  },
  { immediate: true },
);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="project?.name ?? $t('workspaces.detailsTitle')"
      :subtitle="
        project?.spec.project?.description ?? $t('workspaces.subtitle')
      "
    >
      <template #actions>
        <button class="button button--secondary" type="button" @click="load()">
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button>
        <button
          v-if="project"
          class="button button--secondary"
          type="button"
          @click="editorOpen = true"
        >
          <Pencil :size="15" aria-hidden="true" />{{ $t("common.edit") }}
        </button>
      </template>
    </PageHeader>

    <div class="section-stack">
      <WorkspaceTeamPanel />
      <WorkspaceResourceLifecycle :project-id="projectId" />
    </div>

    <ModalDialog
      :open="editorOpen"
      :title="$t('workspaces.detailsTitle')"
      @close="editorOpen = false"
    >
      <form @submit.prevent="updateProject">
        <ProblemNotice :problem="projects.mutationProblem" />
        <div class="form-grid" style="margin-top: 14px">
          <label class="form-field"
            ><span>{{ $t("common.name") }}</span
            ><input v-model="projectForm.name" required maxlength="160"
          /></label>
          <label class="form-field"
            ><span>{{ $t("workspaces.slug") }}</span
            ><input
              v-model="projectForm.slug"
              required
              pattern="[a-z0-9]+(?:-[a-z0-9]+)*"
              maxlength="80"
          /></label>
          <label class="form-field form-field--full"
            ><span>{{ $t("workspaces.description") }}</span
            ><textarea v-model="projectForm.description" maxlength="2000" />
          </label>
          <label class="form-field"
            ><span>{{ $t("workspaces.locale") }}</span
            ><select v-model="projectForm.locale">
              <option value="ru">{{ $t("common.russian") }}</option>
              <option value="en">{{ $t("common.english") }}</option>
            </select></label
          >
        </div>
        <div class="button-row">
          <button
            class="button button--danger"
            type="button"
            @click="deleteProject"
          >
            <Trash2 :size="15" aria-hidden="true" />{{ $t("common.delete") }}
          </button>
          <button
            class="button button--primary"
            type="submit"
            :disabled="projects.mutating"
          >
            {{ $t("common.save") }}
          </button>
        </div>
      </form>
    </ModalDialog>
  </div>
</template>
