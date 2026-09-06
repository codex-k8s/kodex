<script setup lang="ts">
import { Maximize2, Plus, RefreshCw, Search } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type { ContextResourceState } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { listContext, type ContextItem, type ContextKind } from "./api";
import { loadCatalogProject } from "@/features/catalogs/api";
const props = defineProps<{
  kind: ContextKind;
  projectRef?: string;
  agentRef?: string;
}>();
const items = ref<ContextItem[]>([]);
const query = ref("");
const state = ref<ContextResourceState>("ACTIVE");
const total = ref(0);
const cursor = ref("");
const loading = ref(false);
const expanded = ref(false);
const problem = ref<AppProblem>();
let timer: ReturnType<typeof setTimeout> | undefined;
let controller: AbortController | undefined;
let generation = 0;
const cursors = new Set<string>();
const projectNames = ref<Record<string, string>>({});
const groups = computed(() => {
  const result = new Map<string, ContextItem[]>();
  for (const item of items.value)
    result.set(item.projectRef, [...(result.get(item.projectRef) ?? []), item]);
  return [...result].map(([projectRef, entries]) => ({ projectRef, entries }));
});
function title(item: ContextItem): string {
  const revision =
    "draftRevision" in item
      ? (item.draftRevision ?? item.currentRevision)
      : item.currentRevision;
  return revision
    ? "title" in revision
      ? revision.title
      : revision.name
    : item.ref;
}
async function load(more = false): Promise<void> {
  if (more && (loading.value || !cursor.value)) return;
  controller?.abort();
  const active = new AbortController();
  controller = active;
  const current = ++generation;
  loading.value = true;
  problem.value = undefined;
  try {
    const token = more ? cursor.value : undefined;
    const page = await listContext(props.kind, {
      projectRef: props.projectRef,
      agentRef: props.agentRef,
      query: query.value.trim(),
      state: state.value,
      pageToken: token,
      signal: active.signal,
    });
    if (current !== generation) return;
    const next = more ? [...items.value, ...page.items] : page.items;
    if (
      new Set(next.map((item) => item.ref)).size !== next.length ||
      next.length > page.total ||
      (more && page.nextPageToken && cursors.has(page.nextPageToken))
    )
      throw new Error("Invalid context catalog cursor sequence");
    if (!more) cursors.clear();
    if (token) cursors.add(token);
    items.value = next;
    total.value = page.total;
    cursor.value = page.nextPageToken;
    for (const ref of new Set(page.items.map((item) => item.projectRef))) {
      if (projectNames.value[ref]) continue;
      const project = await loadCatalogProject(ref, active.signal);
      if (current !== generation) return;
      projectNames.value[ref] = project.name;
    }
  } catch (error) {
    if (current === generation && !active.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (current === generation) loading.value = false;
  }
}
watch(
  () => [
    props.kind,
    props.projectRef,
    props.agentRef,
    query.value,
    state.value,
  ],
  () => {
    controller?.abort();
    generation += 1;
    if (timer) clearTimeout(timer);
    items.value = [];
    cursor.value = "";
    total.value = 0;
    problem.value = undefined;
    loading.value = true;
    timer = setTimeout(() => void load(), 500);
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  generation += 1;
  controller?.abort();
  if (timer) clearTimeout(timer);
});
</script>
<template>
  <component
    :is="expanded ? ModalDialog : 'section'"
    :title="$t(`contextResources.${kind}`)"
    size="full"
    class="context-catalog"
    @close="expanded = false"
  >
    <header class="context-toolbar">
      <label class="context-search"
        ><Search :size="18" /><input
          v-model="query"
          type="search"
          :aria-label="$t('common.search')"
          maxlength="500"
      /></label>
      <select v-model="state" :aria-label="$t('contextResources.state')">
        <option
          v-for="value in ['ACTIVE', 'ARCHIVED', 'EXPIRED', 'PURGED']"
          :key="value"
          :value="value"
        >
          {{ $t(`contextResources.states.${value}`) }}
        </option>
      </select>
      <span>{{ total }}</span>
      <button
        class="icon-button"
        :disabled="loading"
        :title="$t('vfs.refresh')"
        :aria-label="$t('vfs.refresh')"
        @click="load()"
      >
        <RefreshCw :size="18" />
      </button>
      <button
        v-if="!expanded"
        class="icon-button"
        :title="$t('contextResources.expand')"
        :aria-label="$t('contextResources.expand')"
        @click="expanded = true"
      >
        <Maximize2 :size="18" />
      </button>
      <RouterLink
        class="button button--primary"
        :to="{
          name: 'context-resource',
          params: { kind, resourceRef: 'new' },
          query: { projectRef, agentRef },
        }"
        ><Plus :size="18" />{{ $t("common.create") }}</RouterLink
      >
    </header>
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <section
      v-for="group in groups"
      :key="group.projectRef"
      class="context-group"
    >
      <h3>
        <RouterLink :to="`/projects/${encodeURIComponent(group.projectRef)}`">{{
          projectNames[group.projectRef] ?? group.projectRef
        }}</RouterLink>
      </h3>
      <div class="context-rows" :class="{ 'context-rows--expanded': expanded }">
        <RouterLink
          v-for="item in group.entries"
          :key="item.ref"
          class="context-row"
          :to="{
            name: 'project-context-resource',
            params: {
              kind,
              resourceRef: item.ref,
              projectRef: item.projectRef,
            },
          }"
        >
          <span
            ><strong>{{ title(item) }}</strong
            ><code>{{ item.ref }}</code></span
          ><StatusBadge :state="item.state" /><small>v{{ item.version }}</small>
        </RouterLink>
      </div>
    </section>
    <p v-if="!loading && !problem && !items.length">{{ $t("common.empty") }}</p>
    <button
      v-if="cursor"
      class="button"
      :disabled="loading"
      @click="load(true)"
    >
      {{ $t("impact.more") }}
    </button>
  </component>
</template>
<style scoped>
.context-catalog {
  min-width: 0;
}
.context-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  margin-bottom: 16px;
}
.context-search {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1 1 220px;
  min-width: 0;
}
.context-search input {
  width: 100%;
  min-width: 0;
}
.context-group {
  min-width: 0;
  margin-block: 20px;
}
.context-rows {
  max-height: 576px;
  overflow: auto;
}
.context-rows--expanded {
  max-height: none;
}
.context-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  gap: 12px;
  align-items: center;
  min-height: 96px;
  padding: 12px;
  border-bottom: 1px solid var(--border);
}
.context-row span {
  display: grid;
  gap: 8px;
  min-width: 0;
}
.context-row code,
.context-row strong,
h3 {
  overflow-wrap: anywhere;
}
@media (max-width: 600px) {
  .context-row {
    grid-template-columns: minmax(0, 1fr) auto;
    min-height: 144px;
  }
  .context-row span {
    grid-column: 1 / -1;
  }
  .context-rows {
    max-height: 864px;
  }
  .context-rows--expanded {
    max-height: none;
  }
}
</style>
