-- name: platform__repository_resolveprincipal_3 :one
INSERT INTO control_plane.subjects
			(ref,organization_id,issuer,external_subject_digest,display_name)
			VALUES ($1,$2::uuid,'verified-internal-authority',$3,'Владелец') RETURNING id::text
