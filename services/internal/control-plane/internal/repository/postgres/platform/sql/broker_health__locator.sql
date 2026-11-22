-- name: broker_health__locator :one
WITH source AS (
 SELECT c.ref,false AS shared FROM control_plane.integration_personal_credential_tests t JOIN control_plane.integration_connections c ON c.id=t.connection_id AND c.organization_id=t.organization_id WHERE t.organization_id=@organization_id::uuid AND t.ref=@ref
 UNION ALL
 SELECT c.ref,true FROM control_plane.integration_connection_tests t JOIN control_plane.integration_connections c ON c.id=t.connection_id AND c.organization_id=t.organization_id WHERE t.organization_id=@organization_id::uuid AND t.ref=@ref
) SELECT ref,shared FROM source WHERE (SELECT count(*) FROM source)=1;
