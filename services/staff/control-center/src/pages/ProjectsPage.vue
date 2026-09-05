<script setup lang="ts">
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import { Expand, Search } from "@lucide/vue";
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { searchProjects } from "@/features/projects/api";
import ProjectList from "@/features/projects/ProjectList.vue";
import type {
  Project,
  NextAction,
} from "@/shared/api/generated/openapi/types.gen";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const platform = usePlatformStore();
const route = useRoute();
const router = useRouter();
const dialog = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const form = reactive({ name: "", purpose: "", language: "ru" as "ru" | "en" });
const actions = ref<NextAction[]>([]);
const canCreate = computed(() => actions.value.includes("CREATE_PROJECT"));
const items = ref<Project[]>([]);
const loading = ref(false);
const listProblem = ref<AppProblem>();
const pageToken = ref<string>();
const expanded = ref(false);
const query = computed({
  get: () => (typeof route.query.q === "string" ? route.query.q : ""),
  set: (q: string) => {
    void router.replace({ query: { ...route.query, q: q || undefined } });
  },
});
let controller: AbortController | undefined;
let generation = 0;
let timer: ReturnType<typeof setTimeout> | undefined;
const cursors = new Set<string>();

async function submit(): Promise<void> {
  if (!canCreate.value) return;
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

async function load(more = false): Promise<void> {
  if (more && (!pageToken.value || loading.value || listProblem.value)) return;
  controller?.abort();
  const request = new AbortController();
  controller = request;
  const current = ++generation;
  loading.value = true;
  listProblem.value = undefined;
  try {
    const page = await searchProjects(
      query.value.trim(),
      more ? pageToken.value : undefined,
      request.signal,
    );
    if (request.signal.aborted || current !== generation) return;
    const next = more ? [...items.value, ...page.items] : page.items;
    if (
      new Set(next.map((item) => item.ref)).size !== next.length ||
      (more && page.nextPageToken && cursors.has(page.nextPageToken))
    )
      throw new Error("Invalid project page cursor or duplicate entry");
    if (!more) cursors.clear();
    if (page.nextPageToken) cursors.add(page.nextPageToken);
    items.value = next;
    pageToken.value = page.nextPageToken;
    actions.value = page.nextActions;
    if (route.query.create === "1" && canCreate.value) dialog.value = true;
  } catch (error) {
    if (!request.signal.aborted && current === generation)
      listProblem.value = asProblem(error);
  } finally {
    if (current === generation) loading.value = false;
  }
}
watch(
  query,
  (_value, previous) => {
    controller?.abort();
    generation += 1;
    if (timer) clearTimeout(timer);
    items.value = [];
    pageToken.value = undefined;
    listProblem.value = undefined;
    loading.value = true;
    timer = setTimeout(() => void load(), previous === undefined ? 0 : 500);
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  controller?.abort();
  generation += 1;
  if (timer) clearTimeout(timer);
});
</script>

<template>
  <PageFrame :title="$t('projects.title')" :subtitle="$t('projects.subtitle')">
    <template v-if="canCreate" #actions
      ><button
        class="button button--primary"
        type="button"
        @click="dialog = true"
      >
        {{ $t("projects.new") }}
      </button></template
    >
    <div class="projects-toolbar">
      <label
        ><Search :size="18" /><input
          v-model="query"
          type="search"
          :aria-label="$t('common.search')"
          :placeholder="$t('common.search')"
      /></label>
      <button
        class="icon-button"
        :title="$t('catalog.expand')"
        :aria-label="$t('catalog.expand')"
        @click="expanded = true"
      >
        <Expand :size="18" />
      </button>
    </div>
    <ProblemNotice v-if="listProblem" :problem="listProblem" @retry="load()" />
    <p v-if="loading && !items.length" role="status">
      {{ $t("common.loading") }}
    </p>
    <p v-else-if="!items.length && !listProblem">
      {{ $t("projects.emptyTitle") }}
    </p>
    <ProjectList v-if="!expanded" :items="items" @more="load(true)" />
    <button
      v-if="pageToken && !expanded"
      class="button"
      :disabled="loading"
      @click="load(true)"
    >
      {{ $t("managed.more") }}
    </button>
    <ModalDialog
      v-if="expanded"
      :title="$t('projects.title')"
      size="lg"
      @close="expanded = false"
    >
      <label class="projects-search"
        ><Search :size="18" /><input
          v-model="query"
          type="search"
          :aria-label="$t('common.search')"
          :placeholder="$t('common.search')"
      /></label>
      <ProblemNotice
        v-if="listProblem"
        :problem="listProblem"
        @retry="load()"
      />
      <p v-if="loading && !items.length" role="status">
        {{ $t("common.loading") }}
      </p>
      <p v-else-if="!items.length && !listProblem">
        {{ $t("projects.emptyTitle") }}
      </p>
      <ProjectList :items="items" expanded @more="load(true)" />
      <button
        v-if="pageToken"
        class="button"
        :disabled="loading"
        @click="load(true)"
      >
        {{ $t("managed.more") }}
      </button>
    </ModalDialog>
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
          ><VoiceTextarea
            v-model.trim="form.purpose"
            required
            maxlength="1000"
          />
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
.projects-toolbar,
.projects-toolbar label,
.projects-search {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}
.projects-toolbar {
  justify-content: space-between;
}
.projects-toolbar label {
  flex: 1;
  max-width: 640px;
}
.projects-toolbar input,
.projects-search input {
  min-width: 0;
  width: 100%;
}
</style>
