-- name: artifacts_lifecycle_select_has_queued_dependencies :one
SELECT EXISTS (
    SELECT 1
    FROM control_plane.attachment_set_items item
    JOIN control_plane.runs run
      ON run.input_attachment_set_id = item.attachment_set_id
    JOIN control_plane.run_nodes node
      ON node.root_run_id = run.root_run_id
    WHERE item.artifact_id = @artifact_id::uuid
      AND item.artifact_version = @artifact_version
      AND node.type = 'AGENT_EXECUTION'
      AND node.state = 'QUEUED'

    UNION ALL

    SELECT 1
    FROM control_plane.attachment_set_items item
    JOIN control_plane.session_turns turn
      ON turn.attachment_set_id = item.attachment_set_id
    JOIN control_plane.run_nodes node
      ON node.turn_id = turn.id
    WHERE item.artifact_id = @artifact_id::uuid
      AND item.artifact_version = @artifact_version
      AND node.type = 'AGENT_EXECUTION'
      AND node.state = 'QUEUED'
);
