import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, unlinkSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { planControlPlaneMigration, migrationReadback } from './control-plane-migration.mjs';
import { fingerprint } from './scoped-release.mjs';
import { validateStableReceipt } from './stable-image-publication.mjs';
import { waitMigrationMaintenance, immutableMigrationProfile, validImmutableMigrationPostcondition, validProjectPurgeAuthorityMigrationPostcondition, validOwnerGateExpiryMigrationPostcondition, validRepositoryBundleMigrationPostcondition, validServerWorkflowMigrationPostcondition } from './control-plane-immutable-migration.mjs';

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
    spec:structuredClone(job.spec.template.spec),status:{phase:'Succeeded',initContainerStatuses:[{name:'wait-for-postgresql',restartCount:0,state:{terminated:{exitCode:0}}}],containerStatuses:[{
      name:'migrate',restartCount:0,imageID:`registry.local.kodex/kodex/control-plane@${digest}`,state:{terminated:{exitCode:0}}}]}};
  return {job,pod};
}
test('immutable builder uses canonical least-privilege boundary and exact published image without host source',()=>{
  const before = base(), snapshot = structuredClone(before), plan = makePlan(publication(),before), pod = plan.job.spec.template.spec;
  assert.deepEqual(before,snapshot);
  assert.equal(plan.job.metadata.name,`control-plane-migrate-${version}`);
  assert.equal(plan.job.spec.backoffLimit,0); assert.equal(plan.job.spec.activeDeadlineSeconds,300);
  assert.deepEqual(pod.initContainers,before.spec.template.spec.initContainers);assert.equal(pod.initContainers[0].volumeMounts,undefined); assert.equal(pod.automountServiceAccountToken,false);
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

// Runtime получает только явно readable synthetic source subtree; private outer directory остаётся0700 при любом inherited umask.
// Chmod ограничен свежесозданным source subtree ДОgit init: real worktree/credentials не затрагиваются.
function readableSourceFixture(directory) {
  chmodSync(directory,0o755);
  for(const item of readdirSync(directory,{withFileTypes:true})) {
    const path=join(directory,item.name);
    if(item.isDirectory())readableSourceFixture(path);
    else {assert.equal(item.isFile(),true,'fixture must not contain links');chmodSync(path,0o644);}
  }
}


// Fixture — metadata-only ответ fake PostgreSQL; native proof отдельно исполняет настоящий SQL.
function serverWorkflowProof() {
 return {readOnly:'on',database:'control_plane',gooseVersion:20261026001000,tables:2,runtimeRoles:2,runtimeLogins:1,
 tableOwners:true,runtimeRolesUnprivileged:true,runtimeRead:true,runtimeMutationDenied:true,runtimeColumnMutationDenied:true,
 runtimeGrantOptionDenied:true,publicPrivilegesRevoked:true,ownerWrite:true,authorizationFunction:true,contextsEmpty:true,
 serverWorkflowProfile:'SERVER_DAG_V1',schemaOwner:true,columns:true,constraints:true,indexes:true,triggers:true,
 functionSources:true,functionMetadata:true,functionACL:true,visibilitySource:true,retentionSource:true,runtimeColumnAccess:true,purgeGraphGuard:true,
 workflowColumns:9,workflowConstraints:6,workflowGuards:6,workflowHelpers:4,purgeNodes:105,purgeEdges:284,purgeBaselineEdges:283,
 purgeNodeFingerprint:'f51a0de25bd942a250997775617eeaee',purgeBaselineEdgeFingerprint:'be6e23d9e894687a69a19e9b5777ab09',purgeEdgeFingerprint:'6bf6ad76d8c7ae66e07f75ecf719fdd6'};
}

// Настоящий CLI с отдельным kubectl subprocess доказывает порядок intent/CAS/create и запрет повторов.
function cli(t, profile = 'outbox-index', sourceCase = '') {
  const config=immutableMigrationProfile(profile),version=config.version,sqlFile=config.file,annotation=config.annotation;
  const dir=mkdtempSync('/tmp/cp-immutable-migration-');chmodSync(dir,0o700);t.after(()=>rmSync(dir,{recursive:true,force:true}));
  const repo=join(dir,'source'), migrations=join(repo,'services/internal/control-plane/cmd/cli/migrations');
  mkdirSync(migrations,{recursive:true,mode:0o755});chmodSync(repo,0o755);
  for(const name of config.requiredMigrations??[sqlFile]) if(sourceCase!=='missing-chain'||name!==config.requiredMigrations[0]) writeFileSync(join(migrations,name),'-- Синтетический source-only migration fixture\n',{mode:0o644});
  if(sourceCase==='future') writeFileSync(join(migrations,'20261026001100_future_workflow.sql'),'-- Будущий неподтверждённый профиль\n',{mode:0o644});
  const bundleDirectory=join(repo,'deploy/k8s/base/platform-state');mkdirSync(bundleDirectory,{recursive:true,mode:0o755});
  writeFileSync(join(bundleDirectory,'ca-bundles.yaml'),readFileSync(new URL('../../deploy/k8s/base/platform-state/ca-bundles.yaml',import.meta.url)),{mode:0o644});
  const release=join(repo,'tools/release');mkdirSync(release,{recursive:true,mode:0o755});
  for(const file of [config.preconditionFile,...(config.postconditionFile?[config.postconditionFile]:[])]) writeFileSync(join(release,file),readFileSync(new URL('./'+file,import.meta.url)),{mode:0o644});
  readableSourceFixture(repo);
  const git=(...args)=>execFileSync('git',['-C',repo,...args],{stdio:['ignore','pipe','pipe']}).toString().trim();
  git('init','-q');git('remote','add','origin','https://github.com/codex-k8s/kodex.git');git('add','.');
  git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Оснастка');
  const revision=git('rev-parse','HEAD'), render=join(dir,'render.json');let plan=join(dir,'plan.json'), evidence=join(dir,'evidence.jsonl');
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
const fs=require('fs'),path=${JSON.stringify(state)},journalDirectory=${JSON.stringify(dir)},s=JSON.parse(fs.readFileSync(path)),a=process.argv.slice(2);
const journal=journalDirectory+'/'+(s.retry?'retry-evidence.jsonl':'evidence.jsonl');
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
 const events=fs.readFileSync(journal,'utf8').trim().split('\\n').map(JSON.parse);
 const job=JSON.parse(fs.readFileSync(0,'utf8')),check=job.spec.template.spec.containers[0].args[0]==='check';
 if(events.at(-1).type!==(check?'CHECK_CREATE_INTENT':'CREATE_INTENT'))fail();
 job.metadata.uid=check?'check-created'+(s.retry?'-2':''):s.retry?'created-attempt2':'created';
 job.status={succeeded:1,completionTime:new Date().toISOString(),conditions:[{type:'Complete',status:'True'}]};
 const pod={metadata:{uid:'pod-'+job.metadata.uid,namespace:'kodex-system',ownerReferences:[{kind:'Job',uid:job.metadata.uid,controller:true}]},spec:job.spec.template.spec,
 status:{phase:'Succeeded',initContainerStatuses:[{name:'wait-for-postgresql',restartCount:0,state:{terminated:{exitCode:0}}}],
 containerStatuses:[{name:'migrate',restartCount:0,imageID:job.spec.template.spec.containers[0].image,state:{terminated:{exitCode:0}}}]}};
 if(s.mode===(check?'check-fail':'main-fail')){job.status={failed:1,conditions:[{type:'Failed',status:'True'}]};pod.status.phase='Failed';pod.status.containerStatuses[0].state.terminated.exitCode=1;}
 if(check&&s.mode==='check-running'){job.status={active:1};pod.status.phase='Pending';pod.status.initContainerStatuses=[];pod.status.containerStatuses[0].state={waiting:{}};}
 if(check){s.checkJob=job;s.checkPod=pod;}else{s.job=job;s.pod=pod;}
 if(s.mode===(check?'check-lost-ack':'lost-ack'))fail();out(job);
}else if(a.includes('namespace')){
 save();if(a.includes('kube-system'))process.stdout.write(JSON.stringify({metadata:{uid:'cluster'}}));else process.stdout.write(s.namespaceUID);
}else if(a.includes('bundles.trust.cert-manager.io')){save();process.stdout.write('bundle-uid|1|'+JSON.stringify(s.bundleSpec??{sources:[{secret:{name:'kodex-installation-ca',key:'tls.crt'}}],target:{namespaceSelector:{matchLabels:{'kubernetes.io/metadata.name':'kodex-system'}},configMap:{key:'ca.pem'}}}));
}else if(a.includes('configmap')){save();process.stdout.write('ca-uid|'+s.dependencyVersion+'|'+(s.caLabel??'control-plane-postgresql-ca')+'|'+JSON.stringify([{apiVersion:'trust.cert-manager.io/v1alpha1',kind:'Bundle',name:'control-plane-postgresql-ca',controller:true,uid:s.caOwner??'bundle-uid'}]));
}else if(a.includes('exec')){const sql=fs.readFileSync(0,'utf8');if(!sql.includes('BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;')||!sql.includes('ROLLBACK;'))fail();if(sql.includes('serverWorkflowProfile')){out({...${JSON.stringify(serverWorkflowProof())},...s.postcondition});}else if(sql.includes('runtimeColumnMutationDenied')&&!sql.includes('activeRuntimeRuns')){out({readOnly:'on',database:'control_plane',gooseVersion:20261026000200,tables:2,runtimeRoles:2,runtimeLogins:1,tableOwners:true,runtimeRolesUnprivileged:true,runtimeRead:true,runtimeMutationDenied:true,runtimeColumnMutationDenied:true,runtimeGrantOptionDenied:true,publicPrivilegesRevoked:true,ownerWrite:true,authorizationFunction:true,contextsEmpty:true,...s.postcondition});}else if(sql.includes('expiryColumn')){out({readOnly:'on',database:'control_plane',gooseVersion:20261026000100,tableOwner:'control_plane_owner',expiryColumn:true,stateConstraint:true,resolutionConstraint:true,expiryConstraint:true,dueIndex:true,immutableGuard:true,runtimeExecute:true,publicExecuteRevoked:true,...s.postcondition});}else if(sql.includes('noBodyColumns')){out({readOnly:'on',database:'control_plane',gooseVersion:20261025000200,tableOwner:'control_plane_owner',noBodyColumns:true,foreignKeys:13,deferredForeignKeys:13,guardEnabled:true,retentionConsumer:true,purgeGraphGuard:true,purgeNodes:105,purgeEdges:283,purgeNodeFingerprint:'f51a0de25bd942a250997775617eeaee',purgeEdgeFingerprint:'be6e23d9e894687a69a19e9b5777ab09',...s.postcondition});}else out({readOnly:'on',database:'control_plane',gooseVersion:${config.previousVersion},${config.emptyField}:true,oldClaimSessions:s.deployment.spec.replicas===0?0:2,activeRuntimeRuns:0,activeRunNodes:0,activeSessionTurns:0,claimedRuntimeLeases:0,openBuilds:0,pendingAdmissions:0,pendingPromotions:0,activeArchiveTasks:0,activeModelCatalogTasks:0,activeRenewalTasks:0,...s.precondition});
}else if(a.includes('kodex-postgresql-0')){save();process.stdout.write((s.pgUID??'pg-uid')+'|StatefulSet|kodex-postgresql|sts-uid|kodex-postgresql|Running|');
}else if(a.some(x=>x.startsWith('jsonpath='))){save();process.stdout.write('dependency-'+a[a.indexOf('get')+1]+'|'+s.dependencyVersion+'|'+(s.applicationOwner??'kodex'));
}else if(a.includes('deployment'))out(s.deployment);
else if(a.includes('replicasets,pods')){
 const items=[{kind:'ReplicaSet',metadata:{uid:'rs',ownerReferences:[{kind:'Deployment',uid:'anchor',controller:true}]}}];
 for(let i=0;i<s.deployment.spec.replicas;i++)items.push({kind:'Pod',metadata:{uid:'cp-'+i,ownerReferences:[{kind:'ReplicaSet',uid:'rs',controller:true}]},
 spec:s.deployment.spec.template.spec,status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:[{name:'control-plane',ready:true,imageID:s.deployment.spec.template.spec.containers[0].image}]}});
 out({items});
}
else if(a.includes('pods')){const selector=a[a.indexOf('-l')+1],name=selector.split('=')[1];
 const pod=name===s.checkJob?.metadata.name?s.checkPod:name===s.previousCheckJob?.metadata.name?s.previousCheckPod:name===s.previousJob?.metadata.name?s.previousPod:s.pod;out({items:pod?[pod]:[]});}
else if(a.includes('jobs'))out({items:[s.checkJob,s.previousCheckJob,s.previousJob,s.job,...(s.otherJobs??[])].filter(Boolean)});
else if(a.includes('logs')){save();const check=a.some(x=>x.startsWith('job/control-plane-migration-check-'));process.stdout.write(check?(s.checkLog??'PostgreSQL connection check completed')+'\\n':'goose: current version: '+(s.logVersion??'${version}')+'\\n');}
else if(a.includes('job')){const name=a[a.indexOf('job')+1];
 const job=name===s.checkJob?.metadata.name?s.checkJob:name===s.previousCheckJob?.metadata.name?s.previousCheckJob:name===s.previousJob?.metadata.name?s.previousJob:name===s.job?.metadata.name?s.job:undefined;
 if(job)out(job);else{save();if(!a.includes('--ignore-not-found=true'))process.exit(1);}}
else{save();process.exit(1);}
`,{mode:0o700});
  const run=(phase,extra=[])=>spawnSync(process.execPath,[new URL('./control-plane-migration.mjs',import.meta.url).pathname,phase,
    '--context','k3d-kodex','--profile',profile,'--plan',plan,...(phase==='plan'?['--source',repo,'--revision',revision,'--base-render',render,
      '--publication-plan',publicationPlan,'--publication-evidence',publicationEvidence]:['--evidence',evidence]),
    ...(['apply','stop','resume','check'].includes(phase)?['--confirm','APPLY-STAGING-CP-MIGRATION']:[]),...extra],{encoding:'utf8',timeout:15000,env:{...process.env,PATH:`${dir}:${process.env.PATH}`}});
  return {run,get plan(){return plan;},get evidence(){return evidence;},retry(){const previous={plan,evidence};plan=join(dir,'retry-plan.json');evidence=join(dir,'retry-evidence.jsonl');const value=read(state);value.retry=true;value.previousJob=value.job;value.previousPod=value.pod;value.previousCheckJob=value.checkJob;value.previousCheckPod=value.checkPod;delete value.job;delete value.pod;delete value.checkJob;delete value.checkPod;write(state,value);return ['--predecessor-plan',previous.plan,'--predecessor-evidence',previous.evidence];},publicationPlan,publicationEvidence,repo,read:()=>read(state),edit:fn=>{const value=read(state);fn(value);write(state,value);}};
}
const pass = (f,phase,status='PASS') => {const result=f.run(phase);assert.equal(result.status,0,result.stderr);assert.equal(JSON.parse(result.stdout).status,status);return result;};
const failed = (f,phase) => {const result=f.run(phase);assert.notEqual(result.status,0);assert.equal(JSON.parse(result.stdout).status,'FAIL');return result;};
test('public immutable CLI stop → exact migration → resume preserves old image and never repeats effects',t=>{
  const f=cli(t);f.edit(s=>s.deployment.spec.template.spec.containers[0].image=`registry.local.kodex/kodex/control-plane@sha256:${'e'.repeat(64)}`);
  pass(f,'plan','PLANNED');pass(f,'inspect','CREATE_NOT_ATTEMPTED');pass(f,'check');pass(f,'stop','STOPPED');
  for(const phase of ['apply','inspect','apply'])pass(f,phase);
  pass(f,'resume','RESUMED');pass(f,'resume','RESUMED');pass(f,'inspect');
  const state=f.read();assert.equal(state.calls.filter(a=>a.includes('create')).length,2);assert.equal(state.calls.filter(a=>a.includes('patch')).length,2);
  assert.equal(state.deployment.spec.replicas,2);assert.equal(state.deployment.spec.template.spec.containers[0].image,read(f.plan).anchor.previousImage);
  assert.notEqual(state.deployment.spec.template.spec.containers[0].image,state.job.spec.template.spec.containers[0].image);
  assert.notEqual(f.run('stop').status,0);
  for(const args of state.calls.filter(a=>a.includes('secret')))assert.match(args.at(-1),/^jsonpath=\{\.metadata\.uid\}/);
  assert.equal(JSON.stringify(read(f.plan)).includes('stringData'),false);
});
test('public immutable CLI lost create ACK recovers readback without a second Job',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'check');pass(f,'stop','STOPPED');f.edit(s=>s.mode='lost-ack');assert.notEqual(f.run('apply').status,0);
  pass(f,'inspect');pass(f,'apply');pass(f,'resume','RESUMED');
  assert.equal(f.read().calls.filter(a=>a.includes('create')).length,2);assert.match(readFileSync(f.evidence,'utf8'),/UNKNOWN/);
});
for(const step of ['stop','resume'])test(`public immutable CLI lost ${step} ACK is recovered without repeating PATCH`,t=>{
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'check');
  if(step==='resume'){pass(f,'stop','STOPPED');pass(f,'apply');}
  f.edit(s=>s.mode=step+'-lost-ack');assert.notEqual(f.run(step).status,0);
  pass(f,'inspect');
  pass(f,step,step==='stop'?'STOPPED':'RESUMED');
  if(step==='stop'){pass(f,'apply');pass(f,'resume','RESUMED');}
  assert.equal(f.read().calls.filter(a=>a.includes('patch')).length,2);
});
test('CAS rejection requires inspect before same-plan retry; foreign reservation cannot resume',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'check');f.edit(s=>s.mode='cas');assert.notEqual(f.run('stop').status,0);
  assert.notEqual(f.run('resume').status,0);
  assert.notEqual(f.run('stop').status,0);assert.equal(f.read().calls.filter(a=>a.includes('patch')).length,1);
  pass(f,'inspect');f.edit(s=>s.mode='');pass(f,'stop','STOPPED');
  f.edit(s=>s.deployment.metadata.annotations={[annotation]:'foreign'});assert.notEqual(f.run('resume').status,0);
  assert.equal(f.read().calls.filter(a=>a.includes('create')).length,1);
});
test('foreign Job UID, wrong imageID and wrong goose proof are never PASS or create retries',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'check');pass(f,'stop','STOPPED');pass(f,'apply');
  f.edit(s=>s.job.metadata.uid='foreign');assert.notEqual(f.run('inspect').status,0);
  f.edit(s=>{s.job.metadata.uid='created';s.pod.status.containerStatuses[0].imageID=`sha256:${'e'.repeat(64)}`;});
  let result=f.run('apply');assert.notEqual(result.status,0);assert.equal(JSON.parse(result.stdout).status,'READBACK_UNCONFIRMED');
  f.edit(s=>{s.pod.status.containerStatuses[0].imageID=s.job.spec.template.spec.containers[0].image;s.logVersion='20261025000101';});
  result=f.run('inspect');assert.notEqual(result.status,0);assert.equal(JSON.parse(result.stdout).status,'READBACK_UNCONFIRMED');
  assert.equal(f.read().calls.filter(a=>a.includes('create')).length,2);pass(f,'resume','RESUMED');
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
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'check');pass(f,'stop','STOPPED');
  writeFileSync(join(f.repo,'services/internal/control-plane/cmd/cli/migrations',sqlFile),'-- changed\n');
  assert.notEqual(f.run('apply').status,0);assert.equal(f.read().calls.filter(a=>a.includes('create')).length,1);
  pass(f,'resume','RESUMED');assert.notEqual(f.run('apply').status,0);
});
test('lost create ACK pins the first observed Job UID and refuses later replacements',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'check');pass(f,'stop','STOPPED');f.edit(s=>s.mode='lost-ack');assert.notEqual(f.run('apply').status,0);
  pass(f,'inspect');f.edit(s=>{s.job.metadata.uid='replacement';s.pod.metadata.ownerReferences[0].uid='replacement';});
  const result=f.run('inspect');assert.notEqual(result.status,0);assert.match(result.stderr,/MIGRATION_JOB_REPLACED/);
  assert.notEqual(f.run('resume').status,0);assert.equal(f.read().calls.filter(a=>a.includes('create')).length,2);
});
test('resume denies active or unjoined migrator; failed terminal migration still permits old-image restore',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'check');pass(f,'stop','STOPPED');pass(f,'apply');
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
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'check');f.edit(s=>s.mode='stop-crash');
  const crashed=f.run('stop');assert.equal(crashed.signal,'SIGKILL');assert.equal(existsSync(`${f.evidence}.lock`),true);
  const observed=pass(f,'inspect');assert.equal(JSON.parse(observed.stdout).evidenceReadOnly,true);
  assert.notEqual(f.run('apply').status,0);assert.equal(f.read().calls.filter(a=>a.includes('create')).length,1);
  const pid=Number(readFileSync(`${f.evidence}.lock`,'utf8').trim());assert.throws(()=>process.kill(pid,0),{code:'ESRCH'});
  // Только локальная оснастка имитирует явное operator recovery после доказанного завершения writer.
  unlinkSync(`${f.evidence}.lock`);pass(f,'inspect');pass(f,'apply');pass(f,'resume','RESUMED');
  assert.equal(f.read().calls.filter(a=>a.includes('patch')).length,2);
});
test('check is a separately fenced read-only execution before any CP stop',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');assert.notEqual(f.run('stop').status,0);
  assert.equal(f.read().calls.filter(a=>a.includes('patch')||a.includes('create')).length,0);
  pass(f,'check');pass(f,'check');const s=f.read();
  assert.equal(s.calls.filter(a=>a.includes('create')).length,1);assert.equal(s.calls.filter(a=>a.includes('patch')).length,0);
  assert.deepEqual(s.checkJob.spec.template.spec.containers[0].args,['check']);assert.equal(s.checkJob.spec.ttlSecondsAfterFinished,undefined);
  assert.equal(s.checkJob.spec.template.spec.initContainers[0].volumeMounts,undefined);
  assert.match(readFileSync(f.evidence,'utf8'),/CHECK_READBACK/);
});
for(const mode of ['check-fail','check-running','check-lost-ack'])test(`check ${mode} never stops CP or recreates an unknown Job`,t=>{
  const f=cli(t);pass(f,'plan','PLANNED');f.edit(s=>s.mode=mode);
  const r=f.run('check');if(mode==='check-running')assert.equal(JSON.parse(r.stdout).status,'RUNNING');else assert.notEqual(r.status,0);
  if(mode==='check-lost-ack')pass(f,'inspect');
  else assert.notEqual(f.run('stop').status,0);
  f.run('check');assert.equal(f.read().calls.filter(a=>a.includes('create')).length,1);
  assert.equal(f.read().calls.filter(a=>a.includes('patch')).length,0);
});
test('historical check PASS cannot bypass replaced UID, changed image/message/init, expiry or owner activity',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'check');
  for(const mutate of [s=>s.checkJob.metadata.uid='foreign',s=>s.checkPod.status.containerStatuses[0].imageID='sha256:foreign',
    s=>s.checkLog='goose: current version: 20261025000100',s=>s.checkPod.status.initContainerStatuses[0].state={waiting:{}},
    s=>s.checkJob.status.completionTime=new Date(Date.now()-301000).toISOString(),s=>s.precondition={activeRuntimeRuns:1},
    s=>s.precondition={indexAbsent:false},s=>s.precondition={gooseVersion:20261025000100}]) {
    const before=f.read();f.edit(mutate);assert.notEqual(f.run('stop').status,0);assert.equal(f.read().calls.filter(a=>a.includes('patch')).length,0);
    f.edit(s=>{for(const key of Object.keys(s))delete s[key];Object.assign(s,before);});
  }
});
test('after-stop readonly residual claim prevents DDL create while exact old CP can still resume',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'check');pass(f,'stop','STOPPED');f.edit(s=>s.precondition={oldClaimSessions:1});
  assert.notEqual(f.run('apply').status,0);assert.equal(f.read().job,undefined);pass(f,'resume','RESUMED');
});
function failedParent(t) {
  const f=cli(t);pass(f,'plan','PLANNED');pass(f,'check');pass(f,'stop','STOPPED');f.edit(s=>s.mode='main-fail');
  failed(f,'apply');pass(f,'resume','RESUMED');f.edit(s=>s.mode='');return f;
}
test('explicit attempt2 requires exact failed/resumed predecessor and preserves both original Jobs and receipts',t=>{
  const f=failedParent(t), oldPlan=read(f.plan), oldJournal=readFileSync(f.evidence,'utf8'), previous=f.retry();
  const r=f.run('plan',previous);assert.equal(r.status,0,r.stderr);
  const plan=read(f.plan);assert.equal(plan.attempt,2);assert.equal(plan.predecessor.planSHA256,fingerprint(oldPlan));
  assert.equal(plan.job.metadata.name,`control-plane-migrate-${version}-attempt2`);
  pass(f,'check');pass(f,'stop','STOPPED');pass(f,'apply');pass(f,'resume','RESUMED');
  const s=f.read();assert.equal(s.previousJob.metadata.uid,'created');assert.equal(s.previousJob.status.failed,1);
  assert.equal(s.job.metadata.uid,'created-attempt2');assert.equal(readFileSync(previous[3],'utf8'),oldJournal);
  assert.equal(s.calls.some(a=>a.includes('delete')),false);
  const third=f.retry();const denied=f.run('plan',third);assert.notEqual(denied.status,0);assert.match(denied.stderr,/PREDECESSOR_SCOPE_CHANGED/);
});
test('attempt2 rejects foreign, missing, active or unjoined predecessor and changed durable receipt',t=>{
  const f=failedParent(t), previous=f.retry(), baseline=f.read();
  for(const mutate of [s=>s.previousJob.metadata.uid='foreign',s=>{delete s.previousJob;},s=>s.previousJob.status={active:1},
    s=>s.previousPod.status.containerStatuses[0].state={running:{}}]) {
    f.edit(mutate);assert.notEqual(f.run('plan',previous).status,0);f.edit(s=>Object.assign(s,baseline));
  }
  const r=f.run('plan',previous);assert.equal(r.status,0,r.stderr);
  writeFileSync(previous[3],oldJournalWithNote(readFileSync(previous[3],'utf8')));
  assert.notEqual(f.run('check').status,0);assert.equal(f.read().checkJob,undefined);
});
function oldJournalWithNote(raw) {return raw+JSON.stringify({type:'INSPECT',at:new Date().toISOString()})+'\n';}
test('failed check permits only one explicit successor before any STOP or DDL effect',t=>{
  for(const profile of ['outbox-index','project-purge-authority']) {
    const f=cli(t,profile);pass(f,'plan','PLANNED');f.edit(s=>s.mode='check-fail');failed(f,'check');
    assert.equal(f.read().deployment.spec.replicas,2);assert.equal(f.read().job,undefined);
    const oldPlan=read(f.plan),oldJournal=readFileSync(f.evidence,'utf8');f.edit(s=>s.mode='');const previous=f.retry();
    const planned=f.run('plan',previous);assert.equal(planned.status,0,planned.stderr);
    const plan=read(f.plan);assert.equal(plan.attempt,2);assert.equal(plan.predecessor.stage,'CHECK');
    assert.equal(plan.predecessor.jobUID,'check-created');assert.equal(plan.predecessor.planSHA256,fingerprint(oldPlan));
    pass(f,'check');pass(f,'stop','STOPPED');pass(f,'apply');pass(f,'resume','RESUMED');
    assert.equal(f.read().previousCheckJob.metadata.uid,'check-created');assert.equal(f.read().previousCheckJob.status.failed,1);
    assert.equal(readFileSync(previous[3],'utf8'),oldJournal);assert.equal(f.read().calls.filter(a=>a.includes('create')).length,3);
    assert.equal(f.read().calls.some(a=>a.includes('delete')),false);
    assert.notEqual(f.run('plan',f.retry()).status,0);
  }
});
test('failed check successor rejects UID, spec, marker, active processes and unexpected DDL',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');f.edit(s=>s.mode='check-fail');failed(f,'check');f.edit(s=>s.mode='');
  const previous=f.retry(),baseline=f.read();
  for(const mutate of [s=>s.previousCheckJob.metadata.uid='foreign',s=>{delete s.previousCheckJob;},
    s=>s.previousCheckJob.status={active:1},s=>s.previousCheckJob.spec.template.spec.containers[0].args=['up'],
    s=>s.previousCheckPod.status.containerStatuses[0].state={running:{}},
    s=>s.previousCheckPod.metadata.ownerReferences[0].uid='foreign',
    s=>s.deployment.metadata.annotations={[immutableMigrationProfile('outbox-index').annotation]:'foreign'},
    s=>s.job={metadata:{name:read(previous[1]).job.metadata.name,uid:'unexpected-ddl'}}]) {
    f.edit(s=>{for(const key of Object.keys(s))delete s[key];Object.assign(s,structuredClone(baseline));mutate(s);});
    assert.notEqual(f.run('plan',previous).status,0);
  }
});
test('failed check with STOP intent cannot be reclassified as a harmless connection probe',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');f.edit(s=>s.mode='check-fail');failed(f,'check');
  writeFileSync(f.evidence,readFileSync(f.evidence,'utf8')+JSON.stringify({type:'STOP_INTENT'})+'\n');
  assert.notEqual(f.run('plan',f.retry()).status,0);
});
test('failed check successor pins parent journal and rejects an unconfirmed failure',t=>{
  const f=cli(t);pass(f,'plan','PLANNED');f.edit(s=>s.mode='check-fail');failed(f,'check');f.edit(s=>s.mode='');
  const old=readFileSync(f.evidence,'utf8'),previous=f.retry();
  writeFileSync(previous[3],old.split('\n').filter(line=>!line.includes('CHECK_READBACK')).join('\n'));
  assert.notEqual(f.run('plan',previous).status,0);writeFileSync(previous[3],old);
  const planned=f.run('plan',previous);assert.equal(planned.status,0,planned.stderr);
  writeFileSync(previous[3],oldJournalWithNote(old));assert.notEqual(f.run('check').status,0);
  assert.equal(f.read().checkJob,undefined);
});
test('new readback supports historical v1 attempt without weakening the native init check of v2',()=>{
  const plan=makePlan();plan.version=1;delete plan.attempt;delete plan.checkJob;delete plan.job.spec.template.spec.initContainers;
  const {job,pod}=execution(plan);delete pod.spec.initContainers;pod.status.initContainerStatuses=[];
  assert.equal(migrationReadback(job,plan,`goose: current version: ${version}`,[pod]).status,'PASS');
  const modern=makePlan(),exec=execution(modern);exec.pod.spec.initContainers[0].terminationMessagePath='/dev/termination-log';
  exec.pod.spec.initContainers[0].terminationMessagePolicy='File';exec.pod.spec.initContainers[0].imagePullPolicy='IfNotPresent';
  assert.equal(migrationReadback(exec.job,modern,`goose: current version: ${version}`,[exec.pod]).status,'PASS');
});


test('repository bundle closed profile uses the same canonical immutable transport and distinct source/schema pins',()=>{
 const config=immutableMigrationProfile('repository-bundle');
 const plan=planControlPlaneMigration(base(),source,()=>({revision}),'bundle-fixture','repository-bundle',publication());
 assert.equal(plan.profile,'repository-bundle');assert.equal(plan.migrationVersion,'20261025000200');
 assert.equal(plan.job.metadata.name,'control-plane-migrate-20261025000200');
 assert.equal(plan.checkJob.metadata.name,'control-plane-migration-check-20261025000200');
 assert.deepEqual(plan.job.spec.template.spec.containers,makePlan().job.spec.template.spec.containers);
 assert.deepEqual(plan.job.spec.template.spec.volumes,makePlan().job.spec.template.spec.volumes);
 assert.equal(config.previousVersion,20261025000100);
 assert.throws(()=>immutableMigrationProfile('repository-bundle-v2'),/PROFILE_INVALID/);
 const {job,pod}=execution(plan);job.metadata.annotations={[config.annotation]:fingerprint(plan)};
 assert.equal(migrationReadback(job,plan,'goose: current version: 20261025000200',[pod]).status,'PASS');
 assert.equal(migrationReadback(job,plan,'goose: current version: 20261025000100',[pod]).status,'READBACK_UNCONFIRMED');
});
test('repository bundle schema proof is closed on missing/foreign ownership, bytes, retention and purge guards',()=>{
 const proof={readOnly:'on',database:'control_plane',gooseVersion:20261025000200,tableOwner:'control_plane_owner',noBodyColumns:true,foreignKeys:13,deferredForeignKeys:13,guardEnabled:true,retentionConsumer:true,purgeGraphGuard:true,purgeNodes:105,purgeEdges:283,purgeNodeFingerprint:'f51a0de25bd942a250997775617eeaee',purgeEdgeFingerprint:'be6e23d9e894687a69a19e9b5777ab09'};
 assert.equal(validRepositoryBundleMigrationPostcondition(proof),true);
 assert.equal(validRepositoryBundleMigrationPostcondition({...proof,gooseVersion:20261026000100}),false);
 assert.equal(validRepositoryBundleMigrationPostcondition({...proof,gooseVersion:20261026000100},20261026000100),true);
 assert.equal(validRepositoryBundleMigrationPostcondition({...proof,gooseVersion:20261027000100},20261027000100),false);
 for(const key of Object.keys(proof)){const missing={...proof};delete missing[key];assert.equal(validRepositoryBundleMigrationPostcondition(missing),false,key);}
 for(const [key,value] of Object.entries({gooseVersion:20261025000100,tableOwner:'postgres',noBodyColumns:false,foreignKeys:12,deferredForeignKeys:12,guardEnabled:false,retentionConsumer:false,purgeGraphGuard:false,purgeNodes:104,purgeEdges:289,purgeNodeFingerprint:'0'.repeat(32),purgeEdgeFingerprint:'0'.repeat(32)}))assert.equal(validRepositoryBundleMigrationPostcondition({...proof,[key]:value}),false,key);
});
test('repository bundle actual CLI requires schema proof and resumes old CP after known terminal failed proof',t=>{
 const f=cli(t,'repository-bundle');pass(f,'plan','PLANNED');pass(f,'check');pass(f,'stop','STOPPED');
 f.edit(s=>s.postcondition={retentionConsumer:false});const bad=f.run('apply');assert.notEqual(bad.status,0);assert.match(bad.stderr,/POSTCONDITION_FAILED/);
 assert.equal(f.read().calls.filter(a=>a.includes('create')).length,2);
 f.edit(s=>delete s.postcondition);const good=pass(f,'inspect');assert.equal(JSON.parse(good.stdout).schemaPostcondition,'PASS');
 pass(f,'resume','RESUMED');assert.equal(f.read().deployment.spec.replicas,2);
 assert.equal(f.read().calls.filter(a=>a.includes('create')).length,2);
});

test('corrected postcondition revalidation is read-only and binds resumed execution, source and historical journal',t=>{
 const f=cli(t,'repository-bundle');pass(f,'plan','PLANNED');pass(f,'check');pass(f,'stop','STOPPED');
 f.edit(s=>s.postcondition={purgeGraphGuard:false});assert.notEqual(f.run('apply').status,0);
 const git=(...args)=>execFileSync('git',['-C',f.repo,...args],{encoding:'utf8',stdio:['ignore','pipe','pipe']}).trim();
 const oldRevision=git('rev-parse','HEAD');
 assert.notEqual(f.run('inspect',['--revalidate-postcondition-revision',oldRevision]).status,0);
 pass(f,'resume','RESUMED');
 const sql=join(f.repo,'tools/release/repository-bundle-migration-postcondition.sql');
 writeFileSync(sql,readFileSync(sql,'utf8')+'\n-- Исправленная проверка в отдельной ревизии.\n',{mode:0o644});
 git('add','.');git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Исправить checker');
 const revision=git('rev-parse','HEAD'),args=['--revalidate-postcondition-revision',revision];
 const original=f.run('inspect');assert.notEqual(original.status,0);assert.match(original.stderr,/POSTCONDITION_SOURCE_CHANGED/);
 const journal=readFileSync(f.evidence),planBytes=readFileSync(f.plan);
 assert.notEqual(f.run('inspect',['--revalidate-postcondition-revision',oldRevision]).status,0);
 assert.notEqual(f.run('apply',args).status,0);
 assert.notEqual(f.run('inspect',args).status,0); // Исправленный текст не отменяет FAIL фактического proof.
 f.edit(s=>delete s.postcondition);
 const result=f.run('inspect',args);assert.equal(result.status,0,result.stderr);const proof=JSON.parse(result.stdout);
 assert.equal(proof.status,'PASS');assert.equal(proof.evidenceReadOnly,true);assert.equal(proof.schemaPostcondition,'PASS');
 assert.equal(proof.revalidation.sourceRevision,revision);assert.equal(proof.revalidation.originalPlanSHA256,fingerprint(read(f.plan)));
 assert.equal(proof.revalidation.originalSQLSHA256,read(f.plan).postconditionSQLSHA256);
 assert.notEqual(proof.revalidation.sqlSHA256,proof.revalidation.originalSQLSHA256);
 assert.equal(proof.revalidation.proof.purgeEdges,283);assert.equal(proof.revalidation.execution[0].podUID,'pod-created');
 f.edit(s=>s.deployment.status.readyReplicas=1);assert.notEqual(f.run('inspect',args).status,0);
 f.edit(s=>s.deployment.status.readyReplicas=2);
 f.edit(s=>s.pod.status.containerStatuses[0].imageID=`sha256:${'f'.repeat(64)}`);assert.notEqual(f.run('inspect',args).status,0);
 assert.deepEqual(readFileSync(f.evidence),journal);assert.deepEqual(readFileSync(f.plan),planBytes);
 assert.equal(f.read().calls.filter(a=>a.includes('create')).length,2);
 assert.equal(f.read().calls.filter(a=>a.includes('patch')).length,2);
 const migration=join(f.repo,'services/internal/control-plane/cmd/cli/migrations/20261025000200_repository_bundle_publications.sql');
 writeFileSync(migration,'-- Изменённый applied SQL запрещён.\n',{mode:0o644});git('add','.');
 git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Недопустимый applied SQL');
 const changed=f.run('inspect',['--revalidate-postcondition-revision',git('rev-parse','HEAD')]);
 assert.notEqual(changed.status,0);assert.match(changed.stderr,/REVALIDATION_SOURCE_INVALID/);
 assert.deepEqual(readFileSync(f.evidence),journal);
});


test('owner gate expiry profile binds exact successor schema and immutable migrator execution',()=>{
 const config=immutableMigrationProfile('owner-gate-expiry');
 const plan=planControlPlaneMigration(base(),source,()=>({revision}),'expiry-fixture','owner-gate-expiry',publication());
 assert.equal(plan.profile,'owner-gate-expiry');assert.equal(plan.migrationVersion,'20261026000100');
 assert.equal(plan.job.metadata.name,'control-plane-migrate-20261026000100');
 assert.equal(plan.checkJob.metadata.name,'control-plane-migration-check-20261026000100');
 assert.deepEqual(plan.job.spec.template.spec.containers,makePlan().job.spec.template.spec.containers);
 assert.deepEqual(plan.job.spec.template.spec.volumes,makePlan().job.spec.template.spec.volumes);
 assert.equal(config.previousVersion,20261025000200);
 const {job,pod}=execution(plan);job.metadata.annotations={[config.annotation]:fingerprint(plan)};
 assert.equal(migrationReadback(job,plan,'goose: current version: 20261026000100',[pod]).status,'PASS');
 assert.equal(migrationReadback(job,plan,'goose: current version: 20261025000200',[pod]).status,'READBACK_UNCONFIRMED');
});
test('owner gate expiry schema proof rejects omitted or invalid deadline guards',()=>{
 const proof={readOnly:'on',database:'control_plane',gooseVersion:20261026000100,tableOwner:'control_plane_owner',expiryColumn:true,stateConstraint:true,resolutionConstraint:true,expiryConstraint:true,dueIndex:true,immutableGuard:true,runtimeExecute:true,publicExecuteRevoked:true};
 assert.equal(validOwnerGateExpiryMigrationPostcondition(proof),true);
 for(const key of Object.keys(proof)){
   const missing={...proof};delete missing[key];assert.equal(validOwnerGateExpiryMigrationPostcondition(missing),false,key);
   assert.equal(validOwnerGateExpiryMigrationPostcondition({...proof,[key]:false}),false,key);
 }
});
test('owner gate expiry CLI requires schema proof and never repeats completed DDL after failed readback',t=>{
 const f=cli(t,'owner-gate-expiry');pass(f,'plan','PLANNED');pass(f,'check');pass(f,'stop','STOPPED');
 f.edit(s=>s.postcondition={immutableGuard:false});const bad=f.run('apply');assert.notEqual(bad.status,0);assert.match(bad.stderr,/POSTCONDITION_FAILED/);
 assert.equal(f.read().calls.filter(a=>a.includes('create')).length,2);
 f.edit(s=>delete s.postcondition);const good=pass(f,'inspect');assert.equal(JSON.parse(good.stdout).schemaPostcondition,'PASS');
 pass(f,'resume','RESUMED');assert.equal(f.read().deployment.spec.replicas,2);
 assert.equal(f.read().calls.filter(a=>a.includes('create')).length,2);
});

// Для ACL-профиля схема Job и guard неизвестного create остаются теми же.
test('purge authority profile pins exact migration and retains the immutable job boundary',()=>{
 const config=immutableMigrationProfile('project-purge-authority');
 const plan=planControlPlaneMigration(base(),source,()=>({revision}),'purge-acl-fixture','project-purge-authority',publication());
 assert.equal(config.previousVersion,20261026000100);assert.equal(plan.migrationVersion,'20261026000200');
 assert.equal(plan.job.metadata.name,'control-plane-migrate-20261026000200');
 assert.equal(plan.checkJob.metadata.name,'control-plane-migration-check-20261026000200');
 assert.deepEqual(plan.job.spec.template.spec,makePlan().job.spec.template.spec);
 const {job,pod}=execution(plan);job.metadata.annotations={[config.annotation]:fingerprint(plan)};
 assert.equal(migrationReadback(job,plan,'goose: current version: 20261026000200',[pod]).status,'PASS');
 assert.equal(migrationReadback(job,plan,'goose: current version: 20261026000100',[pod]).status,'READBACK_UNCONFIRMED');
});
test('purge authority proof rejects missing effective ACL, column grants, privileged members and a relaxed authorizer',()=>{
 const proof={readOnly:'on',database:'control_plane',gooseVersion:20261026000200,tables:2,runtimeRoles:2,runtimeLogins:1,
   tableOwners:true,runtimeRolesUnprivileged:true,runtimeRead:true,runtimeMutationDenied:true,runtimeColumnMutationDenied:true,
   runtimeGrantOptionDenied:true,publicPrivilegesRevoked:true,ownerWrite:true,authorizationFunction:true,contextsEmpty:true};
 assert.equal(validProjectPurgeAuthorityMigrationPostcondition(proof),true);
 assert.equal(validImmutableMigrationPostcondition('project-purge-authority',proof),true);
 assert.equal(validImmutableMigrationPostcondition('unknown',proof),false);
 for(const key of Object.keys(proof)){
   const missing={...proof};delete missing[key];assert.equal(validProjectPurgeAuthorityMigrationPostcondition(missing),false,key);
   assert.equal(validProjectPurgeAuthorityMigrationPostcondition({...proof,[key]:false}),false,key);
 }
 for(const value of [0,1,1.5,Infinity]) assert.equal(validProjectPurgeAuthorityMigrationPostcondition({...proof,runtimeRoles:value}),false);
 for(const value of [0,2,1.5,Infinity]) assert.equal(validProjectPurgeAuthorityMigrationPostcondition({...proof,runtimeLogins:value}),false);
});
test('purge authority CLI keeps failed postcondition closed and resumes without repeating DDL',t=>{
 const f=cli(t,'project-purge-authority');pass(f,'plan','PLANNED');pass(f,'check');pass(f,'stop','STOPPED');
 f.edit(s=>s.postcondition={runtimeColumnMutationDenied:false});const bad=f.run('apply');
 assert.notEqual(bad.status,0);assert.match(bad.stderr,/POSTCONDITION_FAILED/);
 assert.equal(f.read().calls.filter(a=>a.includes('create')).length,2);
 f.edit(s=>delete s.postcondition);const good=pass(f,'inspect');assert.equal(JSON.parse(good.stdout).schemaPostcondition,'PASS');
 pass(f,'resume','RESUMED');assert.equal(f.read().deployment.spec.replicas,2);
 assert.equal(f.read().calls.filter(a=>a.includes('create')).length,2);
});
test('expiry schema verification accepts only explicit approved successor versions',()=>{
 const proof={readOnly:'on',database:'control_plane',gooseVersion:20261026000200,tableOwner:'control_plane_owner',expiryColumn:true,
   stateConstraint:true,resolutionConstraint:true,expiryConstraint:true,dueIndex:true,immutableGuard:true,runtimeExecute:true,publicExecuteRevoked:true};
 assert.equal(validOwnerGateExpiryMigrationPostcondition(proof),false);
 assert.equal(validOwnerGateExpiryMigrationPostcondition(proof,20261026000200),true);
 assert.equal(validOwnerGateExpiryMigrationPostcondition({...proof,gooseVersion:20261026000300},20261026000300),false);
 assert.equal(validOwnerGateExpiryMigrationPostcondition({...proof,immutableGuard:false},20261026000200),false);
});

// Проверяются реальные public planner/readback и неизменная credential boundary, а не новая копия Job.
test('server workflow profile pins complete source range and retains canonical maintenance jobs',()=>{
 const config=immutableMigrationProfile('server-workflow');
 const plan=planControlPlaneMigration(base(),source,()=>({revision}),'workflow-fixture','server-workflow',publication());
 assert.equal(config.previousVersion,20261026000200);assert.equal(config.requiredMigrations.length,8);
 assert.throws(()=>config.requiredMigrations.push('foreign.sql'));
 assert.equal(plan.migrationVersion,'20261026001000');
 assert.equal(plan.job.metadata.name,'control-plane-migrate-20261026001000');
 assert.equal(plan.checkJob.metadata.name,'control-plane-migration-check-20261026001000');
 assert.deepEqual(plan.job.spec.template.spec,makePlan().job.spec.template.spec);
 const {job,pod}=execution(plan);job.metadata.annotations={[config.annotation]:fingerprint(plan)};
 assert.equal(migrationReadback(job,plan,'goose: current version: 20261026001000',[pod]).status,'PASS');
 assert.equal(migrationReadback(job,plan,'goose: current version: 20261026000200',[pod]).status,'READBACK_UNCONFIRMED');
});
test('server workflow proof requires schema authority every guard and exact purge graph; old profiles remain closed',()=>{
 const proof=serverWorkflowProof();assert.equal(validServerWorkflowMigrationPostcondition(proof),true);
 assert.equal(validImmutableMigrationPostcondition('server-workflow',proof),true);
 assert.equal(validProjectPurgeAuthorityMigrationPostcondition(proof),false);
 assert.equal(validRepositoryBundleMigrationPostcondition(proof,20261026001000),false);
 assert.equal(validOwnerGateExpiryMigrationPostcondition(proof,20261026001000),false);
 for(const key of Object.keys(proof)){
  const missing={...proof};delete missing[key];assert.equal(validServerWorkflowMigrationPostcondition(missing),false,key);
  assert.equal(validServerWorkflowMigrationPostcondition({...proof,[key]:false}),false,key);
 }
 for(const [key,value] of Object.entries({gooseVersion:20261026001100,workflowColumns:8,workflowGuards:4,workflowConstraints:4,
  workflowHelpers:3,purgeEdges:283,purgeBaselineEdges:284,purgeEdgeFingerprint:'a'.repeat(32),runtimeLogins:2})){
  assert.equal(validServerWorkflowMigrationPostcondition({...proof,[key]:value}),false,key);
 }
});
test('server workflow CLI stops checks restores exact CP and never replays DDL after a failed schema proof',t=>{
 const f=cli(t,'server-workflow');pass(f,'plan','PLANNED');pass(f,'check');pass(f,'stop','STOPPED');
 f.edit(s=>s.postcondition={functionSources:false});const bad=f.run('apply');
 assert.notEqual(bad.status,0);assert.match(bad.stderr,/POSTCONDITION_FAILED/);
 assert.equal(f.read().calls.filter(a=>a.includes('create')).length,2);
 f.edit(s=>delete s.postcondition);pass(f,'inspect');pass(f,'resume','RESUMED');
 assert.equal(f.read().deployment.spec.replicas,2);
 assert.equal(f.read().calls.filter(a=>a.includes('create')).length,2);
});
test('server workflow CLI rejects partial applied schema and unbounded future or missing source before effects',t=>{
 const f=cli(t,'server-workflow');f.edit(s=>s.precondition={serverWorkflowAbsent:false});
 assert.notEqual(f.run('plan').status,0);assert.equal(f.read().calls.filter(a=>a.includes('create')||a.includes('patch')).length,0);
 for(const sourceCase of ['future','missing-chain']){
  const candidate=cli(t,'server-workflow',sourceCase);const result=candidate.run('plan');
  assert.notEqual(result.status,0);assert.match(result.stderr,sourceCase==='future'?/MIGRATION_NOT_SOURCE_HEAD/:/MIGRATION_SOURCE_CHAIN_INVALID/);
  assert.equal(candidate.read().calls.filter(a=>a.includes('create')||a.includes('patch')).length,0);
 }
});
