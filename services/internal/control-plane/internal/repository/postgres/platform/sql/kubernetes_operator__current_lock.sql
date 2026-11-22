-- name: kubernetes_operator__current_lock :one
SELECT source.revision,source.installation_ref,source.identity_namespace,source.content_sha256,source.payload FROM control_plane.kubernetes_operator_current active_source JOIN control_plane.kubernetes_operator_registry_revisions source ON source.revision=active_source.revision WHERE active_source.singleton FOR UPDATE OF active_source;
