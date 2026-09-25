<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import {
  loadProjectTrash,
  moveProjectToTrash,
  purgeProjectFromTrash,
  restoreProjectFromTrash,
  searchProjects,
} from "@/features/projects/api";
import ProjectList from "@/features/projects/ProjectList.vue";
import ProjectFormFields from "@/features/projects/ProjectFormFields.vue";
import { catalogInvalidated } from "@/features/catalogs/api";
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
const trashMode = computed(() => route.query.trash === "1");
const dialog = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const lifecycleTarget = ref<Project>();
const lifecycleAction = ref<"TRASH" | "RESTORE" | "PURGE">("TRASH");
const purgeConfirmation = ref("");
const lifecycleBusy = ref(false);
const lifecycleProblem = ref<AppProblem>();
const form = reactive({ name: "", purpose: "", language: "ru" as "ru" | "en" });
const actions = ref<NextAction[]>([]);
const canCreate = computed(
  () => !trashMode.value && actions.value.includes("CREATE_PROJECT"),
);
const canViewTrash = computed(() =>
  ["OWNER", "ADMINISTRATOR"].includes(platform.bootstrap?.platformRole ?? ""),
);
const items = ref<Project[]>([]);
const loading = ref(false);
const listProblem = ref<AppProblem>();
const pageToken = ref<string>();
const query = computed(() =>
  typeof route.query.q === "string" ? route.query.q : "",
);
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
    const inTrash = trashMode.value;
    const page = inTrash
      ? await loadProjectTrash(
          more ? pageToken.value : undefined,
          request.signal,
        )
      : await searchProjects(
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
    actions.value =
      "nextActions" in page && Array.isArray(page.nextActions)
        ? (page.nextActions as NextAction[])
        : [];
    if (!inTrash && route.query.create === "1" && canCreate.value)
      dialog.value = true;
  } catch (error) {
    if (!request.signal.aborted && current === generation)
      listProblem.value = asProblem(error);
  } finally {
    if (current === generation) loading.value = false;
  }
}
let firstProjectLoad = true;
watch(
  [query, trashMode],
  () => {
    controller?.abort();
    generation += 1;
    if (timer) clearTimeout(timer);
    items.value = [];
    pageToken.value = undefined;
    listProblem.value = undefined;
    loading.value = true;
    timer = setTimeout(() => void load(), firstProjectLoad ? 0 : 150);
    firstProjectLoad = false;
  },
  { immediate: true },
);
watch(
  () =>
    trashMode.value &&
    items.value.some((project) => project.lifecycle === "PURGE_PENDING"),
  (pending, _previous, onCleanup) => {
    if (!pending) return;
    const refresh = setInterval(() => {
      if (!loading.value && !lifecycleBusy.value) void load();
    }, 3000);
    onCleanup(() => clearInterval(refresh));
  },
  { immediate: true },
);
const unsubscribe = platform.$onAction(({ name, args, after, onError }) => {
  if (
    name !== "clearOwnerState" &&
    name !== "reloadPlatformState" &&
    !(name === "reloadPlatformKind" && catalogInvalidated("projects", args[0]))
  )
    return;
  controller?.abort();
  const expected = ++generation;
  if (timer) clearTimeout(timer);
  timer = undefined;
  pageToken.value = undefined;
  cursors.clear();
  const retain =
    name === "reloadPlatformKind" &&
    [
      "RUN",
      "AGENT",
      "WORKFLOW",
      "INTEGRATION_CONNECTION",
      "INTEGRATION_GRANT",
    ].includes(args[0]);
  if (!retain) items.value = [];
  if (!retain) actions.value = [];
  loading.value = name !== "clearOwnerState";
  if (name === "clearOwnerState") {
    dialog.value = false;
    lifecycleTarget.value = undefined;
    return;
  }
  after(() => {
    if (generation === expected) void load();
  });
  onError((error) => {
    if (generation === expected) {
      items.value = [];
      loading.value = false;
      listProblem.value = asProblem(error);
    }
  });
});

function toggleTrash(): void {
  const next = { ...route.query };
  if (trashMode.value) delete next.trash;
  else next.trash = "1";
  void router.push({ query: next });
}

async function confirmLifecycle(): Promise<void> {
  const target = lifecycleTarget.value;
  if (!target || lifecycleBusy.value) return;
  if (
    lifecycleAction.value === "PURGE" &&
    purgeConfirmation.value !== target.name
  )
    return;
  lifecycleBusy.value = true;
  lifecycleProblem.value = undefined;
  try {
    if (lifecycleAction.value === "PURGE") await purgeProjectFromTrash(target);
    else if (lifecycleAction.value === "RESTORE")
      await restoreProjectFromTrash(target);
    else await moveProjectToTrash(target);
    lifecycleTarget.value = undefined;
    purgeConfirmation.value = "";
    await load();
    await platform.reloadPlatformKind("PROJECT");
  } catch (error) {
    lifecycleProblem.value = asProblem(error);
  } finally {
    lifecycleBusy.value = false;
  }
}
function openLifecycle(
  project: Project,
  action: "TRASH" | "RESTORE" | "PURGE",
): void {
  lifecycleAction.value = action;
  lifecycleTarget.value = project;
  lifecycleProblem.value = undefined;
  purgeConfirmation.value = "";
}
onBeforeUnmount(() => {
  unsubscribe();
  controller?.abort();
  generation += 1;
  if (timer) clearTimeout(timer);
});
</script>

<template>
  <PageFrame :title="$t('projects.title')" :subtitle="$t('projects.subtitle')">
    <template #actions>
      <button
        v-if="canViewTrash"
        class="button"
        type="button"
        @click="toggleTrash"
      >
        {{ $t(trashMode ? "projects.backToProjects" : "projects.trash") }}
      </button>
      <button
        v-if="canCreate"
        class="button button--primary"
        type="button"
        @click="dialog = true"
      >
        {{ $t("projects.new") }}
      </button>
    </template>
    <ProblemNotice v-if="listProblem" :problem="listProblem" @retry="load()" />
    <p v-if="loading && !items.length" role="status">
      {{ $t("common.loading") }}
    </p>
    <p v-else-if="!items.length && !listProblem">
      {{ $t(trashMode ? "projects.trashEmpty" : "projects.emptyTitle") }}
    </p>
    <ProjectList
      :items="items"
      :trashed="trashMode"
      @trash="openLifecycle($event, 'TRASH')"
      @restore="openLifecycle($event, 'RESTORE')"
      @purge="openLifecycle($event, 'PURGE')"
    />
    <button
      v-if="pageToken"
      class="button"
      :disabled="loading"
      @click="load(true)"
    >
      {{ $t("managed.more") }}
    </button>
    <ModalDialog
      v-if="lifecycleTarget"
      :title="
        $t(
          lifecycleAction === 'PURGE'
            ? 'projects.purge'
            : lifecycleAction === 'RESTORE'
              ? 'projects.restore'
              : 'projects.trashProject',
        )
      "
      :busy="lifecycleBusy"
      size="sm"
      @close="lifecycleTarget = undefined"
    >
      <p>
        <strong>{{ lifecycleTarget.name }}</strong>
      </p>
      <p>
        {{
          $t(
            lifecycleAction === "PURGE"
              ? "projects.purgeDescription"
              : lifecycleAction === "RESTORE"
                ? "projects.restoreDescription"
                : "projects.trashDescription",
          )
        }}
      </p>
      <label v-if="lifecycleAction === 'PURGE'" class="field">
        <span>{{ $t("projects.purgeConfirmName") }}</span>
        <input
          v-model="purgeConfirmation"
          autocomplete="off"
          data-dialog-initial-focus
        />
      </label>
      <ProblemNotice
        v-if="lifecycleProblem"
        :problem="lifecycleProblem"
        compact
      />
      <template #actions>
        <button
          class="button"
          type="button"
          :disabled="lifecycleBusy"
          @click="lifecycleTarget = undefined"
        >
          {{ $t("common.cancel") }}
        </button>
        <button
          class="button"
          :class="
            lifecycleAction === 'RESTORE' ? 'button--primary' : 'button--danger'
          "
          type="button"
          :disabled="
            lifecycleBusy ||
            (lifecycleAction === 'PURGE' &&
              purgeConfirmation !== lifecycleTarget.name)
          "
          @click="confirmLifecycle"
        >
          {{
            $t(
              lifecycleAction === "PURGE"
                ? "projects.purge"
                : lifecycleAction === "RESTORE"
                  ? "projects.restore"
                  : "projects.trashProject",
            )
          }}
        </button>
      </template>
    </ModalDialog>
    <ModalDialog
      v-if="dialog"
      :title="$t('projects.new')"
      :busy="busy"
      @close="dialog = false"
    >
      <form
        id="project-form"
        class="form-grid"
        :inert="busy"
        @submit.prevent="submit"
      >
        <ProjectFormFields
          :name="form.name"
          :purpose="form.purpose"
          :language="form.language"
          :disabled="busy"
          initial-focus
          @update:name="form.name = $event"
          @update:purpose="form.purpose = $event"
          @update:language="form.language = $event"
        />
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
