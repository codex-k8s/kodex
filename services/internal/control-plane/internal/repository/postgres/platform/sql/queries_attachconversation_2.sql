-- name: platform__queries_attachconversation_2 :one
SELECT p.ref,p.summary,p.state,p.version,p.operations,p.created_at,p.applied_at FROM control_plane.assistant_plans p JOIN control_plane.assistant_conversations c ON c.latest_plan_id=p.id WHERE c.organization_id=$1::uuid AND c.ref=$2
