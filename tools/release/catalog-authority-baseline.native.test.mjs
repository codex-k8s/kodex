import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
import {readFileSync,readdirSync} from 'node:fs';
import {setTimeout as sleep} from 'node:timers/promises';
import {fileURLToPath} from 'node:url';
import {authorityOwnerDatabase,authorityOwnerDatabaseArgs} from './migration-owner-database.mjs';
import {catalogAuthorityBaselineSQL} from './catalog-authority-baseline.mjs';
import {validateRetiredCatalogOwnerState} from './authority-rotation-transition.mjs';

const image='docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7';

test('native PostgreSQL18: exact owner function/read-only SQL, db identity, membership and no writes',async t=>{
 const root=fileURLToPath(new URL('../../',import.meta.url)),name='kodex-routine-baseline-'+randomUUID();
 const docker=(args,input,timeout=30000)=>execFileSync('docker',args,{input,encoding:'utf8',timeout,maxBuffer:4<<20,stdio:['pipe','pipe','pipe']});
 // Cache-only exact image: тест не скачивает образ и не использует host network,
 // volumes, shared databases, установленные credentials или production target.
 const id=docker(['run','-d','--pull=never','--name',name,'--label','kodex.test=routine-api-baseline',
  '--network','none','--memory','256m','--tmpfs','/var/lib/postgresql:rw,nosuid,nodev,size=192m','-e','POSTGRES_HOST_AUTH_METHOD=trust',image]).trim();
 assert.match(id,/^[a-f0-9]{64}$/);
 t.after(()=>docker(['rm','-f',id],undefined,20000));
 let ready=false;
 for(let attempt=0;attempt<100;attempt++){
  try{docker(['exec',id,'pg_isready','-h','127.0.0.1','-U','postgres'],undefined,3000);ready=true;break;}catch{}
  await sleep(200);
 }
 assert.ok(ready,'native PostgreSQL readiness timeout');
 const sql=(text,database='internal_rpc_authority',role='postgres')=>docker(['exec','-i',id,'env','-i','PATH=/usr/local/bin:/usr/bin:/bin',
  'psql','-XqAt','--host=/var/run/postgresql','--port=5432','--username='+role,'--dbname='+database,'--no-password','-v','ON_ERROR_STOP=1'],text);
 sql(readFileSync(`${root}/deploy/k8s/base/platform-state/postgresql/10-bootstrap.sql`),'postgres');
 const directory=`${root}/services/internal/internal-rpc-authority/cmd/cli/migrations`;
 for(const file of readdirSync(directory).filter(name=>/^\d{14}_.+\.sql$/.test(name)).sort()){
  const up=readFileSync(`${directory}/${file}`,'utf8').split('-- +goose Down')[0];sql(up);
 }
 const pod={pod:'kodex-postgresql-0',uid:'10000000-0000-4000-8000-000000000001',ownerUID:'10000000-0000-4000-8000-000000000002'};
 const calls=[];
 const reader=authorityOwnerDatabase((args,query)=>{
  assert.deepEqual(args,authorityOwnerDatabaseArgs);calls.push(String(query));
  return docker(['exec','-i',id,...args.slice(args.indexOf('--')+1)],query);
 },()=>pod);
 const backend=reader.identity(pod);
 assert.equal(backend.database,'internal_rpc_authority');assert.match(backend.systemIdentifier,/^[1-9][0-9]{0,19}$/);assert.equal(typeof backend.databaseOID,'string');
 const owner=reader.read(catalogAuthorityBaselineSQL(),pod,backend);
 assert.equal(owner.status,'EMPTY');assert.ok(Number.isFinite(Date.parse(owner.observedAt)));
 assert.throws(()=>validateRetiredCatalogOwnerState(owner,{ownerOperationID:pod.uid},{sourceRevision:2,sourceDigestSHA256:'a'.repeat(64)},{expectedReadbackCount:14}),/OWNER_REJECTED/);
 const count=calls.filter(query=>query.includes('authority_rotation_status()')).length;
 assert.throws(()=>reader.read(catalogAuthorityBaselineSQL(),pod,{...backend,databaseOID:String(Number(backend.databaseOID)+1)}),/INSTANCE_CHANGED/);
 assert.equal(calls.filter(query=>query.includes('authority_rotation_status()')).length,count);
 assert.throws(()=>reader.read(catalogAuthorityBaselineSQL(),pod,{...backend,database:'control_plane'}),/IDENTITY_INVALID/);
 assert.throws(()=>sql(catalogAuthorityBaselineSQL(),'internal_rpc_authority','ira_control_plane_issuer_g1'));
 // Контрфактический повреждённый VOLATILE owner не может превратить pinned
 // READ ONLY в запись: проверяется реальный отказ ядра PostgreSQL, не mock flag.
 sql(`CREATE TABLE public.baseline_write_probe(value integer);
 CREATE OR REPLACE FUNCTION internal_rpc_authority.authority_rotation_status() RETURNS jsonb
 LANGUAGE plpgsql VOLATILE SECURITY DEFINER AS $$ BEGIN INSERT INTO public.baseline_write_probe VALUES (1); RETURN '{}'::jsonb; END $$;`);
 assert.throws(()=>reader.read(catalogAuthorityBaselineSQL(),pod,backend));
 assert.equal(sql('SELECT count(*) FROM public.baseline_write_probe;').trim(),'0');
 assert.equal(JSON.parse(sql("SELECT json_build_object('database',current_database());")).database,'internal_rpc_authority');
});
