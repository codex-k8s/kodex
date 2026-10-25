-- name: integration_gate_feedback_snapshot :one
SELECT revision.safe_snapshot->>'task', revision.revision_digest
FROM control_plane.runtime_revisions revision
JOIN control_plane.run_nodes node ON node.id=revision.node_id AND node.organization_id=revision.organization_id
JOIN control_plane.runs run ON run.id=revision.run_id AND run.organization_id=revision.organization_id
JOIN control_plane.sessions session ON session.id=revision.session_id AND session.organization_id=revision.organization_id
WHERE revision.ref=$1 AND node.ref=$2 AND run.ref=$3 AND session.ref=$4;
