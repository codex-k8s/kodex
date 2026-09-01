-- name: repository_bootstrap_insert_subjects_ref_issuer_display_name :one
INSERT INTO control_plane.subjects
		(ref,organization_id,issuer,external_subject_digest,display_name,email_masked,kind)
		VALUES ('sys_platform',$1::uuid,'kodex-system',$2,'Kodex','','SERVICE') RETURNING id::text
