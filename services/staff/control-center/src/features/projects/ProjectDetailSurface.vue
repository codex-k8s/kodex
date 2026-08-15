<script setup lang="ts">
import { Pencil, RefreshCw, Trash2 } from "@lucide/vue";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";

import { useProjectsStore } from "@/features/projects/store";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { setProjectReference } from "@/shared/lib/project-scope";

const route = useRoute();
const { t } = useI18n();
const store = useProjectsStore();
const projectId = computed(() => String(route.params.projectId));
const project = computed(() =>
  store.projects.data.find((item) => item.id === projectId.value),
);
const editorOpen = ref(false);
const form = reactive({
  name: "",
  slug: "",
  description: "",
  locale: "ru" as "ru" | "en",
});

watch(
  project,
  (value) => {
    if (!value) return;
    Object.assign(form, {
      name: value.name,
      slug: value.slug,
      description: value.description,
      locale: value.locale,
    });
  },
  { immediate: true },
);

watch(
  projectId,
  () => {
    editorOpen.value = false;
    store.invalidatePending();
    void store.load();
  },
  { immediate: true },
);

async function updateProject(): Promise<void> {
  if (
    !project.value?.nextActions.includes("UPDATE") ||
    !window.confirm(t("workspaces.confirmSave", { name: project.value.name }))
  )
    return;
  const result = await store.update(
    project.value,
    form.name.trim(),
    form.slug.trim(),
    form.description.trim(),
    form.locale,
  );
  if (result) editorOpen.value = false;
}

async function deleteProject(): Promise<void> {
  if (
    !project.value?.nextActions.includes("DELETE") ||
    !window.confirm(t("workspaces.confirmDelete", { name: project.value.name }))
  )
    return;
  if (await store.remove(project.value)) {
    setProjectReference(null);
    window.location.assign("/workspaces");
  }
}
</script>

<template>
  <PageHeader
    :title="project?.name ?? $t('workspaces.detailsTitle')"
    :subtitle="project?.description ?? $t('workspaces.subtitle')"
  >
    <template #actions>
      <button
        class="button button--secondary"
        type="button"
        @click="store.load"
      >
        <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
      </button>
      <button
        v-if="project?.nextActions.includes('UPDATE')"
        class="button button--secondary"
        type="button"
        @click="editorOpen = true"
      >
        <Pencil :size="15" aria-hidden="true" />{{ $t("common.edit") }}
      </button>
    </template>
  </PageHeader>
  <ModalDialog
    :open="editorOpen"
    :title="$t('workspaces.detailsTitle')"
    @close="editorOpen = false"
  >
    <form @submit.prevent="updateProject">
      <ProblemNotice :problem="store.mutationProblem" />
      <div class="form-grid" style="margin-top: 14px">
        <label class="form-field"
          ><span>{{ $t("common.name") }}</span
          ><input v-model="form.name" required maxlength="160"
        /></label>
        <label class="form-field"
          ><span>{{ $t("workspaces.slug") }}</span
          ><input
            v-model="form.slug"
            required
            pattern="[a-z0-9]+(?:-[a-z0-9]+)*"
            maxlength="80"
        /></label>
        <label class="form-field form-field--full"
          ><span>{{ $t("workspaces.description") }}</span
          ><textarea v-model="form.description" maxlength="2000" />
        </label>
        <label class="form-field"
          ><span>{{ $t("workspaces.locale") }}</span
          ><select v-model="form.locale">
            <option value="ru">{{ $t("common.russian") }}</option>
            <option value="en">{{ $t("common.english") }}</option>
          </select></label
        >
      </div>
      <div class="button-row">
        <button
          v-if="project?.nextActions.includes('DELETE')"
          class="button button--danger"
          type="button"
          @click="deleteProject"
        >
          <Trash2 :size="15" aria-hidden="true" />{{ $t("common.delete") }}
        </button>
        <button
          class="button button--primary"
          type="submit"
          :disabled="store.mutating"
        >
          {{ $t("common.save") }}
        </button>
      </div>
    </form>
  </ModalDialog>
</template>
