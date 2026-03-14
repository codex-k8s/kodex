<template>
  <div class="mission-control-page">
    <PageHeader :title="t('pages.missionControl.title')" :hint="t('pages.missionControl.hint')">
      <template #actions>
        <AdaptiveBtn
          variant="tonal"
          icon="mdi-refresh"
          :label="t('common.refresh')"
          :loading="missionControl.refreshing"
          @click="refreshDashboard"
        />
      </template>
    </PageHeader>

    <VAlert v-if="missionControl.error" type="error" variant="tonal" class="mt-4">
      {{ t(missionControl.error.messageKey) }}
    </VAlert>

    <VAlert v-if="realtimeAlert" :type="realtimeAlert.type" variant="tonal" class="mt-4">
      <div class="d-flex flex-column ga-2">
        <div class="font-weight-medium">{{ t(realtimeAlert.titleKey) }}</div>
        <div class="text-body-2">{{ realtimeAlert.text }}</div>
        <div class="d-flex flex-wrap ga-2">
          <AdaptiveBtn
            variant="text"
            icon="mdi-refresh"
            :label="t('pages.missionControl.refreshNow')"
            :loading="missionControl.refreshing"
            @click="refreshDashboard"
          />
          <AdaptiveBtn
            v-if="missionControl.effectiveFreshnessStatus !== 'fresh'"
            variant="text"
            icon="mdi-format-list-bulleted"
            :label="t('pages.missionControl.listFallback')"
            @click="updateRoute({ viewMode: 'list' })"
          />
        </div>
      </div>
    </VAlert>

    <div class="mission-control-summary mt-4">
      <VCard v-for="item in summaryCards" :key="item.key" class="mission-control-summary__card" variant="outlined">
        <VCardText>
          <div class="mission-control-summary__label">{{ t(item.labelKey) }}</div>
          <div class="mission-control-summary__value">{{ item.value }}</div>
          <div class="mission-control-summary__hint">{{ item.hint }}</div>
        </VCardText>
      </VCard>
    </div>

    <VCard class="mt-4 mission-control-toolbar" variant="outlined">
      <VCardText class="mission-control-toolbar__content">
        <VSelect
          :model-value="routeState.activeFilter"
          class="mission-control-toolbar__filter"
          density="compact"
          variant="solo-filled"
          hide-details
          :label="t('pages.missionControl.filterLabel')"
          :items="filterOptions"
          @update:model-value="onFilterChange"
        />

        <VTextField
          v-model="searchInput"
          class="mission-control-toolbar__search"
          density="compact"
          variant="solo-filled"
          hide-details
          clearable
          :label="t('pages.missionControl.searchLabel')"
          prepend-inner-icon="mdi-magnify"
          @click:clear="onSearchClear"
          @keyup.enter="applySearch"
        />

        <VBtnToggle
          :model-value="routeState.viewMode"
          class="mission-control-toolbar__toggle"
          color="primary"
          density="comfortable"
          mandatory
        >
          <VBtn value="board" @click="updateRoute({ viewMode: 'board' })">
            <VIcon icon="mdi-view-dashboard-outline" start />
            {{ t("pages.missionControl.viewModes.board") }}
          </VBtn>
          <VBtn value="list" @click="updateRoute({ viewMode: 'list' })">
            <VIcon icon="mdi-format-list-bulleted" start />
            {{ t("pages.missionControl.viewModes.list") }}
          </VBtn>
        </VBtnToggle>

        <div class="mission-control-toolbar__status">
          <VChip size="small" variant="tonal" :color="freshnessChip.color">
            {{ t("pages.missionControl.freshness") }}: {{ t(freshnessChip.labelKey) }}
          </VChip>
          <VChip size="small" variant="tonal" :color="realtimeChip.color">
            {{ t("pages.missionControl.realtime") }}: {{ t(realtimeChip.labelKey) }}
          </VChip>
        </div>
      </VCardText>
    </VCard>

    <VRow class="mt-4" density="compact">
      <VCol cols="12" :lg="missionControl.selectedRef ? 8 : 12">
        <VCard class="mission-control-canvas" variant="outlined">
          <VCardTitle class="d-flex align-center justify-space-between ga-2 flex-wrap">
            <div>
              <div class="text-subtitle-1 font-weight-bold">{{ t(canvasTitleKey) }}</div>
              <div class="text-body-2 text-medium-emphasis mt-1">
                {{
                  t("pages.missionControl.entitiesVisible", {
                    count: missionControl.entities.length,
                    total: missionControl.snapshot?.summary.total_entities ?? 0,
                  })
                }}
              </div>
            </div>
            <VChip v-if="missionControl.effectiveViewMode !== routeState.viewMode" size="small" variant="tonal" color="warning">
              {{ t("pages.missionControl.listFallbackActive") }}
            </VChip>
          </VCardTitle>

          <VCardText>
            <div v-if="missionControl.loading" class="mission-control-loading">
              <VSkeletonLoader type="article, article, article" />
            </div>

            <template v-else-if="missionControl.entities.length">
              <div v-if="missionControl.effectiveViewMode === 'board'" class="mission-control-board">
                <VCard
                  v-for="state in boardStateOrder"
                  :key="state"
                  class="mission-control-board__column"
                  variant="tonal"
                >
                  <VCardTitle class="mission-control-board__heading">
                    <div class="d-flex align-center justify-space-between ga-2">
                      <span>{{ t(`pages.missionControl.states.${state}`) }}</span>
                      <VChip size="x-small" variant="outlined">{{ groupedEntities[state].length }}</VChip>
                    </div>
                  </VCardTitle>
                  <VCardText class="mission-control-board__content">
                    <div v-if="groupedEntities[state].length" class="mission-control-board__stack">
                      <MissionControlEntityCard
                        v-for="entity in groupedEntities[state]"
                        :key="entity.entity_kind + ':' + entity.entity_public_id"
                        :entity="entity"
                        :selected="selectedEntityKey === entity.entity_kind + ':' + entity.entity_public_id"
                        :locale="locale"
                        @select="openEntity(entity.entity_kind, entity.entity_public_id)"
                      />
                    </div>
                    <div v-else class="text-body-2 text-medium-emphasis">
                      {{ t("pages.missionControl.emptyColumn") }}
                    </div>
                  </VCardText>
                </VCard>
              </div>

              <div v-else class="mission-control-list">
                <VTable density="comfortable" fixed-header>
                  <thead>
                    <tr>
                      <th>{{ t("pages.missionControl.columns.kind") }}</th>
                      <th>{{ t("pages.missionControl.columns.title") }}</th>
                      <th>{{ t("pages.missionControl.columns.state") }}</th>
                      <th>{{ t("pages.missionControl.columns.sync") }}</th>
                      <th>{{ t("pages.missionControl.columns.actor") }}</th>
                      <th>{{ t("pages.missionControl.columns.relations") }}</th>
                      <th>{{ t("pages.missionControl.columns.updated") }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr
                      v-for="entity in missionControl.entities"
                      :key="entity.entity_kind + ':' + entity.entity_public_id"
                      class="mission-control-list__row"
                      :class="{ 'mission-control-list__row--selected': selectedEntityKey === entity.entity_kind + ':' + entity.entity_public_id }"
                      @click="openEntity(entity.entity_kind, entity.entity_public_id)"
                    >
                      <td>
                        <VChip size="x-small" variant="tonal" :color="entityStateColor(entity.state)">
                          {{ t(entityKindLabel(entity.entity_kind)) }}
                        </VChip>
                      </td>
                      <td>
                        <div class="font-weight-medium">{{ entity.title }}</div>
                        <div class="text-body-2 text-medium-emphasis mono">{{ entity.provider_reference.external_id }}</div>
                      </td>
                      <td>{{ t(`pages.missionControl.states.${entity.state}`) }}</td>
                      <td>{{ t(`pages.missionControl.sync.${entity.sync_status}`) }}</td>
                      <td>{{ entity.primary_actor?.display_name || "-" }}</td>
                      <td>{{ entity.relation_count }}</td>
                      <td class="mono">{{ formatCompactDateTime(entity.last_timeline_at, locale) }}</td>
                    </tr>
                  </tbody>
                </VTable>
              </div>

              <div v-if="missionControl.hasMore" class="mt-4 d-flex justify-center">
                <AdaptiveBtn
                  variant="text"
                  icon="mdi-chevron-down"
                  :label="t('pages.missionControl.loadMore')"
                  :loading="missionControl.loadingMore"
                  @click="missionControl.loadSnapshot({ append: true })"
                />
              </div>
            </template>

            <div v-else class="mission-control-empty">
              <VIcon icon="mdi-radar" size="48" class="mb-4 text-medium-emphasis" />
              <div class="text-h6">{{ t("pages.missionControl.emptyTitle") }}</div>
              <div class="text-body-2 text-medium-emphasis mt-2">
                {{ t("pages.missionControl.emptyText") }}
              </div>
            </div>
          </VCardText>
        </VCard>
      </VCol>

      <VCol v-if="missionControl.selectedRef && !display.mobile.value" cols="12" lg="4">
        <MissionControlSidePanel
          :details="missionControl.selectedDetails"
          :loading="missionControl.selectedLoading"
          :error="missionControl.selectedDetailsError"
          :timeline="missionControl.selectedTimeline"
          :timeline-error="missionControl.selectedTimelineError"
          :timeline-loading="missionControl.selectedTimelineLoading"
          :has-more-timeline="missionControl.hasSelectedTimelineMore"
          :locale="locale"
          @close="closeEntity"
          @select-relation="openRelation"
          @load-more-timeline="missionControl.loadSelectedTimeline({ append: true })"
        />
      </VCol>
    </VRow>

    <VDialog v-model="mobilePanelOpen" fullscreen transition="dialog-bottom-transition">
      <VCard>
        <MissionControlSidePanel
          :details="missionControl.selectedDetails"
          :loading="missionControl.selectedLoading"
          :error="missionControl.selectedDetailsError"
          :timeline="missionControl.selectedTimeline"
          :timeline-error="missionControl.selectedTimelineError"
          :timeline-loading="missionControl.selectedTimelineLoading"
          :has-more-timeline="missionControl.hasSelectedTimelineMore"
          :locale="locale"
          @close="closeEntity"
          @select-relation="openRelation"
          @load-more-timeline="missionControl.loadSelectedTimeline({ append: true })"
        />
      </VCard>
    </VDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import { useDisplay } from "vuetify";

import PageHeader from "../../shared/ui/PageHeader.vue";
import AdaptiveBtn from "../../shared/ui/AdaptiveBtn.vue";
import { formatCompactDateTime } from "../../shared/lib/datetime";
import { bindRealtimePageLifecycle } from "../../shared/ws/lifecycle";
import { useMissionControlStore } from "../../features/mission-control/store";
import MissionControlEntityCard from "../../features/mission-control/MissionControlEntityCard.vue";
import MissionControlSidePanel from "../../features/mission-control/MissionControlSidePanel.vue";
import { buildMissionControlRouteQuery, groupMissionControlEntitiesByState, missionControlRouteStateEquals, normalizeMissionControlRouteQuery } from "../../features/mission-control/lib";
import { subscribeMissionControlRealtime } from "../../features/mission-control/realtime";
import { missionControlEntityKindLabelKey, missionControlStateColor } from "../../features/mission-control/presenters";
import type {
  MissionControlEntityKind,
  MissionControlFreshnessStatus,
  MissionControlRealtimeNotice,
  MissionControlRelation,
  MissionControlRouteState,
} from "../../features/mission-control/types";

const route = useRoute();
const router = useRouter();
const display = useDisplay();
const missionControl = useMissionControlStore();
const { t, locale } = useI18n({ useScope: "global" });

const boardStateOrder = ["working", "waiting", "blocked", "review", "recent_critical_updates"] as const;
const mobilePanelOpen = ref(false);
const searchInput = ref("");
const searchDebounceTimer = ref<number | null>(null);
const stopRealtimeRef = ref<(() => void) | null>(null);
const stopLifecycleBindingRef = ref<(() => void) | null>(null);
const activeRouteState = ref<MissionControlRouteState | null>(null);

const routeState = computed(() => normalizeMissionControlRouteQuery(route.query));
const groupedEntities = computed(() => groupMissionControlEntitiesByState(missionControl.entities));
const selectedEntityKey = computed(() => (missionControl.selectedRef ? `${missionControl.selectedRef.entity_kind}:${missionControl.selectedRef.entity_public_id}` : ""));

const summaryCards = computed(() => {
  const summary = missionControl.snapshot?.summary;
  return [
    { key: "total", labelKey: "pages.missionControl.summary.total", value: summary?.total_entities ?? 0, hint: t("pages.missionControl.summaryHints.total") },
    { key: "working", labelKey: "pages.missionControl.summary.working", value: summary?.working_count ?? 0, hint: t("pages.missionControl.summaryHints.working") },
    { key: "waiting", labelKey: "pages.missionControl.summary.waiting", value: summary?.waiting_count ?? 0, hint: t("pages.missionControl.summaryHints.waiting") },
    { key: "blocked", labelKey: "pages.missionControl.summary.blocked", value: summary?.blocked_count ?? 0, hint: t("pages.missionControl.summaryHints.blocked") },
    { key: "review", labelKey: "pages.missionControl.summary.review", value: summary?.review_count ?? 0, hint: t("pages.missionControl.summaryHints.review") },
    {
      key: "critical",
      labelKey: "pages.missionControl.summary.recent_critical_updates",
      value: summary?.recent_critical_updates_count ?? 0,
      hint: t("pages.missionControl.summaryHints.recent_critical_updates"),
    },
  ];
});

const filterOptions = computed(() => [
  { title: t("pages.missionControl.filters.all_active"), value: "all_active" },
  { title: t("pages.missionControl.filters.working"), value: "working" },
  { title: t("pages.missionControl.filters.waiting"), value: "waiting" },
  { title: t("pages.missionControl.filters.blocked"), value: "blocked" },
  { title: t("pages.missionControl.filters.review"), value: "review" },
  { title: t("pages.missionControl.filters.recent_critical_updates"), value: "recent_critical_updates" },
]);

const freshnessChip = computed(() => {
  switch (missionControl.effectiveFreshnessStatus) {
    case "stale":
      return { color: "warning", labelKey: "pages.missionControl.freshnessStates.stale" };
    case "degraded":
      return { color: "error", labelKey: "pages.missionControl.freshnessStates.degraded" };
    case "fresh":
      return { color: "success", labelKey: "pages.missionControl.freshnessStates.fresh" };
    default:
      return { color: "secondary", labelKey: "pages.missionControl.freshnessStates.unknown" };
  }
});

const realtimeChip = computed(() => {
  switch (missionControl.realtimeState) {
    case "connected":
      return { color: "success", labelKey: "pages.missionControl.realtimeStates.connected" };
    case "reconnecting":
      return { color: "warning", labelKey: "pages.missionControl.realtimeStates.reconnecting" };
    case "connecting":
      return { color: "secondary", labelKey: "pages.missionControl.realtimeStates.connecting" };
    default:
      return { color: "secondary", labelKey: "pages.missionControl.realtimeStates.disconnected" };
  }
});

const canvasTitleKey = computed(() =>
  missionControl.effectiveViewMode === "board" ? "pages.missionControl.boardTitle" : "pages.missionControl.listTitle",
);

const realtimeAlert = computed(() => buildRealtimeAlert(missionControl.realtimeNotice, missionControl.effectiveFreshnessStatus));

watch(
  routeState,
  async (next) => {
    searchInput.value = next.search;
    const previous = activeRouteState.value;
    activeRouteState.value = next;

    const queryChanged =
      !previous ||
      next.viewMode !== previous.viewMode ||
      next.activeFilter !== previous.activeFilter ||
      next.search !== previous.search;
    if (queryChanged) {
      missionControl.configureQuery({
        viewMode: next.viewMode,
        activeFilter: next.activeFilter,
        search: next.search,
      });
      await missionControl.loadSnapshot();
      restartRealtime();
    }

    const entityChanged =
      !previous ||
      next.entityKind !== previous.entityKind ||
      next.entityPublicId !== previous.entityPublicId;
    if (entityChanged) {
      if (next.entityKind && next.entityPublicId) {
        await missionControl.loadSelectedEntity({
          entity_kind: next.entityKind,
          entity_public_id: next.entityPublicId,
        });
        if (display.mobile.value) {
          mobilePanelOpen.value = true;
        }
      } else {
        missionControl.clearSelectedEntity();
        mobilePanelOpen.value = false;
      }
    }
  },
  { deep: true, immediate: true },
);

watch(
  () => display.mobile.value,
  (isMobile) => {
    if (!isMobile) {
      mobilePanelOpen.value = false;
    }
  },
  { immediate: true },
);

watch(searchInput, () => {
  scheduleSearch();
});

function clearSearchDebounce(): void {
  if (searchDebounceTimer.value !== null) {
    window.clearTimeout(searchDebounceTimer.value);
    searchDebounceTimer.value = null;
  }
}

function scheduleSearch(): void {
  clearSearchDebounce();
  searchDebounceTimer.value = window.setTimeout(() => {
    applySearch();
    searchDebounceTimer.value = null;
  }, 350);
}

function updateRoute(patch: Partial<MissionControlRouteState>): void {
  const nextState: MissionControlRouteState = {
    ...routeState.value,
    ...patch,
  };

  if ("viewMode" in patch || "activeFilter" in patch || "search" in patch) {
    nextState.entityKind = "";
    nextState.entityPublicId = "";
  }

  if (missionControlRouteStateEquals(nextState, routeState.value)) {
    return;
  }

  void router.replace({
    name: "mission-control",
    query: buildMissionControlRouteQuery(nextState),
  });
}

function applySearch(): void {
  const trimmed = searchInput.value.trim();
  if (trimmed === routeState.value.search) {
    return;
  }
  updateRoute({ search: trimmed });
}

function onSearchClear(): void {
  searchInput.value = "";
  updateRoute({ search: "" });
}

function onFilterChange(value: string): void {
  if (value === routeState.value.activeFilter) {
    return;
  }
  updateRoute({
    activeFilter: value as MissionControlRouteState["activeFilter"],
  });
}

function openEntity(entityKind: MissionControlEntityKind, entityPublicId: string): void {
  updateRoute({
    entityKind,
    entityPublicId,
  });
}

function closeEntity(): void {
  mobilePanelOpen.value = false;
  updateRoute({
    entityKind: "",
    entityPublicId: "",
  });
}

function openRelation(relation: MissionControlRelation): void {
  const current = missionControl.selectedRef;
  if (!current) {
    return;
  }

  const target =
    relation.source_entity_kind === current.entity_kind && relation.source_entity_public_id === current.entity_public_id
      ? { entity_kind: relation.target_entity_kind, entity_public_id: relation.target_entity_public_id }
      : { entity_kind: relation.source_entity_kind, entity_public_id: relation.source_entity_public_id };

  openEntity(target.entity_kind, target.entity_public_id);
}

function stopRealtime(): void {
  stopRealtimeRef.value?.();
  stopRealtimeRef.value = null;
  missionControl.setRealtimeState("closed");
}

function restartRealtime(): void {
  stopRealtime();
  const resumeToken = String(missionControl.snapshot?.realtime_resume_token || "").trim();
  if (resumeToken === "") {
    return;
  }

  missionControl.setRealtimeState("connecting");
  stopRealtimeRef.value = subscribeMissionControlRealtime({
    resumeToken,
    onMessage: (message) => {
      missionControl.setRealtimeState("connected");
      if (
        message.event_kind === "connected" &&
        (message.payload.snapshot_freshness_status === "fresh" || missionControl.realtimeNotice?.kind === "error")
      ) {
        missionControl.clearRealtimeNotice();
      }
      switch (message.event_kind) {
        case "connected":
          missionControl.setConnectedFreshnessStatus(message.payload.snapshot_freshness_status);
          return;
        case "delta":
          missionControl.applyRealtimeDelta(message.payload);
          return;
        case "invalidate":
          missionControl.applyRealtimeNotice({
            kind: "invalidate",
            reason: message.payload.reason,
            refreshScope: message.payload.refresh_scope,
            occurredAt: message.occurred_at,
          });
          return;
        case "stale":
          missionControl.applyRealtimeNotice({
            kind: "stale",
            reason: message.payload.reason,
            staleSince: message.payload.stale_since,
            suggestedRefresh: message.payload.suggested_refresh,
            occurredAt: message.occurred_at,
          });
          return;
        case "degraded":
          missionControl.applyRealtimeNotice({
            kind: "degraded",
            reason: message.payload.reason,
            fallbackMode: message.payload.fallback_mode,
            affectedCapabilities: message.payload.affected_capabilities,
            occurredAt: message.occurred_at,
          });
          return;
        case "resync_required":
          missionControl.applyRealtimeNotice({
            kind: "resync_required",
            reason: message.payload.reason,
            requiredSnapshotId: message.payload.required_snapshot_id,
            droppedEventCount: message.payload.dropped_event_count,
            occurredAt: message.occurred_at,
          });
          return;
        case "error":
          missionControl.applyRealtimeNotice({
            kind: "error",
            code: message.payload.code,
            message: message.payload.message,
            retryable: message.payload.retryable,
            occurredAt: message.occurred_at,
          });
          return;
        case "heartbeat":
          return;
      }
    },
    onStateChange: (state) => {
      missionControl.setRealtimeState(state);
    },
    onInitialMessageTimeout: () => {
      missionControl.applyRealtimeNotice({
        kind: "error",
        code: "realtime_timeout",
        message: "realtime_initial_timeout",
        retryable: true,
        occurredAt: new Date().toISOString(),
      });
    },
  });
}

async function refreshDashboard(): Promise<void> {
  await missionControl.refreshSnapshot();
  restartRealtime();
}

function entityKindLabel(kind: MissionControlEntityKind): string {
  return missionControlEntityKindLabelKey(kind);
}

function entityStateColor(state: Parameters<typeof missionControlStateColor>[0]): string {
  return missionControlStateColor(state);
}

function buildRealtimeAlert(
  notice: MissionControlRealtimeNotice | null,
  freshnessStatus: MissionControlFreshnessStatus,
): { type: "info" | "warning" | "error"; titleKey: string; text: string } | null {
  if (!notice) {
    if (freshnessStatus === "stale") {
      return {
        type: "warning",
        titleKey: "pages.missionControl.alerts.staleTitle",
        text: t("pages.missionControl.alerts.snapshotStaleText"),
      };
    }
    if (freshnessStatus === "degraded") {
      return {
        type: "error",
        titleKey: "pages.missionControl.alerts.degradedTitle",
        text: t("pages.missionControl.alerts.snapshotDegradedText"),
      };
    }
    return null;
  }

  switch (notice.kind) {
    case "invalidate":
      return {
        type: "info",
        titleKey: "pages.missionControl.alerts.invalidateTitle",
        text: t("pages.missionControl.alerts.invalidateText", { reason: notice.reason, scope: notice.refreshScope }),
      };
    case "stale":
      return {
        type: "warning",
        titleKey: "pages.missionControl.alerts.staleTitle",
        text: t("pages.missionControl.alerts.staleText", { reason: notice.reason, since: formatCompactDateTime(notice.staleSince, locale.value) }),
      };
    case "degraded":
      return {
        type: "error",
        titleKey: "pages.missionControl.alerts.degradedTitle",
        text: t("pages.missionControl.alerts.degradedText", {
          reason: notice.reason,
          fallback: notice.fallbackMode,
          capabilities: notice.affectedCapabilities.join(", "),
        }),
      };
    case "resync_required":
      return {
        type: "warning",
        titleKey: "pages.missionControl.alerts.resyncTitle",
        text: t("pages.missionControl.alerts.resyncText", {
          reason: notice.reason,
          dropped: notice.droppedEventCount,
        }),
      };
    case "error":
      return {
        type: "error",
        titleKey: "pages.missionControl.alerts.errorTitle",
        text: t("pages.missionControl.alerts.errorText", { code: notice.code, message: notice.message }),
      };
  }
}

stopLifecycleBindingRef.value = bindRealtimePageLifecycle({
  onResume: () => {
    void refreshDashboard();
  },
  onSuspend: () => {
    stopRealtime();
  },
});

onBeforeUnmount(() => {
  clearSearchDebounce();
  stopRealtime();
  stopLifecycleBindingRef.value?.();
  stopLifecycleBindingRef.value = null;
});
</script>

<style scoped>
.mission-control-summary {
  display: grid;
  gap: 14px;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
}

.mission-control-summary__card {
  border-radius: 24px;
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.98), rgba(246, 248, 252, 0.98)),
    radial-gradient(260px 150px at 100% 0%, rgba(214, 238, 255, 0.6), transparent 60%);
}

.mission-control-summary__label {
  font-size: 0.82rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: rgba(15, 23, 42, 0.56);
}

.mission-control-summary__value {
  margin-top: 10px;
  font-size: 2rem;
  font-weight: 800;
  line-height: 1;
  color: rgb(15, 23, 42);
}

.mission-control-summary__hint {
  margin-top: 8px;
  color: rgba(15, 23, 42, 0.66);
}

.mission-control-toolbar {
  border-radius: 28px;
}

.mission-control-toolbar__content {
  display: flex;
  gap: 12px;
  align-items: center;
  flex-wrap: wrap;
}

.mission-control-toolbar__filter {
  max-width: 280px;
}

.mission-control-toolbar__search {
  min-width: 280px;
  flex: 1 1 320px;
}

.mission-control-toolbar__toggle :deep(.v-btn) {
  text-transform: none;
}

.mission-control-toolbar__status {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-left: auto;
}

.mission-control-canvas {
  border-radius: 28px;
  overflow: hidden;
}

.mission-control-loading {
  padding: 12px 0;
}

.mission-control-board {
  display: grid;
  gap: 14px;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
}

.mission-control-board__column {
  border-radius: 24px;
  min-height: 240px;
}

.mission-control-board__heading {
  font-size: 0.92rem;
}

.mission-control-board__content {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.mission-control-board__stack {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.mission-control-list {
  overflow-x: auto;
}

.mission-control-list__row {
  cursor: pointer;
}

.mission-control-list__row--selected {
  background: rgba(25, 118, 210, 0.06);
}

.mission-control-empty {
  min-height: 360px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}
</style>
