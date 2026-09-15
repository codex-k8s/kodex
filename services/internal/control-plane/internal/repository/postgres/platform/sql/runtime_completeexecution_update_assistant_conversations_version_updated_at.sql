-- name: runtime_completeexecution_update_assistant_conversations_version_updated_at :exec
UPDATE control_plane.assistant_conversations
SET title = CASE WHEN title_source = 'SERVER_DEFAULT' AND $2 <> '' THEN $2 ELSE title END,
    title_source = CASE WHEN title_source = 'SERVER_DEFAULT' AND $2 <> '' THEN 'AGENT_PROPOSED' ELSE title_source END,
    title_revision = CASE WHEN title_source = 'SERVER_DEFAULT' AND $2 <> '' THEN title_revision + 1 ELSE title_revision END,
    version = version + 1,
    updated_at = clock_timestamp()
WHERE session_id = $1::uuid
