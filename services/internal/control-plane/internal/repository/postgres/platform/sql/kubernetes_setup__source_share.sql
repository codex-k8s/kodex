-- name: kubernetes_setup__source_share :one
SELECT source.revision,source.installation_ref,source.identity_namespace,source.content_sha256,source.payload
FROM control_plane.kubernetes_operator_current current
JOIN control_plane.kubernetes_operator_registry_revisions source ON source.revision=current.revision
WHERE current.singleton FOR SHARE OF current;
