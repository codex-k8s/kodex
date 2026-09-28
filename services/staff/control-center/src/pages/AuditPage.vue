<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";
import { SearchX } from "@lucide/vue";

import { usePlatformStore } from "@/features/platform/store";
import { accessProjectOptions } from "@/features/access/entity-pickers";
import type { AuditEvent } from "@/shared/api/generated/openapi/types.gen";
import AsyncState from "@/shared/ui/AsyncState.vue";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import type { AsyncEntityOption } from "@/shared/ui/async-entity-picker";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";
import PageFrame from "@/shared/ui/PageFrame.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const route = useRoute();
const router = useRouter();
const i18n = useI18n();
const query = ref("");
const chosenProject = ref<AsyncEntityOption>();
const searchId = useId();
const projectRef = computed(() =>
  typeof route.query.projectRef === "string"
    ? route.query.projectRef
    : undefined,
);
const selectedProject = computed<AsyncEntityOption | undefined>(() => {
  if (!projectRef.value) return undefined;
  const known = platform.projects[projectRef.value];
  if (known)
    return { ref: known.ref, title: known.name, description: known.purpose };
  if (chosenProject.value?.ref === projectRef.value) return chosenProject.value;
  return { ref: projectRef.value, title: i18n.t("audit.selectedProject") };
});
const showTechnical = computed(() => route.query.technical === "1");
const list = computed(() => platform.auditEvents);
const hasMore = computed(() => Boolean(platform.auditNextPageToken));
const loadingMore = computed(() => Boolean(platform.loading.auditMore));
const listRoot = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const pageSize = useAdaptiveCursorPageSize({
  container: listRoot,
  itemSelector: ".audit-table__row",
  itemCount: () => list.value.length,
  estimatedItemHeight: 62,
});
let searchTimer: ReturnType<typeof setTimeout> | undefined;

function auditLabel(
  group: "executorValue" | "resourceTypeValue",
  value: string,
) {
  const key = `audit.${group}.${value}`;
  return i18n.te(key) ? i18n.t(key) : value;
}

function actionSummary(event: AuditEvent): string {
  const prefix = "runtime-secret-draft.";
  if (!event.action.startsWith(prefix)) return event.safeSummary;
  const action = event.action.slice(prefix.length);
  const key = `audit.secretDraftAction.${action}`;
  return i18n.te(key) ? i18n.t(key) : event.safeSummary;
}

function resourceName(event: AuditEvent): string {
  if (
    event.action.startsWith("runtime-secret-draft.") &&
    event.resourceName === event.safeSummary
  )
    return i18n.t("audit.protectedSecret");
  return event.resourceName;
}

function selectProject(value: string | null | readonly string[]): void {
  const next = typeof value === "string" ? value : undefined;
  if (next === projectRef.value) return;
  void router.replace({ query: { ...route.query, projectRef: next } });
}

function toggleTechnical(event: Event): void {
  const next = { ...route.query };
  if ((event.currentTarget as HTMLInputElement).checked) next.technical = "1";
  else delete next.technical;
  void router.replace({ query: next });
}

async function load(): Promise<void> {
  await platform.loadAudit(
    projectRef.value,
    query.value,
    pageSize.value,
    "",
    showTechnical.value,
  );
}

function loadMore(): Promise<void> {
  return platform.loadMoreAudit(
    projectRef.value,
    query.value,
    pageSize.value,
    "",
    showTechnical.value,
  );
}

function loadSelectedProject(): void {
  if (projectRef.value && !platform.projects[projectRef.value])
    void platform.loadProject(projectRef.value);
}

useCursorInfiniteScroll({
  sentinel,
  enabled: () => hasMore.value && !loadingMore.value,
  loadMore,
});

watch(query, () => {
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => void load(), 250);
});
watch([projectRef, showTechnical], () => {
  loadSelectedProject();
  void load();
});
onMounted(() => {
  loadSelectedProject();
  void load();
});
onUnmounted(() => {
  if (searchTimer) clearTimeout(searchTimer);
});
</script>

<template>
  <PageFrame :title="$t('audit.title')" :subtitle="$t('audit.subtitle')">
    <div class="audit-filters">
      <label class="field audit-search" :for="searchId"
        ><span>{{ $t("audit.search") }}</span
        ><input
          :id="searchId"
          v-model="query"
          name="audit-search"
          type="search"
          :placeholder="$t('audit.searchPlaceholder')"
          autocomplete="off"
      /></label>
      <div class="field audit-project-filter">
        <span>{{ $t("audit.project") }}</span>
        <AsyncEntityPicker
          :model-value="projectRef"
          :selected="selectedProject"
          :load-page="accessProjectOptions"
          :labels="{
            label: $t('audit.project'),
            searchPlaceholder: $t('audit.findProject'),
            loading: $t('common.loading'),
            loadingMore: $t('common.loading'),
            empty: $t('common.empty'),
            error: $t('errors.default'),
            retry: $t('common.retry'),
          }"
          :placeholder="$t('audit.allProjects')"
          :trigger-label="$t('audit.project')"
          @select="chosenProject = $event"
          @update:model-value="selectProject"
        />
      </div>
      <label class="audit-technical-filter">
        <input
          name="audit-show-technical"
          type="checkbox"
          :checked="showTechnical"
          @change="toggleTechnical"
        />
        <span>{{ $t("audit.showTechnical") }}</span>
      </label>
    </div>
    <AsyncState
      :loading="platform.loading.audit"
      :problem="platform.problems.audit"
      :empty="list.length === 0 && !hasMore"
      :empty-title="$t('audit.emptyTitle')"
      @retry="load"
    >
      <template #empty-icon><SearchX :size="20" /></template>
      <div
        ref="listRoot"
        class="audit-table"
        role="table"
        :aria-label="$t('audit.title')"
      >
        <div class="audit-table__header" role="row">
          <strong role="columnheader">{{ $t("audit.time") }}</strong
          ><strong role="columnheader">{{ $t("audit.initiator") }}</strong
          ><strong role="columnheader">{{ $t("audit.action") }}</strong
          ><strong role="columnheader">{{ $t("audit.resource") }}</strong
          ><strong role="columnheader">{{ $t("audit.outcome") }}</strong>
        </div>
        <article
          v-for="event in list"
          :key="event.ref"
          class="audit-table__row"
          role="row"
        >
          <time role="cell" :datetime="event.occurredAt">{{
            new Date(event.occurredAt).toLocaleString()
          }}</time>
          <div role="cell">
            <strong>{{ event.initiator.displayName }}</strong
            ><small>{{ auditLabel("executorValue", event.executor) }}</small>
          </div>
          <div role="cell">
            <strong>{{ actionSummary(event) }}</strong>
            <details class="audit-technical">
              <summary>{{ $t("audit.technicalDetails") }}</summary>
              <small>{{ $t("audit.operationCode") }}: {{ event.action }}</small>
            </details>
          </div>
          <div role="cell">
            <strong>{{ resourceName(event) }}</strong
            ><small>{{
              auditLabel("resourceTypeValue", event.resourceType)
            }}</small>
          </div>
          <StatusBadge role="cell" :state="event.outcome" />
        </article>
      </div>
      <div
        v-if="hasMore || loadingMore || platform.problems.auditMore"
        ref="sentinel"
        class="audit-pagination"
        aria-live="polite"
      >
        <span v-if="loadingMore">{{ $t("audit.loadingMore") }}</span>
        <button
          v-else-if="platform.problems.auditMore"
          class="button"
          type="button"
          @click="loadMore"
        >
          {{ $t("common.retry") }}
        </button>
      </div>
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.audit-filters {
  display: grid;
  grid-template-columns:
    minmax(280px, 520px) minmax(220px, 340px)
    minmax(220px, auto);
  gap: 12px;
  margin-bottom: 18px;
  align-items: end;
}
.audit-filters .field {
  min-width: 0;
}
.audit-search input {
  min-height: 42px;
}
.audit-project-filter :deep(.async-picker__trigger) {
  min-height: 42px;
}
.audit-technical-filter {
  display: inline-flex;
  min-height: 42px;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  cursor: pointer;
}
.audit-table {
  display: grid;
  border: 1px solid var(--border);
  border-radius: 10px;
  overflow: hidden;
  background: var(--surface);
}
.audit-table__header,
.audit-table__row {
  display: grid;
  grid-template-columns: 160px 1fr 1.2fr 1fr auto;
  gap: 12px;
  align-items: center;
  padding: 12px 14px;
}
.audit-table__header {
  background: var(--panel);
  border-bottom: 1px solid var(--border);
}
.audit-table__row + .audit-table__row {
  border-top: 1px solid var(--border);
}
.audit-table__row div {
  display: grid;
  min-width: 0;
  gap: 3px;
}
.audit-table small {
  color: var(--muted);
}
.audit-technical small {
  display: block;
  overflow-wrap: anywhere;
  word-break: break-word;
}
.audit-technical summary {
  color: var(--muted);
  cursor: pointer;
  font-size: 0.78rem;
}
.audit-pagination {
  display: flex;
  min-height: 48px;
  align-items: center;
  justify-content: center;
  color: var(--muted);
}
@media (max-width: 800px) {
  .audit-table__header {
    display: none;
  }
  .audit-table__row {
    grid-template-columns: 1fr auto;
  }
  .audit-table__row > * {
    grid-column: 1/-1;
  }
  .audit-table__row .status-badge {
    grid-column: 2;
    grid-row: 1;
  }
  .audit-table__row time {
    grid-column: 1;
    grid-row: 1;
  }
}
</style>
