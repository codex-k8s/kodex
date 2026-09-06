-- +goose Up
SET ROLE control_plane_owner;
-- +goose StatementBegin
CREATE FUNCTION control_plane.provider_account_active_executions(p_organization_id uuid, p_account_id uuid)
RETURNS bigint LANGUAGE sql VOLATILE SECURITY INVOKER
SET search_path = pg_catalog, control_plane
AS $$
 SELECT count(*) FROM control_plane.runtime_leases lease
 JOIN control_plane.runtime_revisions revision ON revision.id=lease.runtime_revision_id
 WHERE revision.provider_account_id=p_account_id AND revision.organization_id=p_organization_id
   AND lease.organization_id=p_organization_id AND lease.state='CLAIMED'
   AND lease.expires_at>clock_timestamp()
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.provider_account_active_executions(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.provider_account_active_executions(uuid,uuid) TO control_plane_runtime;

-- Общий снимок выбора сохраняет существующий порядок policy и не резервирует slots.
-- +goose StatementBegin
CREATE FUNCTION control_plane.provider_account_selection(p_organization_id uuid,p_agent_ref text)
RETURNS TABLE(account_id uuid,account_ref text,config_ref text,config_version bigint,config_digest text,
 policy_ref text,policy_version bigint,policy_digest text)
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
 SELECT account.id,account.ref,config.ref,config.version_number,config.digest,
   policy.ref,policy.version_number,policy.digest
 FROM control_plane.agents agent
 JOIN control_plane.agent_runtime_config_versions config ON config.id=agent.current_runtime_config_id
 JOIN control_plane.provider_account_policy_versions policy ON policy.id=config.provider_account_policy_id
 JOIN LATERAL jsonb_array_elements(policy.account_candidates) candidate(value) ON true
 JOIN control_plane.provider_accounts account ON account.organization_id=agent.organization_id
  AND account.ref=candidate.value->>'accountRef' AND account.enabled AND account.state='AUTHORIZED'
  AND account.current_credential_revision_id IS NOT NULL
 JOIN control_plane.provider_definitions definition ON definition.stable_key=account.definition_key
  AND definition.stable_key=config.provider
 LEFT JOIN LATERAL (SELECT count(*)::bigint AS active_sessions FROM control_plane.sessions session
  WHERE session.provider_account_id=account.id AND session.state='ACTIVE') usage ON true
 WHERE agent.organization_id=p_organization_id AND agent.ref=p_agent_ref AND agent.enabled AND agent.state='READY'
 ORDER BY CASE policy.mode WHEN 'FIXED' THEN 0::numeric
  WHEN 'LEAST_USED' THEN usage.active_sessions::numeric
  WHEN 'WEIGHTED' THEN usage.active_sessions::numeric/GREATEST((candidate.value->>'weight')::numeric,1)
  ELSE 1000000000::numeric END,account.ref LIMIT 1
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.provider_account_selection(uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.provider_account_selection(uuid,text) TO control_plane_runtime;
RESET ROLE;
