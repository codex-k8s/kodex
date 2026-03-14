import {
  getMissionControlDashboard as getMissionControlDashboardRequest,
  getMissionControlEntity as getMissionControlEntityRequest,
  listMissionControlTimeline as listMissionControlTimelineRequest,
} from "../../shared/api/sdk";

import type {
  MissionControlActiveFilter,
  MissionControlDashboardSnapshot,
  MissionControlEntityDetails,
  MissionControlEntityKind,
  MissionControlTimelineEntry,
  MissionControlViewMode,
} from "./types";

export async function getMissionControlDashboard(params: {
  viewMode: MissionControlViewMode;
  activeFilter: MissionControlActiveFilter;
  search?: string;
  cursor?: string;
  limit?: number;
}): Promise<MissionControlDashboardSnapshot> {
  const resp = await getMissionControlDashboardRequest({
    query: {
      view_mode: params.viewMode,
      active_filter: params.activeFilter,
      search: params.search?.trim() || undefined,
      cursor: params.cursor?.trim() || undefined,
      limit: params.limit,
    },
    throwOnError: true,
  });
  return resp.data;
}

export async function getMissionControlEntity(params: {
  entityKind: MissionControlEntityKind;
  entityPublicId: string;
  timelineLimit?: number;
}): Promise<MissionControlEntityDetails> {
  const resp = await getMissionControlEntityRequest({
    path: {
      entity_kind: params.entityKind,
      entity_public_id: params.entityPublicId,
    },
    query: {
      limit: params.timelineLimit,
    },
    throwOnError: true,
  });
  return resp.data;
}

export async function listMissionControlTimeline(params: {
  entityKind: MissionControlEntityKind;
  entityPublicId: string;
  cursor?: string;
  limit?: number;
}): Promise<{ items: MissionControlTimelineEntry[]; nextCursor: string }> {
  const resp = await listMissionControlTimelineRequest({
    path: {
      entity_kind: params.entityKind,
      entity_public_id: params.entityPublicId,
    },
    query: {
      cursor: params.cursor?.trim() || undefined,
      limit: params.limit,
    },
    throwOnError: true,
  });
  return {
    items: resp.data.items ?? [],
    nextCursor: String(resp.data.next_cursor || ""),
  };
}
