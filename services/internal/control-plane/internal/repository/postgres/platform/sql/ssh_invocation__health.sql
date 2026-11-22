-- name: ssh_invocation__health :one
WITH stamp AS MATERIALIZED (SELECT clock_timestamp() AS at)
SELECT probe.id::text,probe.observed_subject_digest,least(probe.qualified_until,probe.created_at+interval '5 minutes')
FROM control_plane.ssh_identity_health_probes probe
JOIN control_plane.integration_connections connection ON connection.id=probe.connection_id AND connection.organization_id=probe.organization_id
LEFT JOIN control_plane.integration_personal_credential_tests personal ON personal.id=probe.personal_test_id AND personal.organization_id=probe.organization_id
LEFT JOIN control_plane.integration_connection_tests shared ON shared.id=probe.shared_test_id AND shared.organization_id=probe.organization_id
CROSS JOIN stamp
WHERE probe.organization_id=@organization_id::uuid AND probe.connection_id=@connection_id::uuid AND probe.identity_revision_id=@identity_id::uuid
 AND probe.outcome='AUTHENTICATED' AND probe.terminal_state='SUCCEEDED' AND probe.applied
 AND probe.definition_digest=@definition_digest AND probe.configuration_digest=@configuration_digest AND probe.transport_mode=@mode
 AND probe.qualified_until>stamp.at AND probe.created_at+interval '5 minutes'>stamp.at
 AND ((connection.credential_policy='INITIATOR_REQUIRED' AND personal.binding_id=NULLIF(@binding_id,'')::uuid
   AND personal.input_snapshot->>'DefinitionDigest'=@definition_digest AND personal.input_snapshot->>'ConfigurationDigest'=@configuration_digest
   AND (personal.input_snapshot->>'BindingGeneration')::bigint=@generation)
  OR (connection.credential_policy='PROJECT_SHARED' AND shared.broker_identity_revision_id=probe.identity_revision_id
   AND shared.input_snapshot->>'definitionDigest'=@definition_digest AND shared.input_snapshot->'configuration'=@configuration::jsonb
   AND (shared.input_snapshot->>'connectionVersion')::bigint=connection.version-1 AND shared.state='SUCCEEDED'))
ORDER BY probe.created_at DESC,probe.ref DESC LIMIT 1;
