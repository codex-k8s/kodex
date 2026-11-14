-- name: github_scope__selection_proof :one
WITH pages AS (
 SELECT test.github_discovery
 FROM control_plane.integration_personal_credential_tests test
 JOIN control_plane.integration_personal_credentials binding
   ON binding.id=test.binding_id AND binding.organization_id=test.organization_id
 JOIN control_plane.integration_personal_credential_revisions revision
   ON revision.id=test.credential_revision_id AND revision.binding_id=binding.id AND revision.organization_id=binding.organization_id
 JOIN control_plane.integration_connections connection
   ON connection.id=test.connection_id AND connection.organization_id=test.organization_id
 WHERE test.organization_id=@organization_id::uuid AND test.subject_id=@subject_id::uuid
   AND connection.ref=@connection_ref AND connection.version=@connection_version::bigint AND binding.ref=@binding_ref
   AND binding.generation=@binding_generation AND revision.ref=@credential_ref
   AND test.state='SUCCEEDED' AND test.github_discovery IS NOT NULL
   AND test.input_snapshot->'BindingGeneration'=to_jsonb(@binding_generation::bigint)
   AND test.input_snapshot->'ConnectionVersion'=to_jsonb(@connection_version::bigint)
   AND test.input_snapshot->>'ConfigurationDigest'=@configuration_digest
   AND test.input_snapshot->>'DefinitionDigest'=@definition_digest
 ORDER BY test.created_at DESC,test.ref DESC LIMIT 1000
), available AS (
 SELECT DISTINCT repository->>'id' AS id,repository->>'full_name' AS full_name
 FROM pages CROSS JOIN LATERAL jsonb_array_elements(pages.github_discovery->'items') repository
)
SELECT NOT EXISTS (
 SELECT 1 FROM jsonb_array_elements(@repositories::jsonb) selected
 WHERE NOT EXISTS(SELECT 1 FROM available WHERE available.id=selected->>'id' AND available.full_name=selected->>'full_name')
)
