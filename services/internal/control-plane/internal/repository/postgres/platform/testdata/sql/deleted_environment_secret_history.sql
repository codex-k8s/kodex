-- name: deleted_environment_secret_history :one
SELECT environment.state, environment.version, current_version.ref,
       (SELECT jsonb_agg(jsonb_build_array(version.ref, version.digest, version.secret_descriptors)
                        ORDER BY version.version_number)::text
        FROM control_plane.runtime_environment_versions version
        WHERE version.environment_set_id=environment.id)
FROM control_plane.runtime_environment_sets environment
JOIN control_plane.runtime_environment_versions current_version ON current_version.id=environment.current_version_id
WHERE environment.ref=$1;
