import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,chmodSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import {fingerprint} from './scoped-release.mjs';
import {telegramCreateSourceFixture,telegramIntroductionFixture} from './fixtures/telegram-introduction-synthetic.mjs';
import {planTelegramObservabilityRepair,readTelegramObservabilityRepair,telegramObservabilityRepairBound} from './telegram-observability-repair.mjs';

const uid=n=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function fixture(t){
 const directory=mkdtempSync('/tmp/kodex-telegram-observability-repair-');chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const sourceSHA='b'.repeat(40),image='registry.local.kodex/kodex/internal-rpc-authority@sha256:'+'a'.repeat(64),mainImage='registry.local.kodex/kodex/interaction-gateway@sha256:'+'c'.repeat(64);
 const publisher={apiVersion:'apps/v1',kind:'Deployment',metadata:{name:'internal-rpc-authority-publisher',namespace:'kodex-system',uid:uid(1)},
  spec:{template:{metadata:{},spec:{containers:[{name:'publisher',image,env:[{name:'OTEL_SDK_DISABLED',value:'true'}]}]}}}};
 const creator={schema:'kodex.telegram-resource-create-plan/v1',target:{schema:'kodex.telegram-resource-create-target/v1',sourceSHA,publisherUID:uid(1),authorityImage:image},opaque:'original immutable bytes'};
 const creatorPath=directory+'/creator.json';writeFileSync(creatorPath,JSON.stringify(creator)+'\n',{mode:0o600});
 const spec=telegramCreateSourceFixture().find(r=>r.kind==='Deployment').spec;
 for(const container of [...spec.template.spec.containers,...spec.template.spec.initContainers])container.image=container.name==='interaction-gateway'?mainImage:image;
 const beforeSpec=structuredClone(spec);beforeSpec.replicas=0;
 const introduced=telegramIntroductionFixture({sourceSHA,imageRef:mainImage,deploymentUID:uid(9),specSHA256:fingerprint(beforeSpec)});
 Object.assign(introduced,{planPath:creatorPath,createPlanSHA256:fingerprint(creator),targetSHA256:fingerprint(creator.target)});
 const components=['control-plane','control-api-gateway','staff-control-center','egress-gateway','interaction-gateway'].map((name,index)=>({name,uid:uid(5+index),
  ...(name==='control-api-gateway'?{maintenance:true}:{}),...(name==='interaction-gateway'?{introduced:true}:{}),
  beforeSpec:structuredClone(beforeSpec),stoppedSpec:structuredClone(beforeSpec),stagedSpec:structuredClone(beforeSpec),resumedSpec:structuredClone(spec)}));
 const migration={version:2,kind:'CONTROL_PLANE_MIGRATION',profile:'telegram-notifications-backup',migrationVersion:'20261114140000',id:'original-migration',
  source:{path:'/synthetic/application',revision:sourceSHA},portableTarget:{clusterUID:uid(20)},postgres:{database:'control_plane',systemID:'synthetic'},
  postconditionSQLSHA256:'d'.repeat(64),componentHandoff:{components,publicationSHA256:'e'.repeat(64)},imageEvidence:{introducedReceiver:introduced}};
 const rows=[{type:'HEADER',version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256:fingerprint(migration)},
  {type:'CREATE_ACK',jobUID:uid(30)},{type:'TERMINAL_READBACK',jobUID:uid(30)},
  {type:'READBACK',jobUID:uid(30),status:'PASS',schemaPostcondition:'PASS'},
  {type:'PAIR_HANDOFF_ACK',publicationSHA256:migration.componentHandoff.publicationSHA256},
  {type:'RESUME_INTENT',name:'control-plane'},
  {type:'PAIR_RESUME_INTENT',name:'staff-control-center'},
  {type:'PAIR_RESUME_INTENT',name:'egress-gateway'},
  {type:'PAIR_ACTIVATE_INTENT',name:'interaction-gateway'}];
 const evidence=()=>rows.map(e=>JSON.stringify(e)+'\n').join('');
 const input={migration,migrationPlanPath:directory+'/migration.json',migrationEvidencePath:directory+'/migration.jsonl',publisher,observedAt:'2026-11-15T00:00:00.000Z'};
 const plan=()=>planTelegramObservabilityRepair({...input,evidence:evidence()});
 const writeEvidence=()=>writeFileSync(input.migrationEvidencePath,evidence(),{mode:0o600});
 const store=()=>{writeFileSync(input.migrationPlanPath,JSON.stringify(migration)+'\n',{mode:0o600});writeEvidence();const value=plan(),path=directory+'/repair.json';writeFileSync(path,JSON.stringify(value)+'\n',{mode:0o600});return {value,path};};
 return {directory,publisher,migration,creator,creatorPath,rows,evidence,input,plan,store,writeEvidence};
}

test('immutable repair меняет только три canonical env и возвращает effective part, original bytes сохраняются',t=>{
 const f=fixture(t),original=structuredClone(f.migration),{value,path}=f.store(),before=readFileSync(f.input.migrationPlanPath),prefix=readFileSync(f.input.migrationEvidencePath),creator=readFileSync(f.creatorPath);
 const read=readTelegramObservabilityRepair(path,f.input);assert.equal(read.sha256,fingerprint(value));assert.deepEqual(f.migration,original);
 const wanted=structuredClone(value.beforeSpec);for(const c of wanted.template.spec.containers)c.env=[{name:'OTEL_SDK_DISABLED',value:'true'}];
 assert.deepEqual(value.afterSpec,wanted);assert.deepEqual(read.part.beforeSpec,value.beforeSpec);assert.equal(read.part.stoppedSpec.replicas,0);assert.deepEqual(read.part.stagedSpec,read.part.stoppedSpec);assert.deepEqual(read.part.resumedSpec,value.afterSpec);
 assert.deepEqual(value.afterSpec.template.spec.initContainers,value.beforeSpec.template.spec.initContainers);
 f.publisher.spec.template.metadata.annotations={'kubectl.kubernetes.io/restartedAt':'2026-11-15T01:00:00Z'};
 assert.equal(readTelegramObservabilityRepair(path,f.input).sha256,read.sha256);
 assert.deepEqual(readFileSync(f.input.migrationPlanPath),before);assert.deepEqual(readFileSync(f.input.migrationEvidencePath),prefix);assert.deepEqual(readFileSync(f.creatorPath),creator);
});

test('publisher UID закреплён исходным creator metadata proof, replacement с тем же image не усыновляется',t=>{
 for(const mutate of [f=>f.publisher.metadata.uid=uid(99),f=>f.publisher.spec.template.spec.containers[0].image+='foreign',
  f=>f.publisher.spec.template.spec.containers[0].env=[],f=>f.publisher.metadata.deletionTimestamp='2026-11-15T00:00:00Z',
  f=>{f.creator.target.publisherUID=uid(99);writeFileSync(f.creatorPath,JSON.stringify(f.creator)+'\n');}]){
  const f=fixture(t);mutate(f);assert.throws(f.plan,/REPAIR_REJECTED|RUNTIME_OBSERVABILITY_REJECTED/);
 }
});

test('неполный/чужой lifecycle и history не дают plan',t=>{
 for(const mutate of [f=>f.rows[0].planSHA256='f'.repeat(64),f=>f.rows[0].version=2,
  f=>f.rows.splice(2,1),f=>f.rows[3].status='RUNNING',f=>f.rows[3].schemaPostcondition='FAIL',
  f=>f.rows[3].jobUID=uid(99),f=>f.rows[4].publicationSHA256='f'.repeat(64),
  f=>f.rows.splice(5,1),f=>f.rows.splice(6,1),f=>f.rows[8].name='foreign',
  f=>f.rows.unshift(f.rows.pop()),f=>f.rows.push({type:telegramObservabilityRepairBound})]){
  const f=fixture(t);mutate(f);assert.throws(f.plan,/REPAIR_REJECTED/);
 }
});

test('unknown profile, foreign receiver, image drift и ужеисправленный original запрещены',t=>{
 for(const mutate of [f=>f.migration.profile='unknown',f=>f.migration.migrationVersion='20261114130000',
  f=>f.migration.componentHandoff.components.at(-1).uid=uid(99),f=>f.migration.componentHandoff.components.at(-1).resumedSpec.replicas=4,
  f=>f.migration.componentHandoff.components.at(-1).resumedSpec.template.spec.containers[1].image+='foreign',
  f=>f.migration.componentHandoff.components.at(-1).resumedSpec.template.spec.containers[0].env=[{name:'OTEL_SDK_DISABLED',value:'true'}]]){
  const f=fixture(t);mutate(f);f.rows[0].planSHA256=fingerprint(f.migration);assert.throws(f.plan,/REPAIR_REJECTED/);
 }
});

test('reader отвергает changed plan/prefix/migration/paths и чужой либо повторный bound',t=>{
 for(const mutate of [({f,path,value})=>writeFileSync(path,JSON.stringify({...value,extra:true})+'\n'),
  ({f,path,value})=>{value.afterSpec.template.spec.containers[0].image='foreign';writeFileSync(path,JSON.stringify(value)+'\n');},
  ({f})=>writeFileSync(f.input.migrationEvidencePath,' '+f.evidence()),
  ({f})=>writeFileSync(f.input.migrationPlanPath,JSON.stringify({...f.migration,extra:true})+'\n'),
  ({f,value})=>{f.rows.push({type:telegramObservabilityRepairBound,repairSHA256:'f'.repeat(64),jobUID:value.jobUID,deploymentUID:value.deploymentUID});f.writeEvidence();},
  ({f,value})=>{const bound={type:telegramObservabilityRepairBound,repairSHA256:fingerprint(value),jobUID:value.jobUID,deploymentUID:value.deploymentUID};f.rows.push(bound,bound);f.writeEvidence();}]){
  const f=fixture(t),{path,value}=f.store();mutate({f,path,value});assert.throws(()=>readTelegramObservabilityRepair(path,f.input),/REPAIR_REJECTED/);
 }
 const f=fixture(t),{path}=f.store();assert.throws(()=>readTelegramObservabilityRepair(path,{...f.input,migrationEvidencePath:f.directory+'/foreign.jsonl'}),/REPAIR_REJECTED/);
});

test('reader допускает только append same-plan bound, исходный prefix остаётся прежним',t=>{
 const f=fixture(t),{path,value}=f.store(),prefix=readFileSync(f.input.migrationEvidencePath);
 f.rows.push({type:telegramObservabilityRepairBound,repairSHA256:fingerprint(value),jobUID:value.jobUID,deploymentUID:value.deploymentUID});f.writeEvidence();
 assert.equal(readTelegramObservabilityRepair(path,f.input).sha256,fingerprint(value));
 assert.deepEqual(readFileSync(f.input.migrationEvidencePath).subarray(0,prefix.length),prefix);
 f.rows.at(-1).deploymentUID=uid(99);f.writeEvidence();assert.throws(()=>readTelegramObservabilityRepair(path,f.input),/REPAIR_REJECTED/);
});
