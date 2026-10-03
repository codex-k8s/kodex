<script setup lang="ts">
import { ChevronRight, Plus, RefreshCw, Search, X } from "@lucide/vue";
import { onBeforeUnmount, ref, useId, watch } from "vue";
import type { ContextResourceState } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import EntityIcon from "@/shared/ui/EntityIcon.vue";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";
import { listContext, type ContextItem, type ContextKind } from "./api";
import { loadCatalogProject } from "@/features/catalogs/api";
const props = defineProps<{
  kind: ContextKind;
  projectRef?: string;
  agentRef?: string;
}>();
const fieldPrefix = `context-catalog-${useId()}`;
const items = ref<ContextItem[]>([]);
const scrollRoot = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const pageSize = useAdaptiveCursorPageSize({
  container: scrollRoot,
  itemSelector: ".context-row",
  itemCount: () => items.value.length,
  estimatedViewportHeight: 576,
  estimatedItemHeight: 64,
  minimum: 8,
  maximum: 100,
});
const query = ref("");
const state = ref<ContextResourceState>("ACTIVE");
const total = ref(0);
const cursor = ref("");
const loading = ref(false);
const problem = ref<AppProblem>();
let timer: ReturnType<typeof setTimeout> | undefined;
let controller: AbortController | undefined;
let generation = 0;
const cursors = new Set<string>();
const projectNames = ref<Record<string, string>>({});
function title(item: ContextItem): string {
  const revision =
    "draftRevision" in item
      ? (item.draftRevision ?? item.currentRevision)
      : item.currentRevision;
  return revision ? ("title" in revision ? revision.title : revision.name) : "";
}
function revisionNumber(item: ContextItem): number {
  return "draftRevision" in item
    ? ((item.draftRevision ?? item.currentRevision)?.revision ?? 0)
    : (item.currentRevision?.revision ?? 0);
}
function retention(item: ContextItem): string {
  const revision = item.currentRevision;
  return revision && "retentionUntil" in revision
    ? revision.retentionUntil
    : "";
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
      pageSize: pageSize.value,
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
useCursorInfiniteScroll({
  root: scrollRoot,
  sentinel,
  enabled: () => Boolean(cursor.value) && !loading.value && !problem.value,
  loadMore: () => load(true),
});
</script>
<template>
  <section
    class="context-catalog panel"
    :aria-label="$t(`contextResources.${kind}`)"
  >
    <header class="context-toolbar">
      <label class="context-search"
        ><Search :size="18" aria-hidden="true" /><input
          v-model="query"
          :id="`${fieldPrefix}-search`"
          :name="`${fieldPrefix}-search`"
          type="search"
          :aria-label="$t('common.search')"
          :placeholder="$t('common.search')"
          maxlength="500" /><button
          v-if="query"
          type="button"
          :title="$t('contextResources.clearSearch')"
          :aria-label="$t('contextResources.clearSearch')"
          @click="query = ''"
        >
          <X :size="15" aria-hidden="true" /></button
      ></label>
      <select
        v-model="state"
        :id="`${fieldPrefix}-state`"
        :name="`${fieldPrefix}-state`"
        :aria-label="$t('contextResources.state')"
      >
        <option
          v-for="value in ['ACTIVE', 'ARCHIVED', 'EXPIRED', 'PURGED']"
          :key="value"
          :value="value"
        >
          {{ $t(`contextResources.states.${value}`) }}
        </option>
      </select>
      <span class="context-toolbar__count">{{
        $t("files.loadedOfTotal", { loaded: items.length, total })
      }}</span>
      <button
        class="icon-button"
        :disabled="loading"
        :title="$t('vfs.refresh')"
        :aria-label="$t('vfs.refresh')"
        @click="load()"
      >
        <RefreshCw :size="18" />
      </button>
      <RouterLink
        class="button button--primary"
        :to="{
          name: projectRef ? 'project-context-resource' : 'context-resource',
          params: {
            kind,
            resourceRef: 'new',
            ...(projectRef ? { projectRef } : {}),
          },
          query: agentRef ? { agentRef } : {},
        }"
        ><Plus :size="18" />{{ $t("common.create") }}</RouterLink
      >
    </header>
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <div ref="scrollRoot" class="context-catalog__scroll">
      <div v-if="items.length" class="context-catalog__table-wrap">
        <table class="context-catalog__table">
          <thead>
            <tr>
              <th scope="col">{{ $t("common.name") }}</th>
              <th v-if="!projectRef" scope="col">
                {{ $t("contextResources.project") }}
              </th>
              <th scope="col">{{ $t("common.status") }}</th>
              <th scope="col">{{ $t("contextResources.revision") }}</th>
              <th v-if="kind === 'memory'" scope="col">
                {{ $t("contextResources.retention") }}
              </th>
              <th scope="col">{{ $t("roleImages.updatedAt") }}</th>
              <th scope="col" class="context-catalog__open">
                {{ $t("common.open") }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in items" :key="item.ref" class="context-row">
              <td>
                <div class="context-catalog__identity">
                  <EntityIcon :kind="kind === 'skills' ? 'SKILL' : 'MEMORY'" />
                  <RouterLink
                    :to="{
                      name: 'project-context-resource',
                      params: {
                        kind,
                        resourceRef: item.ref,
                        projectRef: item.projectRef,
                      },
                      query: agentRef ? { agentRef } : {},
                    }"
                    :title="title(item) || $t('common.noData')"
                    >{{ title(item) || $t("common.noData") }}</RouterLink
                  >
                </div>
              </td>
              <td v-if="!projectRef">
                <RouterLink
                  :to="`/projects/${encodeURIComponent(item.projectRef)}`"
                >
                  {{ projectNames[item.projectRef] ?? $t("common.noData") }}
                </RouterLink>
              </td>
              <td><StatusBadge :state="item.state" /></td>
              <td>
                <span v-if="revisionNumber(item)"
                  >rev {{ revisionNumber(item) }}</span
                >
                <span v-else>{{ $t("common.noData") }}</span>
                <StatusBadge
                  v-if="'draftRevision' in item && item.draftRevision"
                  :state="item.draftRevision.state"
                />
              </td>
              <td v-if="kind === 'memory'">
                <time v-if="retention(item)" :datetime="retention(item)">
                  {{
                    new Date(retention(item)).toLocaleDateString($i18n.locale)
                  }}
                </time>
                <span v-else>{{ $t("common.noData") }}</span>
              </td>
              <td>
                <time :datetime="item.updatedAt">{{
                  new Date(item.updatedAt).toLocaleString($i18n.locale)
                }}</time>
              </td>
              <td class="context-catalog__open">
                <RouterLink
                  class="icon-button"
                  :to="{
                    name: 'project-context-resource',
                    params: {
                      kind,
                      resourceRef: item.ref,
                      projectRef: item.projectRef,
                    },
                    query: agentRef ? { agentRef } : {},
                  }"
                  :aria-label="$t('common.open')"
                  :title="$t('common.open')"
                  ><ChevronRight :size="18" aria-hidden="true"
                /></RouterLink>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p
        v-if="!loading && !problem && !items.length"
        class="context-catalog__empty"
      >
        {{
          $t(
            query
              ? "contextResources.emptySearch"
              : kind === "skills"
                ? "contextResources.emptySkills"
                : "contextResources.emptyMemory",
          )
        }}
      </p>
      <div
        v-if="cursor"
        ref="sentinel"
        class="context-catalog__sentinel"
        role="status"
      >
        <span v-if="loading">{{ $t("common.loading") }}</span>
      </div>
    </div>
  </section>
</template>
<style scoped>
.context-catalog {
  min-width: 0;
  padding: 0;
  overflow: hidden;
}
.context-toolbar {
  display: flex;
  min-height: 58px;
  gap: 8px;
  align-items: center;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
}
.context-toolbar select {
  width: 160px;
  min-height: var(--control-height);
  flex: 0 0 160px;
}
.context-toolbar__count {
  margin-left: auto;
  color: var(--muted);
  font-size: 0.78rem;
  white-space: nowrap;
}
.context-search {
  display: flex;
  min-width: 210px;
  flex: 1 1 320px;
  align-items: center;
  gap: 7px;
  padding: 0 9px;
  border: 1px solid var(--border-strong);
  border-radius: 6px;
}
.context-search input {
  width: 100%;
  min-width: 0;
  min-height: var(--control-height);
  padding: 0;
  border: 0;
  outline: 0;
}
.context-search button {
  display: grid;
  width: 28px;
  height: 28px;
  flex: 0 0 28px;
  place-items: center;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--muted);
  cursor: pointer;
}
.context-catalog__scroll {
  max-height: 576px;
  overflow: auto;
}
.context-catalog__table-wrap {
  min-width: 0;
  overflow-x: auto;
}
.context-catalog__table {
  width: 100%;
  min-width: 850px;
  border-collapse: collapse;
}
.context-catalog__table th {
  padding: 10px 12px;
  background: var(--surface);
  color: var(--muted);
  font-size: 0.8rem;
  font-weight: 600;
  text-align: left;
  white-space: nowrap;
}
.context-catalog__table th:first-child {
  width: 36%;
}
.context-catalog__table td {
  height: 64px;
  padding: 9px 12px;
  border-top: 1px solid var(--border);
  vertical-align: middle;
}
.context-catalog__table td:nth-child(4) > * + * {
  margin-left: 6px;
}
.context-catalog__identity {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}
.context-catalog__identity a {
  min-width: 0;
  color: var(--text);
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  text-decoration: none;
  white-space: nowrap;
}
.context-catalog__identity a:hover {
  color: var(--accent-strong);
  text-decoration: underline;
}
.context-catalog__open {
  width: 68px;
  text-align: center !important;
}
.context-catalog__empty {
  margin: 0;
  padding: 44px 20px;
  color: var(--muted);
  text-align: center;
}
.context-catalog__sentinel {
  min-height: 1px;
}
@media (max-width: 600px) {
  .context-toolbar {
    flex-wrap: wrap;
  }
  .context-toolbar select {
    width: auto;
    flex: 1 1 160px;
  }
}
</style>
