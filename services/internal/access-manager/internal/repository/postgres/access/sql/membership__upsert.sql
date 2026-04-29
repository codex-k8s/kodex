-- name: membership__upsert :exec
INSERT INTO access_memberships (
    id, subject_type, subject_id, target_type, target_id, role_hint, status, source,
    version, created_at, updated_at
) VALUES (
    @id, @subject_type, @subject_id, @target_type, @target_id, @role_hint, @status, @source,
    @version, @created_at, @updated_at
)
ON CONFLICT (subject_type, subject_id, target_type, target_id) DO UPDATE SET
    id = EXCLUDED.id,
    role_hint = EXCLUDED.role_hint,
    status = EXCLUDED.status,
    source = EXCLUDED.source,
    version = EXCLUDED.version,
    created_at = EXCLUDED.created_at,
    updated_at = EXCLUDED.updated_at;
