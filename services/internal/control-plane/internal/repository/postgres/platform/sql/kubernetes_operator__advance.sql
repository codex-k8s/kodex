-- name: kubernetes_operator__advance :exec
INSERT INTO control_plane.kubernetes_operator_current(singleton,revision) VALUES(true,@revision) ON CONFLICT(singleton) DO UPDATE SET revision=EXCLUDED.revision;
