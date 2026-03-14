import type { LocationQuery, LocationQueryRaw } from "vue-router";

import type {
  MissionControlBoardGroups,
  MissionControlEntityCard,
  MissionControlEntityKind,
  MissionControlEntityRef,
  MissionControlRealtimeEvent,
  MissionControlRouteState,
} from "./types";

export const missionControlDefaultViewMode = "board";
export const missionControlDefaultActiveFilter = "all_active";

const missionControlStates = ["working", "waiting", "blocked", "review", "recent_critical_updates"] as const;
const missionControlViewModes = ["board", "list"] as const;
const missionControlActiveFilters = ["working", "waiting", "blocked", "review", "recent_critical_updates", "all_active"] as const;
const missionControlEntityKinds = ["work_item", "discussion", "pull_request", "agent"] as const;
const missionControlRealtimeKinds = ["connected", "invalidate", "stale", "degraded", "resync_required", "heartbeat", "error"] as const;

type JsonRecord = Record<string, unknown>;

function isJsonRecord(value: unknown): value is JsonRecord {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function isString(value: unknown): value is string {
  return typeof value === "string";
}

function asTrimmedString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === "string");
}

function isMissionControlViewMode(value: string): value is MissionControlRouteState["viewMode"] {
  return (missionControlViewModes as readonly string[]).includes(value);
}

function isMissionControlActiveFilter(value: string): value is MissionControlRouteState["activeFilter"] {
  return (missionControlActiveFilters as readonly string[]).includes(value);
}

export function isMissionControlEntityKind(value: string): value is MissionControlEntityKind {
  return (missionControlEntityKinds as readonly string[]).includes(value);
}

function asQueryString(value: LocationQuery[string]): string {
  if (typeof value === "string") return value.trim();
  if (Array.isArray(value)) {
    return typeof value[0] === "string" ? value[0].trim() : "";
  }
  return "";
}

export function normalizeMissionControlRouteQuery(query: LocationQuery): MissionControlRouteState {
  const viewMode = asQueryString(query.view);
  const activeFilter = asQueryString(query.filter);
  const search = asQueryString(query.q);
  const entityKind = asQueryString(query.entity_kind);
  const entityPublicId = asQueryString(query.entity_id);

  return {
    viewMode: isMissionControlViewMode(viewMode) ? viewMode : missionControlDefaultViewMode,
    activeFilter: isMissionControlActiveFilter(activeFilter) ? activeFilter : missionControlDefaultActiveFilter,
    search,
    entityKind: isMissionControlEntityKind(entityKind) ? entityKind : "",
    entityPublicId: isMissionControlEntityKind(entityKind) && entityPublicId !== "" ? entityPublicId : "",
  };
}

export function buildMissionControlRouteQuery(state: MissionControlRouteState): LocationQueryRaw {
  return {
    view: state.viewMode !== missionControlDefaultViewMode ? state.viewMode : undefined,
    filter: state.activeFilter !== missionControlDefaultActiveFilter ? state.activeFilter : undefined,
    q: state.search.trim() !== "" ? state.search.trim() : undefined,
    entity_kind: state.entityKind || undefined,
    entity_id: state.entityPublicId || undefined,
  };
}

export function missionControlRouteStateEquals(left: MissionControlRouteState, right: MissionControlRouteState): boolean {
  return (
    left.viewMode === right.viewMode &&
    left.activeFilter === right.activeFilter &&
    left.search === right.search &&
    left.entityKind === right.entityKind &&
    left.entityPublicId === right.entityPublicId
  );
}

export function missionControlEntityKey(entity: MissionControlEntityRef): string {
  return `${entity.entity_kind}:${entity.entity_public_id}`;
}

export function missionControlRelationKey(relation: {
  relation_kind: string;
  source_kind: string;
  source_entity_kind: string;
  source_entity_public_id: string;
  target_entity_kind: string;
  target_entity_public_id: string;
  direction: string;
}): string {
  return [
    relation.relation_kind,
    relation.source_kind,
    relation.source_entity_kind,
    relation.source_entity_public_id,
    relation.target_entity_kind,
    relation.target_entity_public_id,
    relation.direction,
  ].join(":");
}

export function groupMissionControlEntitiesByState(entities: MissionControlEntityCard[]): MissionControlBoardGroups {
  return missionControlStates.reduce<MissionControlBoardGroups>(
    (acc, state) => {
      acc[state] = entities.filter((entity) => entity.state === state);
      return acc;
    },
    {
      working: [],
      waiting: [],
      blocked: [],
      review: [],
      recent_critical_updates: [],
    },
  );
}

export function resolveMissionControlEffectiveViewMode(
  preferredViewMode: MissionControlRouteState["viewMode"],
  freshnessStatus: "fresh" | "stale" | "degraded" | "",
): MissionControlRouteState["viewMode"] {
  if (freshnessStatus === "degraded") {
    return "list";
  }
  return preferredViewMode;
}

function parseEntityRef(value: unknown): MissionControlEntityRef | null {
  if (!isJsonRecord(value)) return null;
  const entityKind = asTrimmedString(value.entity_kind);
  const entityPublicId = asTrimmedString(value.entity_public_id);
  if (!isMissionControlEntityKind(entityKind) || entityPublicId === "") {
    return null;
  }
  return {
    entity_kind: entityKind,
    entity_public_id: entityPublicId,
  };
}

export function parseMissionControlRealtimeEnvelope(raw: string): MissionControlRealtimeEvent | null {
  const text = String(raw || "").trim();
  if (text === "") return null;

  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return null;
  }
  if (!isJsonRecord(parsed)) return null;

  const eventKind = asTrimmedString(parsed.event_kind);
  if (!(missionControlRealtimeKinds as readonly string[]).includes(eventKind)) {
    return null;
  }

  const snapshotID = asTrimmedString(parsed.snapshot_id);
  const resumeToken = asTrimmedString(parsed.resume_token);
  const occurredAt = asTrimmedString(parsed.occurred_at);
  const payload = parsed.payload;
  if (snapshotID === "" || occurredAt === "" || !isJsonRecord(payload)) {
    return null;
  }

  switch (eventKind) {
    case "connected": {
      const freshnessStatus = asTrimmedString(payload.snapshot_freshness_status);
      const serverCursor = asTrimmedString(payload.server_cursor);
      if ((freshnessStatus !== "fresh" && freshnessStatus !== "stale" && freshnessStatus !== "degraded") || serverCursor === "") {
        return null;
      }
      return {
        event_kind: "connected",
        snapshot_id: snapshotID,
        resume_token: resumeToken,
        occurred_at: occurredAt,
        payload: {
          snapshot_freshness_status: freshnessStatus,
          server_cursor: serverCursor,
        },
      };
    }
    case "invalidate": {
      const reason = asTrimmedString(payload.reason);
      const refreshScope = asTrimmedString(payload.refresh_scope);
      const refs = Array.isArray(payload.affected_entity_refs)
        ? payload.affected_entity_refs.map(parseEntityRef).filter((item): item is MissionControlEntityRef => item !== null)
        : [];
      if (reason === "" || refreshScope === "") {
        return null;
      }
      return {
        event_kind: "invalidate",
        snapshot_id: snapshotID,
        resume_token: resumeToken,
        occurred_at: occurredAt,
        payload: {
          reason,
          refresh_scope: refreshScope,
          affected_entity_refs: refs,
        },
      };
    }
    case "stale": {
      const reason = asTrimmedString(payload.reason);
      const staleSince = asTrimmedString(payload.stale_since);
      const suggestedRefresh = asTrimmedString(payload.suggested_refresh);
      if (reason === "" || staleSince === "" || suggestedRefresh === "") {
        return null;
      }
      return {
        event_kind: "stale",
        snapshot_id: snapshotID,
        resume_token: resumeToken,
        occurred_at: occurredAt,
        payload: {
          reason,
          stale_since: staleSince,
          suggested_refresh: suggestedRefresh,
        },
      };
    }
    case "degraded": {
      const reason = asTrimmedString(payload.reason);
      const fallbackMode = asTrimmedString(payload.fallback_mode);
      const affectedCapabilities = isStringArray(payload.affected_capabilities) ? [...payload.affected_capabilities] : [];
      if (reason === "" || fallbackMode === "") {
        return null;
      }
      return {
        event_kind: "degraded",
        snapshot_id: snapshotID,
        resume_token: resumeToken,
        occurred_at: occurredAt,
        payload: {
          reason,
          fallback_mode: fallbackMode,
          affected_capabilities: affectedCapabilities,
        },
      };
    }
    case "resync_required": {
      const reason = asTrimmedString(payload.reason);
      const requiredSnapshotID = asTrimmedString(payload.required_snapshot_id);
      const droppedEventCount = Number(payload.dropped_event_count);
      if (reason === "" || requiredSnapshotID === "" || !Number.isFinite(droppedEventCount)) {
        return null;
      }
      return {
        event_kind: "resync_required",
        snapshot_id: snapshotID,
        resume_token: resumeToken,
        occurred_at: occurredAt,
        payload: {
          reason,
          required_snapshot_id: requiredSnapshotID,
          dropped_event_count: Math.max(0, Math.trunc(droppedEventCount)),
        },
      };
    }
    case "heartbeat": {
      const serverTime = asTrimmedString(payload.server_time);
      const heartbeatSnapshotID = asTrimmedString(payload.snapshot_id);
      if (serverTime === "" || heartbeatSnapshotID === "") {
        return null;
      }
      return {
        event_kind: "heartbeat",
        snapshot_id: snapshotID,
        resume_token: resumeToken,
        occurred_at: occurredAt,
        payload: {
          server_time: serverTime,
          snapshot_id: heartbeatSnapshotID,
        },
      };
    }
    case "error": {
      const code = asTrimmedString(payload.code);
      const message = isString(payload.message) ? payload.message : "";
      const retryable = payload.retryable;
      if (code === "" || message === "" || typeof retryable !== "boolean") {
        return null;
      }
      return {
        event_kind: "error",
        snapshot_id: snapshotID,
        resume_token: resumeToken,
        occurred_at: occurredAt,
        payload: {
          code,
          message,
          retryable,
        },
      };
    }
    default:
      return null;
  }
}
