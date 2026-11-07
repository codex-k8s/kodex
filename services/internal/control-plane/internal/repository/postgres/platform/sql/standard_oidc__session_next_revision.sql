-- name: standard_oidc__session_next_revision :one
SELECT nextval('control_plane.oidc_identity_session_revisions');
