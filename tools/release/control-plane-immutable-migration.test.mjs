import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, unlinkSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { planControlPlaneMigration, migrationReadback } from './control-plane-migration.mjs';
import { fingerprint } from './scoped-release.mjs';
import { validateStableReceipt } from './stable-image-publication.mjs';
import { waitMigrationMaintenance } from './control-plane-immutable-migration.mjs';

const revision = 'a'.repeat(40), digest = `sha256:${'b'.repeat(64)}`;
const source = {path:'/fixture/source',revision};
const version = '20261025000100', annotation = 'kodex.dev/cp-outbox-index-migration-plan';
const sqlFile = `${version}_outbox_unpublished_predecessor_index.sql`;
const read = path => JSON.parse(readFileSync(path,'utf8'));
const write = (path,value) => writeFileSync(path,JSON.stringify(value),{mode:0o600});

// Реальный repo-owned immutable Job проверяется без копии его credential boundary в оснастке.
function base() {
  const job = JSON.parse(execFileSync('yq',['-o=json','.',new URL('../../deploy/k8s/base/control-plane/migration-job.yaml',import.meta.url).pathname],{encoding:'utf8'}));
  Object.assign(job.metadata,{uid:'anchor',resourceVersion:'7',namespace:'kodex-system'});
  Object.assign(job.metadata.labels,{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'staging','kodex.dev/local-profile':'stable'});
  return job;
}
function publication(sha = revision) {
  const receipt = {schema:'kodex.stable-local-build/v1',component:'control-plane',sourceSHA:sha,
    contextSHA256:'c'.repeat(64),archiveSHA256:'d'.repeat(64),imageDigest:digest,publication:'NOT_RUN',admission:'NOT_RUN',nodePull:'NOT_RUN'};
  const publicationPlan = {schema:'kodex.stable-image-publication/v2',artifactProfile:'built',context:'k3d-kodex',
    clusterUID:'cluster',namespaceUID:'namespace',receipt,image:validateStableReceipt(receipt),archiveSHA256:receipt.archiveSHA256};
  return {publicationPlan,publicationEvidence:[{schema:'kodex.stable-image-publication-attempt/v1',status:'INTENT',
    mode:'apply',planSHA256:fingerprint(publicationPlan),component:'control-plane',digest},
  {status:'PASS',phase:'REGISTRY_PUBLICATION_ONLY',nodePull:'NOT_RUN'}]};
}
function makePlan(evidence = publication(), before = base()) {
  return planControlPlaneMigration(before,source,()=>({revision}),'fixture','outbox-index',evidence);
}
function execution(plan) {
  const job = structuredClone(plan.job);
  job.metadata.uid = 'created'; job.metadata.annotations = {[annotation]:fingerprint(plan)};
  job.status = {succeeded:1,conditions:[{type:'Complete',status:'True'}]};
  const pod = {metadata:{uid:'pod',namespace:'kodex-system',ownerReferences:[{kind:'Job',uid:'created',controller:true}]},
    spec:structuredClone(job.spec.template.spec),status:{phase:'Succeeded',containerStatuses:[{
      name:'migrate',restartCount:0,imageID:`registry.local.kodex/kodex/control-plane@${digest}`,state:{terminated:{exitCode:0}}}]}};
  return {job,pod};
}
test('immutable builder uses canonical least-privilege boundary and exact published image without host source',()=>{
  const before = base(), snapshot = structuredClone(before), plan = makePlan(publication(),before), pod = plan.job.spec.template.spec;
  assert.deepEqual(before,snapshot);
  assert.equal(plan.job.metadata.name,`control-plane-migrate-${version}`);
  assert.equal(plan.job.spec.backoffLimit,0); assert.equal(plan.job.spec.activeDeadlineSeconds,300);
  assert.equal(pod.initContainers,undefined); assert.equal(pod.automountServiceAccountToken,false);
  assert.equal(pod.containers[0].image,`registry.local.kodex/kodex/control-plane@${digest}`);
  assert.deepEqual(pod.containers[0].command,['/usr/local/bin/control-plane-cli']);
  assert.deepEqual(pod.containers[0].args,['up']);
  const expectedVolumes=structuredClone(before.spec.template.spec.volumes);expectedVolumes[1].configMap.defaultMode=288;
  assert.deepEqual(pod.volumes,expectedVolumes);
  assert.deepEqual(pod.containers[0].env,before.spec.template.spec.containers[0].env);
  assert.equal(JSON.stringify(plan.job).includes('hostPath'),false);
  assert.equal(JSON.stringify(plan.job).includes('/workspace'),false);
});
test('builder rejects altered credentials, authority, command, source and unconfirmed image publication',()=>{
  for (const mutate of [j=>j.metadata.namespace='foreign',j=>j.metadata.labels['app.kubernetes.io/part-of']='foreign',j=>j.spec.template.spec.serviceAccountName='default',
    j=>j.spec.template.spec.automountServiceAccountToken=true,j=>j.spec.template.spec.volumes[0].secret.secretName='foreign',
    j=>j.spec.template.spec.volumes.push({name:'source',hostPath:{path:'/tmp'}}),
    j=>j.spec.template.spec.containers[0].envFrom=[{secretRef:{name:'foreign'}}],
    j=>j.spec.template.spec.containers[0].args=['down'],j=>j.spec.template.spec.containers[0].volumeMounts[0].readOnly=false]) {
    const before=base(); mutate(before); assert.throws(()=>makePlan(publication(),before));
  }
  for (const mutate of [e=>e.publicationEvidence.pop(),e=>e.publicationEvidence[0].planSHA256='e'.repeat(64),
    e=>e.publicationEvidence[1].status='FAIL',e=>e.publicationEvidence[0].component='api-gateway',
    e=>e.publicationPlan.receipt.sourceSHA='e'.repeat(40),e=>e.publicationPlan.image.digest=`sha256:${'e'.repeat(64)}`,
    e=>e.publicationPlan.artifactProfile='retained']) {
    const evidence=publication(); mutate(evidence); assert.throws(()=>makePlan(evidence));
  }
  assert.throws(()=>planControlPlaneMigration(base(),source,()=>({revision:'e'.repeat(40)}),'fixture','outbox-index',publication()),/SOURCE_REVISION_MISMATCH/);
});
test('native repo-owned staging kustomize render is accepted without invented application labels',()=>{
  const rendered=execFileSync('kubectl',['kustomize',new URL('../../deploy/k8s/overlays/staging/control-plane',import.meta.url).pathname],
    {encoding:'utf8',timeout:15000,maxBuffer:4<<20,env:{...process.env,KUBECONFIG:'/dev/null'},stdio:['ignore','pipe','pipe']});
  const native=JSON.parse(execFileSync('yq',['-o=json','select(.kind == "Job" and .metadata.name == "control-plane-migrate")','-'],
    {input:rendered,encoding:'utf8',timeout:5000,maxBuffer:4<<20,stdio:['pipe','pipe','pipe']}));
  assert.equal(native.metadata.labels['app.kubernetes.io/part-of'],undefined);
  Object.assign(native.metadata,{uid:'anchor',resourceVersion:'7'});
  const plan=makePlan(publication(),native);assert.equal(plan.job.metadata.labels['app.kubernetes.io/part-of'],'kodex');
  assert.equal(plan.job.metadata.namespace,'kodex-system');assert.equal(plan.job.metadata.labels['kodex.dev/environment'],'staging');
});
test('PASS needs exact goose version and one exact Job-owned non-restarted execution with real imageID',()=>{
  const plan=makePlan(),{job,pod}=execution(plan), logs=`goose: successfully migrated database to version: ${version}\n`;
  assert.equal(migrationReadback(job,plan,logs,[pod]).status,'PASS');
  assert.equal(migrationReadback(job,plan,logs,[]).status,'READBACK_UNCONFIRMED');
  assert.equal(migrationReadback(job,plan,'goose: current version: 20261025000099\n',[pod]).status,'READBACK_UNCONFIRMED');
  const other=structuredClone(pod);other.status.containerStatuses[0].imageID=`sha256:${'e'.repeat(64)}`;
  assert.equal(migrationReadback(job,plan,logs,[other]).status,'READBACK_UNCONFIRMED');
  for(const mutate of [p=>p.metadata.ownerReferences[0].uid='foreign',p=>p.spec.containers[0].envFrom=[{secretRef:{name:'foreign'}}],
    p=>p.spec.volumes[0].secret.secretName='foreign',p=>p.status.containerStatuses[0].restartCount=1]) {
    const changed=structuredClone(pod);mutate(changed);assert.throws(()=>migrationReadback(job,plan,logs,[changed]));
  }
  assert.throws(()=>migrationReadback(job,plan,logs,[pod,pod]),/EXECUTION_SCOPE_CHANGED/);
  const changed=structuredClone(job);changed.spec.backoffLimit=1;
  assert.throws(()=>migrationReadback(changed,plan,logs,[pod]),/LIFECYCLE_CHANGED/);
});

// Настоящий CLI с отдельным kubectl subprocess доказывает порядок intent/CAS/create и запрет повторов.
function cli(t) {
  const dir=mkdtempSync('/tmp/cp-immutable-migration-');chmodSync(dir,0o700);t.after(()=>rmSync(dir,{recursive:true,force:true}));
  const repo=join(dir,'source'), migrations=join(repo,'services/internal/control-plane/cmd/cli/migrations');
  mkdirSync(migrations,{recursive:true,mode:0o755});chmodSync(repo,0o755);
  writeFileSync(join(migrations,sqlFile),'-- Синтетический source-only migration fixture\n',{mode:0o644});
  const bundleDirectory=join(repo,'deploy/k8s/base/platform-state');mkdirSync(bundleDirectory,{recursive:true,mode:0o755});
  writeFileSync(join(bundleDirectory,'ca-bundles.yaml'),readFileSync(new URL('../../deploy/k8s/base/platform-state/ca-bundles.yaml',import.meta.url)),{mode:0o644});
  const git=(...args)=>execFileSync('git',['-C',repo,...args],{stdio:['ignore','pipe','pipe']}).toString().trim();
  git('init','-q');git('remote','add','origin','https://github.com/codex-k8s/kodex.git');git('add','.');
  git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Оснастка');
  const revision=git('rev-parse','HEAD'), render=join(dir,'render.json'), plan=join(dir,'plan.json'), evidence=join(dir,'evidence.jsonl');
  const image=publication(revision), publicationPlan=join(dir,'publication-plan.json'), publicationEvidence=join(dir,'publication-evidence.jsonl');
  write(render,base());write(publicationPlan,image.publicationPlan);
  writeFileSync(publicationEvidence,image.publicationEvidence.map(row=>JSON.stringify(row)).join('\n')+'\n',{mode:0o600});
  const deployment={apiVersion:'apps/v1',kind:'Deployment',metadata:{uid:'anchor',resourceVersion:'7',generation:1,name:'control-plane',namespace:'kodex-system',
    labels:{'app.kubernetes.io/name':'control-plane','app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'staging','kodex.dev/local-profile':'stable'}},
    spec:{replicas:2,template:{spec:{containers:[{name:'control-plane',image:`registry.local.kodex/kodex/control-plane@${digest}`}]}}},
    status:{observedGeneration:1,replicas:2,updatedReplicas:2,readyReplicas:2,availableReplicas:2}};
  const state=join(dir,'state.json');write(state,{deployment,calls:[],mode:'',dependencyVersion:'1',namespaceUID:'namespace'});
  const executable=join(dir,'kubectl');
  writeFileSync(executable,`#!/usr/bin/env node
const fs=require('fs'),path=${JSON.stringify(state)},journal=${JSON.stringify(evidence)},s=JSON.parse(fs.readFileSync(path)),a=process.argv.slice(2);
s.calls.push(a);const save=()=>fs.writeFileSync(path,JSON.stringify(s));const out=x=>{save();process.stdout.write(JSON.stringify(x));};
const fail=()=>{save();process.exit(1);};
if(a.includes('patch')){
 const events=fs.readFileSync(journal,'utf8').trim().split('\\n').map(JSON.parse);if(!['STOP_INTENT','RESUME_INTENT'].includes(events.at(-1).type))fail();
 const p=JSON.parse(a[a.indexOf('-p')+1]);if(s.mode==='cas'||p[0].value!==s.deployment.metadata.uid||p[1].value!==s.deployment.metadata.resourceVersion)fail();
 if(p[2].value!==s.deployment.spec.replicas)fail();s.deployment.spec.replicas=p[3].value;s.deployment.metadata.annotations=p[4].value;
 s.deployment.metadata.resourceVersion=String(Number(s.deployment.metadata.resourceVersion)+1);s.deployment.metadata.generation++;
 s.deployment.status={observedGeneration:s.deployment.metadata.generation,...Object.fromEntries(['replicas','updatedReplicas','readyReplicas','availableReplicas'].map(key=>[key,p[3].value]))};
 if(s.mode==='stop-crash'&&p[3].value===0){save();process.kill(process.ppid,'SIGKILL');process.exit(1);}
 if((s.mode==='stop-lost-ack'&&p[3].value===0)||(s.mode==='resume-lost-ack'&&p[3].value===2))fail();out(s.deployment);
}else if(a.includes('create')){
 const events=fs.readFileSync(journal,'utf8').trim().split('\\n').map(JSON.parse);if(events.at(-1).type!=='CREATE_INTENT')fail();
 s.job=JSON.parse(fs.readFileSync(0,'utf8'));s.job.metadata.uid='created';s.job.status={succeeded:1,conditions:[{type:'Complete',status:'True'}]};
 s.pod={metadata:{uid:'pod',namespace:'kodex-system',ownerReferences:[{kind:'Job',uid:'created',controller:true}]},spec:s.job.spec.template.spec,
 status:{phase:'Succeeded',containerStatuses:[{name:'migrate',restartCount:0,imageID:s.job.spec.template.spec.containers[0].image,state:{terminated:{exitCode:0}}}]}};
 if(s.mode==='lost-ack')fail();out(s.job);
}else if(a.includes('namespace')){
 save();if(a.includes('kube-system'))process.stdout.write(JSON.stringify({metadata:{uid:'cluster'}}));else process.stdout.write(s.namespaceUID);
}else if(a.includes('bundles.trust.cert-manager.io')){save();process.stdout.write('bundle-uid|1|'+JSON.stringify(s.bundleSpec??{sources:[{secret:{name:'kodex-installation-ca',key:'tls.crt'}}],target:{namespaceSelector:{matchLabels:{'kubernetes.io/metadata.name':'kodex-system'}},configMap:{key:'ca.pem'}}}));
}else if(a.includes('configmap')){save();process.stdout.write('ca-uid|'+s.dependencyVersion+'|'+(s.caLabel??'control-plane-postgresql-ca')+'|'+JSON.stringify([{apiVersion:'trust.cert-manager.io/v1alpha1',kind:'Bundle',name:'control-plane-postgresql-ca',controller:true,uid:s.caOwner??'bundle-uid'}]));
}else if(a.some(x=>x.startsWith('jsonpath='))){save();process.stdout.write('dependency-'+a[a.indexOf('get')+1]+'|'+s.dependencyVersion+'|'+(s.applicationOwner??'kodex'));
}else if(a.includes('deployment'))out(s.deployment);
else if(a.includes('replicasets,pods')){
 const items=[{kind:'ReplicaSet',metadata:{uid:'rs',ownerReferences:[{kind:'Deployment',uid:'anchor',controller:true}]}}];
 for(let i=0;i<s.deployment.spec.replicas;i++)items.push({kind:'Pod',metadata:{uid:'cp-'+i,ownerReferences:[{kind:'ReplicaSet',uid:'rs',controller:true}]},
 spec:s.deployment.spec.template.spec,status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:[{name:'control-plane',ready:true,imageID:s.deployment.spec.template.spec.containers[0].image}]}});
 out({items});
}
else if(a.includes('pods'))out({items:s.pod?[s.pod]:[]});
else if(a.includes('jobs'))out({items:[...(s.job?[s.job]:[]),...(s.otherJobs??[])]});
else if(a.includes('logs')){save();process.stdout.write('goose: current version: '+(s.logVersion??'${version}')+'\\n');}
else if(a.includes('control-plane-migrate')){save();if(!a.includes('--ignore-not-found=true'))process.exit(1);}
else if(s.job)out(s.job);else{save();if(!a.includes('--ignore-not-found=true'))process.exit(1);}
`,{mode:0o700});
  const run=(phase,extra=[])=>spawnSync(process.execPath,[new URL('./control-plane-migration.mjs',import.meta.url).pathname,phase,
    '--context','k3d-kodex','--profile','outbox-index','--plan',plan,...(phase==='plan'?['--source',repo,'--revision',revision,'--base-render',render,
      '--publication-plan',publicationPlan,'--publication-evidence',publicationEvidence]:['--evidence',evidence]),
    ...(['apply','stop','resume'].includes(phase)?['--confirm','APPLY-STAGING-CP-MIGRATION']:[]),...extra],{encoding:'utf8',timeout:15000,env:{...process.env,PATH:`${dir}:${process.env.PATH}`}});
  return {run,plan,evidence,publicationPlan,publicationEvidence,repo,read:()=>read(state),edit:fn=>{const value=read(state);fn(value);write(state,value);}};
}
const pass = (f,phase,status='PASS') => {const result=f.run(phase);assert.equal(result.status,0,result.stderr);assert.equal(JSON.parse(result.stdout).status,status);return result;};
test('public immutable CLI stop → exact migration → resume preserves old image and never repeats effects',t=>{
  const f=cli(t);f.edit(s=>s.deployment.spec.template.spec.containers[0].image=`registry.local.kodex/kodex/control-plane@sha256:${'e'.repeat(64)}`);
  pass(f,'plan','PLANNED');pass(f,'inspect','CREATE_NOT_ATTEMPTED');pass(f,'stop','STOPPED');
  for(const phase of ['apply','inspect','apply'])pass(f,phase);
  pass(f,'resume','RESUMED');pass(f,'resume','RESUMED');pass(f,'inspect');
  const state=f.read();assert.equal(state.calls.filter(a=>a.includes('create')).length,1);assert.equal(state.calls.filter(a=>a.includes('patch')).length,2);
  assert.equal(state.deployment.spec.replicas,2);assert.equal(state.deployment.spec.template.spec.containers[0].image,read(f.plan).anchor.previousImage);
  assert.notEqual(state.deployment.spec.template.spec.containers[0].image,state.job.spec.template.spec.containers[0].image);
  assert.notEqual(f.run('stop').status,0);
  for(const args of state.calls.filter(a=>a.includes('secret')))assert.match(args.at(-1),/^jsonpath=\{\.metadata\.uid\}/);
  assert.equal(JSON.stringify(read(f.plan)).includes('stringData'),false);
});
test('public immutable CLI lost create ACK recovers readback without a second Job',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'stop','STOPPED');f.edit(s=>s.mode='lost-ack');assert.notEqual(f.run('apply').status,0);
  pass(f,'inspect');pass(f,'apply');pass(f,'resume','RESUMED');
  assert.equal(f.read().calls.filter(a=>a.includes('create')).length,1);assert.match(readFileSync(f.evidence,'utf8'),/UNKNOWN/);
});
for(const step of ['stop','resume'])test(`public immutable CLI lost ${step} ACK is recovered without repeating PATCH`,t=>{
  const f=cli(t);pass(f,'plan','PLANNED');
  if(step==='resume'){pass(f,'stop','STOPPED');pass(f,'apply');}
  f.edit(s=>s.mode=step+'-lost-ack');assert.notEqual(f.run(step).status,0);
  pass(f,'inspect',step==='stop'?'STOPPED_NOT_CREATED':'PASS');
  pass(f,step,step==='stop'?'STOPPED':'RESUMED');
  if(step==='stop'){pass(f,'apply');pass(f,'resume','RESUMED');}
  assert.equal(f.read().calls.filter(a=>a.includes('patch')).length,2);
});
test('CAS rejection requires inspect before same-plan retry; foreign reservation cannot resume',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');f.edit(s=>s.mode='cas');assert.notEqual(f.run('stop').status,0);
  assert.notEqual(f.run('resume').status,0);
  assert.notEqual(f.run('stop').status,0);assert.equal(f.read().calls.filter(a=>a.includes('patch')).length,1);
  pass(f,'inspect','CREATE_NOT_ATTEMPTED');f.edit(s=>s.mode='');pass(f,'stop','STOPPED');
  f.edit(s=>s.deployment.metadata.annotations={[annotation]:'foreign'});assert.notEqual(f.run('resume').status,0);
  assert.equal(f.read().calls.filter(a=>a.includes('create')).length,0);
});
test('foreign Job UID, wrong imageID and wrong goose proof are never PASS or create retries',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'stop','STOPPED');pass(f,'apply');
  f.edit(s=>s.job.metadata.uid='foreign');assert.notEqual(f.run('inspect').status,0);
  f.edit(s=>{s.job.metadata.uid='created';s.pod.status.containerStatuses[0].imageID=`sha256:${'e'.repeat(64)}`;});
  let result=f.run('apply');assert.notEqual(result.status,0);assert.equal(JSON.parse(result.stdout).status,'READBACK_UNCONFIRMED');
  f.edit(s=>{s.pod.status.containerStatuses[0].imageID=s.job.spec.template.spec.containers[0].image;s.logVersion='20261025000101';});
  result=f.run('inspect');assert.notEqual(result.status,0);assert.equal(JSON.parse(result.stdout).status,'READBACK_UNCONFIRMED');
  assert.equal(f.read().calls.filter(a=>a.includes('create')).length,1);pass(f,'resume','RESUMED');
});
for(const mutate of [s=>s.deployment.metadata.uid='replacement',s=>s.deployment.metadata.resourceVersion='9',
  s=>s.dependencyVersion='2',s=>s.namespaceUID='replacement',s=>s.otherJobs=[{status:{active:1}}]]) {
  test('immutable stop rejects metadata/namespace/dependency/active Job drift before any write',t=>{
    const f=cli(t);pass(f,'plan','PLANNED');f.edit(mutate);assert.notEqual(f.run('stop').status,0);
    assert.equal(f.read().calls.filter(a=>a.includes('create')||a.includes('patch')).length,0);
  });
}
test('publication wrong scope rejects planning before reservation',t=>{
  const f=cli(t), p=read(f.publicationPlan);p.namespaceUID='foreign';write(f.publicationPlan,p);
  assert.notEqual(f.run('plan').status,0);assert.equal(f.read().calls.filter(a=>a.includes('create')||a.includes('patch')).length,0);
});
test('CA accepts actual trust-manager ownership without an application label and pins the Bundle source',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');
  const ca=read(f.plan).dependencies.find(item=>item.kind==='configmap');assert.equal(ca.bundle.uid,'bundle-uid');
  for(const mutate of [s=>s.caOwner='foreign',s=>s.caLabel='foreign',s=>s.applicationOwner='foreign',
    s=>s.bundleSpec={sources:[{secret:{name:'foreign',key:'tls.crt'}}],target:{configMap:{key:'ca.pem'}}}]) {
    f.edit(mutate);assert.notEqual(f.run('stop').status,0);
    f.edit(s=>{delete s.caOwner;delete s.caLabel;delete s.applicationOwner;delete s.bundleSpec;});
  }
  assert.equal(f.read().calls.filter(a=>a.includes('patch')||a.includes('create')).length,0);
});
test('migration plan refuses an incomplete or non-stable CP rollout before any write',t=>{
  const f=cli(t);f.edit(s=>s.deployment.status.updatedReplicas=1);assert.notEqual(f.run('plan').status,0);
  f.edit(s=>{s.deployment.status.updatedReplicas=2;s.deployment.metadata.labels['kodex.dev/local-profile']='hot-reload';});
  assert.notEqual(f.run('plan').status,0);assert.equal(f.read().calls.filter(a=>a.includes('patch')||a.includes('create')).length,0);
});
test('source drift after stop denies create but does not prevent restoring the exact old application',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'stop','STOPPED');
  writeFileSync(join(f.repo,'services/internal/control-plane/cmd/cli/migrations',sqlFile),'-- changed\n');
  assert.notEqual(f.run('apply').status,0);assert.equal(f.read().calls.filter(a=>a.includes('create')).length,0);
  pass(f,'resume','RESUMED');assert.notEqual(f.run('apply').status,0);
});
test('lost create ACK pins the first observed Job UID and refuses later replacements',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'stop','STOPPED');f.edit(s=>s.mode='lost-ack');assert.notEqual(f.run('apply').status,0);
  pass(f,'inspect');f.edit(s=>{s.job.metadata.uid='replacement';s.pod.metadata.ownerReferences[0].uid='replacement';});
  const result=f.run('inspect');assert.notEqual(result.status,0);assert.match(result.stderr,/MIGRATION_JOB_REPLACED/);
  assert.notEqual(f.run('resume').status,0);assert.equal(f.read().calls.filter(a=>a.includes('create')).length,1);
});
test('resume denies active or unjoined migrator; failed terminal migration still permits old-image restore',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'stop','STOPPED');pass(f,'apply');
  f.edit(s=>{s.job.status={active:1};s.pod.status.phase='Running';s.pod.status.containerStatuses[0].state={running:{}};});
  assert.notEqual(f.run('resume').status,0);assert.equal(f.read().deployment.spec.replicas,0);
  f.edit(s=>s.job.status={failed:1,conditions:[{type:'Failed',status:'True'}]});
  assert.notEqual(f.run('resume').status,0);assert.equal(f.read().deployment.spec.replicas,0);
  f.edit(s=>{s.pod.status.phase='Failed';s.pod.status.containerStatuses[0].state={terminated:{exitCode:1}};});
  pass(f,'resume','RESUMED');assert.equal(f.read().calls.filter(a=>a.includes('patch')).length,2);
});
test('bounded maintenance timeout leaves no implied success and readback recovery repeats no effect',async()=>{
  let ticks=0,calls=0;
  const clock={now:()=>ticks,sleep:async ms=>{ticks+=ms;},budget:500};
  await assert.rejects(waitMigrationMaintenance(()=>{calls++;return false;},v=>v,'MIGRATION_CP_STOP_TIMEOUT',clock),/STOP_TIMEOUT/);
  assert.equal(calls,3);assert.equal(ticks,500);
  assert.equal(await waitMigrationMaintenance(()=>true,v=>v,'MIGRATION_CP_STOP_TIMEOUT',clock),true);
});
test('SIGKILL after stop permits read-only inspect; mutations remain fenced by the original journal lock',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');f.edit(s=>s.mode='stop-crash');
  const crashed=f.run('stop');assert.equal(crashed.signal,'SIGKILL');assert.equal(existsSync(`${f.evidence}.lock`),true);
  const observed=pass(f,'inspect','STOPPED_NOT_CREATED');assert.equal(JSON.parse(observed.stdout).evidenceReadOnly,true);
  assert.notEqual(f.run('apply').status,0);assert.equal(f.read().calls.filter(a=>a.includes('create')).length,0);
  const pid=Number(readFileSync(`${f.evidence}.lock`,'utf8').trim());assert.throws(()=>process.kill(pid,0),{code:'ESRCH'});
  // Только локальная оснастка имитирует явное operator recovery после доказанного завершения writer.
  unlinkSync(`${f.evidence}.lock`);pass(f,'inspect','STOPPED_NOT_CREATED');pass(f,'apply');pass(f,'resume','RESUMED');
  assert.equal(f.read().calls.filter(a=>a.includes('patch')).length,2);
});
