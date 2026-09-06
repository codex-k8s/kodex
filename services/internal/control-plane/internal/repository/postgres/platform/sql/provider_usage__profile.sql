-- name: provider_usage__profile :one
SELECT stable_key, provider, model, runtime_revision, enabled
FROM control_plane.runtime_profiles WHERE stable_key = @profile_ref;
