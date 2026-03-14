import type { LocationQuery, LocationQueryRaw } from "vue-router";

import type {
  MissionControlBoardGroups,
  MissionControlEntityCard,
  MissionControlEntityKind,
  MissionControlEntityRef,
  MissionControlRelation,
  MissionControlRealtimeEvent,
  MissionControlRouteState,
  MissionControlTimelineEntry,
} from "./types";

export const missionControlDefaultViewMode = "board";
export const missionControlDefaultActiveFilter = "all_active";

const missionControlStates = ["working", "waiting", "blocked", "review", "recent_critical_updates"] as const;
const missionControlViewModes = ["board", "list"] as const;
const missionControlActiveFilters = ["working", "waiting", "blocked", "review", "recent_critical_updates", "all_active"] as const;
const missionControlEntityKinds = ["work_item", "discussion", "pull_request", "agent"] as const;
const missionControlRealtimeKinds = ["connected", "delta", "invalidate", "stale", "degraded", "resync_required", "heartbeat", "error"] as const;
const missionControlSyncStatuses = ["synced", "pending_sync", "failed", "degraded"] as const;
const missionControlProviderKinds = ["github", "platform"] as const;
const missionControlRelationKinds = ["linked_to", "blocks", "blocked_by", "formalized_from", "owned_by", "assigned_to", "tracked_by_command"] as const;
const missionControlRelationSourceKinds = ["platform", "provider", "command", "voice_candidate"] as const;
const missionControlRelationDirections = ["outbound", "inbound", "bidirectional"] as const;
const missionControlTimelineSourceKinds = ["provider", "platform", "command", "voice_candidate"] as const;
const missionControlBadges = ["blocked", "owner_review", "waiting_mcp", "realtime_stale", "voice_candidate"] as const;

type JsonRecord = Record<string, unknown>;

function isJsonRecord(value: unknown): value is JsonRecord {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function isString(value: unknown): value is string {
  return typeof value === "string";
}

function isNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function isBoolean(value: unknown): value is boolean {
  return typeof value === "boolean";
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

function parseProviderReference(value: unknown): MissionControlEntityCard["provider_reference"] | null {
  if (!isJsonRecord(value)) return null;
  const provider = asTrimmedString(value.provider);
  const externalID = asTrimmedString(value.external_id);
  const url = asTrimmedString(value.url);
  if (!(missionControlProviderKinds as readonly string[]).includes(provider) || externalID === "") {
    return null;
  }
  return {
    provider: provider as MissionControlEntityCard["provider_reference"]["provider"],
    external_id: externalID,
    url: url === "" ? undefined : url,
  };
}

function parsePrimaryActor(value: unknown): MissionControlEntityCard["primary_actor"] | undefined {
  if (!isJsonRecord(value)) return undefined;
  const actorType = asTrimmedString(value.actor_type);
  const actorID = asTrimmedString(value.actor_id);
  const displayName = asTrimmedString(value.display_name);
  if (actorType === "" || actorID === "" || displayName === "") {
    return undefined;
  }
  return {
    actor_type: actorType,
    actor_id: actorID,
    display_name: displayName,
  };
}

function parseMissionControlEntityCard(value: unknown): MissionControlEntityCard | null {
  if (!isJsonRecord(value)) return null;
  const entityKind = asTrimmedString(value.entity_kind);
  const entityPublicID = asTrimmedString(value.entity_public_id);
  const title = asTrimmedString(value.title);
  const state = asTrimmedString(value.state);
  const syncStatus = asTrimmedString(value.sync_status);
  const providerReference = parseProviderReference(value.provider_reference);
  const relationCount = value.relation_count;
  const projectionVersion = value.projection_version;
  const badges = isStringArray(value.badges)
    ? value.badges.filter((badge) => (missionControlBadges as readonly string[]).includes(badge))
    : [];

  if (
    !isMissionControlEntityKind(entityKind) ||
    entityPublicID === "" ||
    title === "" ||
    !(missionControlStates as readonly string[]).includes(state) ||
    !(missionControlSyncStatuses as readonly string[]).includes(syncStatus) ||
    providerReference === null ||
    !isNumber(relationCount) ||
    !isNumber(projectionVersion)
  ) {
    return null;
  }

  const lastTimelineAt = asTrimmedString(value.last_timeline_at);
  const primaryActor = parsePrimaryActor(value.primary_actor);

  return {
    entity_kind: entityKind,
    entity_public_id: entityPublicID,
    title,
    state: state as MissionControlEntityCard["state"],
    sync_status: syncStatus as MissionControlEntityCard["sync_status"],
    provider_reference: providerReference,
    primary_actor: primaryActor,
    relation_count: relationCount,
    last_timeline_at: lastTimelineAt === "" ? undefined : lastTimelineAt,
    badges: badges as MissionControlEntityCard["badges"],
    projection_version: projectionVersion,
  };
}

function parseMissionControlRelation(value: unknown): MissionControlRelation | null {
  if (!isJsonRecord(value)) return null;
  const relationKind = asTrimmedString(value.relation_kind);
  const sourceKind = asTrimmedString(value.source_kind);
  const sourceEntityKind = asTrimmedString(value.source_entity_kind);
  const sourceEntityPublicID = asTrimmedString(value.source_entity_public_id);
  const targetEntityKind = asTrimmedString(value.target_entity_kind);
  const targetEntityPublicID = asTrimmedString(value.target_entity_public_id);
  const direction = asTrimmedString(value.direction);

  if (
    !(missionControlRelationKinds as readonly string[]).includes(relationKind) ||
    !(missionControlRelationSourceKinds as readonly string[]).includes(sourceKind) ||
    !isMissionControlEntityKind(sourceEntityKind) ||
    sourceEntityPublicID === "" ||
    !isMissionControlEntityKind(targetEntityKind) ||
    targetEntityPublicID === "" ||
    !(missionControlRelationDirections as readonly string[]).includes(direction)
  ) {
    return null;
  }

  return {
    relation_kind: relationKind as MissionControlRelation["relation_kind"],
    source_kind: sourceKind as MissionControlRelation["source_kind"],
    source_entity_kind: sourceEntityKind,
    source_entity_public_id: sourceEntityPublicID,
    target_entity_kind: targetEntityKind,
    target_entity_public_id: targetEntityPublicID,
    direction: direction as MissionControlRelation["direction"],
  };
}

function parseMissionControlTimelineEntry(value: unknown): MissionControlTimelineEntry | null {
  if (!isJsonRecord(value)) return null;
  const entryID = asTrimmedString(value.entry_id);
  const entityKind = asTrimmedString(value.entity_kind);
  const entityPublicID = asTrimmedString(value.entity_public_id);
  const sourceKind = asTrimmedString(value.source_kind);
  const sourceRef = asTrimmedString(value.source_ref);
  const occurredAt = asTrimmedString(value.occurred_at);
  const summary = asTrimmedString(value.summary);

  if (
    entryID === "" ||
    !isMissionControlEntityKind(entityKind) ||
    entityPublicID === "" ||
    !(missionControlTimelineSourceKinds as readonly string[]).includes(sourceKind) ||
    sourceRef === "" ||
    occurredAt === "" ||
    summary === "" ||
    !isBoolean(value.is_read_only)
  ) {
    return null;
  }

  const bodyMarkdown = asTrimmedString(value.body_markdown);
  const commandID = asTrimmedString(value.command_id);
  const providerURL = asTrimmedString(value.provider_url);

  return {
    entry_id: entryID,
    entity_kind: entityKind,
    entity_public_id: entityPublicID,
    source_kind: sourceKind as MissionControlTimelineEntry["source_kind"],
    source_ref: sourceRef,
    occurred_at: occurredAt,
    summary,
    body_markdown: bodyMarkdown === "" ? undefined : bodyMarkdown,
    command_id: commandID === "" ? undefined : commandID,
    provider_url: providerURL === "" ? undefined : providerURL,
    is_read_only: value.is_read_only,
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
    case "delta": {
      const deltaEntities = Array.isArray(payload.delta_entities)
        ? payload.delta_entities
            .map(parseMissionControlEntityCard)
            .filter((item): item is MissionControlEntityCard => item !== null)
        : null;
      const deltaRelations = Array.isArray(payload.delta_relations)
        ? payload.delta_relations
            .map(parseMissionControlRelation)
            .filter((item): item is MissionControlRelation => item !== null)
        : null;
      const deltaTimelineEntries = Array.isArray(payload.delta_timeline_entries)
        ? payload.delta_timeline_entries
            .map(parseMissionControlTimelineEntry)
            .filter((item): item is MissionControlTimelineEntry => item !== null)
        : null;
      if (deltaEntities === null || deltaRelations === null || deltaTimelineEntries === null || !isStringArray(payload.changed_command_ids)) {
        return null;
      }
      return {
        event_kind: "delta",
        snapshot_id: snapshotID,
        resume_token: resumeToken,
        occurred_at: occurredAt,
        payload: {
          delta_entities: deltaEntities,
          delta_relations: deltaRelations,
          delta_timeline_entries: deltaTimelineEntries,
          changed_command_ids: payload.changed_command_ids,
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
