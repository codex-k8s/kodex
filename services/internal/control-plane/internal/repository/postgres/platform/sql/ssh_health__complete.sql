-- name: ssh_health__complete :exec
UPDATE control_plane.ssh_identity_health_probes SET outcome=@outcome,observed_subject_digest=NULLIF(@subject_digest,''),qualified_until=@qualified_until,terminal_state=@state,applied=@applied
WHERE id=@id::uuid AND outcome IS NULL;
