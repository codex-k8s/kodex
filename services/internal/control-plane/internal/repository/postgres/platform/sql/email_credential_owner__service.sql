-- name: email_credential_owner__service :one
SELECT id::text FROM control_plane.subjects
WHERE organization_id=@organization_id::uuid AND ref=@subject_ref AND kind='SERVICE' AND active
FOR SHARE;
