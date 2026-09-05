-- name: stt_runtime_actor :one
SELECT actor.id::text, actor.ref, actor.display_name, organization.ref
FROM control_plane.runs run
JOIN control_plane.runs root ON root.id=run.root_run_id AND root.organization_id=run.organization_id
JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id
JOIN control_plane.organizations organization ON organization.id=run.organization_id
WHERE run.organization_id=$1::uuid AND run.ref=$2 AND actor.active;
