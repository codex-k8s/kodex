-- name: access_resolve_binding_subject :one
SELECT @subject_kind,
       COALESCE(subject.id, group_row.id)::text,
       COALESCE(subject.ref, group_row.ref),
       COALESCE(subject.display_name, group_row.display_name),
       COALESCE(subject.active, group_row.state = 'ACTIVE')
FROM (SELECT 1) singleton
LEFT JOIN control_plane.subjects subject
  ON @subject_kind IN ('USER', 'SERVICE')
 AND subject.organization_id = @organization_id::uuid
 AND subject.ref = @subject_ref
 AND subject.kind = @subject_kind
LEFT JOIN control_plane.oidc_groups group_row
  ON @subject_kind = 'OIDC_GROUP'
 AND group_row.organization_id = @organization_id::uuid
 AND group_row.ref = @subject_ref
WHERE subject.id IS NOT NULL OR group_row.id IS NOT NULL
