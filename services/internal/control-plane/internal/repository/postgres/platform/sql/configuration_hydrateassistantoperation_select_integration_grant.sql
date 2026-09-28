WITH admission AS (
    SELECT *
    FROM control_plane.integration_grant_admission(
        $1::uuid,
        $2::uuid,
        NULLIF($3, '')::uuid,
        $4,
        $5,
        $6,
        $7,
        $8,
        'GRANT',
        NULL
    )
)
SELECT connection.name,
       admission.connection_version,
       admission.recipient_name,
       admission.recipient_version,
       admission.reason,
       COALESCE(grant_row.enabled, false),
       COALESCE(grant_row.approval_scope_paths, '{}'::text[])
FROM admission
JOIN control_plane.integration_connections connection
  ON connection.id = admission.connection_id
LEFT JOIN control_plane.integration_grants grant_row
  ON grant_row.organization_id = $1::uuid
 AND grant_row.connection_id = admission.connection_id
 AND grant_row.capability_key = admission.capability_key
 AND grant_row.target_kind = admission.recipient_kind
 AND grant_row.target_ref = admission.recipient_ref
WHERE admission.project_ref = $5
  AND admission.recipient_kind = $6
  AND admission.recipient_ref = $7
  AND admission.capability_key = $8
