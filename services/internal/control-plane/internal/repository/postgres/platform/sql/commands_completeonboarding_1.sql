-- name: platform__commands_completeonboarding_1 :exec
UPDATE control_plane.installation SET onboarding_completed_at=COALESCE(onboarding_completed_at,clock_timestamp()) WHERE singleton
