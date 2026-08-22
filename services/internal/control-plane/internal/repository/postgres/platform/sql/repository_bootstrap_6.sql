-- name: platform__repository_bootstrap_6 :exec
INSERT INTO control_plane.runtime_profiles
		(stable_key,name,provider,model,runtime_revision,resource_limits)
		VALUES ($1,'Базовая среда',$2,$3,'runtime-v1',$4)
