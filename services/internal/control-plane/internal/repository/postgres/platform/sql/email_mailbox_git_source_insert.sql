-- name: email_mailbox_git_source_insert :exec
INSERT INTO control_plane.email_mailbox_git_sources(source,mailbox_key,configuration_set_id) VALUES($1,$2,$3::uuid);
