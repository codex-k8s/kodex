-- +goose Up
SET ROLE control_plane_owner;

-- Network policy v2 добавляет owner-managed web access и отдельный
-- authenticated runtime proxy на 8084. Существующие версии остаются
-- неизменяемой историей; для каждого активного окружения создаётся новая
-- дочерняя версия с тем же core и новой exact network policy.
-- +goose StatementBegin
DO $$
DECLARE
    unsupported_count bigint;
BEGIN
    SELECT count(*)
      INTO unsupported_count
      FROM control_plane.runtime_environment_sets environment
      JOIN control_plane.runtime_environment_versions current
        ON current.id = environment.current_version_id
     WHERE NOT (current.network_policy ? 'web_access')
       AND (
         current.network_policy <> CASE current.kubernetes_access_profile->>'kind'
           WHEN 'NONE' THEN '{
             "deny_by_default": true,
             "egress": [
               {"destination":"DNS","protocol":"TCP","port":53},
               {"destination":"DNS","protocol":"UDP","port":53},
               {"destination":"PROVIDER_PROXY","protocol":"TCP","port":8080},
               {"destination":"RUNTIME_CALLBACK","protocol":"TCP","port":8444}
             ]
           }'::jsonb
           WHEN 'READ_OWN_EXECUTION' THEN '{
             "deny_by_default": true,
             "egress": [
               {"destination":"DNS","protocol":"TCP","port":53},
               {"destination":"DNS","protocol":"UDP","port":53},
               {"destination":"KUBERNETES_API","protocol":"TCP","port":443},
               {"destination":"PROVIDER_PROXY","protocol":"TCP","port":8080},
               {"destination":"RUNTIME_CALLBACK","protocol":"TCP","port":8444}
             ]
           }'::jsonb
           ELSE NULL
         END
       );
    IF unsupported_count <> 0 THEN
        RAISE EXCEPTION 'unsupported legacy runtime network policy: % row(s)', unsupported_count;
    END IF;
END $$;
-- +goose StatementEnd

CREATE TEMP TABLE issue_1789_environment_targets ON COMMIT DROP AS
SELECT environment.id AS environment_id,
       current.*,
       (SELECT max(version_number) + 1
          FROM control_plane.runtime_environment_versions version
         WHERE version.environment_set_id = environment.id) AS next_version,
       CASE current.kubernetes_access_profile->>'kind'
         WHEN 'NONE' THEN '{
           "deny_by_default": true,
           "egress": [
             {"destination":"DNS","protocol":"TCP","port":53},
             {"destination":"DNS","protocol":"UDP","port":53},
             {"destination":"PROVIDER_PROXY","protocol":"TCP","port":8084},
             {"destination":"RUNTIME_CALLBACK","protocol":"TCP","port":8444}
           ],
           "web_access":{"mode":"NONE","rules":[]}
         }'::jsonb
         WHEN 'READ_OWN_EXECUTION' THEN '{
           "deny_by_default": true,
           "egress": [
             {"destination":"DNS","protocol":"TCP","port":53},
             {"destination":"DNS","protocol":"UDP","port":53},
             {"destination":"KUBERNETES_API","protocol":"TCP","port":443},
             {"destination":"PROVIDER_PROXY","protocol":"TCP","port":8084},
             {"destination":"RUNTIME_CALLBACK","protocol":"TCP","port":8444}
           ],
           "web_access":{"mode":"NONE","rules":[]}
         }'::jsonb
       END AS next_network_policy,
       CASE current.kubernetes_access_profile->>'kind'
         WHEN 'NONE' THEN '7d4998b5d8c1db3a90002ea8e56bc4c1103a5facbf5eba9b313355f3b55ca765'
         WHEN 'READ_OWN_EXECUTION' THEN 'de9f8cf5f3f75e49dc9d0df8e4776a33e17fbdc1501536d6284ec746342830d9'
       END AS next_network_digest
  FROM control_plane.runtime_environment_sets environment
  JOIN control_plane.runtime_environment_versions current
    ON current.id = environment.current_version_id
 WHERE NOT (current.network_policy ? 'web_access');

WITH inserted AS (
    INSERT INTO control_plane.runtime_environment_versions
        (ref, organization_id, environment_set_id, version_number, parent_version_id,
         non_secret_values, secret_descriptors, role_image_artifact_id, selected_tools,
         core_digest, resource_policy, volume_policy, network_policy, kubernetes_access_profile,
         resources_digest, volumes_digest, network_digest, rbac_digest, digest, created_by)
    SELECT 'renvv_' || replace(gen_random_uuid()::text, '-', ''),
           organization_id,
           environment_set_id,
           next_version,
           id,
           non_secret_values,
           secret_descriptors,
           role_image_artifact_id,
           selected_tools,
           core_digest,
           resource_policy,
           volume_policy,
           next_network_policy,
           kubernetes_access_profile,
           resources_digest,
           volumes_digest,
           next_network_digest,
           rbac_digest,
           encode(digest(
             convert_to('runtime-environment-v2', 'UTF8') || decode('00', 'hex') ||
             convert_to(core_digest, 'UTF8') || decode('00', 'hex') ||
             convert_to(resources_digest, 'UTF8') || decode('00', 'hex') ||
             convert_to(volumes_digest, 'UTF8') || decode('00', 'hex') ||
             convert_to(next_network_digest, 'UTF8') || decode('00', 'hex') ||
             convert_to(rbac_digest, 'UTF8') || decode('00', 'hex'),
             'sha256'), 'hex'),
           created_by
      FROM issue_1789_environment_targets
    RETURNING id, environment_set_id
)
UPDATE control_plane.runtime_environment_sets environment
   SET current_version_id = inserted.id,
       version = environment.version + 1,
       updated_at = clock_timestamp()
  FROM inserted
 WHERE environment.id = inserted.environment_set_id;

RESET ROLE;
