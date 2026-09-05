-- name: email_mailbox_publication_finish_lock :one
SELECT publication.state,publication.organization_id::text,organization.ref,publication.connection_id::text,connection.ref,
    publication.connection_version,connection.version,connection.enabled AND connection.state<>'DELETED',
    COALESCE(publication.configuration_set_id::text,''),COALESCE(publication.configuration_revision_id::text,''),
    publication.kind,publication.document,publication.digest,publication.callback_at IS NOT NULL,publication.expires_at,
    publication.created_by::text
FROM control_plane.email_mailbox_publications publication
JOIN control_plane.organizations organization ON organization.id=publication.organization_id
JOIN control_plane.integration_connections connection ON connection.id=publication.connection_id AND connection.organization_id=publication.organization_id
WHERE publication.ref=$1 AND publication.claimant=$2 AND publication.claim_generation=$3 AND publication.lease_expires_at>clock_timestamp()
    AND publication.state IN ('PENDING','READY') AND publication.applied_at IS NOT NULL
FOR UPDATE OF publication,connection;
