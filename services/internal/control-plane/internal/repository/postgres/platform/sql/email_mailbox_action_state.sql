-- name: email_mailbox_action_state :one
SELECT connection.enabled AND connection.state<>'DELETED',
    EXISTS(SELECT 1 FROM control_plane.managed_configuration_revisions revision
        JOIN control_plane.managed_configuration_sets configuration ON configuration.id=revision.configuration_set_id
        WHERE configuration.organization_id=$1::uuid AND configuration.ref=$3 AND revision.state IN ('DRAFT','VALID','INVALID')),
    EXISTS(SELECT 1 FROM control_plane.email_mailbox_publications WHERE state='PENDING')
FROM control_plane.integration_connections connection WHERE connection.organization_id=$1::uuid AND connection.ref=$2;
