<script setup lang="ts">
import { ChevronRight, PackageOpen, Search } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import { usePlatformStore } from "@/features/platform/store";
import type { Project } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";
import WorkflowCard from "@/features/workflows/catalog/WorkflowCard.vue";
import AgentCard from "@/features/agents/catalog/AgentCard.vue";
import { toAgentCatalogItem } from "@/features/agents/catalog/model";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import EntityIcon from "@/shared/ui/EntityIcon.vue";
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
}>();
const tableKind = computed(
  () => props.kind !== "agents" && props.kind !== "workflows",
);
const entityIconKind = computed(
  () =>
    (
      ({
        agents: "AGENT",
        workflows: "WORKFLOW",
        automations: "AUTOMATION",
        environments: "ENVIRONMENT",
        secrets: "SECRET",
        members: "MEMBER",
      }) as const
    )[props.kind],
);
const platform = usePlatformStore();
const searchId = useId();
const query = ref("");
const items = ref<CatalogEntry[]>([]);
const projects = ref<Record<string, Project>>({});
const pageToken = ref<string>();
const loading = ref(false);
const problem = ref<AppProblem>();
const scrollRoot = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const pageSize = useAdaptiveCursorPageSize({
  container: scrollRoot,
  itemSelector: ".organization-catalog__row, .agent-card, .workflow-card",
  itemCount: () => items.value.length,
  estimatedItemHeight:
    props.kind === "agents" ? 242 : props.kind === "workflows" ? 300 : 64,
  estimatedColumns:
    props.kind === "agents" || props.kind === "workflows" ? 3 : 1,
});
useCursorInfiniteScroll({
  root: tableKind.value ? undefined : scrollRoot,
  sentinel,
  enabled: () => Boolean(pageToken.value) && !loading.value && !problem.value,
  loadMore: () => load(true),
});
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
  return [...result].map(([ref, entries]) => ({
    ref,
    entries,
    name: projects.value[ref]?.name,
  }));
});
function entryRoute(entry: CatalogEntry) {
  return props.kind === "members"
    ? {
        name: "project-access",
        params: { projectRef: entry.projectRef },
        query: { memberRef: entry.subjectRef },
      }
    : entry.path;
}
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
      pageSize.value,
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
onBeforeUnmount(() => {
  disposed = true;
  unsubscribe();
  controller?.abort();
  if (timer) clearTimeout(timer);
  generation += 1;
});
</script>
<template>
  <section
    ref="scrollRoot"
    class="organization-catalog"
    :class="{ 'organization-catalog--table': tableKind }"
  >
    <label
      v-if="items.length || query.trim()"
      class="organization-catalog__search"
      :for="searchId"
      ><Search :size="18" /><input
        :id="searchId"
        v-model="query"
        :name="searchId"
        type="search"
        :aria-label="$t('common.search')"
        :placeholder="$t('common.search')"
    /></label>
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <p v-if="loading && !items.length" role="status">
      {{ $t("common.loading") }}
    </p>
    <div
      v-else-if="!items.length && !problem"
      class="organization-catalog__empty"
    >
      <PackageOpen :size="28" aria-hidden="true" />
      <h2>
        {{
          query.trim()
            ? $t("catalog.emptySearchTitle")
            : $t(
                `catalog.${projectRef ? "emptyTitle" : "emptyGlobalTitle"}.${kind}`,
              )
        }}
      </h2>
      <p>
        {{
          query.trim()
            ? $t("catalog.emptySearchHelp")
            : projectRef
              ? $t(`catalog.emptyHelp.${kind}`)
              : $t("catalog.emptyGlobalHelp")
        }}
      </p>
      <RouterLink
        v-if="!query.trim() && !projectRef"
        class="button button--primary"
        to="/projects"
        >{{ $t("catalog.chooseProject") }}</RouterLink
      >
    </div>
    <div
      v-if="tableKind && items.length"
      class="organization-catalog__table-wrap"
    >
      <table
        class="organization-catalog__table"
        :class="{ 'organization-catalog__table--project': !!projectRef }"
      >
        <thead>
          <tr>
            <th v-if="!projectRef" scope="col">
              {{ $t("catalog.table.project") }}
            </th>
            <th scope="col">{{ $t("catalog.table.name") }}</th>
            <th scope="col">{{ $t("catalog.table.details") }}</th>
            <th scope="col">{{ $t("catalog.table.state") }}</th>
            <th scope="col">
              {{
                $t(
                  kind === "members"
                    ? "catalog.table.role"
                    : "catalog.table.version",
                )
              }}
            </th>
            <th scope="col" class="organization-catalog__open-heading">
              {{ $t("catalog.table.open") }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="entry in items"
            :key="entry.ref"
            class="organization-catalog__row"
          >
            <td v-if="!projectRef">
              <div class="organization-catalog__identity">
                <EntityIcon kind="PROJECT" />
                <RouterLink
                  :to="`/projects/${encodeURIComponent(entry.projectRef)}/${kind}`"
                  :title="projects[entry.projectRef]?.name ?? $t('app.project')"
                >
                  {{ projects[entry.projectRef]?.name ?? $t("app.project") }}
                </RouterLink>
              </div>
            </td>
            <td>
              <div class="organization-catalog__identity">
                <EntityIcon :kind="entityIconKind" />
                <RouterLink :to="entryRoute(entry)" :title="entry.title">{{
                  entry.title
                }}</RouterLink>
              </div>
            </td>
            <td>
              <span
                class="organization-catalog__description"
                :title="entry.description"
                >{{ entry.description || $t("common.noData") }}</span
              >
              <small
                v-if="entry.meta.some(Boolean)"
                :title="entry.meta.filter(Boolean).join(' · ')"
                >{{ entry.meta.filter(Boolean).join(" · ") }}</small
              >
            </td>
            <td><StatusBadge :state="entry.state" /></td>
            <td>
              <span v-if="kind === 'members' && entry.role">{{
                $t(`access.platformRoles.${entry.role}`)
              }}</span>
              <span v-else>v{{ entry.version }}</span>
            </td>
            <td class="organization-catalog__open-cell">
              <RouterLink
                :to="entryRoute(entry)"
                class="icon-button"
                :aria-label="$t('common.open')"
                :title="$t('common.open')"
                ><ChevronRight :size="18" aria-hidden="true"
              /></RouterLink>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div
      v-if="!tableKind && groups.length"
      class="organization-catalog__groups"
      :class="{ 'organization-catalog__groups--project': Boolean(projectRef) }"
    >
      <section
        v-for="group in groups"
        :key="group.ref"
        class="organization-catalog__group"
      >
        <header v-if="!projectRef">
          <div class="organization-catalog__project">
            <EntityIcon kind="PROJECT" :size="16" />
            <RouterLink
              class="organization-catalog__project-link"
              :to="`/projects/${encodeURIComponent(group.ref)}`"
              >{{ group.name ?? $t("app.project") }}</RouterLink
            >
          </div>
          <RouterLink
            class="button organization-catalog__manage"
            :to="`/projects/${encodeURIComponent(group.ref)}/${kind}`"
            >{{ $t("catalog.openInProject") }}</RouterLink
          >
        </header>
        <div
          class="organization-catalog__items organization-catalog__items--cards"
        >
          <template v-for="entry in group.entries" :key="entry.ref">
            <WorkflowCard v-if="entry.workflow" :workflow="entry.workflow" />
            <AgentCard
              v-else-if="entry.agent"
              :item="toAgentCatalogItem(entry.agent)"
              :to="entry.path"
            />
          </template>
        </div>
      </section>
    </div>
    <div
      ref="sentinel"
      class="organization-catalog__sentinel"
      aria-hidden="true"
    />
  </section>
</template>
<style scoped>
.organization-catalog {
  display: grid;
  gap: 20px;
  min-width: 0;
  max-height: calc(100dvh - 240px);
  overflow-y: auto;
  overscroll-behavior: contain;
}
.organization-catalog--table {
  max-height: none;
  overflow: visible;
  overscroll-behavior: auto;
}
.organization-catalog__sentinel {
  height: 1px;
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
.organization-catalog__groups {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
  align-items: start;
  gap: 20px 12px;
}
.organization-catalog__groups--project {
  grid-template-columns: minmax(0, 1fr);
}
.organization-catalog__empty {
  display: grid;
  min-height: 220px;
  place-items: center;
  align-content: center;
  gap: 8px;
  padding: 28px;
  border: 1px dashed var(--border-strong);
  border-radius: 8px;
  color: var(--muted);
  background: var(--surface);
  text-align: center;
}
.organization-catalog__empty svg {
  color: var(--accent-strong);
}
.organization-catalog__empty h2,
.organization-catalog__empty p {
  max-width: 560px;
  margin: 0;
}
.organization-catalog__empty h2 {
  color: var(--text);
  font-size: 1rem;
}
.organization-catalog__empty p {
  line-height: 1.5;
}
.organization-catalog__group > header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}
.organization-catalog__project {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 9px;
}
.organization-catalog__project :deep(.entity-icon) {
  width: 28px;
  height: 28px;
}
.organization-catalog__project-link {
  min-width: 0;
  overflow: hidden;
  color: var(--text);
  font-weight: 600;
  text-decoration: none;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.organization-catalog__project-link:hover {
  text-decoration: underline;
}
.organization-catalog__table-wrap {
  overflow-x: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.organization-catalog__table {
  width: 100%;
  min-width: 760px;
  table-layout: fixed;
  border-collapse: collapse;
}
.organization-catalog__table th,
.organization-catalog__table td {
  padding: 9px 12px;
  text-align: left;
  vertical-align: middle;
}
.organization-catalog__table th {
  color: var(--muted);
  font-size: 0.72rem;
  font-weight: 600;
}
.organization-catalog__table th:first-child {
  width: 21%;
}
.organization-catalog__table th:nth-child(2) {
  width: 25%;
}
.organization-catalog__table th:nth-child(3) {
  width: 32%;
}
.organization-catalog__table th:nth-child(4) {
  width: 11%;
}
.organization-catalog__table th:nth-child(5) {
  width: 7%;
}
.organization-catalog__table th:last-child {
  width: 4%;
}
.organization-catalog__table--project th:first-child {
  width: 30%;
}
.organization-catalog__table--project th:nth-child(2) {
  width: 42%;
}
.organization-catalog__table--project th:nth-child(3) {
  width: 14%;
}
.organization-catalog__table--project th:nth-child(4) {
  width: 10%;
}
.organization-catalog__row {
  height: 64px;
  border-top: 1px solid var(--border);
}
.organization-catalog__row:hover {
  background: var(--panel);
}
.organization-catalog__identity {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 10px;
}
.organization-catalog__identity a {
  overflow: hidden;
  color: var(--text);
  font-weight: 600;
  text-decoration: none;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.organization-catalog__identity a:hover {
  color: var(--accent-strong);
  text-decoration: underline;
}
.organization-catalog__description,
.organization-catalog__row small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.organization-catalog__row small {
  margin-top: 3px;
  color: var(--muted);
}
.organization-catalog__open-heading,
.organization-catalog__open-cell {
  text-align: center !important;
}
.organization-catalog__manage {
  flex: none;
  min-height: 30px;
  padding: 4px 9px;
  font-size: 12px;
}
.organization-catalog__items {
  min-width: 0;
}
.organization-catalog__items--cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 12px;
}
.organization-catalog__items--cards {
  grid-template-columns: minmax(0, 1fr);
}
.organization-catalog__groups--project .organization-catalog__items--cards {
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 320px), 1fr));
}
.organization-catalog__items--cards :deep(.agent-card),
.organization-catalog__items--cards :deep(.workflow-card) {
  box-sizing: border-box;
  height: 100%;
}
@media (max-width: 1000px) {
  .organization-catalog__groups,
  .organization-catalog__items--cards {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
