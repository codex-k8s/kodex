insert into matter_codex_mattermost_bot_identities (
	project_id,
	role_id,
	username,
	display_name,
	mattermost_user_id,
	token_secret_ref,
	status,
	last_error
) values ($1, $2, $3, $4, $5, $6, $7, $8)
on conflict (role_id) do update set
	username = excluded.username,
	display_name = excluded.display_name,
	mattermost_user_id = case when excluded.mattermost_user_id <> '' then excluded.mattermost_user_id else matter_codex_mattermost_bot_identities.mattermost_user_id end,
	token_secret_ref = case when excluded.token_secret_ref <> '' then excluded.token_secret_ref else matter_codex_mattermost_bot_identities.token_secret_ref end,
	status = excluded.status,
	last_error = excluded.last_error,
	updated_at = now()
returning
	id,
	project_id,
	role_id,
	username,
	display_name,
	mattermost_user_id,
	token_secret_ref,
	status,
	last_error,
	created_at,
	updated_at,
	(xmax = 0) as created;
