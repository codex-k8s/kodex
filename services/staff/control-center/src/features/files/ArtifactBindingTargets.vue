<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type {
  Artifact,
  ArtifactBindingTarget,
  ArtifactBindingTargetPage,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { nearScrollEnd } from "@/shared/ui/async-entity-picker";
import { bindingTargetEditable, loadBindingTargets } from "./binding-targets";

const props = defineProps<{ artifact: Artifact; busy: boolean }>();
const emit = defineEmits<{
  change: [target: ArtifactBindingTarget, artifactVersion: number];
  refresh: [];
}>();
const query = ref("");
const expanded = ref(false);
const page = ref<ArtifactBindingTargetPage>();
const items = ref<ArtifactBindingTarget[]>([]);
const loading = ref(false);
const problem = ref<AppProblem>();
const hasMore = computed(() => Boolean(page.value?.nextPageToken));
let generation = 0;
let controller: AbortController | undefined;
let timer: ReturnType<typeof setTimeout> | undefined;
const cursors = new Set<string>();

async function load(more = false) {
  if (props.busy || (more && (loading.value || !hasMore.value))) return;
  controller?.abort();
  const active = new AbortController();
  controller = active;
  const current = ++generation;
  const token = more ? page.value?.nextPageToken : undefined;
  const digest = more ? page.value?.digest : undefined;
  if (!more) {
    page.value = undefined;
    items.value = [];
    cursors.clear();
  }
  loading.value = true;
  problem.value = undefined;
  try {
    const result = await loadBindingTargets(
      props.artifact,
      query.value,
      token,
      digest,
      active.signal,
    );
    if (current !== generation || active.signal.aborted) return;
    const next = more ? [...items.value, ...result.items] : result.items;
    if (
      new Set(next.map((item) => item.agentRef)).size !== next.length ||
      next.length > result.total ||
      (result.nextPageToken && cursors.has(result.nextPageToken))
    )
      throw new Error("Repeated artifact binding target page");
    items.value = next;
    page.value = result;
    if (result.nextPageToken) cursors.add(result.nextPageToken);
  } catch (error) {
    if (current !== generation || active.signal.aborted) return;
    const value = asProblem(error);
    if (more && value.status === 412) {
      await load();
      return;
    }
    problem.value = value;
    items.value = [];
    page.value = undefined;
  } finally {
    if (current === generation) loading.value = false;
  }
}
function refresh() {
  controller?.abort();
  generation++;
  clearTimeout(timer);
  items.value = [];
  page.value = undefined;
  loading.value = true;
  problem.value = undefined;
  timer = setTimeout(() => {
    void load();
  }, 500);
}
function retry() {
  if (problem.value?.status === 412) emit("refresh");
  else void load();
}
function change(item: ArtifactBindingTarget) {
  if (
    props.busy ||
    loading.value ||
    !page.value ||
    page.value.artifactRef !== props.artifact.ref ||
    page.value.artifactVersion !== props.artifact.version ||
    !bindingTargetEditable(item)
  )
    return;
  emit("change", item, page.value.artifactVersion);
}
function scroll(event: Event) {
  if (
    event.currentTarget instanceof HTMLElement &&
    nearScrollEnd(event.currentTarget)
  )
    void load(true);
}
watch(
  () => [
    props.artifact.ref,
    props.artifact.version,
    props.artifact.projectRef,
    query.value,
  ],
  refresh,
  { immediate: true },
);
watch(
  () => props.busy,
  (busy, previous) => {
    if (!busy && previous) refresh();
  },
);
onBeforeUnmount(() => {
  controller?.abort();
  generation++;
  clearTimeout(timer);
});
</script>

<template>
  <component
    :is="expanded ? ModalDialog : 'div'"
    :title="$t('files.binding')"
    :busy="busy"
    size="lg"
    @close="expanded = false"
  >
    <div class="binding-targets" :aria-busy="loading || busy">
      <header>
        <h3 v-if="!expanded">{{ $t("files.binding") }}</h3>
        <button
          v-if="!expanded"
          type="button"
          class="button"
          @click="expanded = true"
        >
          {{ $t("common.expand") }}
        </button>
      </header>
      <p>{{ $t("files.bindingHint") }}</p>
      <label class="binding-targets__search">
        <span>{{ $t("common.search") }}</span>
        <input v-model="query" type="search" maxlength="200" :disabled="busy" />
      </label>
      <p v-if="page">
        {{ $t("files.bindingTargetTotal", { count: page.total }) }}
      </p>
      <ProblemNotice v-if="problem" :problem="problem" @retry="retry" />
      <p v-else-if="loading && !page" role="status">
        {{ $t("common.loading") }}
      </p>
      <p v-else-if="page && !items.length">{{ $t("common.empty") }}</p>
      <div class="binding-targets__rows" @scroll="scroll">
        <label
          v-for="item in items"
          :key="item.agentRef"
          class="binding-targets__row"
        >
          <input
            type="checkbox"
            :aria-label="item.name"
            :checked="item.bound"
            :disabled="busy || loading || !bindingTargetEditable(item)"
            @change="change(item)"
          />
          <span
            ><strong>{{ item.name }}</strong
            ><StatusBadge :state="item.state" /><small>{{
              $t(
                `files.bindingReasons.${item.bound ? item.unbindReason : item.bindReason}`,
              )
            }}</small></span
          >
        </label>
        <button
          v-if="hasMore"
          type="button"
          class="button"
          :disabled="loading || busy"
          @click="load(true)"
        >
          {{ $t("common.loadMore") }}
        </button>
      </div>
    </div>
  </component>
</template>

<style scoped>
.binding-targets {
  display: grid;
  min-width: 0;
  gap: 12px;
}
.binding-targets header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
}
.binding-targets__search {
  display: grid;
  gap: 6px;
}
.binding-targets__search input {
  min-width: 0;
  width: 100%;
}
.binding-targets__rows {
  max-height: min(432px, 60dvh);
  overflow: auto;
  overscroll-behavior: contain;
}
.binding-targets__row {
  display: flex;
  min-height: 72px;
  align-items: center;
  gap: 10px;
  padding: 10px 0;
}
.binding-targets__row > span {
  display: grid;
  min-width: 0;
  gap: 4px;
  overflow-wrap: anywhere;
}
.binding-targets__row input {
  flex: 0 0 auto;
}
</style>
