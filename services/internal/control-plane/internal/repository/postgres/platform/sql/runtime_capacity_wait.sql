-- name: runtime_capacity_wait :one
WITH changed AS (
    UPDATE control_plane.run_nodes
    SET progress_summary = @reason, version = version + 1
    WHERE organization_id = @organization_id::uuid AND id = @node_id::uuid
      AND state = 'QUEUED' AND progress_summary IS DISTINCT FROM @reason
    RETURNING root_run_id
), presentation AS (
    UPDATE control_plane.runs root
    SET state = 'QUEUED',
        started_at = CASE WHEN EXISTS (SELECT 1 FROM control_plane.runtime_revisions revision WHERE revision.root_run_id = root.id) THEN root.started_at ELSE NULL END,
        presentation_metadata = jsonb_set(COALESCE(root.presentation_metadata, '{}'::jsonb), '{runtimeQueueReason}', to_jsonb(@queue_reason::text)),
        version = version + 1, updated_at = clock_timestamp()
    WHERE root.id = (SELECT root_run_id FROM control_plane.run_nodes WHERE id = @node_id::uuid AND organization_id = @organization_id::uuid AND state = 'QUEUED')
      AND (EXISTS(SELECT 1 FROM changed) OR root.state <> 'QUEUED' OR NOT root.presentation_metadata ? 'runtimeQueueReason')
      AND root.organization_id = @organization_id::uuid
      AND root.state IN ('QUEUED', 'RUNNING')
      AND NOT EXISTS (SELECT 1 FROM control_plane.run_nodes active
                      WHERE active.root_run_id = root.id AND active.type = 'AGENT_EXECUTION' AND active.state = 'RUNNING')
    RETURNING root.id, root.state
)
SELECT EXISTS(SELECT 1 FROM changed) OR EXISTS(SELECT 1 FROM presentation),
       COALESCE((SELECT state FROM presentation), root.state)
FROM control_plane.run_nodes node
JOIN control_plane.runs root ON root.id = node.root_run_id
WHERE node.id = @node_id::uuid AND node.organization_id = @organization_id::uuid;
