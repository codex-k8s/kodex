import { defineStore } from "pinia";

import { normalizeApiError, type ApiError } from "../../shared/api/errors";
import { getMissionControlDashboard, getMissionControlEntity, listMissionControlTimeline } from "./api";
import { missionControlDefaultActiveFilter, missionControlDefaultViewMode, missionControlEntityKey, missionControlRelationKey, resolveMissionControlEffectiveViewMode } from "./lib";
import type {
  MissionControlDashboardSnapshot,
  MissionControlEntityCard,
  MissionControlEntityDetails,
  MissionControlFreshnessStatus,
  MissionControlEntityRef,
  MissionControlRealtimeEvent,
  MissionControlRealtimeNotice,
  MissionControlRealtimeState,
  MissionControlRelation,
  MissionControlTimelineEntry,
} from "./types";

const missionControlDefaultLimit = 200;
const missionControlTimelineLimit = 50;

function uniqueMissionControlEntities(items: MissionControlEntityCard[]): MissionControlEntityCard[] {
  const byKey = new Map<string, MissionControlEntityCard>();
  for (const item of items) {
    byKey.set(missionControlEntityKey({ entity_kind: item.entity_kind, entity_public_id: item.entity_public_id }), item);
  }
  return Array.from(byKey.values());
}

function uniqueMissionControlRelations(items: MissionControlRelation[]): MissionControlRelation[] {
  const byKey = new Map<string, MissionControlRelation>();
  for (const item of items) {
    byKey.set(missionControlRelationKey(item), item);
  }
  return Array.from(byKey.values());
}

function uniqueMissionControlTimeline(items: MissionControlTimelineEntry[]): MissionControlTimelineEntry[] {
  const byKey = new Map<string, MissionControlTimelineEntry>();
  for (const item of items) {
    byKey.set(item.entry_id, item);
  }
  return Array.from(byKey.values()).sort((left, right) => {
    if (left.occurred_at === right.occurred_at) {
      return left.entry_id < right.entry_id ? 1 : -1;
    }
    return left.occurred_at < right.occurred_at ? 1 : -1;
  });
}

function freshnessStatusFromNotice(
  snapshot: MissionControlDashboardSnapshot | null,
  notice: MissionControlRealtimeNotice | null,
  connectedFreshnessStatus: MissionControlFreshnessStatus,
): MissionControlFreshnessStatus {
  if (notice?.kind === "degraded") return "degraded";
  if (notice?.kind === "stale") return "stale";
  if (connectedFreshnessStatus !== "") return connectedFreshnessStatus;
  if (snapshot) return snapshot.freshness_status;
  return "";
}

function relationTouchesEntity(relation: MissionControlRelation, ref: MissionControlEntityRef): boolean {
  return (
    (relation.source_entity_kind === ref.entity_kind && relation.source_entity_public_id === ref.entity_public_id) ||
    (relation.target_entity_kind === ref.entity_kind && relation.target_entity_public_id === ref.entity_public_id)
  );
}

function timelineEntryBelongsToEntity(entry: MissionControlTimelineEntry, ref: MissionControlEntityRef): boolean {
  return entry.entity_kind === ref.entity_kind && entry.entity_public_id === ref.entity_public_id;
}

function updateSelectedDetailsWithRealtimeDelta(
  details: MissionControlEntityDetails,
  ref: MissionControlEntityRef,
  payload: Extract<MissionControlRealtimeEvent, { event_kind: "delta" }>["payload"],
): MissionControlEntityDetails {
  const nextEntity =
    payload.delta_entities.find(
      (entity) => entity.entity_kind === ref.entity_kind && entity.entity_public_id === ref.entity_public_id,
    ) ?? details.entity;
  const nextRelations = uniqueMissionControlRelations([
    ...details.relations,
    ...payload.delta_relations.filter((relation) => relationTouchesEntity(relation, ref)),
  ]);
  const nextTimelinePreview = uniqueMissionControlTimeline([
    ...details.timeline_preview,
    ...payload.delta_timeline_entries.filter((entry) => timelineEntryBelongsToEntity(entry, ref)),
  ]);

  return {
    ...details,
    entity: nextEntity,
    relations: nextRelations,
    timeline_preview: nextTimelinePreview,
  };
}

export const useMissionControlStore = defineStore("missionControl", {
  state: () => ({
    viewMode: missionControlDefaultViewMode as "board" | "list",
    activeFilter: missionControlDefaultActiveFilter as
      | "working"
      | "waiting"
      | "blocked"
      | "review"
      | "recent_critical_updates"
      | "all_active",
    search: "",
    snapshot: null as MissionControlDashboardSnapshot | null,
    entities: [] as MissionControlEntityCard[],
    relations: [] as MissionControlRelation[],
    loading: false,
    loadingMore: false,
    refreshing: false,
    error: null as ApiError | null,
    selectedRef: null as MissionControlEntityRef | null,
    selectedDetails: null as MissionControlEntityDetails | null,
    selectedTimeline: [] as MissionControlTimelineEntry[],
    selectedTimelineCursor: "",
    selectedLoading: false,
    selectedTimelineLoading: false,
    selectedDetailsError: null as ApiError | null,
    selectedTimelineError: null as ApiError | null,
    realtimeState: "closed" as MissionControlRealtimeState | "closed",
    realtimeNotice: null as MissionControlRealtimeNotice | null,
    connectedFreshnessStatus: "" as MissionControlFreshnessStatus,
    snapshotRequestId: 0,
    detailsRequestId: 0,
    timelineRequestId: 0,
  }),
  getters: {
    effectiveFreshnessStatus(state): MissionControlFreshnessStatus {
      return freshnessStatusFromNotice(state.snapshot, state.realtimeNotice, state.connectedFreshnessStatus);
    },
    effectiveViewMode(state): "board" | "list" {
      return resolveMissionControlEffectiveViewMode(state.viewMode, freshnessStatusFromNotice(state.snapshot, state.realtimeNotice, state.connectedFreshnessStatus));
    },
    hasMore(state): boolean {
      return String(state.snapshot?.next_page_cursor || "").trim() !== "";
    },
    hasSelectedTimelineMore(state): boolean {
      return state.selectedTimelineCursor.trim() !== "";
    },
  },
  actions: {
    configureQuery(params: {
      viewMode: "board" | "list";
      activeFilter: "working" | "waiting" | "blocked" | "review" | "recent_critical_updates" | "all_active";
      search: string;
    }): void {
      this.viewMode = params.viewMode;
      this.activeFilter = params.activeFilter;
      this.search = params.search.trim();
    },

    setRealtimeState(state: MissionControlRealtimeState | "closed"): void {
      this.realtimeState = state;
    },

    clearRealtimeNotice(): void {
      this.realtimeNotice = null;
    },

    applyRealtimeNotice(notice: MissionControlRealtimeNotice): void {
      this.realtimeNotice = notice;
    },

    setConnectedFreshnessStatus(status: MissionControlFreshnessStatus): void {
      this.connectedFreshnessStatus = status;
    },

    applyRealtimeDelta(payload: Extract<MissionControlRealtimeEvent, { event_kind: "delta" }>["payload"]): void {
      this.entities = uniqueMissionControlEntities([...this.entities, ...payload.delta_entities]);
      this.relations = uniqueMissionControlRelations([...this.relations, ...payload.delta_relations]);

      const selectedRef = this.selectedRef;
      if (!selectedRef) {
        return;
      }

      if (this.selectedDetails) {
        this.selectedDetails = updateSelectedDetailsWithRealtimeDelta(this.selectedDetails, selectedRef, payload);
      }

      const selectedTimelineEntries = payload.delta_timeline_entries.filter((entry) => timelineEntryBelongsToEntity(entry, selectedRef));
      if (selectedTimelineEntries.length > 0) {
        this.selectedTimeline = uniqueMissionControlTimeline([...this.selectedTimeline, ...selectedTimelineEntries]);
        this.selectedTimelineError = null;
      }
    },

    async loadSnapshot(options: { append?: boolean; refresh?: boolean } = {}): Promise<void> {
      const requestId = this.snapshotRequestId + 1;
      this.snapshotRequestId = requestId;

      const append = options.append === true;
      const refresh = options.refresh === true;
      if (append) {
        this.loadingMore = true;
      } else if (refresh) {
        this.refreshing = true;
      } else {
        this.loading = true;
      }
      if (!append) {
        this.error = null;
      }

      const cursor = append ? String(this.snapshot?.next_page_cursor || "") : "";
      try {
        const snapshot = await getMissionControlDashboard({
          viewMode: this.viewMode,
          activeFilter: this.activeFilter,
          search: this.search,
          cursor,
          limit: missionControlDefaultLimit,
        });
        if (this.snapshotRequestId !== requestId) {
          return;
        }

        if (append && this.snapshot) {
          this.snapshot = {
            ...snapshot,
            entities: [],
            relations: [],
          };
          this.entities = uniqueMissionControlEntities([...this.entities, ...(snapshot.entities ?? [])]);
          this.relations = uniqueMissionControlRelations([...this.relations, ...(snapshot.relations ?? [])]);
        } else {
          this.snapshot = snapshot;
          this.entities = snapshot.entities ?? [];
          this.relations = snapshot.relations ?? [];
        }
        this.connectedFreshnessStatus = snapshot.freshness_status;

        if ((this.realtimeNotice?.kind === "stale" || this.realtimeNotice?.kind === "degraded") && snapshot.freshness_status === "fresh") {
          this.realtimeNotice = null;
        }
      } catch (error) {
        if (this.snapshotRequestId !== requestId) {
          return;
        }
        this.error = normalizeApiError(error);
      } finally {
        if (this.snapshotRequestId !== requestId) {
          return;
        }
        this.loading = false;
        this.loadingMore = false;
        this.refreshing = false;
      }
    },

    async refreshSnapshot(): Promise<void> {
      await this.loadSnapshot({ refresh: true });
      if (this.selectedRef) {
        await this.loadSelectedEntity(this.selectedRef);
      }
    },

    async loadSelectedEntity(ref: MissionControlEntityRef): Promise<void> {
      this.selectedRef = ref;
      const requestId = this.detailsRequestId + 1;
      this.detailsRequestId = requestId;
      this.selectedLoading = true;
      this.selectedDetailsError = null;
      this.selectedTimelineError = null;
      this.selectedTimelineCursor = "";

      try {
        const details = await getMissionControlEntity({
          entityKind: ref.entity_kind,
          entityPublicId: ref.entity_public_id,
          timelineLimit: missionControlTimelineLimit,
        });
        if (this.detailsRequestId !== requestId) {
          return;
        }
        this.selectedDetails = details;
        this.selectedTimeline = uniqueMissionControlTimeline(details.timeline_preview ?? []);
        this.selectedTimelineCursor = "";
        await this.loadSelectedTimeline({ append: false });
      } catch (error) {
        if (this.detailsRequestId !== requestId) {
          return;
        }
        this.selectedDetails = null;
        this.selectedTimeline = [];
        this.selectedTimelineCursor = "";
        this.selectedDetailsError = normalizeApiError(error);
        this.selectedTimelineError = null;
      } finally {
        if (this.detailsRequestId !== requestId) {
          return;
        }
        this.selectedLoading = false;
      }
    },

    clearSelectedEntity(): void {
      this.selectedRef = null;
      this.selectedDetails = null;
      this.selectedTimeline = [];
      this.selectedTimelineCursor = "";
      this.selectedLoading = false;
      this.selectedTimelineLoading = false;
      this.selectedDetailsError = null;
      this.selectedTimelineError = null;
    },

    async loadSelectedTimeline(options: { append: boolean }): Promise<void> {
      if (!this.selectedRef) {
        return;
      }
      const requestId = this.timelineRequestId + 1;
      this.timelineRequestId = requestId;
      this.selectedTimelineLoading = true;
      this.selectedTimelineError = null;
      const cursor = options.append ? this.selectedTimelineCursor : "";

      try {
        const response = await listMissionControlTimeline({
          entityKind: this.selectedRef.entity_kind,
          entityPublicId: this.selectedRef.entity_public_id,
          cursor,
          limit: missionControlTimelineLimit,
        });
        if (this.timelineRequestId !== requestId) {
          return;
        }
        this.selectedTimeline = uniqueMissionControlTimeline(options.append ? [...this.selectedTimeline, ...response.items] : response.items);
        this.selectedTimelineCursor = response.nextCursor;
      } catch (error) {
        if (this.timelineRequestId !== requestId) {
          return;
        }
        this.selectedTimelineError = normalizeApiError(error);
      } finally {
        if (this.timelineRequestId !== requestId) {
          return;
        }
        this.selectedTimelineLoading = false;
      }
    },
  },
});
