-- Только read-only состояние renewal; credential bytes и личные данные не читаются.
BEGIN READ ONLY;
SELECT json_build_object(
  'at', clock_timestamp(),
  'tasks', (SELECT COALESCE(json_agg(json_build_object(
      'ref', task.ref, 'state', task.state, 'generation', task.generation,
      'accountVersion', task.account_version, 'outputAccountVersion', task.output_account_version,
      'authorized', task.authorized_at IS NOT NULL, 'expired', task.expires_at <= clock_timestamp())), '[]'::json)
    FROM control_plane.provider_credential_renewal_tasks task),
  'accounts', (SELECT COALESCE(json_agg(json_build_object(
      'ref', account.ref, 'state', account.state, 'enabled', account.enabled,
      'version', account.version, 'credentialRevision', credential.revision_number,
      'catalogFailure', (SELECT observation.failure FROM control_plane.provider_model_catalog_observations observation
        WHERE observation.provider_account_id = account.id AND observation.account_version = account.version
          AND observation.provider_credential_revision_id = credential.id ORDER BY observation.created_at DESC, observation.id DESC LIMIT 1),
      'catalog', (SELECT json_build_object('source', observation.source, 'observedAt', observation.observed_at,
          'expiresAt', observation.expires_at, 'modelCount', jsonb_array_length(observation.models))
        FROM control_plane.provider_model_catalog_observations observation
        WHERE observation.provider_account_id = account.id AND observation.account_version = account.version
          AND observation.provider_credential_revision_id = credential.id ORDER BY observation.created_at DESC, observation.id DESC LIMIT 1))), '[]'::json)
    FROM control_plane.provider_accounts account
    LEFT JOIN control_plane.provider_credential_revisions credential ON credential.id = account.current_credential_revision_id
    WHERE account.definition_key = 'openai-codex'));
COMMIT;
