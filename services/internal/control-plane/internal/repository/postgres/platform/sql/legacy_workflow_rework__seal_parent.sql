-- name: legacy_workflow_rework__seal_parent :one
WITH sealed AS (
    UPDATE control_plane.run_nodes node SET state='CANCELLED',finished_at=clock_timestamp(),version=node.version+1,
        next_actions='{}'::text[]
    WHERE node.id=@parent_node_id::uuid AND node.organization_id=@organization_id::uuid
        AND node.root_run_id=@root_run_id::uuid AND node.state='QUEUED'
        AND NOT EXISTS (SELECT 1 FROM control_plane.runtime_revisions revision WHERE revision.node_id=node.id)
        AND NOT EXISTS (SELECT 1 FROM control_plane.runtime_leases lease WHERE lease.node_id=node.id AND lease.state='CLAIMED')
    RETURNING node.id,node.turn_id
), closed_turn AS (
    UPDATE control_plane.session_turns turn SET state='CANCELLED',completed_at=clock_timestamp()
    FROM sealed WHERE turn.id=sealed.turn_id AND turn.state='QUEUED' RETURNING turn.id
)
SELECT id::text FROM sealed;
