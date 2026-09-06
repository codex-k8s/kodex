<script setup lang="ts">
import { Expand, Search } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { usePlatformStore } from "@/features/platform/store";
import type { Project } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import WorkflowCard from "@/features/workflows/catalog/WorkflowCard.vue";
import AgentCard from "@/features/agents/catalog/AgentCard.vue";
import { toAgentCatalogItem } from "@/features/agents/catalog/model";
import { nearScrollEnd } from "@/shared/ui/async-entity-picker";
import {
  loadCatalog,
  catalogInvalidated,
  loadCatalogProject,
  type CatalogEntry,
  type CatalogKind,
} from "./api";
const props = defineProps<{
  kind: CatalogKind;
  projectRef?: string;
  expanded?: boolean;
}>();
const platform = usePlatformStore();
const query = ref("");
const items = ref<CatalogEntry[]>([]);
const projects = ref<Record<string, Project>>({});
const pageToken = ref<string>();
const loading = ref(false);
const problem = ref<AppProblem>();
const expandedProject = ref<string>();
let controller: AbortController | undefined;
let generation = 0;
let disposed = false;
const cursors = new Set<string>();
let timer: ReturnType<typeof setTimeout> | undefined;
const groups = computed(() => {
  const result = new Map<string, CatalogEntry[]>();
  for (const item of items.value) {
    const group = result.get(item.projectRef) ?? [];
    group.push(item);
    result.set(item.projectRef, group);
  }
  return [...result]
    .map(([ref, entries]) => ({
      ref,
      entries,
      name: projects.value[ref]?.name,
    }))
    .sort((a, b) => (a.name ?? "").localeCompare(b.name ?? ""));
});
async function load(more = false): Promise<void> {
  if (more && (!pageToken.value || loading.value)) return;
  controller?.abort();
  const current = ++generation;
  const request = new AbortController();
  controller = request;
  loading.value = true;
  problem.value = undefined;
  try {
    const token = more ? pageToken.value : undefined;
    const page = await loadCatalog(
      props.kind,
      query.value.trim(),
      request.signal,
      token,
      props.projectRef,
    );
    if (request.signal.aborted || current !== generation) return;
    const next = more ? [...items.value, ...page.items] : page.items;
    if (
      next.some(
        (item) => props.projectRef && item.projectRef !== props.projectRef,
      ) ||
      (page.nextPageToken &&
        (page.nextPageToken === token ||
          (more && cursors.has(page.nextPageToken)))) ||
      new Set(next.map((item) => item.ref)).size !== next.length
    )
      throw new Error("Invalid organization catalog cursor or duplicate entry");
    if (!more) cursors.clear();
    if (token) cursors.add(token);
    items.value = next;
    pageToken.value = page.nextPageToken || undefined;
    const missing = [
      ...new Set(page.items.map((item) => item.projectRef)),
    ].filter((ref) => !projects.value[ref]);
    // Названия проектов читаются по тем же authoritative owner boundaries, не выводятся из refs.
    for (const ref of missing) {
      const project = await loadCatalogProject(ref, request.signal);
      if (current !== generation) return;
      projects.value[ref] = project;
    }
  } catch (error) {
    if (!request.signal.aborted && current === generation)
      problem.value = asProblem(error);
  } finally {
    if (current === generation) loading.value = false;
  }
}
function invalidate(retain = false): void {
  controller?.abort();
  generation += 1;
  if (timer) clearTimeout(timer);
  timer = undefined;
  if (!retain) {
    items.value = [];
    projects.value = {};
  }
  pageToken.value = undefined;
  cursors.clear();
  problem.value = undefined;
  loading.value = false;
  if (!retain) expandedProject.value = undefined;
}
watch(
  () => [props.kind, props.projectRef, query.value],
  () => {
    invalidate();
    loading.value = true;
    timer = setTimeout(() => {
      void load();
    }, 500);
  },
  { immediate: true, flush: "sync" },
);
const unsubscribe = platform.$onAction(({ name, args, after, onError }) => {
  if (name === "clearOwnerState") {
    invalidate();
    return;
  }
  if (
    name !== "reloadPlatformState" &&
    !(name === "reloadPlatformKind" && catalogInvalidated(props.kind, args[0]))
  )
    return;
  invalidate(
    name === "reloadPlatformKind" &&
      ["RUN", "INTEGRATION_CONNECTION", "INTEGRATION_GRANT"].includes(args[0]),
  );
  loading.value = true;
  const expected = generation;
  after(() => {
    if (!disposed && expected === generation) void load();
  });
  onError((error) => {
    if (disposed || expected !== generation) return;
    loading.value = false;
    problem.value = asProblem(error);
  });
});
function scroll(event: Event): void {
  if (
    event.currentTarget instanceof HTMLElement &&
    nearScrollEnd(event.currentTarget) &&
    !problem.value
  )
    void load(true);
}
onBeforeUnmount(() => {
  disposed = true;
  unsubscribe();
  controller?.abort();
  if (timer) clearTimeout(timer);
  generation += 1;
});
</script>
<template>
  <section class="organization-catalog">
    <label class="organization-catalog__search"
      ><Search :size="18" /><input
        v-model="query"
        type="search"
        :aria-label="$t('common.search')"
        :placeholder="$t('common.search')"
    /></label>
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <p v-if="loading && !items.length" role="status">
      {{ $t("common.loading") }}
    </p>
    <p v-else-if="!items.length && !problem">{{ $t("common.empty") }}</p>
    <section
      v-for="group in groups"
      :key="group.ref"
      class="organization-catalog__group"
    >
      <header v-if="!expanded">
        <RouterLink :to="`/projects/${encodeURIComponent(group.ref)}`">{{
          group.name ?? $t("app.project")
        }}</RouterLink
        ><button
          class="icon-button"
          :title="$t('catalog.expand')"
          :aria-label="$t('catalog.expand')"
          @click="expandedProject = group.ref"
        >
          <Expand :size="18" />
        </button>
      </header>
      <div
        class="organization-catalog__items"
        :class="{
          'organization-catalog__items--expanded': expanded,
          'organization-catalog__items--cards':
            kind === 'workflows' || kind === 'agents',
        }"
        @scroll.passive="scroll"
      >
        <template v-for="entry in group.entries" :key="entry.ref">
          <WorkflowCard v-if="entry.workflow" :workflow="entry.workflow" />
          <AgentCard
            v-else-if="entry.agent"
            :item="toAgentCatalogItem(entry.agent)"
            :to="entry.path"
          />
          <RouterLink
            v-else
            :to="entry.path"
            class="organization-catalog__entry"
            ><div>
              <h3 :title="entry.title">{{ entry.title }}</h3>
              <p :title="entry.description">{{ entry.description }}</p>
              <small v-if="entry.role">{{
                $t(`access.platformRoles.${entry.role}`)
              }}</small>
              <small :title="entry.meta.filter(Boolean).join(' · ')">{{
                entry.meta.filter(Boolean).join(" · ")
              }}</small>
            </div>
            <StatusBadge :state="entry.state" /><span
              >v{{ entry.version }}</span
            ></RouterLink
          >
        </template>
      </div>
    </section>
    <button
      v-if="pageToken"
      class="button"
      :disabled="loading"
      @click="load(true)"
    >
      {{ $t("managed.more") }}
    </button>
    <ModalDialog
      v-if="expandedProject"
      :title="projects[expandedProject]?.name ?? $t('app.project')"
      size="xl"
      @close="expandedProject = undefined"
      ><OrganizationCatalog
        :kind="kind"
        :project-ref="expandedProject"
        expanded
    /></ModalDialog>
  </section>
</template>
<style scoped>
.organization-catalog {
  display: grid;
  gap: 20px;
  min-width: 0;
}
.organization-catalog__search {
  display: flex;
  align-items: center;
  gap: 8px;
  max-width: 640px;
}
.organization-catalog__search input {
  min-width: 0;
  width: 100%;
}
.organization-catalog__group {
  min-width: 0;
}
.organization-catalog__group > header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}
.organization-catalog__items {
  max-height: 672px;
  overflow-y: auto;
}
.organization-catalog__items--expanded {
  max-height: 65vh;
}
.organization-catalog__items--cards {
  --card-height: 360px;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  grid-auto-rows: var(--card-height);
  gap: 12px;
  max-height: calc(3 * var(--card-height) + 24px);
}
.organization-catalog__items--cards :deep(.agent-card),
.organization-catalog__items--cards :deep(.workflow-card) {
  box-sizing: border-box;
  height: 100%;
}
.organization-catalog__items--cards.organization-catalog__items--expanded {
  max-height: 65vh;
}
@media (max-width: 1000px) {
  .organization-catalog__items--cards {
    --card-height: 400px;
    grid-template-columns: minmax(0, 1fr);
    max-height: calc(6 * var(--card-height) + 60px);
  }
}
.organization-catalog__entry {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 12px;
  height: 112px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border);
  color: inherit;
  text-decoration: none;
}
.organization-catalog__entry h3 {
  font-size: 15px;
  line-height: 20px;
  margin: 0;
}
.organization-catalog__entry h3,
.organization-catalog__entry p,
.organization-catalog__entry small {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.organization-catalog__entry p,
.organization-catalog__entry small {
  margin: 4px 0 0;
  color: var(--muted);
  -webkit-line-clamp: 1;
}
.organization-catalog__entry p {
  font-size: 14px;
  line-height: 18px;
}
.organization-catalog__entry small {
  font-size: 12px;
  line-height: 16px;
}
.organization-catalog__entry h3,
.organization-catalog__entry p,
.organization-catalog__entry small {
  overflow-wrap: anywhere;
}
.organization-catalog__entry > div {
  min-width: 0;
}
@media (max-width: 600px) {
  .organization-catalog__entry {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .organization-catalog__entry > span:last-child {
    display: none;
  }
}
</style>
