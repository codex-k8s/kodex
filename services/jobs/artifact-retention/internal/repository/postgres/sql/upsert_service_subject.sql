-- name: upsert_service_subject :one
INSERT INTO control_plane.subjects (
    organization_id,
    ref,
    issuer,
    external_subject_digest,
    display_name,
    kind
)
VALUES (
    @organization_id::uuid,
    'svc_retention_' || replace(@organization_id::text, '-', ''),
    'kodex:artifact-retention',
    encode(digest(@organization_id::text, 'sha256'), 'hex'),
    'Artifact retention',
    'SERVICE'
)
ON CONFLICT (organization_id, issuer, external_subject_digest)
DO UPDATE SET active = true, updated_at = clock_timestamp()
RETURNING id::text;
