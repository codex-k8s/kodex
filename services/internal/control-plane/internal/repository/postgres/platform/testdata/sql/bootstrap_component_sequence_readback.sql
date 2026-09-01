-- name: bootstrap_component_sequence_readback :one
SELECT platform_sequence, authority_proof_sequence
FROM control_plane.installation
WHERE singleton
