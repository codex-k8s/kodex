-- name: stt_provider_fixture_activate :exec
UPDATE control_plane.provider_accounts SET current_credential_revision_id = $2::uuid WHERE ref = $1;
