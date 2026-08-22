-- name: platform__queries_getbootstrapstate_1 :one
SELECT i.bootstrapped_at,i.onboarding_completed_at,
		(SELECT count(*) FROM control_plane.projects p WHERE p.organization_id=$1::uuid AND p.lifecycle='ACTIVE')
		FROM control_plane.installation i WHERE singleton
