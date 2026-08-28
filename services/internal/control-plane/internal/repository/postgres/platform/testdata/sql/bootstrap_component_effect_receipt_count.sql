SELECT count(*)
FROM control_plane.integration_effect_receipts
WHERE effect_key = $1
