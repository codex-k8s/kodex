-- name: email_mailbox_observation_advance :exec
WITH target AS MATERIALIZED (
    SELECT id,organization_id FROM control_plane.integration_connections
    WHERE organization_id=@organization_id::uuid AND id=@connection_id::uuid
      AND definition_key='email' AND lifecycle_state='ACTIVE' AND enabled
      AND version=@current_version
), anchors AS (
    SELECT publication.ref,publication.organization_id,publication.connection_id,publication.connection_version
    FROM control_plane.email_mailbox_publications publication,target
    WHERE publication.organization_id=target.organization_id AND publication.connection_id=target.id
      AND publication.state IN ('PENDING','READY')
    UNION
    SELECT publication.ref,effect.organization_id,effect.connection_id,effect.connection_version
    FROM control_plane.email_mailbox_publication_bindings effect
    JOIN control_plane.email_mailbox_publications publication ON publication.ref=effect.publication_ref
    JOIN target ON target.id=effect.connection_id AND target.organization_id=effect.organization_id
    WHERE publication.state IN ('PENDING','READY')
)
INSERT INTO control_plane.email_mailbox_observation_receipts
    (publication_ref,organization_id,connection_id,previous_version,current_version)
SELECT anchors.ref,anchors.organization_id,anchors.connection_id,@previous_version,@current_version
FROM anchors
WHERE COALESCE((SELECT max(receipt.current_version)
    FROM control_plane.email_mailbox_observation_receipts receipt
    WHERE receipt.publication_ref=anchors.ref AND receipt.connection_id=anchors.connection_id
      AND receipt.organization_id=anchors.organization_id),anchors.connection_version)=@previous_version;
