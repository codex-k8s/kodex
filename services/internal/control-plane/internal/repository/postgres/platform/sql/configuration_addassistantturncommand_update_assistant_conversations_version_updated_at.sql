-- name: configuration_addassistantturncommand_update_assistant_conversations_version_updated_at :one
WITH superseded_plan AS (
    UPDATE control_plane.assistant_plans AS plan
    SET state = 'STALE',
        validation_problems = ARRAY['superseded-by-new-turn'],
        version = plan.version + 1
    FROM control_plane.assistant_conversations AS conversation
    WHERE conversation.id = $1::uuid
      AND plan.id = conversation.latest_plan_id
      AND plan.state IN ('DRAFT', 'VALID', 'INVALID')
    RETURNING plan.id
)
UPDATE control_plane.assistant_conversations
SET version = version + 1,
    latest_plan_id = NULL,
    updated_at = clock_timestamp()
WHERE id = $1::uuid
RETURNING title,
          title_source,
          title_revision,
          state,
          version,
          context_route,
          context_entity_kind,
          context_entity_ref,
          context_entity_name,
          context_entity_version,
          allowed_operations,
          created_at,
          updated_at;
