-- name: platform__commands_emitplatformevent_1 :one
UPDATE control_plane.installation SET platform_sequence=platform_sequence+1 WHERE singleton RETURNING platform_sequence
