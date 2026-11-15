import assert from 'node:assert/strict';
import {createHash,randomBytes} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {chmodSync,existsSync,mkdtempSync,readFileSync,rmSync,statSync,statfsSync,writeFileSync} from 'node:fs';
import {dirname,join,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {validateTelegramOwnerHistory} from './telegram-authority-owner.mjs';
import {validateTelegramAuthorityDeliveryState,telegramPolicyTransition} from './authority-telegram-transition.mjs';
import {validateAuthorityDistributePrecondition,validateAuthorityDistributePostcondition} from './authority-distribute-status-migration-model.mjs';

// Disposable native proof: только loopback PostgreSQL и private Go overlay.
// Результат доказывает representation, а не выдаёт authority107 admission.
assert.ok(process.argv.length===2||process.argv.length===3&&process.argv[2]==='--distribute-status','native profile argument rejected');
const distribute=process.argv[2]==='--distribute-status';
const root=resolve(fileURLToPath(new URL('../../',import.meta.url)));
const image='docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7';
const relative='services/internal/internal-rpc-authority/cmd/cli/rotation_operation_component_test.go';
const expectedSHA='bd7934d0a6601817dabccbabd3691ed1bf07ab1be1c2c4b4fa3fe69909aa029d';
const marker="\tboundaryExec(t, ctx, admin, `DELETE FROM internal_rpc_authority.authority_snapshot_readbacks WHERE readback_id::text LIKE '2191%'`)";
const sha=bytes=>createHash('sha256').update(bytes).digest('hex');
const run=(command,args,options={})=>execFileSync(command,args,{encoding:'utf8',timeout:30000,maxBuffer:8*1024*1024,...options});
const started=Date.now(),token=randomBytes(12).toString('hex');
const directory=mkdtempSync('/tmp/kodex-telegram-owner-native-');chmodSync(directory,0o700);
const cidFile=join(directory,'container.id');
let container,summary;
const cleanup=()=>{
 try{
  if(!container&&existsSync(cidFile)){container=readFileSync(cidFile,'utf8').trim();assert.match(container,/^[a-f0-9]{64}$/);assert.equal(run('docker',['inspect','--format','{{index .Config.Labels \"kodex.native-owner\"}}',container]).trim(),token);}
  if(container){const id=container;try{run('docker',['stop','--time','5',id],{timeout:15000});}catch{try{run('docker',['kill',id]);}catch{}}
   let remaining;
   for(let attempt=0;attempt<50;attempt++){
    remaining=run('docker',['ps','-a','--no-trunc','--filter','id='+id,'--format','{{.ID}}|{{.State}}']).trim();
    if(!remaining)break;
    assert.ok([id+'|exited',id+'|dead'].includes(remaining),'native PostgreSQL did not join');
    // --rm cleanup может ещё удалять exact terminal container; никаких force/retry effect.
    Atomics.wait(new Int32Array(new SharedArrayBuffer(4)),0,0,100);
   }
   assert.equal(remaining,'','native PostgreSQL container remains');container=undefined;
  }
 }finally{rmSync(directory,{recursive:true,force:true});}
};
for(const signal of ['SIGINT','SIGTERM'])process.once(signal,()=>{cleanup();process.exit(signal==='SIGINT'?130:143);});
try{
 assert.equal(statfsSync(directory).type,0x01021994,'native overlay directory must use tmpfs');
 const source=readFileSync(join(root,relative),'utf8');assert.equal(sha(source),expectedSHA,'native overlay source pin changed');assert.equal(source.split(marker).length,2,'native overlay marker is not unique');
 const go=process.env.KODEX_NATIVE_GO??'go';assert.match(run(go,['version']),/^go version go1\.26\.10 /,'native proof requires Go1.26.10');
 for(const key of ['GOCACHE','GOTMPDIR','TMPDIR']){const path=process.env[key];assert.ok(path&&resolve(path).startsWith('/tmp/'),`${key} must use a private RAM directory`);const info=statSync(path);assert.ok(info.isDirectory()&&info.uid===process.getuid()&&(info.mode&0o077)===0,`${key} must be owned mode0700`);assert.equal(statfsSync(path).type,0x01021994,`${key} must use tmpfs`);}
 const overlayPath=join(directory,'overlay.json'),copied=join(directory,'rotation_operation_component_test.go'),output=join(directory,'wire.json');
 const helper=`
// captureTelegramOwnerWire observes real SQL, serializes the actual Go DTO and reads SQL again without changing the owner operation.
// captureTelegramOwnerWire читает настоящий SQL, сериализует настоящий Go DTO и повторяет SQL без изменения owner operation.
func captureTelegramOwnerWire(t *testing.T, ctx context.Context, connection *pgx.Conn) {
 t.Helper()
 var raw, later []byte
 if err := connection.QueryRow(ctx, rotationStatusSQL).Scan(&raw); err != nil { t.Fatal(err) }
 status, err := decodeRotationStatus(raw)
 if err != nil { t.Fatal(err) }
 serialized, err := json.Marshal(status)
 if err != nil { t.Fatal(err) }
 if err := connection.QueryRow(ctx, rotationStatusSQL).Scan(&later); err != nil { t.Fatal(err) }
 proof, err := json.Marshal(struct { SQLRaw json.RawMessage \`json:"sqlRaw"\`; GoDTO json.RawMessage \`json:"goDTO"\`; LateSQL json.RawMessage \`json:"lateSQL"\` }{raw, serialized, later})
 if err != nil { t.Fatal(err) }
 path := os.Getenv("KODEX_AUTHORITY_OWNER_WIRE_OUTPUT")
 if path == "" { t.Fatal("native owner output is required") }
 file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
 if err != nil { t.Fatal(err) }
 if _, err := file.Write(proof); err != nil { file.Close(); t.Fatal(err) }
 if err := file.Sync(); err != nil { file.Close(); t.Fatal(err) }
 if err := file.Close(); err != nil { t.Fatal(err) }
}
`;
 const patched=source.replace('import (','import (\n\t"encoding/json"\n\t"os"').replace(marker,'\tcaptureTelegramOwnerWire(t, ctx, migrator)\n'+marker)+helper;
 writeFileSync(copied,patched,{mode:0o600});writeFileSync(overlayPath,JSON.stringify({Replace:{[join(root,relative)]:copied}}),{mode:0o600});
 container=run('docker',['run','--cidfile',cidFile,'--pull=never','--rm','-d','--name','kodex-telegram-owner-native-'+token,'--label','kodex.native-owner='+token,
  '--user','70:70','--read-only','--cap-drop=ALL','--security-opt','no-new-privileges:true','--memory=1g','--memory-swap=1g','--cpus=1','--pids-limit=128',
  '--tmpfs','/var/lib/postgresql:rw,nosuid,nodev,size=536870912,uid=70,gid=70,mode=0700',
  '--tmpfs','/var/run/postgresql:rw,nosuid,nodev,size=16777216,uid=70,gid=70,mode=0700','--tmpfs','/tmp:rw,nosuid,nodev,size=16777216,uid=70,gid=70,mode=0700',
  '-e','POSTGRES_HOST_AUTH_METHOD=trust','-p','127.0.0.1::5432',image]).trim();
 assert.match(container,/^[a-f0-9]{64}$/);assert.equal(run('docker',['inspect','--format','{{index .Config.Labels "kodex.native-owner"}}',container]).trim(),token);
 const port=run('docker',['inspect','--format','{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}',container]).trim();assert.match(port,/^[1-9][0-9]{0,4}$/);
 for(let index=0;index<30;index++){
  try{run('docker',['exec',container,'pg_isready','-U','postgres'],{stdio:'pipe'});break;}catch(error){if(index===29)throw error;Atomics.wait(new Int32Array(new SharedArrayBuffer(4)),0,0,1000);}
 }
 run('docker',['exec','-i',container,'psql','-Xq','-U','postgres','-d','postgres','--no-password','-v','ON_ERROR_STOP=1'],{input:readFileSync(join(root,'deploy/k8s/base/platform-state/postgresql/10-bootstrap.sql'))});
 const goLog=run(go,['test','-p=1','-count=1','-timeout=180s','-overlay',overlayPath,'./cmd/cli','-run','^TestAuthorityBaselineGooseComponent$'],{
  cwd:join(root,'services/internal/internal-rpc-authority'),timeout:240000,
  env:{...process.env,GOTOOLCHAIN:'local',GOENV:'off',GOWORK:'off',GOPROXY:'off',GOFLAGS:'',GOMAXPROCS:'2',GOMEMLIMIT:'2GiB',KODEX_AUTHORITY_MIGRATION_TEST_PORT:port,KODEX_AUTHORITY_OWNER_WIRE_OUTPUT:output,KODEX_AUTHORITY_DISTRIBUTE_UPGRADE_TEST:distribute?'1':'',KODEX_AUTHORITY_DISTRIBUTE_PROOF_OUTPUT:distribute?output:'',KODEX_AUTHORITY_STATE_SQL_FILE:join(root,'tools/release/authority-distribute-status-migration-state.sql'),KODEX_AUTHORITY_STATE_SQL_SHA256:sha(readFileSync(join(root,'tools/release/authority-distribute-status-migration-state.sql')))}});
 process.stdout.write(goLog);
 const wireBytes=readFileSync(output),proof=JSON.parse(wireBytes);
 if(distribute){
  assert.equal(proof.schema,'kodex.authority-distribute-status-native/v1');
  assert.equal(proof.before.definitionSHA256,'c615a3d183ca67cadff5cf027d12adb7207bf9a4cf456dea02affe6f63b86af1');
  assert.match(proof.after.definitionSHA256,/^[a-f0-9]{64}$/);assert.notEqual(proof.after.definitionSHA256,proof.before.definitionSHA256);
  for(const field of ['oid','owner','acl'])assert.equal(proof.after[field],proof.before[field]);
  assert.equal(proof.operationPublications,0);assert.equal(proof.currentStatus.actualReadbackCount,0);assert.ok(proof.negativeBindings>=22);
  assert.equal(Object.hasOwn(proof.oldStatus,'sourceRevision'),false);assert.equal(Object.hasOwn(proof.oldStatus,'sourceDigestSHA256'),false);
  const identity={sourceRevision:proof.currentStatus.registryRevision,sourceDigestSHA256:proof.currentStatus.registrySourceDigestSHA256};
  assert.throws(()=>validateTelegramAuthorityDeliveryState(proof.oldStatus,identity,proof.currentStatus.operationId),/DELIVERY_OWNER_REJECTED/);
  validateTelegramAuthorityDeliveryState(proof.currentStatus,identity,proof.currentStatus.operationId);
  validateTelegramAuthorityDeliveryState(proof.goDTO,identity,proof.currentStatus.operationId);
  const rotation={policyTransition:{profile:telegramPolicyTransition},ownerOperationID:proof.currentStatus.operationId,
   registry:{sourceRevision:proof.currentStatus.registryRevision,sourceDigestSHA256:proof.currentStatus.registrySourceDigestSHA256}};
  validateAuthorityDistributePrecondition(proof.beforeObserver,rotation);
  validateAuthorityDistributePostcondition(proof.beforeObserver,proof.afterObserver,rotation);
  summary={schema:'kodex.authority-distribute-status-native-proof/v1',result:'PASS',go:'1.26.10',postgres:image,oldDefinitionSHA256:proof.before.definitionSHA256,newDefinitionSHA256:proof.after.definitionSHA256,
   functionIdentityRetained:true,observerSQLSHA256:sha(readFileSync(join(root,'tools/release/authority-distribute-status-migration-state.sql'))),ledgerSHA256:proof.ledgerSHA256,negativeBindings:proof.negativeBindings,operationPublications:0,actualReadbacks:0,wireSHA256:sha(wireBytes),elapsedMS:Date.now()-started};
 }else{
 assert.deepEqual(Object.keys(proof).sort(),['goDTO','lateSQL','sqlRaw']);
 assert.equal(proof.sqlRaw.status,'RETIRED');assert.ok(proof.sqlRaw.operationId&&proof.sqlRaw.previousNotAfter);
 assert.match(proof.sqlRaw.previousNotAfter,/\.[0-9]{1,6}\+00:00$/);assert.match(proof.goDTO.previousNotAfter,/Z$/);
 validateTelegramOwnerHistory(proof.sqlRaw,proof.goDTO);validateTelegramOwnerHistory(proof.goDTO,proof.lateSQL);
 assert.equal(sha(readFileSync(join(root,relative))),expectedSHA,'source Go changed');
 summary={schema:'kodex.telegram-authority-owner-native-proof/v1',result:'PASS',go:'1.26.10',postgres:image,sourceSHA256:expectedSHA,wireSHA256:sha(wireBytes),representations:['SQL_RAW','ACTUAL_GO_DTO_JSON','LATE_SQL'],elapsedMS:Date.now()-started};
 }
}finally{cleanup();}
console.log(JSON.stringify({...summary,containerJoined:true}));
