import test from 'node:test';
import assert from 'node:assert/strict';
import {telegramEgressRevisionRepairSpec,readTelegramEgressRevisionCandidate,planTelegramEgressRevisionRepair,readTelegramEgressRevisionRepair} from './telegram-egress-revision-repair.mjs';
import {sourceArchiveHash,validateStableReceipt} from './stable-image-publication.mjs';
import {fingerprint} from './scoped-release.mjs';
import {execFileSync} from 'node:child_process';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import {join,dirname} from 'node:path';

const image=digest=>'registry.local.kodex/kodex/egress-gateway@sha256:'+digest.repeat(64);
function fixture(){
 return {replicas:2,selector:{matchLabels:{app:'egress-gateway'}},template:{metadata:{labels:{app:'egress-gateway'}},spec:{
  serviceAccountName:'egress-gateway',volumes:[{name:'base-policy',configMap:{name:'synthetic-base'}}],
  containers:[{name:'egress-gateway',image:image('a'),env:[
   {name:'EGRESS_GATEWAY_EXPECTED_POLICY_REVISION',value:'release-synthetic'},
   {name:'EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST',value:'b'.repeat(64)},
   {name:'EGRESS_GATEWAY_TELEGRAM_POLICY_FILE',value:'/var/run/config/kodex/egress-gateway-telegram/policy.json'},
   {name:'EGRESS_GATEWAY_TELEGRAM_POLICY_DIGEST',value:'91bf766d2cb635882309927bbbc6e4d45d6b91998b88f436efbcfd59fd5520f0'}
  ],securityContext:{readOnlyRootFilesystem:true},readinessProbe:{httpGet:{path:'/readyz',port:'metrics'}}}]}}};
}

test('egress repair сохраняет base policy, probes, replicas, volumes и добавляет только отдельный pin/image',()=>{
 const before=fixture(),saved=structuredClone(before),after=telegramEgressRevisionRepairSpec(before,image('c'));
 const expected=structuredClone(before);expected.template.spec.containers[0].image=image('c');
 expected.template.spec.containers[0].env.push({name:'EGRESS_GATEWAY_TELEGRAM_POLICY_REVISION',value:'2026-09-05.1'});
 assert.deepEqual(after,expected);assert.deepEqual(before,saved);
});

test('egress repair отклоняет mutable/foreign/zero image и отсутствие реальной замены',()=>{
 for(const candidate of [image('a'),'registry.local.kodex/kodex/egress-gateway:latest',image('0'),image('c').replace('egress-gateway','control-plane')])
  assert.throws(()=>telegramEgressRevisionRepairSpec(fixture(),candidate),/REPAIR_REJECTED/);
});

test('egress repair отклоняет уже назначенный pin, duplicate env, valueFrom и чужой Telegram digest',()=>{
 for(const mutate of [
  e=>e.push({name:'EGRESS_GATEWAY_TELEGRAM_POLICY_REVISION',value:'2026-09-05.1'}),
  e=>e.push({...e[0]}),e=>{delete e[2].value;e[2].valueFrom={configMapKeyRef:{name:'foreign',key:'value'}};},
  e=>e[3].value='d'.repeat(64),e=>e.splice(0,1)
 ]){const before=fixture();mutate(before.template.spec.containers[0].env);assert.throws(()=>telegramEgressRevisionRepairSpec(before,image('c')),/REPAIR_REJECTED/);}
});

test('egress repair не принимает другой workload, дополнительные containers или остановленный predecessor',()=>{
 for(const mutate of [f=>f.replicas=0,f=>f.replicas=1.5,f=>f.template.spec.containers[0].name='foreign',
  f=>f.template.spec.containers.push(structuredClone(f.template.spec.containers[0]))]){
  const before=fixture();mutate(before);assert.throws(()=>telegramEgressRevisionRepairSpec(before,image('c')),/REPAIR_REJECTED/);
 }
});

async function sourceFixture(t){
 const directory=mkdtempSync('/tmp/kodex-egress-candidate-'),source=join(directory,'source');mkdirSync(source,{mode:0o755});
 t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const git=(...args)=>execFileSync('git',['-C',source,...args],{encoding:'utf8',stdio:['ignore','pipe','pipe']}).trim();
 const files=['services/external/egress-gateway/internal/app/app.go','services/external/egress-gateway/internal/app/config.go'];
 for(const path of [...files,'libs/synthetic.txt']){mkdirSync(dirname(join(source,path)),{recursive:true,mode:0o755});writeFileSync(join(source,path),'synthetic predecessor\n',{mode:0o644});}
 git('init','-q');git('remote','add','origin','https://github.com/codex-k8s/kodex.git');git('add','.');
 const commit=()=>git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Синтетический source');commit();const oldSHA=git('rev-parse','HEAD');
 for(const path of files)writeFileSync(join(source,path),'synthetic qualified successor\n');git('add','.');commit();const sourceSHA=git('rev-parse','HEAD');
 const uid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
 const receipt={schema:'kodex.stable-local-build/v1',component:'egress-gateway',sourceSHA,contextSHA256:await sourceArchiveHash(sourceSHA,source),
  archiveSHA256:'e'.repeat(64),imageDigest:'sha256:'+'c'.repeat(64),publication:'NOT_RUN',admission:'NOT_RUN',nodePull:'NOT_RUN'};
 const publication={schema:'kodex.stable-image-publication/v2',artifactProfile:'built',source:sourceSHA,context:'k3d-synthetic',clusterUID:uid(1),namespaceUID:uid(2),
  receipt,image:validateStableReceipt(receipt),archiveSHA256:receipt.archiveSHA256};
 const write=(path,value)=>writeFileSync(path,JSON.stringify(value)+'\n',{mode:0o600});
 const publicationPath=join(directory,'publication.json'),publicationEvidencePath=join(directory,'publication.jsonl');write(publicationPath,publication);
 const publicationEvents=[{schema:'kodex.stable-image-publication-attempt/v1',status:'INTENT',mode:'apply',planSHA256:fingerprint(publication),component:'egress-gateway',digest:receipt.imageDigest},
  {status:'PASS',phase:'REGISTRY_PUBLICATION_ONLY',nodePull:'NOT_RUN'}];
 writeFileSync(publicationEvidencePath,publicationEvents.map(e=>JSON.stringify(e)+'\n').join(''),{mode:0o600});
 const nodes=[{name:'k3d-synthetic-server-0',uid:uid(3),imageRef:image('c'),repoDigestVerified:true}];
 const candidate={schema:'kodex.telegram-egress-revision-candidate/v1',source:{path:source,revision:sourceSHA},publicationPlanPath:publicationPath,publicationEvidencePath,
  nodeProof:{schema:'kodex.telegram-egress-node-proof/v1',sourceSHA,imageRef:image('c'),clusterUID:uid(1),namespaceUID:uid(2),bindingSHA256:'f'.repeat(64),nodes}};
 const candidatePath=join(directory,'candidate.json');write(candidatePath,candidate);
 const migration={version:2,kind:'CONTROL_PLANE_MIGRATION',profile:'telegram-notifications-backup',migrationVersion:'20261114140000',id:uid(9),
  context:'k3d-synthetic',kubeconfig:'/synthetic/kubeconfig',clusterUID:uid(1),namespaceUID:uid(2),source:{path:'/synthetic/old-source',revision:oldSHA},
  portableTarget:{clusterUID:uid(1)},postgres:{database:'control_plane',systemID:'synthetic'},postconditionSQLSHA256:'a'.repeat(64),
  imageEvidence:{components:[{nodeProof:{nodes}}]},componentHandoff:{publicationSHA256:'b'.repeat(64),components:[{name:'egress-gateway',uid:uid(10),replicas:2,resumedSpec:fixture()}]}};
 const migrationPath=join(directory,'migration.json'),evidencePath=join(directory,'migration.jsonl');write(migrationPath,migration);
 const events=[{type:'HEADER',version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256:fingerprint(migration)},{type:'TERMINAL_READBACK',jobUID:uid(11)},
  {type:'READBACK',jobUID:uid(11),status:'PASS',schemaPostcondition:'PASS'},{type:'PAIR_HANDOFF_ACK',publicationSHA256:migration.componentHandoff.publicationSHA256},{type:'PAIR_RESUME_INTENT',name:'egress-gateway'}];
 const evidence=events.map(e=>JSON.stringify(e)+'\n').join('');writeFileSync(evidencePath,evidence,{mode:0o600});
 return {directory,source,git,write,candidate,candidatePath,migration,migrationPath,evidencePath,evidence,publicationPath,publicationEvidencePath};
}

test('candidate связывает реальный Git archive, registry receipt и node proof; repair prefix остаётся immutable',async t=>{
 const f=await sourceFixture(t),candidate=await readTelegramEgressRevisionCandidate(f.candidatePath,f.migration);
 const repair=planTelegramEgressRevisionRepair({migration:f.migration,migrationPlanPath:f.migrationPath,migrationEvidencePath:f.evidencePath,
  evidence:f.evidence,candidate,observedAt:'2026-11-15T02:00:00Z'}),path=join(f.directory,'repair.json');f.write(path,repair);
 const bound=await readTelegramEgressRevisionRepair(path,{migration:f.migration});assert.equal(bound.sha256,fingerprint(repair));
 assert.equal(bound.part.resumedSpec.template.spec.containers[0].image,image('c'));assert.equal(readFileSync(f.evidencePath,'utf8'),f.evidence);
 for(const change of [r=>r.afterSpec.replicas=9,r=>r.evidencePrefixSHA256='f'.repeat(64),r=>r.candidate.imageRef=image('d')]){
  const changed=structuredClone(repair);change(changed);f.write(path,changed);await assert.rejects(()=>readTelegramEgressRevisionRepair(path,{migration:f.migration}));
 }
});

test('candidate отказывает при unqualified/foreign node, image, source, dirty checkout и несовпадающем target',async t=>{
 const f=await sourceFixture(t),original=structuredClone(f.candidate);
 for(const mutate of [c=>c.nodeProof=null,c=>c.nodeProof.sourceSHA='d'.repeat(40),c=>c.nodeProof.imageRef=image('d'),
  c=>c.nodeProof.nodes[0].uid='10000000-0000-4000-8000-000000000099',c=>c.nodeProof.nodes.push({...c.nodeProof.nodes[0]}),c=>c.nodeProof.nodes[0].repoDigestVerified=false]){
  const candidate=structuredClone(original);mutate(candidate);f.write(f.candidatePath,candidate);
  await assert.rejects(()=>readTelegramEgressRevisionCandidate(f.candidatePath,f.migration));
 }
 f.write(f.candidatePath,original);await assert.rejects(()=>readTelegramEgressRevisionCandidate(f.candidatePath,{...f.migration,clusterUID:'10000000-0000-4000-8000-000000000099'}));
 writeFileSync(join(f.source,'untracked.txt'),'synthetic\n');await assert.rejects(()=>readTelegramEgressRevisionCandidate(f.candidatePath,f.migration),/SOURCE_CHECKOUT_NOT_EXACT/);
});
