-- name: access_sync_upsert_group :one
INSERT INTO control_plane.oidc_groups
    (ref, organization_id, issuer, external_group_digest, display_name, last_seen_at, synchronized_at)
VALUES (@ref, @organization_id::uuid, @issuer, @external_group_digest, @display_name, @observed_at, @observed_at)
ON CONFLICT (organization_id, issuer, external_group_digest)
DO UPDATE SET display_name = EXCLUDED.display_name, state = 'ACTIVE',
              last_seen_at = EXCLUDED.last_seen_at,
              synchronized_at = EXCLUDED.synchronized_at,
              version = control_plane.oidc_groups.version + 1
RETURNING id::text, ref
