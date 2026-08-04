-- name: MattermostEventKeysetAdmit :one
SELECT keyset_revision, high_watermark, served_generation, keyset_sha256, retired_generations
FROM control_plane.admit_mattermost_event_keyset(
    @producer_id, @keyset_revision, @high_watermark, @served_generation,
    @keyset_sha256, @retired_generations, @active_generations, @key_identities::jsonb
);
