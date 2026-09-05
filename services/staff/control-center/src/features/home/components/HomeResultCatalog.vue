<script setup lang="ts">
import { Maximize2 } from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { loadHomeResultPage, type HomeResultItem } from "../result-catalog";
import type { RunFilter } from "@/features/workboard/model";
import { usePlatformStore } from "@/features/platform/store";
import { getArtifact } from "@/shared/api/generated/openapi/sdk.gen";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { asProblem, unwrap, type AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import GateProjectFilter from "@/features/workboard/components/GateProjectFilter.vue";
import HomeResultRows from "./HomeResultRows.vue";
const props = defineProps<{
  kind: "RUN" | "ARTIFACT" | "SESSION";
  fixedFilter?: "FAILED";
}>();
const emit = defineEmits<{ total: [value: number | undefined] }>();
const platform = usePlatformStore();
const items = ref<HomeResultItem[]>([]);
const total = ref<number>();
const query = ref("");
const projectRef = ref("");
const runFilter = ref<RunFilter | "FAILED">(props.fixedFilter ?? "ACTIVE");
const cursor = ref<string>();
const loading = ref(false);
const problem = ref<AppProblem>();
const expanded = ref(false);
const artifact = ref<Artifact>();
const artifactBusy = ref(false);
const artifactProblem = ref<AppProblem>();
const title = computed(() =>
  props.fixedFilter
    ? "home.failedRuns"
    : props.kind === "SESSION"
      ? "common.continue"
      : props.kind === "RUN"
        ? runFilter.value === "ACTIVE"
          ? "workboard.runningNow"
          : "runs.title"
        : "workboard.recentResults",
);
let controller: AbortController | undefined;
let generation = 0;
let timer: ReturnType<typeof setTimeout> | undefined;
let artifactController: AbortController | undefined;
let artifactGeneration = 0;
const seen = new Set<string>();
async function load(more = false) {
  if (more && (loading.value || !cursor.value)) return;
  controller?.abort();
  const active = new AbortController();
  controller = active;
  const current = ++generation;
  const pageToken = more ? cursor.value : undefined;
  loading.value = true;
  problem.value = undefined;
  if (!more) {
    items.value = [];
    total.value = undefined;
    emit("total", undefined);
    cursor.value = undefined;
    seen.clear();
  }
  try {
    const page = await loadHomeResultPage(
      {
        kind: props.kind,
        projectRef: projectRef.value,
        query: query.value,
        runFilter: runFilter.value,
      },
      pageToken,
      active.signal,
    );
    if (current !== generation || active.signal.aborted) return;
    const next = more ? [...items.value, ...page.items] : page.items;
    if (
      (page.nextPageToken && seen.has(page.nextPageToken)) ||
      new Set(next.map((item) => item.sessionRef ?? item.ref)).size !==
        next.length
    )
      throw new Error("Repeated Home result cursor or item");
    items.value = next;
    total.value = page.total;
    emit("total", page.total);
    cursor.value = page.nextPageToken;
    if (cursor.value) seen.add(cursor.value);
  } catch (error) {
    if (
      current === generation &&
      !active.signal.aborted &&
      more &&
      asProblem(error).status === 412
    ) {
      await load();
      return;
    }
    if (current === generation && !active.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (current === generation) loading.value = false;
  }
}
function refresh() {
  clearTimeout(timer);
  controller?.abort();
  generation++;
  items.value = [];
  total.value = undefined;
  cursor.value = undefined;
  loading.value = false;
  emit("total", undefined);
  timer = setTimeout(() => void load(), 250);
}
function closeArtifact() {
  artifactController?.abort();
  artifactGeneration++;
  artifact.value = undefined;
  artifactBusy.value = false;
  artifactProblem.value = undefined;
}
async function open(item: HomeResultItem) {
  if (!item.artifact) return;
  closeArtifact();
  const current = ++artifactGeneration;
  const active = new AbortController();
  artifactController = active;
  artifactBusy.value = true;
  try {
    const result = (
      await unwrap(
        getArtifact({
          path: { artifactRef: item.ref },
          signal: requestSignal(active.signal),
        }),
      )
    ).data;
    if (current !== artifactGeneration || active.signal.aborted) return;
    if (result.ref !== item.ref)
      throw new Error("Home artifact readback mismatch");
    artifact.value = result;
  } catch (error) {
    if (current === artifactGeneration && !active.signal.aborted)
      artifactProblem.value = asProblem(error);
  } finally {
    if (current === artifactGeneration) artifactBusy.value = false;
  }
}
async function download() {
  const currentArtifact = artifact.value;
  if (!currentArtifact?.nextActions.includes("DOWNLOAD") || artifactBusy.value)
    return;
  const current = artifactGeneration;
  artifactBusy.value = true;
  try {
    const blob = await platform.downloadArtifactContent(
      currentArtifact.ref,
      "DOWNLOAD",
    );
    if (current !== artifactGeneration) return;
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = currentArtifact.fileName;
    anchor.hidden = true;
    document.body.append(anchor);
    anchor.click();
    anchor.remove();
    URL.revokeObjectURL(url);
  } catch (error) {
    if (current === artifactGeneration)
      artifactProblem.value = asProblem(error);
  } finally {
    if (current === artifactGeneration) artifactBusy.value = false;
  }
}
watch([query, projectRef, runFilter], refresh);
watch(
  () => platform.loading.runs,
  (loading, previous) => {
    if (props.kind !== "ARTIFACT" && previous && !loading) refresh();
  },
);
watch(
  () =>
    props.kind !== "ARTIFACT"
      ? platform.runList
          .map((item) => `${item.ref}:${String(item.version)}`)
          .join("|")
      : Object.values(platform.artifacts)
          .map((item) => `${item.ref}:${String(item.version)}`)
          .join("|"),
  refresh,
);
onMounted(() => void load());
onBeforeUnmount(() => {
  controller?.abort();
  generation++;
  clearTimeout(timer);
  closeArtifact();
});
</script>
<template>
  <section class="home-result-catalog" :data-kind="kind">
    <header>
      <h3>{{ $t(title) }}</h3>
      <span v-if="total !== undefined">{{ total }}</span
      ><button
        type="button"
        class="button button--ghost"
        :title="$t('common.expand')"
        :aria-label="$t('common.expand')"
        @click="expanded = true"
      >
        <Maximize2 :size="16" />
      </button>
    </header>
    <label class="home-result-search"
      ><span>{{ $t("common.search") }}</span
      ><input v-model="query" type="search" maxlength="200"
    /></label>
    <GateProjectFilter v-model="projectRef" />
    <label v-if="kind === 'RUN' && !fixedFilter" class="home-result-search"
      ><span>{{ $t("home.stateFilter") }}</span
      ><select v-model="runFilter">
        <option value="ACTIVE">{{ $t("home.activeFilter") }}</option>
        <option value="TERMINAL">{{ $t("home.terminalFilter") }}</option>
        <option value="ALL">{{ $t("common.all") }}</option>
      </select></label
    >
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <p v-else-if="total === 0">{{ $t("common.empty") }}</p>
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <HomeResultRows
      :items="items"
      :loading="loading"
      :more="cursor"
      @more="load(true)"
      @open="open"
    />
    <ModalDialog
      v-if="expanded"
      :title="$t(title)"
      size="xl"
      @close="expanded = false"
    >
      <label class="home-result-search"
        ><span>{{ $t("common.search") }}</span
        ><input v-model="query" type="search" maxlength="200"
      /></label>
      <GateProjectFilter v-model="projectRef" />
      <label v-if="kind === 'RUN' && !fixedFilter" class="home-result-search"
        ><span>{{ $t("home.stateFilter") }}</span
        ><select v-model="runFilter">
          <option value="ACTIVE">{{ $t("home.activeFilter") }}</option>
          <option value="TERMINAL">{{ $t("home.terminalFilter") }}</option>
          <option value="ALL">{{ $t("common.all") }}</option>
        </select></label
      >
      <p v-if="total !== undefined">{{ total }}</p>
      <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
      <p v-else-if="total === 0">{{ $t("common.empty") }}</p>
      <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
      <HomeResultRows
        :items="items"
        :loading="loading"
        :more="cursor"
        @more="load(true)"
        @open="open"
      />
    </ModalDialog>
    <ModalDialog
      v-if="artifact || artifactBusy || artifactProblem"
      :title="artifact?.fileName ?? $t('files.title')"
      @close="closeArtifact"
    >
      <p v-if="artifactBusy" role="status">{{ $t("common.loading") }}</p>
      <ProblemNotice v-if="artifactProblem" :problem="artifactProblem" />
      <template v-if="artifact"
        ><p>
          {{ artifact.mediaType }} · {{ artifact.scanState }} ·
          {{ artifact.createdAt }}
        </p>
        <button
          type="button"
          class="button"
          :disabled="artifactBusy || !artifact.nextActions.includes('DOWNLOAD')"
          @click="download"
        >
          {{ $t("common.download") }}
        </button></template
      >
    </ModalDialog>
  </section>
</template>
<style scoped>
.home-result-catalog {
  min-width: 0;
  background: var(--surface);
  border: 1px solid var(--hairline);
  border-radius: var(--radius-md);
  overflow: hidden;
}
header {
  display: flex;
  gap: 8px;
  align-items: center;
  padding: 9px 16px;
}
header h3 {
  margin: 0;
  font-size: 0.84rem;
}
header button {
  margin-left: auto;
}
.home-result-search {
  display: grid;
  gap: 4px;
  padding: 8px 16px;
}
input,
select {
  width: 100%;
  min-width: 0;
}
p {
  padding: 0 16px;
  overflow-wrap: anywhere;
}
</style>
