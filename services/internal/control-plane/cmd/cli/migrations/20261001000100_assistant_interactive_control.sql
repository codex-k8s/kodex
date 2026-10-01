-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.runs
    ADD COLUMN dispatch_priority smallint NOT NULL DEFAULT 0
        CHECK (dispatch_priority IN (0, 1));

CREATE INDEX runs_session_dispatch_order
    ON control_plane.runs (session_id, dispatch_priority DESC, created_at)
    WHERE session_id IS NOT NULL
      AND state IN ('QUEUED', 'RUNNING', 'WAITING_HUMAN', 'CANCELLING');

UPDATE control_plane.runtime_profiles
SET model = 'gpt-5.6-sol',
    version = version + 1
WHERE stable_key = 'builtin-safe-runtime'
  AND provider = 'openai-codex'
  AND model <> 'gpt-5.6-sol'
  AND runtime_revision = 'runtime-v1';

WITH target AS (
    SELECT agent.id AS agent_id,
           agent.organization_id,
           current.provider_account_policy_id,
           current.runtime_profile_key,
           current.provider,
           policy.ref AS policy_ref,
           policy.version_number AS policy_version,
           policy.digest AS policy_digest,
           current.created_by,
           (SELECT max(version_number) + 1
              FROM control_plane.agent_runtime_config_versions version
             WHERE version.agent_id = agent.id) AS next_version
    FROM control_plane.agents agent
    JOIN control_plane.agent_runtime_config_versions current
      ON current.id = agent.current_runtime_config_id
    JOIN control_plane.provider_account_policy_versions policy
      ON policy.id = current.provider_account_policy_id
    WHERE current.provider = 'openai-codex'
      AND current.model <> 'gpt-5.6-sol'
), inserted AS (
    INSERT INTO control_plane.agent_runtime_config_versions
        (ref, organization_id, agent_id, version_number, provider_account_policy_id,
         runtime_profile_key, provider, model, digest, created_by)
    SELECT 'rconf_' || replace(gen_random_uuid()::text, '-', ''),
           organization_id,
           agent_id,
           next_version,
           provider_account_policy_id,
           runtime_profile_key,
           provider,
           'gpt-5.6-sol',
           encode(digest(convert_to(runtime_profile_key, 'UTF8') || decode('00', 'hex') ||
                         convert_to(provider, 'UTF8') || decode('00', 'hex') ||
                         convert_to('gpt-5.6-sol', 'UTF8') || decode('00', 'hex') ||
                         convert_to(policy_ref, 'UTF8') || decode('00', 'hex') ||
                         convert_to(policy_version::text, 'UTF8') || decode('00', 'hex') ||
                         convert_to(policy_digest, 'UTF8') || decode('00', 'hex'), 'sha256'), 'hex'),
           created_by
    FROM target
    RETURNING id, agent_id
)
UPDATE control_plane.agents agent
SET current_runtime_config_id = inserted.id,
    version = version + 1,
    updated_at = clock_timestamp()
FROM inserted
WHERE agent.id = inserted.agent_id;

CREATE TEMP TABLE issue_1789_overlay_targets ON COMMIT DROP AS
SELECT agent.id AS agent_id,
       current.id AS parent_id,
       current.organization_id,
       current.version_number + 1 AS next_version,
       CASE
         WHEN btrim(regexp_replace(current.content,
              E'(^|\\n)[ \\t]*model_reasoning_effort[ \\t]*=[^\\n]*(\\n|$)', E'\\1', 'g')) = ''
         THEN E'model_reasoning_effort = "medium"\\n'
         ELSE rtrim(regexp_replace(current.content,
              E'(^|\\n)[ \\t]*model_reasoning_effort[ \\t]*=[^\\n]*(\\n|$)', E'\\1', 'g')) ||
              E'\\nmodel_reasoning_effort = "medium"\\n'
       END AS content,
       current.created_by,
       current.schema_revision,
       current.schema_digest
FROM control_plane.agents agent
JOIN control_plane.agent_config_overlay_versions current
  ON current.id = agent.current_config_overlay_id
WHERE current.content !~ E'(^|\\n)[ \\t]*model_reasoning_effort[ \\t]*=[ \\t]*"medium"[ \\t]*(\\n|$)';

UPDATE control_plane.agent_config_overlay_versions current
SET state = 'SUPERSEDED'
FROM issue_1789_overlay_targets target
WHERE current.id = target.parent_id;

WITH inserted AS (
    INSERT INTO control_plane.agent_config_overlay_versions
        (ref, organization_id, agent_id, version_number, parent_version_id, state,
         content, digest, validation_errors, diagnostics, schema_revision,
         schema_digest, created_by, validated_at, published_at)
    SELECT 'cov_' || replace(gen_random_uuid()::text, '-', ''),
           organization_id,
           agent_id,
           next_version,
           parent_id,
           'PUBLISHED',
           content,
           encode(digest(convert_to(content, 'UTF8'), 'sha256'), 'hex'),
           '[]'::jsonb,
           '[]'::jsonb,
           schema_revision,
           schema_digest,
           created_by,
           clock_timestamp(),
           clock_timestamp()
    FROM issue_1789_overlay_targets
    RETURNING id, agent_id
)
UPDATE control_plane.agents agent
SET current_config_overlay_id = inserted.id,
    version = version + 1,
    updated_at = clock_timestamp()
FROM inserted
WHERE agent.id = inserted.agent_id;

RESET ROLE;
