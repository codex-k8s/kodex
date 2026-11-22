import test from 'node:test';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {readFileSync} from 'node:fs';
import {migrationOwnerDatabaseArgs,authorityOwnerDatabaseArgs} from './migration-owner-database.mjs';
import {pairedTokenOwnerQueries,pairedTokenOwnerDatabase,readPairedAuthorityOwner,requirePairedAuthorityReader} from './paired-token-owner-database.mjs';

const postgres={pod:'kodex-postgresql-0',uid:'10000000-0000-4000-8000-000000000071',ownerUID:'10000000-0000-4000-8000-000000000070'};
const backend=database=>({systemIdentifier:'18446744073709551615',databaseOID:database==='control_plane'?'16384':'20000',database,port:'5432',serverAddress:null,serverPort:null});
const epoch=()=>({profile:'PAIRED_TOKEN_DATABASE_EPOCH',readOnly:'on',isolation:'repeatable read',database:'control_plane',systemIdentifier:'18446744073709551615',port:'5432',serverAddress:null,serverPort:null,
 databases:{control_plane:'16384',internal_rpc_authority:'20000'}});
const packet=authority=>({profile:authority?'PAIRED_TOKEN_AUTHORITY110':'PAIRED_TOKEN_CP051',readOnly:'on',isolation:'repeatable read',backend:backend(authority?'internal_rpc_authority':'control_plane'),
 ...(authority?{head:{database:'internal_rpc_authority',readOnly:'on',gooseVersion:20261115000200},state:{status:'RETIRED'}}:{proof:{schemaMatches:true}})});

test('paired fixed SQL materializes only the canonical CP catalog and emits one bounded RR result per database',()=>{
 const queries=pairedTokenOwnerQueries(),canonical=readFileSync(new URL('./workflow-file-provenance-backup-migration.sql',import.meta.url),'utf8');
 const body=canonical.slice(canonical.indexOf('WITH qualified_catalog'),-('\nROLLBACK;\n'.length)).trimEnd().slice(0,-1);
 const original='\n, complete_catalog AS (',materialized='\n, complete_catalog AS MATERIALIZED (';
 assert.equal(body.split(original).length,2);
 assert.equal(queries.controlPlane.sql.split(materialized).length,2);
 assert.ok(queries.controlPlane.sql.replace(materialized,original).includes(body));
 for(const q of Object.values(queries)){
  assert.equal(q.sha256,createHash('sha256').update(q.sql).digest('hex'));
  assert.match(q.sql,/BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;/);
  assert.match(q.sql,/ROLLBACK;\n$/);assert.match(q.sql,/pg_catalog\.pg_control_system\(\)/);
  assert.ok(!q.sql.includes('/*__PAIRED_'));
 }
 assert.match(queries.controlPlane.sql,/statement_timeout='15s'/);
 assert.match(queries.authority.sql,/statement_timeout='5s'/);
 assert.match(queries.authority.sql,/authority_rotation_status\(\)/);
});

test('paired transport uses two proof sessions plus one final database epoch without caller SQL',()=>{
 const calls=[],queries=pairedTokenOwnerQueries();
 const database=pairedTokenOwnerDatabase((args,sql)=>{calls.push({args,sql});return JSON.stringify(String(sql)===queries.epoch.sql?epoch():packet(args.includes('--dbname=internal_rpc_authority')));},postgres);
 const cp=database.readControlPlane(),authority=readPairedAuthorityOwner(database.readAuthority);
 assert.deepEqual(cp.backend,backend('control_plane'));assert.deepEqual(authority.backend,backend('internal_rpc_authority'));
 database.verifyEpoch(cp.backend,authority.backend,()=>postgres);
 assert.equal(calls.length,3);assert.deepEqual(calls.map(c=>c.args),[migrationOwnerDatabaseArgs,authorityOwnerDatabaseArgs,migrationOwnerDatabaseArgs]);
 assert.deepEqual(calls.map(c=>String(c.sql)),[queries.controlPlane.sql,queries.authority.sql,queries.epoch.sql]);
 assert.equal(cp.sqlSHA256,queries.controlPlane.sha256);assert.equal(authority.sqlSHA256,queries.authority.sha256);
 assert.deepEqual(authority.postgres,postgres);
 assert.throws(()=>readPairedAuthorityOwner(()=>authority),/PAIRED_OWNER_READER_REQUIRED/);
 assert.throws(()=>requirePairedAuthorityReader(JSON.parse(JSON.stringify(authority))),/PAIRED_OWNER_READER_REQUIRED/);
});

test('paired backend/read-only/isolation/head and exact envelopes close malformed or foreign responses',()=>{
 for(const authority of [false,true])for(const change of [
  p=>p.backend.systemIdentifier=18446744073709551615,p=>p.backend.databaseOID=99,p=>p.backend.database='foreign',
  p=>p.backend.serverAddress='127.0.0.1',p=>p.backend.serverPort=5432,p=>p.readOnly='off',p=>p.isolation='read committed',
  p=>p.profile='foreign',p=>p.extra=true,p=>delete p.backend,
  ...(authority?[p=>p.head.gooseVersion=20261115000100,p=>p.head.readOnly='off',p=>p.state=null]:[p=>p.proof=null]),
 ]){
  const value=packet(authority);change(value);
  const d=pairedTokenOwnerDatabase(()=>JSON.stringify(value),postgres);
  assert.throws(()=>authority?readPairedAuthorityOwner(d.readAuthority):d.readControlPlane());
 }
 for(const text of ['null','[]','{}','{"profile":"first","profile":"second"}']){
  const d=pairedTokenOwnerDatabase(()=>text,postgres);assert.throws(()=>d.readControlPlane());
 }
});


test('final database epoch closes each same-Pod database replacement, foreign server, malformed fields and scope drift',()=>{
 for(const change of [
  e=>e.databases.control_plane='99',e=>e.databases.internal_rpc_authority='99',e=>e.systemIdentifier='12345678901234567890',
  e=>delete e.databases.control_plane,e=>e.databases.internal_rpc_authority=20000,e=>e.databases.extra='99',
  e=>e.readOnly='off',e=>e.isolation='read committed',e=>e.serverAddress='127.0.0.1',
 ]){
  const value=epoch();change(value);
  const d=pairedTokenOwnerDatabase(()=>JSON.stringify(value),postgres);
  assert.throws(()=>d.verifyEpoch(backend('control_plane'),backend('internal_rpc_authority'),()=>postgres));
 }
 for(const when of ['before','after']){
  let snapshots=0,queries=0;
  const d=pairedTokenOwnerDatabase(()=>{queries++;return JSON.stringify(epoch());},postgres);
  assert.throws(()=>d.verifyEpoch(backend('control_plane'),backend('internal_rpc_authority'),()=>{
   snapshots++;return {...postgres,...((when==='before'||snapshots===2)?{uid:'10000000-0000-4000-8000-000000000099'}:{})};
  }),/POSTGRES_CHANGED/);
  assert.equal(queries,when==='before'?0:1);
 }
});
