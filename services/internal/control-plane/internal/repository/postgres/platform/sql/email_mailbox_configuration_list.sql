-- name: email_mailbox_configuration_list :many
WITH visible AS MATERIALIZED (
    SELECT configuration.ref FROM control_plane.managed_configuration_sets configuration
    JOIN control_plane.email_mailbox_configuration_sets owner ON owner.configuration_set_id=configuration.id
        AND owner.organization_id=configuration.organization_id
    JOIN control_plane.integration_connections connection ON connection.id=owner.connection_id
        AND connection.organization_id=owner.organization_id AND connection.definition_key='email' AND connection.state<>'DELETED'
    WHERE configuration.organization_id=$1::uuid AND connection.ref=$2
        AND ($3='' OR configuration.name ILIKE '%'||$3||'%')
), page AS (SELECT ref FROM visible WHERE ref>$4 ORDER BY ref LIMIT $5)
SELECT COALESCE(page.ref,''),totals.total FROM (SELECT count(*) AS total FROM visible) totals
LEFT JOIN page ON true ORDER BY page.ref;
