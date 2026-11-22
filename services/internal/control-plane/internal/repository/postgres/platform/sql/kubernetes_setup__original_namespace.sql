-- name: kubernetes_setup__original_namespace :one
SELECT identity_namespace FROM control_plane.kubernetes_operator_registry_revisions WHERE revision=@registry_revision;
