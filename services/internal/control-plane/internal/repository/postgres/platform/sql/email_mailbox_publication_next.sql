-- name: email_mailbox_publication_next :one
SELECT GREATEST(COALESCE((SELECT max(revision) FROM control_plane.email_mailbox_publications),0),
    COALESCE((SELECT revision FROM control_plane.email_configuration_watermark WHERE singleton),0))+1,
    EXISTS(SELECT 1 FROM control_plane.email_mailbox_publications WHERE state='PENDING');
