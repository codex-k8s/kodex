-- name: catalog_card_connection :exec
WITH connection AS (
 INSERT INTO control_plane.integration_connections(ref,organization_id,definition_key,name,state,masked_credentials_state,created_by)
 SELECT 'conn_card_projection',a.organization_id,d.stable_key,'Card projection connection','TESTING','NOT_CONFIGURED',a.created_by
 FROM control_plane.agents a CROSS JOIN LATERAL (
  SELECT stable_key FROM control_plane.integration_definitions WHERE enabled ORDER BY stable_key LIMIT 1
 ) d WHERE a.ref=$1
 RETURNING id,organization_id,created_by
)
INSERT INTO control_plane.integration_grants(ref,organization_id,connection_id,capability_key,target_kind,target_ref,created_by)
SELECT 'grant_card_projection',organization_id,id,'synthetic.card.read','AGENT',$1,created_by FROM connection;
