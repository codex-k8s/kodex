-- name: assistant_project_move_pending_plans :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.assistant_plans
    WHERE organization_id=$1::uuid AND conversation_ref=$2
      AND state NOT IN ('APPLIED', 'REJECTED')
);
