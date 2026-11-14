import test from 'node:test';
import assert from 'node:assert/strict';
import { migrationOwnerDatabase, migrationOwnerDatabaseArgs, validateMigrationDatabaseIdentity } from './migration-owner-database.mjs';

const pod={pod:'kodex-postgresql-0',uid:'pod-1',ownerUID:'sts-1'};
const backend={systemIdentifier:'18446744073709551615',databaseOID:'16384',database:'control_plane',port:'5432',serverAddress:null,serverPort:null};

test('owner SQL использует только exact container/local socket и очищенное окружение',()=>{
  const calls=[];
  const reader=migrationOwnerDatabase((args,sql)=>{
    calls.push({args,sql});return JSON.stringify(sql.includes('pg_catalog.pg_control_system()')?backend:{active:0});
  },()=>pod);
  assert.deepEqual(reader.identity(pod),backend);
  assert.deepEqual(reader.read('BEGIN READ ONLY; SELECT 0; ROLLBACK;',pod,backend),{active:0});
  assert.equal(calls.length,4);
  for(const {args} of calls) {
    assert.deepEqual(args,migrationOwnerDatabaseArgs);
    assert.deepEqual(args.slice(args.indexOf('--')+1,args.indexOf('psql')),['env','-i','PATH=/usr/local/bin:/usr/bin:/bin']);
    assert.equal(args[args.indexOf('-c')+1],'postgresql');
    assert.ok(args.includes('-XqAt')&&args.includes('--host=/var/run/postgresql')&&args.includes('--port=5432')&&args.includes('--no-password'));
  }
  for(const patch of [{systemIdentifier:18446744073709551615},{databaseOID:16384},{database:'foreign'},{port:'5433'},
    {serverAddress:'127.0.0.1'},{serverPort:5432},{extra:true}])
    assert.throws(()=>validateMigrationDatabaseIdentity({...backend,...patch}),/MIGRATION_DATABASE_IDENTITY_INVALID/);
});

test('owner SQL не принимает прежний backend pin либо смену backend/Pod после запроса',()=>{
  for(const when of ['before','after','pod-after']) {
    let identityCalls=0,queried=false;
    const reader=migrationOwnerDatabase((_args,sql)=>{
      if(sql.includes('pg_catalog.pg_control_system()')) {
        identityCalls++;return JSON.stringify({...backend,...((when==='before'||when==='after'&&identityCalls===2)?{databaseOID:'99'}:{})});
      }
      queried=true;return '{"active":0}';
    },()=>({...pod,...(when==='pod-after'&&queried?{uid:'replacement'}:{})}));
    assert.throws(()=>reader.read('BEGIN READ ONLY; SELECT 0; ROLLBACK;',pod,backend),
      when==='pod-after'?/MIGRATION_POSTGRES_IDENTITY_CHANGED/:/MIGRATION_DATABASE_INSTANCE_CHANGED/);
    assert.equal(queried,when!=='before');
  }
});
