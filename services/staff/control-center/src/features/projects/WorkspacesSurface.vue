<script setup lang="ts">
import { ArrowRight, Plus, RefreshCw } from "@lucide/vue";
import { onMounted, reactive, ref } from "vue";

import { useProjectsStore } from "@/features/projects/store";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { setProjectReference } from "@/shared/lib/project-scope";

const store = useProjectsStore();
const modalOpen = ref(false);
const form = reactive({
  name: "",
  slug: "",
  description: "",
  locale: "ru" as "ru" | "en",
});

async function submit(): Promise<void> {
  const created = await store.create(
    form.name.trim(),
    form.slug.trim(),
    form.description.trim(),
    form.locale,
  );
  if (!created) return;
  modalOpen.value = false;
  Object.assign(form, { name: "", slug: "", description: "", locale: "ru" });
  openProject(created.id);
}

function openProject(projectId: string): void {
  setProjectReference(projectId);
  window.location.assign(`/workspaces/${projectId}`);
}
onMounted(store.load);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('workspaces.title')"
      :subtitle="$t('workspaces.subtitle')"
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
          class="button button--primary"
          type="button"
          @click="modalOpen = true"
        >
          <Plus :size="16" aria-hidden="true" />{{ $t("workspaces.create") }}
        </button>
      </template>
    </PageHeader>
    <AsyncPanel
      :phase="store.projects.phase"
      :problem="store.projects.problem"
      @retry="store.load"
    >
      <div class="resource-grid">
        <article
          v-for="project in store.projects.data"
          :key="project.id"
          class="resource-card"
        >
          <div class="resource-card__header">
            <h3>{{ project.name }}</h3>
            <StatusBadge :state="project.state" />
          </div>
          <p>{{ project.description || $t("common.noValue") }}</p>
          <div class="resource-card__meta">
            <span>{{ project.slug }}</span
            ><span>{{
              $t("common.version", { version: project.version })
            }}</span>
          </div>
          <a
            class="button button--secondary"
            :href="`/workspaces/${project.id}`"
            @click.prevent="openProject(project.id)"
            >{{ $t("workspaces.open")
            }}<ArrowRight :size="15" aria-hidden="true"
          /></a>
        </article>
      </div>
      <div v-if="store.nextPageToken" class="panel__footer">
        <button
          class="button button--secondary"
          type="button"
          @click="store.loadMore"
        >
          {{ $t("common.loadMore") }}
        </button>
      </div>
    </AsyncPanel>
    <ModalDialog
      :open="modalOpen"
      :title="$t('workspaces.createTitle')"
      @close="modalOpen = false"
    >
      <form @submit.prevent="submit">
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
              maxlength="80"
              pattern="[a-z0-9]+(?:-[a-z0-9]+)*"
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
            class="button button--secondary"
            type="button"
            @click="modalOpen = false"
          >
            {{ $t("common.cancel") }}</button
          ><button
            class="button button--primary"
            type="submit"
            :disabled="store.mutating"
          >
            {{ $t("common.create") }}
          </button>
        </div>
      </form>
    </ModalDialog>
  </div>
</template>
