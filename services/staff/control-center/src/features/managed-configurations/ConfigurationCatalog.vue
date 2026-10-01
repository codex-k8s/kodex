<script setup lang="ts">
import { ChevronRight, Plus, Search } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import { RouterLink } from "vue-router";
import type { ManagedConfigurationSummary } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import EntityIcon from "@/shared/ui/EntityIcon.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";
import { loadCatalogProject } from "@/features/catalogs/api";
import { usePlatformStore } from "@/features/platform/store";
import { selectedProjectRef } from "@/shared/project-context";
import OpenAPIImportDialog from "./OpenAPIImportDialog.vue";
import {
  configurationProjectScopeValid,
  configurationRequiresProject,
  listConfigurations,
  type ConfigurationKind,
} from "./api";
const props = defineProps<{
  kind: ConfigurationKind;
  projectRef?: string;
  autoOpenImport?: boolean;
}>();
const emit = defineEmits<{ created: [configurationRef: string] }>();
const platform = usePlatformStore();
const query = ref("");
const searchId = useId();
const items = ref<ManagedConfigurationSummary[]>([]);
const projectNames = ref<Record<string, string>>({});
const list = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const pageSize = useAdaptiveCursorPageSize({
  container: list,
  itemSelector: ".configuration-catalog__row",
  itemCount: () => items.value.length,
  estimatedItemHeight: 64,
  estimatedColumns: 1,
  minimum: 8,
  maximum: 100,
});
const nextPageToken = ref<string>();
const total = ref(0);
const loading = ref(false);
const importOpen = ref(
  props.kind === "INTEGRATION_DEFINITION" && props.autoOpenImport,
);
const problem = ref<AppProblem>();
const cursors = new Set<string>();
const projectRequired = computed(
  () =>
    configurationRequiresProject(props.kind) &&
    !configurationProjectScopeValid(props.kind, props.projectRef),
);
const showProjectColumn = computed(
  () => configurationRequiresProject(props.kind) && !props.projectRef,
);
let generation = 0;
let controller: AbortController | undefined;
let timer: ReturnType<typeof setTimeout> | undefined;

function applyRealtimeCatalog(): boolean {
  if (query.value.trim()) return false;
  const scopeProjectRef = selectedProjectRef();
  if (props.projectRef && props.projectRef !== scopeProjectRef) return false;
  const snapshot = platform.realtimeSnapshot(
    "MANAGED_CONFIGURATION",
    scopeProjectRef,
  );
  if (!snapshot) {
    items.value = [];
    total.value = 0;
    nextPageToken.value = undefined;
    loading.value = !platform.realtimeAvailableKinds.length;
    return true;
  }
  items.value = Object.values(platform.managedConfigurations).filter(
    (item) =>
      item.kind === props.kind &&
      (!props.projectRef || item.projectRef === props.projectRef),
  );
  const page = platform.managedConfigurationPages[props.kind];
  total.value = page?.total ?? items.value.length;
  nextPageToken.value = page?.nextPageToken;
  projectNames.value = {};
  problem.value = undefined;
  loading.value = false;
  cursors.clear();
  return true;
}

async function load(more = false): Promise<void> {
  if (more && (!nextPageToken.value || loading.value)) return;
  controller?.abort();
  const request = new AbortController();
  controller = request;
  const current = ++generation;
  loading.value = true;
  problem.value = undefined;
  try {
    const token = more ? nextPageToken.value : undefined;
    const page = await listConfigurations({
      kind: props.kind,
      projectRef: props.projectRef,
      query: query.value.trim(),
      pageToken: token,
      pageSize: pageSize.value,
      signal: request.signal,
    });
    if (request.signal.aborted || generation !== current) return;
    const next = more ? [...items.value, ...page.items] : page.items;
    if (
      !Number.isSafeInteger(page.total) ||
      page.total < 0 ||
      next.some(
        (item) =>
          item.kind !== props.kind ||
          (configurationRequiresProject(props.kind) && !item.projectRef) ||
          (props.projectRef && item.projectRef !== props.projectRef),
      ) ||
      new Set(next.map((item) => item.ref)).size !== next.length ||
      (page.nextPageToken &&
        (page.nextPageToken === token ||
          (more && cursors.has(page.nextPageToken))))
    )
      throw new Error("Invalid managed configuration catalog");
    const missingProjects = showProjectColumn.value
      ? [
          ...new Set(
            page.items
              .map((item) => item.projectRef)
              .filter((ref): ref is string => Boolean(ref)),
          ),
        ].filter((ref) => !more || !projectNames.value[ref])
      : [];
    const loadedProjects: Record<string, string> = {};
    for (let offset = 0; offset < missingProjects.length; offset += 4) {
      const projects = await Promise.all(
        missingProjects.slice(offset, offset + 4).map(async (ref) => {
          const project = await loadCatalogProject(ref, request.signal);
          if (project.ref !== ref)
            throw new Error("Invalid configuration project lookup scope");
          return project;
        }),
      );
      if (generation !== current) return;
      for (const project of projects)
        loadedProjects[project.ref] = project.name;
    }
    if (!more) cursors.clear();
    if (token) cursors.add(token);
    items.value = next;
    projectNames.value = { ...projectNames.value, ...loadedProjects };
    total.value = page.total;
    nextPageToken.value = page.nextPageToken || undefined;
  } catch (error) {
    if (!request.signal.aborted && generation === current)
      problem.value = asProblem(error);
  } finally {
    if (generation === current) loading.value = false;
  }
}
watch(
  () => [
    props.kind,
    props.projectRef,
    query.value,
    platform.managedConfigurationRealtimeRevision,
    platform.realtimeAvailableKinds.join(","),
  ],
  () => {
    controller?.abort();
    generation += 1;
    if (timer) clearTimeout(timer);
    items.value = [];
    total.value = 0;
    nextPageToken.value = undefined;
    problem.value = undefined;
    if (applyRealtimeCatalog()) return;
    loading.value = true;
    timer = setTimeout(() => {
      void load();
    }, 500);
  },
  { immediate: true, flush: "sync" },
);
watch(
  () => [props.kind, props.autoOpenImport],
  () => {
    if (props.kind === "INTEGRATION_DEFINITION" && props.autoOpenImport)
      importOpen.value = true;
  },
);
onBeforeUnmount(() => {
  controller?.abort();
  if (timer) clearTimeout(timer);
  generation += 1;
});
useCursorInfiniteScroll({
  root: list,
  sentinel,
  enabled: () =>
    Boolean(nextPageToken.value) && !loading.value && !problem.value,
  loadMore: () => load(true),
});
function created(configurationRef: string): void {
  importOpen.value = false;
  emit("created", configurationRef);
}
</script>
<template>
  <section class="configuration-catalog">
    <header>
      <label :for="searchId"
        ><Search :size="18" /><input
          :id="searchId"
          v-model="query"
          name="managed-configuration-search"
          type="search"
          :placeholder="$t('common.search')"
          :aria-label="$t('common.search')"
      /></label>
      <RouterLink
        v-if="!projectRequired"
        class="button button--primary"
        :to="
          kind === 'ROLE_IMAGE'
            ? { name: 'role-image-new', params: { projectRef } }
            : {
                name: 'configuration',
                params: { kind, configurationRef: 'new' },
                query: projectRef ? { projectRef } : {},
              }
        "
        ><Plus :size="18" />{{ $t("common.create") }}</RouterLink
      >
      <button
        v-else
        class="button button--primary"
        disabled
        aria-describedby="managed-catalog-project-required"
      >
        <Plus :size="18" />{{ $t("common.create") }}
      </button>
      <button
        v-if="kind === 'INTEGRATION_DEFINITION'"
        class="button"
        type="button"
        @click="importOpen = true"
      >
        {{ $t("managed.openapiImport.open") }}
      </button>
    </header>
    <p
      v-if="projectRequired"
      id="managed-catalog-project-required"
      role="status"
    >
      {{ $t("managed.projectRequired") }}
    </p>
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <p v-if="loading && !items.length && !projectRequired" role="status">
      {{ $t("common.loading") }}
    </p>
    <div
      v-else-if="!items.length && !problem && !projectRequired"
      class="configuration-catalog__empty"
      role="status"
    >
      <strong>{{
        $t(query.trim() ? "managed.searchEmptyTitle" : "managed.emptyTitle")
      }}</strong>
      <p>
        {{ $t(query.trim() ? "managed.searchEmptyText" : "managed.emptyText") }}
      </p>
    </div>
    <div ref="list" class="configuration-catalog__list">
      <table
        v-if="items.length"
        class="configuration-catalog__table"
        :class="{
          'configuration-catalog__table--with-project': showProjectColumn,
        }"
      >
        <thead>
          <tr>
            <th scope="col">{{ $t("catalog.table.name") }}</th>
            <th v-if="showProjectColumn" scope="col">
              {{ $t("catalog.table.project") }}
            </th>
            <th scope="col">{{ $t("managed.catalogSource") }}</th>
            <th scope="col">{{ $t("catalog.table.state") }}</th>
            <th scope="col">{{ $t("managed.catalogRevision") }}</th>
            <th scope="col" class="configuration-catalog__open-heading">
              {{ $t("catalog.table.open") }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="item in items"
            :key="item.ref"
            class="configuration-catalog__row"
          >
            <td>
              <RouterLink
                class="configuration-catalog__identity"
                :to="{
                  name: 'configuration',
                  params: { kind: item.kind, configurationRef: item.ref },
                }"
                :title="item.name"
              >
                <EntityIcon
                  :kind="
                    item.kind === 'ROLE_IMAGE'
                      ? 'ROLE_IMAGE'
                      : item.kind === 'INTEGRATION_DEFINITION'
                        ? 'INTEGRATION'
                        : 'CONFIGURATION'
                  "
                />
                <strong>{{ item.name }}</strong>
              </RouterLink>
            </td>
            <td v-if="showProjectColumn" class="configuration-catalog__project">
              <RouterLink
                :to="`/projects/${encodeURIComponent(item.projectRef!)}`"
                :title="projectNames[item.projectRef!]"
              >
                {{ projectNames[item.projectRef!] ?? $t("common.noData") }}
              </RouterLink>
            </td>
            <td>
              <strong>{{
                $t(`roleImages.managedBy.${item.managedBy}`)
              }}</strong>
              <small v-if="item.source" :title="item.source">{{
                item.source
              }}</small>
              <small v-if="item.sourceRevision" :title="item.sourceRevision">{{
                item.sourceRevision
              }}</small>
            </td>
            <td>
              <StatusBadge
                :state="
                  item.archived
                    ? 'ARCHIVED'
                    : (item.currentRevision?.state ?? 'DRAFT')
                "
              />
            </td>
            <td>v{{ item.currentRevision?.revision ?? item.version }}</td>
            <td class="configuration-catalog__open-cell">
              <RouterLink
                class="icon-button"
                :to="{
                  name: 'configuration',
                  params: { kind: item.kind, configurationRef: item.ref },
                }"
                :aria-label="$t('catalog.table.open')"
                :title="$t('catalog.table.open')"
                ><ChevronRight :size="18" aria-hidden="true"
              /></RouterLink>
            </td>
          </tr>
        </tbody>
      </table>
      <div
        v-if="nextPageToken"
        ref="sentinel"
        class="configuration-catalog__sentinel"
        role="status"
      >
        <span v-if="loading">{{ $t("common.loading") }}</span>
        <span class="sr-only">{{ items.length }}/{{ total }}</span>
      </div>
    </div>
    <OpenAPIImportDialog
      v-if="importOpen"
      @close="importOpen = false"
      @created="created"
    />
  </section>
</template>
<style scoped>
.configuration-catalog {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.configuration-catalog > header {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.configuration-catalog label {
  display: flex;
  gap: 8px;
  align-items: center;
  min-width: 0;
  flex: 1;
}
.configuration-catalog input {
  width: 100%;
  min-width: 0;
}
.configuration-catalog__list {
  max-height: min(720px, calc(100dvh - 220px));
  overflow: auto;
  contain: layout paint;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.configuration-catalog__empty {
  padding: 16px;
  border: 1px dashed var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.configuration-catalog__empty p {
  margin: 4px 0 0;
  color: var(--muted);
}
.configuration-catalog__table {
  width: 100%;
  min-width: 760px;
  table-layout: fixed;
  border-collapse: collapse;
}
.configuration-catalog__table th {
  position: sticky;
  z-index: 1;
  top: 0;
  padding: 11px 12px;
  background: var(--panel);
  color: var(--muted);
  font-size: 12px;
  font-weight: 600;
  text-align: left;
}
.configuration-catalog__table th:nth-child(1) {
  width: 44%;
}
.configuration-catalog__table th:nth-child(2) {
  width: 30%;
}
.configuration-catalog__table th:nth-child(3) {
  width: 15%;
}
.configuration-catalog__table th:nth-child(4) {
  width: 7%;
}
.configuration-catalog__table th:nth-child(5) {
  width: 4%;
  min-width: 52px;
}
.configuration-catalog__table--with-project th:nth-child(1) {
  width: 30%;
}
.configuration-catalog__table--with-project th:nth-child(2) {
  width: 20%;
}
.configuration-catalog__table--with-project th:nth-child(3) {
  width: 24%;
}
.configuration-catalog__table--with-project th:nth-child(4) {
  width: 14%;
}
.configuration-catalog__table--with-project th:nth-child(5) {
  width: 7%;
}
.configuration-catalog__table--with-project th:nth-child(6) {
  width: 5%;
  min-width: 52px;
}
.configuration-catalog__table td {
  height: 64px;
  padding: 7px 12px;
  border-top: 1px solid var(--border);
  vertical-align: middle;
}
.configuration-catalog__row {
  min-height: 64px;
}
.configuration-catalog__sentinel {
  min-height: 1px;
}
.configuration-catalog__identity {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  color: var(--text);
  text-decoration: none;
}
.configuration-catalog__identity:hover strong {
  color: var(--accent-strong);
  text-decoration: underline;
}
.configuration-catalog__project a {
  display: block;
  overflow: hidden;
  color: var(--text);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.configuration-catalog__identity strong,
.configuration-catalog__row td > strong,
.configuration-catalog__row small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.configuration-catalog__row small {
  margin-top: 2px;
  color: var(--muted);
}
.configuration-catalog__open-heading,
.configuration-catalog__open-cell {
  text-align: center !important;
}
.configuration-catalog__open-cell .icon-button {
  display: inline-flex;
}
</style>
