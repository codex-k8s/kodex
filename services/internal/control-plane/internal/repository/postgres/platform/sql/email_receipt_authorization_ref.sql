-- name: email_receipt_authorization_ref :one
SELECT authorization_ref FROM control_plane.email_effect_receipts WHERE id=$1::uuid;
