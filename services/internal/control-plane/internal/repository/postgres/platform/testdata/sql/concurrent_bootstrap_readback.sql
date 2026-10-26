-- name: concurrent_bootstrap_readback :one
SELECT jsonb_build_object(
 'organizations',(SELECT count(*) FROM control_plane.organizations),
 'subjects',(SELECT count(*) FROM control_plane.subjects),
 'instruction_versions',(SELECT count(*) FROM control_plane.instruction_versions),
 'environment_versions',(SELECT count(*) FROM control_plane.runtime_environment_versions),
 'provider_credentials',(SELECT count(*) FROM control_plane.provider_credential_revisions),
 'audit_events',(SELECT count(*) FROM control_plane.audit_events),
 'outbox_events',(SELECT count(*) FROM control_plane.outbox_events)
)::text,
COALESCE((SELECT jsonb_object_agg(stable_key,version) FROM control_plane.integration_definitions),'{}'::jsonb)::text;
