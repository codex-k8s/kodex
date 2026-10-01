<script setup lang="ts">
import {
  ChevronRight,
  List,
  PackageOpen,
  Pencil,
  Play,
  Search,
} from "@lucide/vue";
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import { usePlatformStore } from "@/features/platform/store";
import { useRuntimeStore } from "@/features/runtime/store";
import type { PlatformResourceKind } from "@/shared/api/generated/asyncapi/PlatformResourceKind";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";
import AgentAvatar from "@/features/agents/catalog/AgentAvatar.vue";
import { toAgentCatalogItem } from "@/features/agents/catalog/model";
import { workflowLaunchReadiness } from "@/features/platform/workflow-launch";
import type { Workflow } from "@/shared/api/generated/openapi/types.gen";
import SafeSummary from "@/shared/ui/SafeSummary.vue";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import EntityIcon from "@/shared/ui/EntityIcon.vue";
import {
  agentCatalogEntry,
  environmentCatalogEntry,
  loadCatalog,
  membershipCatalogEntry,
  scheduleCatalogEntry,
  secretCatalogEntry,
  workflowCatalogEntry,
  type CatalogEntry,
  type CatalogKind,
} from "./api";
const props = defineProps<{
  kind: CatalogKind;
  projectRef?: string;
}>();
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
const runtime = useRuntimeStore();
const searchId = useId();
const query = ref("");
const items = ref<CatalogEntry[]>([]);
const projects = computed(() => platform.projects);
const pageToken = ref<string>();
const loading = ref(false);
const problem = ref<AppProblem>();
const scrollRoot = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const pageSize = useAdaptiveCursorPageSize({
  container: scrollRoot,
  itemSelector: ".organization-catalog__row",
  itemCount: () => items.value.length,
  estimatedItemHeight: 64,
  estimatedColumns: 1,
});
useCursorInfiniteScroll({
  sentinel,
  enabled: () => Boolean(pageToken.value) && !loading.value && !problem.value,
  loadMore: () => load(true),
});
let controller: AbortController | undefined;
let generation = 0;
const cursors = new Set<string>();
let timer: ReturnType<typeof setTimeout> | undefined;
const realtimeKind = computed<PlatformResourceKind | undefined>(() => {
  switch (props.kind) {
    case "agents":
      return "AGENT";
    case "workflows":
      return "WORKFLOW";
    case "automations":
      return "SCHEDULE";
    case "environments":
      return "RUNTIME_ENVIRONMENT";
    case "members":
      return "MEMBERSHIP";
    case "secrets":
      return "RUNTIME_SECRET";
  }
  return undefined;
});
const realtimeVersion = computed(() => {
  const scope = props.projectRef;
  const snapshot = realtimeKind.value
    ? platform.realtimeSnapshot(realtimeKind.value, scope)
    : undefined;
  const values = (() => {
    switch (props.kind) {
      case "agents":
        return Object.values(platform.agents);
      case "workflows":
        return Object.values(platform.workflows);
      case "automations":
        return Object.values(platform.schedules);
      case "environments":
        return Object.values(runtime.environments);
      case "members":
        return Object.values(platform.memberships);
      case "secrets":
        return Object.values(platform.runtimeSecrets);
    }
  })();
  return JSON.stringify([
    snapshot?.scopeKey,
    snapshot?.nextPageToken,
    snapshot?.total,
    values
      .filter((value) => !scope || value.projectRef === scope)
      .map((value) => [value.ref, value.version]),
  ]);
});
function workflowLaunch(workflow: Workflow) {
  return workflowLaunchReadiness(workflow);
}
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
  }
  pageToken.value = undefined;
  cursors.clear();
  problem.value = undefined;
  loading.value = false;
}
function applyRealtime(): boolean {
  const kind = realtimeKind.value;
  if (!kind) return false;
  const snapshot = platform.realtimeSnapshot(kind, props.projectRef);
  if (!snapshot) {
    loading.value = true;
    return true;
  }
  controller?.abort();
  generation += 1;
  const scope = props.projectRef;
  const values = (() => {
    switch (props.kind) {
      case "agents":
        return Object.values(platform.agents).map(agentCatalogEntry);
      case "workflows":
        return Object.values(platform.workflows).map(workflowCatalogEntry);
      case "automations":
        return Object.values(platform.schedules).map(scheduleCatalogEntry);
      case "environments":
        return Object.values(runtime.environments).map(environmentCatalogEntry);
      case "members":
        return Object.values(platform.memberships).map(membershipCatalogEntry);
      case "secrets":
        return Object.values(platform.runtimeSecrets).map(secretCatalogEntry);
    }
  })().filter((value) => !scope || value.projectRef === scope);
  if (
    new Set(values.map((value) => value.ref)).size !== values.length ||
    values.some((value) => scope && value.projectRef !== scope)
  )
    throw new Error("Invalid realtime organization catalog scope");
  cursors.clear();
  items.value = values;
  pageToken.value = snapshot.nextPageToken;
  problem.value = undefined;
  loading.value = false;
  return true;
}
watch(
  () => [props.kind, props.projectRef, query.value],
  () => {
    invalidate();
    if (!query.value.trim() && applyRealtime()) return;
    loading.value = true;
    timer = setTimeout(() => {
      void load();
    }, 500);
  },
  { immediate: true, flush: "sync" },
);
watch(realtimeVersion, () => {
  if (!query.value.trim()) applyRealtime();
});
onBeforeUnmount(() => {
  controller?.abort();
  if (timer) clearTimeout(timer);
  generation += 1;
});
</script>
<template>
  <section ref="scrollRoot" class="organization-catalog">
    <label
      v-if="items.length || query.trim()"
      class="organization-catalog__search"
      :for="searchId"
      ><Search :size="18" /><input
        :id="searchId"
        v-model="query"
        :name="searchId"
        type="search"
        :aria-label="
          kind === 'members'
            ? $t('catalog.memberSearchPlaceholder')
            : $t('catalog.searchPlaceholder')
        "
        :placeholder="
          kind === 'members'
            ? $t('catalog.memberSearchPlaceholder')
            : $t('catalog.searchPlaceholder')
        "
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
    <div v-if="items.length" class="organization-catalog__table-wrap">
      <table
        class="organization-catalog__table"
        :class="{
          'organization-catalog__table--project': !!projectRef,
          'organization-catalog__table--actions':
            kind === 'agents' || kind === 'workflows',
        }"
      >
        <thead>
          <tr>
            <th scope="col">{{ $t("catalog.table.name") }}</th>
            <th v-if="!projectRef" scope="col">
              {{ $t("catalog.table.project") }}
            </th>
            <th scope="col">{{ $t("catalog.table.details") }}</th>
            <th scope="col">{{ $t("catalog.table.state") }}</th>
            <th scope="col">
              {{
                $t(
                  kind === "members"
                    ? "catalog.table.role"
                    : kind === "agents"
                      ? "agents.runtime"
                      : kind === "workflows"
                        ? "catalog.table.activity"
                        : "catalog.table.version",
                )
              }}
            </th>
            <th scope="col" class="organization-catalog__open-heading">
              {{
                $t(
                  kind === "agents" || kind === "workflows"
                    ? "common.actions"
                    : "catalog.table.open",
                )
              }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="entry in items"
            :key="entry.ref"
            class="organization-catalog__row"
          >
            <td>
              <div class="organization-catalog__identity">
                <AgentAvatar
                  v-if="entry.agent"
                  :initials="toAgentCatalogItem(entry.agent).initials"
                  :source="toAgentCatalogItem(entry.agent).avatarUrl"
                  :tone="toAgentCatalogItem(entry.agent).avatarTone"
                  size="compact"
                />
                <EntityIcon v-else :kind="entityIconKind" />
                <RouterLink :to="entryRoute(entry)" :title="entry.title">{{
                  entry.title
                }}</RouterLink>
              </div>
            </td>
            <td v-if="!projectRef">
              <RouterLink
                class="organization-catalog__project-link"
                :to="`/projects/${encodeURIComponent(entry.projectRef)}/${kind}`"
                :title="projects[entry.projectRef]?.name ?? $t('app.project')"
              >
                {{ projects[entry.projectRef]?.name ?? $t("app.project") }}
              </RouterLink>
            </td>
            <td>
              <span
                class="organization-catalog__description"
                :title="entry.description"
                >{{ entry.description || $t("common.noData") }}</span
              >
              <small
                v-if="
                  entry.agent?.roleDefinitionName &&
                  entry.agent.roleDefinitionName !== entry.title
                "
                >{{ entry.agent.roleDefinitionName }}</small
              >
              <small v-if="entry.agent?.currentActivity"
                ><SafeSummary :content="entry.agent.currentActivity"
              /></small>
              <small
                v-if="entry.workflow"
                :title="
                  $t('catalog.workflowSummary', {
                    stages: entry.workflow.cardSummary.stageCount,
                    agents: entry.workflow.cardSummary.uniqueAgentCount,
                    gates: entry.workflow.cardSummary.pendingGateCount,
                  })
                "
                >{{
                  $t("catalog.workflowSummary", {
                    stages: entry.workflow.cardSummary.stageCount,
                    agents: entry.workflow.cardSummary.uniqueAgentCount,
                    gates: entry.workflow.cardSummary.pendingGateCount,
                  })
                }}</small
              >
              <small
                v-if="
                  !entry.agent && !entry.workflow && entry.meta.some(Boolean)
                "
                :title="entry.meta.filter(Boolean).join(' · ')"
                >{{ entry.meta.filter(Boolean).join(" · ") }}</small
              >
            </td>
            <td><StatusBadge :state="entry.state" /></td>
            <td>
              <span v-if="kind === 'members' && entry.role">{{
                $t(`access.platformRoles.${entry.role}`)
              }}</span>
              <template v-else-if="entry.agent">
                <strong class="organization-catalog__runtime-name">{{
                  entry.agent.runtimeName
                }}</strong>
                <small
                  >{{
                    entry.agent.runtimeModel ||
                    entry.agent.runtimeProvider ||
                    $t("common.noData")
                  }}<template v-if="!entry.agent.runtimeReady">
                    · {{ $t("states.UNAVAILABLE") }}</template
                  ></small
                >
              </template>
              <template v-else-if="entry.workflow">
                <span>{{
                  $t("catalog.activeRuns", {
                    count: entry.workflow.cardSummary.activeRunCount,
                  })
                }}</span>
                <small>{{
                  $t("catalog.pendingGates", {
                    count: entry.workflow.cardSummary.pendingGateCount,
                  })
                }}</small>
              </template>
              <span v-else>v{{ entry.version }}</span>
            </td>
            <td class="organization-catalog__open-cell">
              <nav
                class="organization-catalog__actions"
                :aria-label="entry.title"
              >
                <RouterLink
                  :to="entryRoute(entry)"
                  class="icon-button"
                  :aria-label="$t('common.open')"
                  :title="$t('common.open')"
                  ><ChevronRight :size="18" aria-hidden="true"
                /></RouterLink>
                <RouterLink
                  v-if="
                    entry.agent?.currentRunRef && entry.agent.state !== 'READY'
                  "
                  :to="`/runs/${encodeURIComponent(entry.agent.currentRunRef)}`"
                  class="icon-button"
                  :aria-label="$t('entityCards.openRun')"
                  :title="$t('entityCards.openRun')"
                  ><List :size="18" aria-hidden="true"
                /></RouterLink>
                <template v-if="entry.workflow">
                  <RouterLink
                    v-if="workflowLaunch(entry.workflow)?.allowedToSubmit"
                    :to="{
                      path: `/projects/${encodeURIComponent(entry.projectRef)}/runs/new`,
                      query: {
                        targetType: 'WORKFLOW',
                        targetRef: entry.workflow.ref,
                      },
                    }"
                    class="icon-button"
                    :aria-label="$t('common.launch')"
                    :title="$t('common.launch')"
                    ><Play :size="18" aria-hidden="true"
                  /></RouterLink>
                  <button
                    v-else
                    class="icon-button"
                    type="button"
                    disabled
                    :aria-label="$t('common.launch')"
                    :title="
                      workflowLaunch(entry.workflow)
                        ? $t(
                            `workflowLaunch.reasons.${workflowLaunch(entry.workflow)!.reason}`,
                          )
                        : $t('workflowLaunch.missing')
                    "
                  >
                    <Play :size="18" aria-hidden="true" />
                  </button>
                  <RouterLink
                    :to="`/projects/${encodeURIComponent(entry.projectRef)}/runs`"
                    class="icon-button"
                    :aria-label="$t('nav.runs')"
                    :title="$t('nav.runs')"
                    ><List :size="18" aria-hidden="true"
                  /></RouterLink>
                  <RouterLink
                    v-if="entry.workflow.nextActions.includes('EDIT')"
                    :to="`${entry.path}#workflow-editor`"
                    class="icon-button"
                    :aria-label="$t('common.edit')"
                    :title="$t('common.edit')"
                    ><Pencil :size="18" aria-hidden="true"
                  /></RouterLink>
                </template>
              </nav>
            </td>
          </tr>
        </tbody>
      </table>
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
  width: 25%;
}
.organization-catalog__table th:nth-child(2) {
  width: 21%;
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
.organization-catalog__table--actions th:first-child {
  width: 27%;
}
.organization-catalog__table--actions th:nth-child(2) {
  width: 20%;
}
.organization-catalog__table--actions th:nth-child(3) {
  width: 22%;
}
.organization-catalog__table--actions th:nth-child(4) {
  width: 10%;
}
.organization-catalog__table--actions th:nth-child(5) {
  width: 11%;
}
.organization-catalog__table--actions th:last-child {
  width: 10%;
}
.organization-catalog__table--actions.organization-catalog__table--project
  th:first-child {
  width: 28%;
}
.organization-catalog__table--actions.organization-catalog__table--project
  th:nth-child(2) {
  width: 32%;
}
.organization-catalog__table--actions.organization-catalog__table--project
  th:nth-child(3) {
  width: 12%;
}
.organization-catalog__table--actions.organization-catalog__table--project
  th:nth-child(4) {
  width: 13%;
}
.organization-catalog__table--actions.organization-catalog__table--project
  th:last-child {
  width: 15%;
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
  min-width: 0;
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
.organization-catalog__project-link {
  display: block;
  overflow: hidden;
  color: var(--text);
  font-weight: 600;
  text-decoration: none;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.organization-catalog__project-link:hover {
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
.organization-catalog__runtime-name {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.organization-catalog__actions {
  display: flex;
  justify-content: center;
  gap: 2px;
}
.organization-catalog__open-heading,
.organization-catalog__open-cell {
  text-align: center !important;
}
</style>
