-- name: email_mailbox_publication_view :one
SELECT publication.ref,publication.revision,publication.digest,publication.state,COALESCE(revision.ref,''),
    publication.created_at,publication.ready_at,publication.failure_code
FROM control_plane.email_mailbox_publications publication
JOIN control_plane.integration_connections connection ON connection.organization_id=publication.organization_id AND connection.ref=$2
LEFT JOIN control_plane.email_mailbox_publication_bindings effect ON effect.publication_ref=publication.ref AND effect.connection_id=connection.id
LEFT JOIN control_plane.managed_configuration_revisions revision
    ON revision.id=CASE WHEN effect.publication_ref IS NULL THEN publication.configuration_revision_id ELSE effect.revision_id END
WHERE publication.organization_id=$1::uuid AND (connection.id=publication.connection_id OR effect.publication_ref IS NOT NULL) AND publication.kind<>'RECOVERY'
ORDER BY publication.revision DESC LIMIT 1;
