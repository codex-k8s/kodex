-- name: schema__inspect :many
WITH target_tables(table_name) AS (
    VALUES
        ('runtime_event_schema_versions'::text),
        ('runtime_event_cursors'::text),
        ('runtime_inbox_events'::text),
        ('runtime_inbox_repairs'::text)
), target_indexes(index_name) AS (
    VALUES
        ('runtime_inbox_events_claim_idx'::text),
        ('runtime_inbox_events_lease_idx'::text),
        ('runtime_inbox_events_ordering_idx'::text),
        ('runtime_inbox_events_retention_idx'::text),
        ('runtime_inbox_events_dead_letter_idx'::text),
        ('runtime_inbox_repairs_event_idx'::text)
), objects AS (
    SELECT
        'marker'::text AS object_kind,
        component AS object_name,
        version::text || '|' || encode(schema_digest, 'hex') AS signature
    FROM runtime_event_schema_versions
    WHERE component = @schema_component

    UNION ALL

    SELECT
        'function',
        procedure.proname,
        format_type(procedure.prorettype, NULL) || '|' ||
        pg_catalog.oidvectortypes(procedure.proargtypes) || '|' ||
        procedure.provolatile::text || '|' ||
        procedure.proparallel::text || '|' ||
        CASE WHEN procedure.prosecdef THEN '1' ELSE '0' END || '|' ||
        CASE WHEN pg_catalog.pg_has_role(
            session_user,
            procedure.proowner,
            'MEMBER'
        )
            THEN '1' ELSE '0' END || '|' ||
        COALESCE(array_to_string(procedure.proconfig, ','), '-') || '|' ||
        regexp_replace(procedure.prosrc, '[[:space:]]+', '', 'g')
    FROM pg_catalog.pg_proc AS procedure
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = procedure.pronamespace
    WHERE namespace.nspname = @schema_name
      AND procedure.proname = 'runtime_event_ordering_key'
      AND pg_catalog.oidvectortypes(procedure.proargtypes) =
          'text, text, text, text'

    UNION ALL

    SELECT
        'table',
        relation.relname,
        relation.relkind::text || '|' || relation.relpersistence::text || '|' ||
        CASE WHEN pg_catalog.pg_has_role(
            session_user,
            relation.relowner,
            'MEMBER'
        )
            THEN '1' ELSE '0' END || '|' ||
        CASE WHEN relation.relrowsecurity THEN '1' ELSE '0' END || '|' ||
        CASE WHEN relation.relforcerowsecurity THEN '1' ELSE '0' END || '|' ||
        CASE WHEN relation.relispartition THEN '1' ELSE '0' END || '|' ||
        CASE WHEN relation.relhasrules THEN '1' ELSE '0' END || '|' ||
        CASE WHEN relation.relhastriggers THEN '1' ELSE '0' END || '|' ||
        CASE WHEN relation.relhassubclass THEN '1' ELSE '0' END || '|' ||
        CASE WHEN EXISTS (
            SELECT 1
            FROM pg_catalog.pg_inherits AS inheritance
            WHERE inheritance.inhrelid = relation.oid
        ) THEN '1' ELSE '0' END || '|' ||
        relation.relreplident::text || '|' || access_method.amname
    FROM pg_catalog.pg_class AS relation
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = relation.relnamespace
    JOIN pg_catalog.pg_am AS access_method
      ON access_method.oid = relation.relam
    JOIN target_tables AS target ON target.table_name = relation.relname
    WHERE namespace.nspname = @schema_name

    UNION ALL

    SELECT
        'column',
        relation.relname || '.' || attribute.attname,
        format_type(attribute.atttypid, attribute.atttypmod) || '|' ||
        CASE WHEN attribute.attnotnull THEN '1' ELSE '0' END || '|' ||
        COALESCE(NULLIF(attribute.attidentity, ''), '-') || '|' ||
        COALESCE(NULLIF(attribute.attgenerated, ''), '-') || '|' ||
        COALESCE(
            regexp_replace(
                pg_catalog.pg_get_expr(definition.adbin, definition.adrelid, true),
                '[[:space:]()]',
                '',
                'g'
            ),
            '-'
        )
    FROM pg_catalog.pg_attribute AS attribute
    JOIN pg_catalog.pg_class AS relation ON relation.oid = attribute.attrelid
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = relation.relnamespace
    JOIN target_tables AS target ON target.table_name = relation.relname
    LEFT JOIN pg_catalog.pg_attrdef AS definition
      ON definition.adrelid = attribute.attrelid
     AND definition.adnum = attribute.attnum
    WHERE namespace.nspname = @schema_name
      AND attribute.attnum > 0
      AND NOT attribute.attisdropped

    UNION ALL

    SELECT
        'constraint',
        relation.relname || '.' || constraint_record.conname,
        constraint_record.contype::text || '|' ||
        CASE WHEN constraint_record.convalidated THEN '1' ELSE '0' END || '|' ||
        CASE WHEN constraint_record.condeferrable THEN '1' ELSE '0' END || '|' ||
        CASE WHEN constraint_record.condeferred THEN '1' ELSE '0' END || '|' ||
        CASE
            WHEN constraint_record.conindid = 0 THEN '-'
            WHEN index_record.indisvalid AND index_record.indisready
                AND index_record.indislive THEN '1'
            ELSE '0'
        END || '|' ||
        regexp_replace(
            pg_catalog.pg_get_constraintdef(constraint_record.oid, true),
            '[[:space:]()]',
            '',
            'g'
        )
    FROM pg_catalog.pg_constraint AS constraint_record
    JOIN pg_catalog.pg_class AS relation
      ON relation.oid = constraint_record.conrelid
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = relation.relnamespace
    JOIN target_tables AS target ON target.table_name = relation.relname
    LEFT JOIN pg_catalog.pg_index AS index_record
      ON index_record.indexrelid = constraint_record.conindid
    WHERE namespace.nspname = @schema_name

    UNION ALL

    SELECT
        'index',
        index_relation.relname,
        CASE WHEN index_record.indisunique THEN '1' ELSE '0' END || '|' ||
        CASE WHEN index_record.indisprimary THEN '1' ELSE '0' END || '|' ||
        CASE WHEN index_record.indisexclusion THEN '1' ELSE '0' END || '|' ||
        CASE WHEN index_record.indimmediate THEN '1' ELSE '0' END || '|' ||
        CASE WHEN index_record.indisvalid THEN '1' ELSE '0' END || '|' ||
        CASE WHEN index_record.indisready THEN '1' ELSE '0' END || '|' ||
        CASE WHEN index_record.indislive THEN '1' ELSE '0' END || '|' ||
        CASE WHEN index_record.indisreplident THEN '1' ELSE '0' END || '|' ||
        CASE WHEN index_record.indnullsnotdistinct THEN '1' ELSE '0' END || '|' ||
        index_record.indnatts::text || '|' ||
        index_record.indnkeyatts::text || '|' ||
        access_method.amname || '|' ||
        (
            SELECT string_agg(
                pg_catalog.pg_get_indexdef(
                    index_record.indexrelid,
                    position,
                    true
                ),
                ',' ORDER BY position
            )
            FROM generate_series(1, index_record.indnkeyatts) AS position
        ) || '|' ||
        COALESCE(
            regexp_replace(
                pg_catalog.pg_get_expr(
                    index_record.indpred,
                    index_record.indrelid,
                    true
                ),
                '[[:space:]()]',
                '',
                'g'
            ),
            '-'
        ) || '|' ||
        regexp_replace(
            replace(
                pg_catalog.pg_get_indexdef(index_record.indexrelid),
                format('%I.', @schema_name::text),
                ''
            ),
            '[[:space:]()]',
            '',
            'g'
        )
    FROM pg_catalog.pg_index AS index_record
    JOIN pg_catalog.pg_class AS index_relation
      ON index_relation.oid = index_record.indexrelid
    JOIN pg_catalog.pg_class AS table_relation
      ON table_relation.oid = index_record.indrelid
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = table_relation.relnamespace
    JOIN pg_catalog.pg_am AS access_method
      ON access_method.oid = index_relation.relam
    JOIN target_indexes AS target ON target.index_name = index_relation.relname
    WHERE namespace.nspname = @schema_name

    UNION ALL

    SELECT
        'privilege',
        relation.relname,
        CASE WHEN
            pg_catalog.has_table_privilege(
                session_user::text,
                relation.oid,
                'SELECT'
            )
            AND pg_catalog.has_table_privilege(
                session_user::text,
                relation.oid,
                'INSERT'
            ) = (relation.relname <> 'runtime_event_schema_versions')
            AND pg_catalog.has_table_privilege(
                session_user::text,
                relation.oid,
                'UPDATE'
            ) = (
                relation.relname IN (
                    'runtime_event_cursors',
                    'runtime_inbox_events'
                )
            )
            AND pg_catalog.has_table_privilege(
                session_user::text,
                relation.oid,
                'DELETE'
            ) = (relation.relname = 'runtime_inbox_events')
            AND NOT pg_catalog.has_table_privilege(
                session_user::text,
                relation.oid,
                'TRUNCATE'
            )
            AND NOT pg_catalog.has_table_privilege(
                session_user::text,
                relation.oid,
                'REFERENCES'
            )
            AND NOT pg_catalog.has_table_privilege(
                session_user::text,
                relation.oid,
                'TRIGGER'
            )
            AND NOT pg_catalog.has_table_privilege(
                session_user::text,
                relation.oid,
                'MAINTAIN'
            )
        THEN '1' ELSE '0' END
    FROM pg_catalog.pg_class AS relation
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = relation.relnamespace
    JOIN target_tables AS target ON target.table_name = relation.relname
    WHERE namespace.nspname = @schema_name

    UNION ALL

    SELECT
        'privilege',
        'runtime_event_ordering_key',
        CASE WHEN pg_catalog.has_function_privilege(
            session_user::text,
            procedure.oid,
            'EXECUTE'
        ) THEN '1' ELSE '0' END
    FROM pg_catalog.pg_proc AS procedure
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = procedure.pronamespace
    WHERE namespace.nspname = @schema_name
      AND procedure.proname = 'runtime_event_ordering_key'
      AND pg_catalog.oidvectortypes(procedure.proargtypes) =
          'text, text, text, text'

    UNION ALL

    SELECT
        'privilege',
        'schema',
        CASE WHEN
            pg_catalog.has_schema_privilege(
                session_user::text,
                namespace.oid,
                'USAGE'
            )
            AND NOT pg_catalog.has_schema_privilege(
                session_user::text,
                namespace.oid,
                'CREATE'
            )
            AND NOT pg_catalog.pg_has_role(
                session_user,
                namespace.nspowner,
                'MEMBER'
            )
        THEN '1' ELSE '0' END
    FROM pg_catalog.pg_namespace AS namespace
    WHERE namespace.nspname = @schema_name
)
SELECT object_kind, object_name, signature
FROM objects
ORDER BY object_kind, object_name;
