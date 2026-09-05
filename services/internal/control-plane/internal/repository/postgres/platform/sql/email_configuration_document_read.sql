-- name: email_configuration_document_read :one
SELECT d.document,d.digest FROM control_plane.email_configuration_watermark w
JOIN control_plane.email_configuration_documents d ON d.revision=w.revision AND d.digest=w.digest
WHERE w.singleton;
