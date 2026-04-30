-- name: user_identity__create :exec
INSERT INTO access_user_identities (
    id, user_id, provider, subject, email_at_login, last_login_at
) VALUES (
    @id, @user_id, @provider, @subject, @email_at_login, @last_login_at
);
