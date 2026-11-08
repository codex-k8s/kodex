-- name: bootstrap_component_sequence_readback :one
SELECT platform_sequence,
    (SELECT revision FROM control_plane.authority_proof_revision_counter WHERE singleton)
FROM control_plane.installation
WHERE singleton
