-- +goose Up
-- Сохраняет полномочия закреплённых пакетов GitLab при доставке операции чтения.
SET ROLE control_plane_owner;
SET LOCAL lock_timeout='8s';
SET LOCAL statement_timeout='45s';
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.integration_pinned_capabilities(p_connection uuid)
RETURNS jsonb LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $pinned$
 SELECT CASE
  WHEN (c.definition_version,c.definition_digest)=(d.definition_version,d.digest) THEN d.capabilities
  WHEN (c.definition_key,d.definition_version,d.digest,c.definition_version,c.definition_digest)=(
   'gitlab','2.1.0','ce4be39772c9051cedf9ec4f5ed41d38d0ded0e5352befa3e79576558ba07bbb',
   '2.0.0','ce63966754a8575e0139b7f2887a6d0e4a8516e7fcfc0e4c4d888f134c51499e')
  THEN (
   SELECT jsonb_agg(cap ORDER BY position)
   FROM jsonb_array_elements(d.capabilities) WITH ORDINALITY AS prior(cap,position)
   WHERE cap->>'key'<>'gitlab.merge_request.approval.read'
  )
  ELSE COALESCE(
   control_plane.integration_policy_legacy_capabilities(c.definition_key,d.definition_version,d.digest,c.definition_version,c.definition_digest),
   CASE WHEN (c.definition_key,d.definition_version,d.digest,c.definition_version,c.definition_digest)=(
    'gitlab','2.1.0','ce4be39772c9051cedf9ec4f5ed41d38d0ded0e5352befa3e79576558ba07bbb',
    '1.2.1','1662b4b83b9a6b62c13ed448e8457ba547eb743f9c296349a4389c8ac1909062')
   THEN control_plane.integration_policy_legacy_capabilities(
    'gitlab','2.0.0','ce63966754a8575e0139b7f2887a6d0e4a8516e7fcfc0e4c4d888f134c51499e',c.definition_version,c.definition_digest) END,
   (
    SELECT revision.content::jsonb #> '{spec,capabilities}'
    FROM control_plane.managed_configuration_bindings binding
    JOIN control_plane.managed_configuration_sets configuration ON configuration.id=binding.configuration_set_id AND configuration.organization_id=binding.organization_id
    JOIN control_plane.managed_configuration_revisions revision ON revision.id=binding.configuration_revision_id AND revision.organization_id=binding.organization_id AND revision.configuration_set_id=configuration.id
    WHERE binding.organization_id=c.organization_id AND binding.consumer_kind='INTEGRATION_CONNECTION' AND binding.consumer_ref=c.ref
      AND binding.configuration_kind='INTEGRATION_DEFINITION' AND configuration.kind='INTEGRATION_DEFINITION'
      AND revision.state='PUBLISHED' AND revision.content_format='JSON'
      AND encode(public.digest(convert_to(revision.content,'UTF8'),'sha256'),'hex')=c.definition_digest
      AND revision.content::jsonb #>> '{metadata,key}'=c.definition_key AND revision.content::jsonb #>> '{metadata,version}'=c.definition_version
   ))
 END
 FROM control_plane.integration_connections c JOIN control_plane.integration_definitions d ON d.stable_key=c.definition_key
 WHERE c.id=p_connection;
$pinned$;
-- +goose StatementEnd
RESET ROLE;
