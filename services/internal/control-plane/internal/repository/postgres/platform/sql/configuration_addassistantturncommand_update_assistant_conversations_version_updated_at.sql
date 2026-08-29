-- name: configuration_addassistantturncommand_update_assistant_conversations_version_updated_at :one
UPDATE control_plane.assistant_conversations
SET version = version + 1,
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
