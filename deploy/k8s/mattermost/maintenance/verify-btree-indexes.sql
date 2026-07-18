create extension if not exists amcheck;

do $$
declare
	index_record record;
	failures text[] := '{}';
begin
	for index_record in
		select indexes.oid::regclass as index_name
		from pg_class indexes
		join pg_namespace namespaces on namespaces.oid = indexes.relnamespace
		join pg_am access_methods on access_methods.oid = indexes.relam
		where indexes.relkind = 'i'
			and access_methods.amname = 'btree'
			and namespaces.nspname not in ('pg_catalog', 'information_schema')
		order by indexes.oid
	loop
		begin
			perform bt_index_check(index_record.index_name, true);
		exception when others then
			failures := array_append(
				failures,
				format('%s [%s] %s', index_record.index_name, sqlstate, sqlerrm)
			);
		end;
	end loop;

	if cardinality(failures) > 0 then
		raise exception using
			message = 'B-tree integrity verification failed',
			detail = array_to_string(failures, E'\n'),
			hint = 'Restore indexes before starting Mattermost writers; see docs/runbooks/postgres-image-change.md';
	end if;
end $$;
