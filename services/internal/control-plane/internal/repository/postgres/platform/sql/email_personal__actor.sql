-- name: email_personal__actor :one
SELECT id::text FROM control_plane.subjects
WHERE organization_id=@organization_id::uuid AND id=@subject_id::uuid AND kind='USER' AND active
FOR SHARE;
