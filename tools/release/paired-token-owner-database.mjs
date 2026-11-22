import {readFileSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {fingerprint} from './scoped-release.mjs';
import {parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {migrationOwnerDatabaseArgs,authorityOwnerDatabaseArgs,validateMigrationDatabaseIdentity,validateAuthorityDatabaseIdentity} from './migration-owner-database.mjs';

const ensure=(ok,code)=>{if(!ok)throw Error(code);};
const digest=value=>createHash('sha256').update(value).digest('hex');
const readers=new WeakMap();
const sources=Object.freeze({
 cp:['./workflow-file-provenance-backup-migration.sql','432c2173945ab7b2925fca6178634426cf0c81b1c88ce28388d8e7c9e1f6cba7'],
 head:['./paired-token-authority-head.sql','632b74e83edc77b5b9a2e0d4c8dd7c462474e05ead193070cbb11e2a56adbadf'],
 state:['./catalog-authority-baseline-readback.sql','af23479e604042a981e2f1428e1100a7140248eb0ae80035566b006da56cf97f'],
 cpEnvelope:['./paired-token-control-plane-owner.sql','5e6e98b58dcbc28957d0f5c8dd4a8b02a44af579190d0de4892d3ffe1466d32d'],
 authorityEnvelope:['./paired-token-authority-owner.sql','96b6b1641d53a52182d196f3557192336778efcd0738c53c2c1bb90290e7b19c'],
 epoch:['./paired-token-database-epoch.sql','fd20231fe983fe30b582cc6c671414a056fa1893a6558a494fe1cc1b565f8d9f'],
});
const object=value=>value!==null&&typeof value==='object'&&!Array.isArray(value);
const exact=(value,keys)=>object(value)&&Object.keys(value).sort().join(',')===[...keys].sort().join(',');
function source(key){const [path,expected]=sources[key],raw=readFileSync(new URL(path,import.meta.url));ensure(digest(raw)===expected,'PAIRED_OWNER_SQL_CHANGED');return raw.toString('utf8');}
function statement(raw,start){const end='\nROLLBACK;\n',index=raw.indexOf(start);ensure(index>=0&&raw.indexOf(start,index+1)===-1&&raw.endsWith(end),'PAIRED_OWNER_SQL_CHANGED');const sql=raw.slice(index,-end.length).trimEnd();ensure(sql.endsWith(';'),'PAIRED_OWNER_SQL_CHANGED');return sql.slice(0,-1);}
function insert(template,marker,value){ensure(template.split(marker).length===2,'PAIRED_OWNER_SQL_CHANGED');return template.replace(marker,()=>value);}

// Составление из проверенных по SHA запросов. PostgreSQL вычисляет полный
// каталог CP один раз в том же снимке для всех проекций и проверок.
export function pairedTokenOwnerQueries(){
 const cpStatement=insert(statement(source('cp'),'WITH qualified_catalog'),
  '\n, complete_catalog AS (','\n, complete_catalog AS MATERIALIZED (');
 const cp=insert(source('cpEnvelope'),'/*__PAIRED_CONTROL_PLANE_CANONICAL__*/',cpStatement);
 const authority=insert(insert(source('authorityEnvelope'),'/*__PAIRED_AUTHORITY_HEAD__*/',statement(source('head'),'SELECT pg_catalog.json_build_object(')),
  '/*__PAIRED_AUTHORITY_STATUS__*/',statement(source('state'),'SELECT internal_rpc_authority.authority_rotation_status()'));
 const epoch=source('epoch');
 return Object.freeze({controlPlane:Object.freeze({sql:cp,sha256:digest(cp)}),authority:Object.freeze({sql:authority,sha256:digest(authority)}),epoch:Object.freeze({sql:epoch,sha256:digest(epoch)})});
}

function decode(raw,authority){
 const text=String(raw);ensure(Buffer.byteLength(text)<=4<<20,'PAIRED_OWNER_RESPONSE_BOUND_EXCEEDED');
 const value=parseAuthorityPrivateJSON(text),keys=['profile','readOnly','isolation','backend',...(authority?['head','state']:['proof'])];
 ensure(exact(value,keys)&&value.profile===(authority?'PAIRED_TOKEN_AUTHORITY110':'PAIRED_TOKEN_CP051')&&value.readOnly==='on'&&value.isolation==='repeatable read','PAIRED_OWNER_RESPONSE_REJECTED');
 (authority?validateAuthorityDatabaseIdentity:validateMigrationDatabaseIdentity)(value.backend);
 if(authority){
  ensure(exact(value.head,['database','readOnly','gooseVersion'])&&value.head.database==='internal_rpc_authority'&&value.head.readOnly==='on'&&value.head.gooseVersion===20261115000200,
   'PAIRED_TOKEN_RELEASE_SCHEMA_REJECTED');
  ensure(object(value.state),'PAIRED_OWNER_RESPONSE_REJECTED');
 }else ensure(object(value.proof),'PAIRED_OWNER_RESPONSE_REJECTED');
 return value;
}

function decodeEpoch(raw){
 const text=String(raw);ensure(Buffer.byteLength(text)<=16384,'PAIRED_OWNER_RESPONSE_BOUND_EXCEEDED');
 const value=parseAuthorityPrivateJSON(text);
 ensure(exact(value,['profile','readOnly','isolation','systemIdentifier','database','port','serverAddress','serverPort','databases'])&&
  value.profile==='PAIRED_TOKEN_DATABASE_EPOCH'&&value.readOnly==='on'&&value.isolation==='repeatable read'&&value.database==='control_plane'&&
  exact(value.databases,['control_plane','internal_rpc_authority']),'PAIRED_OWNER_EPOCH_REJECTED');
 const identity=database=>({systemIdentifier:value.systemIdentifier,databaseOID:value.databases[database],database,port:value.port,serverAddress:value.serverAddress,serverPort:value.serverPort});
 validateMigrationDatabaseIdentity(identity('control_plane'));validateAuthorityDatabaseIdentity(identity('internal_rpc_authority'));
 ensure(value.databases.control_plane!==value.databases.internal_rpc_authority,'PAIRED_OWNER_EPOCH_REJECTED');
 return value;
}

// Единственное соединение каждого read связывает actual backend и owner state.
// PostgreSQL Pod/STS freshness остаётся у enclosing capture до и после SQL;
// этот raw adapter сам не создаёт release proof и не используется generic CAS.
export function pairedTokenOwnerDatabase(kube,postgres){
 ensure(typeof kube==='function'&&exact(postgres,['pod','uid','ownerUID'])&&postgres.pod==='kodex-postgresql-0'&&
  ['uid','ownerUID'].every(key=>typeof postgres[key]==='string'&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(postgres[key])),'PAIRED_OWNER_POSTGRES_REQUIRED');
 const pin=Object.freeze(structuredClone(postgres));
 const read=authority=>{
  const queries=pairedTokenOwnerQueries(),query=authority?queries.authority:queries.controlPlane;
  const value=decode(kube([...(authority?authorityOwnerDatabaseArgs:migrationOwnerDatabaseArgs)],Buffer.from(query.sql)),authority);
  return Object.freeze({...value,postgres:structuredClone(pin),sqlSHA256:query.sha256});
 };
 const readAuthority=()=>read(true);readers.set(readAuthority,{postgres:pin});
 // Заключительная глобальная pg_database выборка обнаруживает DROP/RESTORE
 // одной БД при прежних Pod/StatefulSet. Она не переиспользует ранний identity.
 const readEpoch=(controlPlane,authority)=>{
  validateMigrationDatabaseIdentity(controlPlane);validateAuthorityDatabaseIdentity(authority);
  const query=pairedTokenOwnerQueries().epoch,value=decodeEpoch(kube([...migrationOwnerDatabaseArgs],Buffer.from(query.sql)));
  ensure(value.systemIdentifier===controlPlane.systemIdentifier&&value.systemIdentifier===authority.systemIdentifier&&
   value.databases.control_plane===controlPlane.databaseOID&&value.databases.internal_rpc_authority===authority.databaseOID,
   'PAIRED_OWNER_DATABASE_EPOCH_CHANGED');
  return Object.freeze(value);
 };
 const verifyEpoch=(controlPlane,authority,postgresIdentity)=>{
  ensure(typeof postgresIdentity==='function'&&fingerprint(postgresIdentity())===fingerprint(pin),'PAIRED_OWNER_POSTGRES_CHANGED');
  const value=readEpoch(controlPlane,authority);
  ensure(fingerprint(postgresIdentity())===fingerprint(pin),'PAIRED_OWNER_POSTGRES_CHANGED');
  return value;
 };
 return Object.freeze({readControlPlane:()=>read(false),readAuthority,readEpoch,verifyEpoch});
}

// Новый baseline port принимает только функцию fixed database adapter, не JSON
// из файла/response и не произвольный callback с готовым authority state.
export function requirePairedAuthorityReader(reader){ensure(readers.has(reader),'PAIRED_OWNER_READER_REQUIRED');return reader;}
export function readPairedAuthorityOwner(reader){
 requirePairedAuthorityReader(reader);const result=reader();
 ensure(fingerprint(result.postgres)===fingerprint(readers.get(reader).postgres),'PAIRED_OWNER_POSTGRES_CHANGED');
 return result;
}
