-- name: configuration_addassistantturncommand_update_assistant_conversations_version_updated_at :one
UPDATE control_plane.assistant_conversations
SET version = version + 1,
    latest_plan_id = NULL,
    title = CASE
      WHEN title_source = 'SERVER_DEFAULT' AND title = 'i18n:NEW_ASSISTANT_CONVERSATION'
      THEN left(regexp_replace(trim($8), '[[:space:]]+', ' ', 'g'), 80)
      ELSE title
    END,
    title_revision = CASE
      WHEN title_source = 'SERVER_DEFAULT' AND title = 'i18n:NEW_ASSISTANT_CONVERSATION'
      THEN title_revision + 1
      ELSE title_revision
    END,
    context_route = $2,
    context_entity_kind = $3,
    context_entity_ref = $4,
    context_entity_name = $5,
    context_entity_version = $6,
    allowed_operations = $7,
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
