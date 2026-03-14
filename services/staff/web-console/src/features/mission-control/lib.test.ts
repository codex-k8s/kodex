import test from "node:test";
import assert from "node:assert/strict";

import {
  buildMissionControlRouteQuery,
  groupMissionControlEntitiesByState,
  missionControlEntityKey,
  normalizeMissionControlRouteQuery,
  parseMissionControlRealtimeEnvelope,
  resolveMissionControlEffectiveViewMode,
} from "./lib.ts";

test("normalizeMissionControlRouteQuery applies defaults and accepts valid entity deep-link", () => {
  const state = normalizeMissionControlRouteQuery({
    view: "list",
    filter: "blocked",
    q: "review",
    entity_kind: "pull_request",
    entity_id: "codex-k8s/codex-k8s/pull/1",
  });

  assert.deepEqual(state, {
    viewMode: "list",
    activeFilter: "blocked",
    search: "review",
    entityKind: "pull_request",
    entityPublicId: "codex-k8s/codex-k8s/pull/1",
  });
});

test("buildMissionControlRouteQuery omits default values", () => {
  const query = buildMissionControlRouteQuery({
    viewMode: "board",
    activeFilter: "all_active",
    search: "",
    entityKind: "",
    entityPublicId: "",
  });

  assert.deepEqual(query, {
    view: undefined,
    filter: undefined,
    q: undefined,
    entity_kind: undefined,
    entity_id: undefined,
  });
});

test("groupMissionControlEntitiesByState keeps all entity buckets stable", () => {
  const groups = groupMissionControlEntitiesByState([
    {
      entity_kind: "work_item",
      entity_public_id: "issue-1",
      title: "Issue 1",
      state: "blocked",
      sync_status: "failed",
      provider_reference: { provider: "github", external_id: "repo#1" },
      relation_count: 1,
      badges: [],
      projection_version: 1,
    },
    {
      entity_kind: "agent",
      entity_public_id: "agent/dev",
      title: "Dev",
      state: "working",
      sync_status: "synced",
      provider_reference: { provider: "platform", external_id: "agent/dev" },
      relation_count: 0,
      badges: ["waiting_mcp"],
      projection_version: 2,
    },
  ]);

  assert.equal(groups.blocked.length, 1);
  assert.equal(groups.working.length, 1);
  assert.equal(groups.review.length, 0);
  assert.equal(missionControlEntityKey({ entity_kind: "agent", entity_public_id: "agent/dev" }), "agent:agent/dev");
});

test("resolveMissionControlEffectiveViewMode forces list while degraded", () => {
  assert.equal(resolveMissionControlEffectiveViewMode("board", "degraded"), "list");
  assert.equal(resolveMissionControlEffectiveViewMode("board", "fresh"), "board");
});

test("parseMissionControlRealtimeEnvelope validates degraded payload", () => {
  const parsed = parseMissionControlRealtimeEnvelope(
    JSON.stringify({
      event_kind: "degraded",
      snapshot_id: "snapshot-1",
      resume_token: "resume-1",
      occurred_at: "2026-03-14T10:00:00Z",
      payload: {
        reason: "snapshot_degraded",
        fallback_mode: "explicit_refresh",
        affected_capabilities: ["realtime_delta"],
      },
    }),
  );

  assert.deepEqual(parsed, {
    event_kind: "degraded",
    snapshot_id: "snapshot-1",
    resume_token: "resume-1",
    occurred_at: "2026-03-14T10:00:00Z",
    payload: {
      reason: "snapshot_degraded",
      fallback_mode: "explicit_refresh",
      affected_capabilities: ["realtime_delta"],
    },
  });
});

test("parseMissionControlRealtimeEnvelope validates delta payload", () => {
  const parsed = parseMissionControlRealtimeEnvelope(
    JSON.stringify({
      event_kind: "delta",
      snapshot_id: "snapshot-2",
      resume_token: "resume-2",
      occurred_at: "2026-03-14T11:00:00Z",
      payload: {
        delta_entities: [
          {
            entity_kind: "work_item",
            entity_public_id: "issue-2",
            title: "Issue 2",
            state: "working",
            sync_status: "synced",
            provider_reference: {
              provider: "github",
              external_id: "repo#2",
            },
            relation_count: 2,
            badges: ["owner_review", "unknown_badge"],
            projection_version: 4,
          },
        ],
        delta_relations: [
          {
            relation_kind: "linked_to",
            source_kind: "provider",
            source_entity_kind: "work_item",
            source_entity_public_id: "issue-2",
            target_entity_kind: "pull_request",
            target_entity_public_id: "repo/pull/2",
            direction: "outbound",
          },
        ],
        delta_timeline_entries: [
          {
            entry_id: "timeline-2",
            entity_kind: "work_item",
            entity_public_id: "issue-2",
            source_kind: "provider",
            source_ref: "comment-2",
            occurred_at: "2026-03-14T11:00:00Z",
            summary: "Updated status",
            is_read_only: true,
          },
        ],
        changed_command_ids: ["command-2"],
      },
    }),
  );

  assert.deepEqual(parsed, {
    event_kind: "delta",
    snapshot_id: "snapshot-2",
    resume_token: "resume-2",
    occurred_at: "2026-03-14T11:00:00Z",
    payload: {
      delta_entities: [
        {
          entity_kind: "work_item",
          entity_public_id: "issue-2",
          title: "Issue 2",
          state: "working",
          sync_status: "synced",
          provider_reference: {
            provider: "github",
            external_id: "repo#2",
            url: undefined,
          },
          primary_actor: undefined,
          relation_count: 2,
          last_timeline_at: undefined,
          badges: ["owner_review"],
          projection_version: 4,
        },
      ],
      delta_relations: [
        {
          relation_kind: "linked_to",
          source_kind: "provider",
          source_entity_kind: "work_item",
          source_entity_public_id: "issue-2",
          target_entity_kind: "pull_request",
          target_entity_public_id: "repo/pull/2",
          direction: "outbound",
        },
      ],
      delta_timeline_entries: [
        {
          entry_id: "timeline-2",
          entity_kind: "work_item",
          entity_public_id: "issue-2",
          source_kind: "provider",
          source_ref: "comment-2",
          occurred_at: "2026-03-14T11:00:00Z",
          summary: "Updated status",
          body_markdown: undefined,
          command_id: undefined,
          provider_url: undefined,
          is_read_only: true,
        },
      ],
      changed_command_ids: ["command-2"],
    },
  });
});

test("parseMissionControlRealtimeEnvelope rejects invalid payload", () => {
  assert.equal(
    parseMissionControlRealtimeEnvelope(
      JSON.stringify({
        event_kind: "connected",
        snapshot_id: "snapshot-1",
        resume_token: "resume-1",
        occurred_at: "2026-03-14T10:00:00Z",
        payload: {
          snapshot_freshness_status: "broken",
          server_cursor: "cursor-1",
        },
      }),
    ),
    null,
  );
});
