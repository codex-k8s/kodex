-- name: schema__inspect :many
WITH target_tables(table_name, expected_privileges) AS (
    VALUES
        ('runtime_event_schema_versions'::text, ARRAY['SELECT']::text[]),
        ('runtime_event_cursors'::text, ARRAY['INSERT', 'SELECT', 'UPDATE']::text[]),
        ('runtime_inbox_events'::text, ARRAY['DELETE', 'INSERT', 'SELECT', 'UPDATE']::text[]),
        ('runtime_inbox_repairs'::text, ARRAY['INSERT', 'SELECT']::text[])
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
        CASE WHEN target.index_name IS NULL
            THEN 'extension_index' ELSE 'index' END,
        index_relation.relname,
        CASE WHEN target.index_name IS NULL THEN
            CASE WHEN
                index_relation.relname LIKE 'postgresinbox_ext_%'
                AND NOT index_record.indisunique
                AND NOT index_record.indisprimary
                AND NOT index_record.indisexclusion
                AND index_record.indimmediate
                AND index_record.indisvalid
                AND index_record.indisready
                AND index_record.indislive
                AND NOT index_record.indisreplident
                AND NOT index_record.indnullsnotdistinct
                AND index_record.indnatts = index_record.indnkeyatts
                AND access_method.amname = 'btree'
                AND index_record.indpred IS NULL
                AND index_record.indexprs IS NULL
                AND NOT EXISTS (
                    SELECT 1
                    FROM generate_series(
                        0,
                        index_record.indnkeyatts - 1
                    ) AS position
                    JOIN pg_catalog.pg_attribute AS indexed_attribute
                      ON indexed_attribute.attrelid = index_record.indrelid
                     AND indexed_attribute.attnum = index_record.indkey[position]
                    JOIN pg_catalog.pg_opclass AS operator_class
                      ON operator_class.oid = index_record.indclass[position]
                    WHERE index_record.indkey[position] <= 0
                       OR index_record.indoption[position] <> 0
                       OR NOT operator_class.opcdefault
                       OR operator_class.opcmethod <> access_method.oid
                       OR NOT indexed_attribute.atttypid = operator_class.opcintype
                       OR NOT index_record.indcollation[position] IN (
                           0,
                           indexed_attribute.attcollation
                       )
                )
            THEN '1' ELSE '0' END
        ELSE
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
        ) END
    FROM pg_catalog.pg_index AS index_record
    JOIN pg_catalog.pg_class AS index_relation
      ON index_relation.oid = index_record.indexrelid
    JOIN pg_catalog.pg_class AS table_relation
      ON table_relation.oid = index_record.indrelid
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = table_relation.relnamespace
    JOIN pg_catalog.pg_am AS access_method
      ON access_method.oid = index_relation.relam
    LEFT JOIN target_indexes AS target ON target.index_name = index_relation.relname
    WHERE namespace.nspname = @schema_name
      AND EXISTS (
          SELECT 1 FROM target_tables AS table_target
          WHERE table_target.table_name = table_relation.relname
      )
      AND NOT EXISTS (
          SELECT 1
          FROM pg_catalog.pg_constraint AS index_constraint
          WHERE index_constraint.conindid = index_record.indexrelid
      )

    UNION ALL

    SELECT
        'privilege',
        relation.relname,
        CASE WHEN
            ARRAY(
                SELECT privilege_name
                FROM unnest(ARRAY[
                    'DELETE', 'INSERT', 'MAINTAIN', 'REFERENCES',
                    'SELECT', 'TRIGGER', 'TRUNCATE', 'UPDATE'
                ]::text[]) AS privilege_name
                WHERE pg_catalog.has_table_privilege(
                    session_user::text,
                    relation.oid,
                    privilege_name
                )
                ORDER BY privilege_name
            ) = target.expected_privileges
            AND NOT EXISTS (
                SELECT 1
                FROM pg_catalog.aclexplode(COALESCE(
                    relation.relacl,
                    pg_catalog.acldefault('r', relation.relowner)
                )) AS acl
                WHERE acl.grantee <> relation.relowner
                  AND NOT (
                      acl.grantee = (
                          SELECT oid FROM pg_catalog.pg_roles
                          WHERE rolname = session_user
                      )
                      AND acl.grantor = relation.relowner
                      AND acl.privilege_type = ANY(target.expected_privileges)
                      AND NOT acl.is_grantable
                  )
            )
            AND NOT EXISTS (
                SELECT 1
                FROM unnest(target.expected_privileges) AS expected(privilege)
                WHERE NOT EXISTS (
                    SELECT 1
                    FROM pg_catalog.aclexplode(COALESCE(
                        relation.relacl,
                        pg_catalog.acldefault('r', relation.relowner)
                    )) AS acl
                    WHERE acl.grantee = (
                        SELECT oid FROM pg_catalog.pg_roles
                        WHERE rolname = session_user
                    )
                      AND acl.grantor = relation.relowner
                      AND acl.privilege_type = expected.privilege
                      AND NOT acl.is_grantable
                )
            )
            AND NOT EXISTS (
                SELECT 1
                FROM pg_catalog.pg_attribute AS column_record
                WHERE column_record.attrelid = relation.oid
                  AND column_record.attnum > 0
                  AND NOT column_record.attisdropped
                  AND column_record.attacl IS NOT NULL
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
        CASE WHEN
            pg_catalog.has_function_privilege(
                session_user::text,
                procedure.oid,
                'EXECUTE'
            )
            AND NOT pg_catalog.has_function_privilege(
                session_user::text,
                procedure.oid,
                'EXECUTE WITH GRANT OPTION'
            )
            AND NOT EXISTS (
                SELECT 1
                FROM pg_catalog.aclexplode(COALESCE(
                    procedure.proacl,
                    pg_catalog.acldefault('f', procedure.proowner)
                )) AS acl
                WHERE acl.grantee <> procedure.proowner
                  AND NOT (
                      acl.grantee = (
                          SELECT oid FROM pg_catalog.pg_roles
                          WHERE rolname = session_user
                      )
                      AND acl.grantor = procedure.proowner
                      AND acl.privilege_type = 'EXECUTE'
                      AND NOT acl.is_grantable
                  )
            )
        THEN '1' ELSE '0' END
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
            AND NOT pg_catalog.has_schema_privilege(
                session_user::text,
                namespace.oid,
                'USAGE WITH GRANT OPTION'
            )
            AND NOT EXISTS (
                SELECT 1
                FROM pg_catalog.aclexplode(COALESCE(
                    namespace.nspacl,
                    pg_catalog.acldefault('n', namespace.nspowner)
                )) AS acl
                WHERE acl.grantee <> namespace.nspowner
                  AND NOT (
                      acl.grantee = (
                          SELECT oid FROM pg_catalog.pg_roles
                          WHERE rolname = session_user
                      )
                      AND acl.grantor = namespace.nspowner
                      AND acl.privilege_type = 'USAGE'
                      AND NOT acl.is_grantable
                  )
            )
        THEN '1' ELSE '0' END
    FROM pg_catalog.pg_namespace AS namespace
    WHERE namespace.nspname = @schema_name

    UNION ALL

    SELECT
        'privilege',
        'principal',
        CASE WHEN
            role_record.rolcanlogin
            AND NOT role_record.rolsuper
            AND NOT role_record.rolcreatedb
            AND NOT role_record.rolcreaterole
            AND NOT role_record.rolreplication
            AND NOT role_record.rolbypassrls
            AND current_user = session_user
            AND NOT EXISTS (
                SELECT 1
                FROM pg_catalog.pg_auth_members AS membership
                WHERE membership.member = role_record.oid
                   OR membership.roleid = role_record.oid
            )
        THEN '2' ELSE '0' END
    FROM pg_catalog.pg_roles AS role_record
    WHERE role_record.rolname = session_user

    UNION ALL

    SELECT
        'privilege',
        'sequences',
        CASE WHEN
        NOT EXISTS (
            SELECT 1
            FROM pg_catalog.pg_class AS owned_sequence
            JOIN pg_catalog.pg_namespace AS owned_namespace
              ON owned_namespace.oid = owned_sequence.relnamespace
            WHERE owned_namespace.nspname = @schema_name
              AND owned_sequence.relkind = 'S'
              AND pg_catalog.pg_has_role(
                  session_user,
                  owned_sequence.relowner,
                  'MEMBER'
              )
        )
        AND NOT EXISTS (
            SELECT 1
            FROM pg_catalog.pg_class AS sequence_record
            JOIN pg_catalog.pg_namespace AS sequence_namespace
              ON sequence_namespace.oid = sequence_record.relnamespace
            CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(
                sequence_record.relacl,
                pg_catalog.acldefault('s', sequence_record.relowner)
            )) AS acl
            WHERE sequence_namespace.nspname = @schema_name
              AND sequence_record.relkind = 'S'
              AND acl.grantee <> sequence_record.relowner
              AND (
                  acl.grantee <> (
                      SELECT oid FROM pg_catalog.pg_roles
                      WHERE rolname = session_user
                  )
                  OR acl.grantor <> sequence_record.relowner
                  OR acl.is_grantable
                  OR acl.privilege_type NOT IN ('SELECT', 'USAGE')
              )
        ) THEN '1' ELSE '0' END
)
SELECT object_kind, object_name, signature
FROM objects
ORDER BY object_kind, object_name;
