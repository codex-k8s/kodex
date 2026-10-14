-- name: artifact_preview_effect_counts :one
SELECT
    (SELECT count(*) FROM control_plane.artifact_download_grants grant_row
     JOIN control_plane.artifact_heads artifact ON artifact.id=grant_row.artifact_id
     WHERE artifact.ref=ANY($1::text[]) AND grant_row.purpose='PREVIEW' AND grant_row.consumed_at IS NOT NULL),
    (SELECT count(*) FROM control_plane.audit_events audit
     WHERE audit.resource_kind='ARTIFACT' AND audit.resource_ref=ANY($1::text[]) AND audit.action='artifact.preview');
