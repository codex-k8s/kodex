with inserted as (
	insert into matter_codex_runtime_revisions (
		digest, manifest, account_alias, authorization_revision
	) values ($1, $2::jsonb, $3, $4)
	on conflict (digest) do nothing
	returning id, digest, manifest::text, account_alias, authorization_revision, created_at
), existing as (
	select id, digest, manifest::text, account_alias, authorization_revision, created_at
	from matter_codex_runtime_revisions
	where digest = $1
		and manifest = $2::jsonb
		and account_alias = $3
		and authorization_revision = $4
)
select * from inserted
union all
select * from existing
limit 1;
