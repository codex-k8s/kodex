-- name: platform__artifacts_changeartifactbinding_1 :one
SELECT a.id::text,a.project_id::text,p.ref,a.version FROM control_plane.artifacts a JOIN control_plane.projects p ON p.id=a.project_id WHERE a.organization_id=$1::uuid AND a.ref=$2 AND a.scan_state='CLEAN' FOR UPDATE
