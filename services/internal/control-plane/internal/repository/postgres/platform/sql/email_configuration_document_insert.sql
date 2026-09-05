-- name: email_configuration_document_insert :exec
INSERT INTO control_plane.email_configuration_documents(revision,digest,document)
VALUES($1,$2,$3::jsonb) ON CONFLICT(revision) DO NOTHING;
