import { fingerprint } from './scoped-release.mjs';

const ensure=(ok,code)=>{if(!ok)throw Error(code);};
// Только локальный PostgreSQL-контейнер: ни PGHOST/PGSERVICE, ни .psqlrc,
// ни окружение вызывающего не могут выбрать другой backend или SQL startup hook.
export const migrationOwnerDatabaseArgs=Object.freeze(['-n','kodex-system','exec','-i','kodex-postgresql-0','-c','postgresql','--',
  'env','-i','PATH=/usr/local/bin:/usr/bin:/bin','psql','-XqAt','--host=/var/run/postgresql','--port=5432',
  '--username=postgres','--dbname=control_plane','--no-password','-v','ON_ERROR_STOP=1']);
const identitySQL="SELECT pg_catalog.json_build_object('systemIdentifier',system_identifier::pg_catalog.text,'databaseOID',oid::pg_catalog.text,'database',pg_catalog.current_database(),'port',pg_catalog.current_setting('port'),'serverAddress',pg_catalog.inet_server_addr(),'serverPort',pg_catalog.inet_server_port()) FROM pg_catalog.pg_control_system(),pg_catalog.pg_database WHERE datname=pg_catalog.current_database();\n";

// Идентификаторы PostgreSQL остаются строками, включая 64-bit system identifier.
export function validateMigrationDatabaseIdentity(value) {
  ensure(value && Object.keys(value).sort().join(',')==='database,databaseOID,port,serverAddress,serverPort,systemIdentifier' &&
    typeof value.systemIdentifier==='string' && /^[1-9][0-9]{0,19}$/.test(value.systemIdentifier) &&
    typeof value.databaseOID==='string' && /^[1-9][0-9]{0,9}$/.test(value.databaseOID) &&
    value.database==='control_plane' && value.port==='5432' && value.serverAddress===null && value.serverPort===null,
  'MIGRATION_DATABASE_IDENTITY_INVALID');
  return value;
}

// Caller передаёт только уже проверенный по source hash SQL закрытого профиля.
// Portable plan закрепляет backend; каждый запрос проверяет его и Pod до и после.
export function migrationOwnerDatabase(kube,postgresIdentity) {
  const execute=sql=>JSON.parse(kube([...migrationOwnerDatabaseArgs],sql));
  const pod=expected=>ensure(fingerprint(postgresIdentity())===fingerprint(expected),'MIGRATION_POSTGRES_IDENTITY_CHANGED');
  const identity=expectedPod=>{
    pod(expectedPod);
    const result=validateMigrationDatabaseIdentity(execute(identitySQL));
    pod(expectedPod);
    return result;
  };
  return {
    identity,
    read(sql,expectedPod,expectedBackend) {
      pod(expectedPod);
      if(expectedBackend) {
        validateMigrationDatabaseIdentity(expectedBackend);
        ensure(fingerprint(identity(expectedPod))===fingerprint(expectedBackend),'MIGRATION_DATABASE_INSTANCE_CHANGED');
      }
      const result=execute(sql);
      if(expectedBackend) ensure(fingerprint(identity(expectedPod))===fingerprint(expectedBackend),'MIGRATION_DATABASE_INSTANCE_CHANGED');
      pod(expectedPod);
      return result;
    },
  };
}
