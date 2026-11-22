-- name: email_mailbox_credentials_list :many
WITH visible AS MATERIALIZED (
    SELECT descriptor.name,descriptor.generation,descriptor.kind
    FROM control_plane.email_credential_descriptors descriptor
    JOIN control_plane.integration_connections connection ON connection.id=descriptor.connection_id
        AND connection.organization_id=descriptor.organization_id
    JOIN control_plane.subjects owner ON owner.id=connection.mailbox_owner_subject_id
        AND owner.organization_id=connection.organization_id AND owner.active
    WHERE descriptor.organization_id=$1::uuid AND connection.ref=$2 AND ($3='' OR descriptor.kind=$3)
      AND connection.mailbox_owner_state='ACTIVE'
      AND descriptor.mailbox_owner_generation=connection.mailbox_owner_generation
      AND descriptor.created_by=connection.mailbox_owner_configured_by
), page AS (SELECT * FROM visible WHERE name||'.'||lpad(generation::text,20,'0')>$4 ORDER BY name,generation LIMIT $5)
SELECT COALESCE(page.name,''),COALESCE(page.generation,0),COALESCE(page.kind,''),totals.total
FROM (SELECT count(*) AS total FROM visible) totals LEFT JOIN page ON true ORDER BY page.name,page.generation;
