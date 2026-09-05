-- name: secret_impact_target :one
SELECT secret.ref,secret.version,revision.revision,project.ref,project.id::text
FROM control_plane.runtime_secrets secret
JOIN control_plane.projects project ON project.id=secret.project_id AND project.lifecycle='ACTIVE'
JOIN control_plane.runtime_secret_revisions revision ON revision.secret_id=secret.id
WHERE secret.organization_id=$1::uuid AND secret.ref=$2 AND secret.state='ACTIVE'
  AND revision.state='ACTIVE'
  AND revision.revision=CASE WHEN $3::bigint=0 THEN secret.current_revision ELSE $3::bigint END;
