import assert from 'node:assert/strict';
import test from 'node:test';
import {mkdtempSync,writeFileSync,readFileSync,rmSync,mkdirSync} from 'node:fs';
import {execFileSync,spawnSync} from 'node:child_process';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {main as rotationMain,readCatalogAuthorityTerminal,verifyCatalogAuthorityJobPod} from './authority-rotation-transition.mjs';
import {main as projectionMain} from './control-plane-policy-projection.mjs';
import {fingerprint} from './scoped-release.mjs';
import {catalogPolicyTransition} from './authority-catalog-transition.mjs';
import {fixture,uid,prepareAdmission} from './fixtures/authority-catalog-synthetic.mjs';

function prerequisites(){
  const labels={'app.kubernetes.io/name':'internal-rpc-authority','app.kubernetes.io/component':'migrator'};
  const sa={kind:'ServiceAccount',metadata:{name:'internal-rpc-authority-migrator',labels},automountServiceAccountToken:false};
  const egress={kind:'NetworkPolicy',metadata:{name:'internal-rpc-authority-migrator'},spec:{podSelector:{matchLabels:labels},policyTypes:['Ingress','Egress'],ingress:[],egress:[
    {to:[{namespaceSelector:{matchLabels:{'kubernetes.io/metadata.name':'kube-system'}},podSelector:{matchLabels:{'k8s-app':'kube-dns'}}}],ports:[{port:53,protocol:'UDP'},{port:53,protocol:'TCP'}]},
    {to:[{podSelector:{matchLabels:{'app.kubernetes.io/name':'kodex-postgresql'}}}],ports:[{port:5432,protocol:'TCP'}]},
  ]}};
  const ingress={kind:'NetworkPolicy',metadata:{name:'internal-rpc-authority-postgresql-from-migrator'},spec:{podSelector:{matchLabels:{'app.kubernetes.io/name':'kodex-postgresql'}},policyTypes:['Ingress'],ingress:[{from:[{podSelector:{matchLabels:labels}}],ports:[{port:5432,protocol:'TCP'}]}]}};
  return {sa,egress,ingress};
}

function cliFixture(t,{revision='a'.repeat(40)}={}){
  const f=fixture(),directory=mkdtempSync(join(tmpdir(),'catalog-authority-cli-'));
  t.after(()=>rmSync(directory,{recursive:true,force:true}));
  const kubeconfig=join(directory,'kubeconfig'),profile=join(directory,'target.json'),output=join(directory,'plan.json');
  writeFileSync(kubeconfig,'synthetic-unused-private-fixture',{mode:0o600});
  writeFileSync(profile,JSON.stringify(f.profile),{mode:0o600});
  const calls=[],{sa,egress,ingress}=prerequisites(),jobs=new Map(),jobPods=new Map(),jobLogs=new Map(),losses=new Set(),effects=[];
  let jobSequence=30;
  const state={slices:{items:[]},isolationPods:{items:[]},completeStatus:false};
  const expectedPrefix=['--kubeconfig',kubeconfig,'--context',f.profile.context,'--request-timeout=30s'];
  const run=(command,args,input)=>{
    assert.equal(command,'kubectl');assert.deepEqual(args.slice(0,expectedPrefix.length),expectedPrefix);
    const tail=args.slice(expectedPrefix.length),argv=[];
    for(let i=0;i<tail.length;i++){if(tail[i]==='-n'){assert.equal(tail[++i],'kodex-system');}else argv.push(tail[i]);}
    calls.push(argv);
    if(argv[0]==='config')return f.profile.context;
    if(argv[0]==='auth')return 'yes';
    if(argv[0]==='get'){
      const [,kind,name]=argv;
      if(kind==='replicasets,pods,jobs')return '';
      if(kind==='endpointslices')return JSON.stringify(state.slices);
      if(kind==='pods'){
        const selector=argv[argv.indexOf('-l')+1];
        if(selector.startsWith('batch.kubernetes.io/job-name='))return JSON.stringify(jobPods.get(selector.split('=')[1])??{items:[]});
        return JSON.stringify(state.isolationPods);
      }
      if(kind==='job')return jobs.has(name)?JSON.stringify(jobs.get(name)):'';
      if(kind==='serviceaccount')return JSON.stringify(sa);
      if(kind==='networkpolicy')return JSON.stringify(name==='internal-rpc-authority-migrator'?egress:ingress);
      if(kind==='secret'||name==='internal-rpc-authority-postgresql-ca')return kind+'/'+name;
      const value=f.get(kind,name);if(!value&&argv.includes('--ignore-not-found'))return '';
      assert.ok(value,'unexpected synthetic GET');return JSON.stringify(value);
    }
    if(argv[0]==='create'&&argv.includes('--dry-run=server'))return input;
    if(argv[0]==='replace'){
      const desired=JSON.parse(input),key=(desired.kind==='ConfigMap'?'configmap':'deployment')+'/'+desired.metadata.name;
      const current=f.resources.get(key);assert.ok(current);
      assert.equal(current.metadata.uid,desired.metadata.uid);assert.equal(current.metadata.resourceVersion,desired.metadata.resourceVersion);
      effects.push(key);Object.assign(current,desired);current.metadata.resourceVersion=String(Number(current.metadata.resourceVersion)+1);
      const operation=desired.kind==='ConfigMap'?'registry':'publisher';
      if(losses.delete(operation))throw Error('SYNTHETIC_LOST_ACK');return JSON.stringify(current);
    }
    if(argv[0]==='create'){
      const desired=JSON.parse(input);
      if(desired.kind==='ConfigMap'){
        const key='configmap/'+desired.metadata.name;assert.equal(f.resources.has(key),false);
        desired.metadata.uid=uid(40);desired.metadata.resourceVersion='1';f.resources.set(key,desired);effects.push(key);
        if(losses.delete('projection'))throw Error('SYNTHETIC_LOST_ACK');return JSON.stringify(desired);
      }
      assert.equal(desired.kind,'Job');assert.equal(jobs.has(desired.metadata.name),false);
      desired.metadata.uid=uid(++jobSequence);desired.metadata.resourceVersion='1';
      const command=desired.spec.template.spec.containers[0].args[0];
      desired.status=command==='rotation-status'&&state.completeStatus?{succeeded:1}:{active:1};
      jobs.set(desired.metadata.name,desired);effects.push('job/'+desired.metadata.name);
      if(state.ownerStatus)jobLogs.set(desired.metadata.name,JSON.stringify(state.ownerStatus)+'\n');
      if(desired.status.succeeded===1)jobPods.set(desired.metadata.name,{items:[completedJobPod(desired)]});
      if(losses.delete('job'))throw Error('SYNTHETIC_LOST_ACK');return JSON.stringify(desired);
    }
    if(argv[0]==='patch'){
      assert.equal(argv[1],'deployment');assert.equal(argv[2],'control-plane');
      const operations=JSON.parse(readFileSync(argv[argv.indexOf('--patch-file')+1],'utf8'));
      for(const operation of operations){
        const keys=operation.path.slice(1).split('/');let parent=f.deployment;for(const key of keys.slice(0,-1))parent=parent[key];
        if(operation.op==='test')assert.deepEqual(parent[keys.at(-1)],operation.value);else parent[keys.at(-1)]=operation.value;
      }
      f.deployment.metadata.resourceVersion=String(Number(f.deployment.metadata.resourceVersion)+1);
      effects.push('patch/control-plane');if(losses.delete('pointer'))throw Error('SYNTHETIC_LOST_ACK');return 'patched';
    }
    if(argv[0]==='logs')return jobLogs.get(argv[1].replace('job/',''))??'';
    throw new Error('UNEXPECTED_SYNTHETIC_EFFECT');
  };
  const source=fileURLToPath(new URL('../..',import.meta.url)).replace(/\/$/,'');
  const args=()=>['plan','--policy-transition',catalogPolicyTransition,'--context',f.profile.context,
    '--kubeconfig',kubeconfig,'--portable-target',profile,'--source',source,'--revision',revision,'--action','status','--output',output];
  const kube=argv=>run('kubectl',[...expectedPrefix,...argv]);
  const execFileSync=(program,argv,options)=>run(program,argv,options?.input);
  return {...f,directory,kubeconfig,profilePath:profile,output,calls,args,source,revision,jobs,jobPods,jobLogs,losses,effects,state,kube,
    io:{run,execFileSync,inspectSource:()=>({revision})}};
}

function completedJobPod(job){
  const spec=structuredClone(job.spec.template.spec),statuses=containers=>containers.map(container=>({name:container.name,imageID:container.image,restartCount:0,state:{terminated:{exitCode:0}}}));
  return {apiVersion:'v1',kind:'Pod',metadata:{name:job.metadata.name+'-pod',uid:uid(100),namespace:'kodex-system',
    ownerReferences:[{kind:'Job',controller:true,name:job.metadata.name,uid:job.metadata.uid}]},spec,
    status:{phase:'Succeeded',containerStatuses:statuses(spec.containers),initContainerStatuses:statuses(spec.initContainers??[]),ephemeralContainerStatuses:statuses(spec.ephemeralContainers??[])}};
}

test('public portable status planner pins context/kubeconfig and uses only bounded reads plus server dry-run',async t=>{
  const f=cliFixture(t);
  await rotationMain(f.args(),f.io);
  const plan=JSON.parse(readFileSync(f.output,'utf8'));
  assert.equal(plan.kubeconfig,f.kubeconfig);
  assert.equal(plan.policyTransition.targetSHA256,f.target.transition.targetSHA256);
  assert.equal(plan.cliImage,f.publisher.spec.template.spec.containers[0].image);
  assert.equal(f.calls.filter(args=>args[0]==='create'&&args.includes('--dry-run=server')).length,1);
  assert.equal(f.calls.filter(args=>['patch','replace','delete'].includes(args[0])).length,0);
  assert.equal(f.calls.filter(args=>args[0]==='config').length,0);
});

test('public portable planner rejects changed installation/CP before dry-run and never falls back to ambient kubeconfig',async t=>{
  for(const mutate of [f=>{f.installation.data['profile.json']='foreign';},f=>{f.deployment.metadata.uid='00000000-0000-4000-8000-000000000099';},
    f=>{rmSync(f.kubeconfig);}]) {
    const f=cliFixture(t);mutate(f);
    await assert.rejects(rotationMain(f.args(),f.io));
    assert.equal(f.calls.filter(args=>['create','replace','patch','delete'].includes(args[0])).length,0);
  }
});

test('catalog portable scope cannot silently enable legacy77 on production labels',async t=>{
  const f=cliFixture(t),args=f.args();
  args.splice(args.indexOf('--policy-transition'),2);
  await assert.rejects(rotationMain(args,f.io),/EXACT_STAGING_CONTEXT_REQUIRED/);
  assert.equal(f.calls.length,0);
});

test('public106 rotation keeps registry/publisher/Job single across three lost ACKs and allows readers behind isolation',async t=>{
  const f=cliFixture(t),admission=prepareAdmission(f),args=f.args();args[args.indexOf('--action')+1]='rotate';args.push(...admission.flags);
  await rotationMain(args,f.io);
  const plan=JSON.parse(readFileSync(f.output,'utf8')),evidence=join(f.directory,'authority.jsonl');
  f.losses.add('registry');f.losses.add('publisher');f.losses.add('job');
  const apply=['apply','--kubeconfig',f.kubeconfig,'--context',f.profile.context,'--plan',f.output,'--evidence',evidence,'--confirm','APPLY-PORTABLE-CATALOG-AUTHORITY'];
  await rotationMain(apply,f.io);
  assert.equal(JSON.parse(f.registry.data['authority-policy.json']).policy_revision,106);
  assert.equal(f.effects.filter(key=>key.startsWith('configmap/')).length,1);
  assert.equal(f.effects.filter(key=>key.startsWith('deployment/')).length,1);
  assert.equal(f.effects.filter(key=>key.startsWith('job/')).length,1);
  assert.equal(readFileSync(evidence,'utf8').split('\n').filter(Boolean).map(JSON.parse).filter(row=>row.status==='UNKNOWN').length,3);
  f.api.spec.replicas=3;f.api.status.replicas=3;
  await rotationMain(['resume',...apply.slice(1,-2)],f.io);
  assert.equal(f.effects.length,3);
  assert.equal(plan.maintenance.proof.trafficPlanSHA256,admission.proof.trafficPlanSHA256);
});

test('public CP projection recovers both lost ACKs without a second create or pointer patch',async t=>{
  const f=cliFixture(t),admission=prepareAdmission(f);f.registry.data['authority-policy.json']=f.desired.raw;
  const output=join(f.directory,'projection-plan.json'),evidence=join(f.directory,'projection.jsonl');
  await projectionMain(['plan','--policy-transition',catalogPolicyTransition,'--context',f.profile.context,'--kubeconfig',f.kubeconfig,
    '--portable-target',f.profilePath,...admission.flags,'--output',output],f.io);
  f.losses.add('projection');f.losses.add('pointer');
  const flags=['--kubeconfig',f.kubeconfig,'--context',f.profile.context,'--plan',output,'--evidence',evidence];
  await projectionMain(['apply',...flags,'--confirm','APPLY-PORTABLE-CATALOG-PROJECTION'],f.io);
  await projectionMain(['resume',...flags],f.io);
  await projectionMain(['observe',...flags],f.io);
  assert.equal(f.effects.filter(key=>key.startsWith('configmap/control-plane-policy-projection-')).length,1);
  assert.equal(f.effects.filter(key=>key==='patch/control-plane').length,1);
  assert.equal(readFileSync(evidence,'utf8').split('\n').filter(Boolean).map(JSON.parse).filter(row=>row.status==='UNKNOWN').length,2);
});

async function terminalFixture(t,options){
  const f=cliFixture(t,options),admission=prepareAdmission(f),args=f.args();args[args.indexOf('--action')+1]='rotate';args.push(...admission.flags);
  await rotationMain(args,f.io);
  const evidence=join(f.directory,'authority.jsonl');
  await rotationMain(['apply','--kubeconfig',f.kubeconfig,'--context',f.profile.context,'--plan',f.output,'--evidence',evidence,'--confirm','APPLY-PORTABLE-CATALOG-AUTHORITY'],f.io);
  const plan=JSON.parse(readFileSync(f.output,'utf8')),now=Date.now();
  const ownerStatus={observedAt:new Date(now).toISOString(),operationId:plan.ownerOperationID,status:'RETIRED',operationStatus:'RETIRED',phase:'RETIRE',
    registryRevision:plan.registry.sourceRevision,registrySourceDigestSHA256:plan.registry.sourceDigestSHA256,baseRevision:1,baseDigestSHA256:'c'.repeat(64),
    sourceRevision:4,sourceDigestSHA256:'d'.repeat(64),intentId:uid(90),protocolVersion:2,expectedReadbackCount:16,actualReadbackCount:16,
    switchNotBefore:new Date(now-90000).toISOString(),previousNotAfter:new Date(now-5000).toISOString(),completedAt:new Date(now-1000).toISOString()};
  f.state.ownerStatus=ownerStatus;f.state.completeStatus=true;
  const freshOutput=join(f.directory,'fresh-plan.json'),freshEvidence=join(f.directory,'fresh.jsonl'),freshArgs=f.args();freshArgs[freshArgs.indexOf('--output')+1]=freshOutput;
  await rotationMain(freshArgs,f.io);
  await rotationMain(['apply','--kubeconfig',f.kubeconfig,'--context',f.profile.context,'--plan',freshOutput,'--evidence',freshEvidence,'--confirm','APPLY-PORTABLE-CATALOG-AUTHORITY'],f.io);
  const freshPlan=JSON.parse(readFileSync(freshOutput,'utf8')),freshJob=[...f.jobs.values()].find(job=>job.spec.template.spec.containers[0].args[0]==='rotation-status');
  const input={plan,freshPlan,events:readFileSync(evidence,'utf8').trim().split('\n').map(JSON.parse),
    freshEvents:readFileSync(freshEvidence,'utf8').trim().split('\n').map(JSON.parse),admissionProof:admission.proof,now};
  const read=()=>readCatalogAuthorityTerminal(input,f.kube,f.io);
  return {...f,input,read,ownerStatus,freshJob,admission,evidence,freshOutput,freshEvidence};
}

test('fresh terminal getter reads exact canonical status Job and owner RETIRED without effects',async t=>{
  const f=await terminalFixture(t),before=f.effects.length,proof=await f.read();
  assert.equal(proof.policyRevision,106);assert.equal(proof.operationID,f.input.plan.ownerOperationID);
  assert.equal(proof.expectedReadbackCount,16);assert.equal(proof.actualReadbackCount,16);
  assert.equal(proof.registryRevision,2);assert.equal(proof.snapshotRevision,4);
  assert.equal(proof.trafficPlanSHA256,f.admission.proof.trafficPlanSHA256);
  assert.equal(f.effects.length,before);assert.equal(Object.isFrozen(proof),true);
});

test('fresh terminal owner rejects stale/foreign/incomplete operation and duplicate log fields',async t=>{
  const f=await terminalFixture(t),name=f.freshJob.metadata.name,before=f.effects.length;
  for(const mutate of [value=>value.observedAt=new Date(f.input.now-30001).toISOString(),value=>value.operationId=uid(99),
    value=>value.status=value.operationStatus='WAITING_RETIRE',value=>value.expectedReadbackCount=15,value=>value.actualReadbackCount=15,
    value=>value.registryRevision=106,value=>value.registrySourceDigestSHA256='f'.repeat(64),value=>value.sourceRevision=2,
    value=>value.extra='forbidden']){
    const changed=structuredClone(f.ownerStatus);mutate(changed);f.jobLogs.set(name,JSON.stringify(changed)+'\n');
    await assert.rejects(f.read());
  }
  f.jobLogs.set(name,JSON.stringify(f.ownerStatus).replace('"status":"RETIRED"','"status":"WAITING_RETIRE","status":"RETIRED"')+'\n');
  await assert.rejects(f.read(),/PRIVATE_JSON_REJECTED/);
  assert.equal(f.effects.length,before);
});

test('fresh authority Job proof rejects extra containers, altered credentials/command and incomplete CRI status',async t=>{
  const f=await terminalFixture(t),job=f.freshJob,original=f.jobPods.get(job.metadata.name);
  for(const mutate of [pod=>pod.spec.containers.push({name:'debug',image:job.spec.template.spec.containers[0].image}),
    pod=>pod.spec.initContainers.push({name:'debug',image:job.spec.template.spec.containers[0].image}),
    pod=>pod.spec.ephemeralContainers=[{name:'debug'}],pod=>pod.spec.containers[0].command=['/bin/sh'],
    pod=>pod.spec.volumes[0].secret={secretName:'foreign'},pod=>pod.spec.serviceAccountName='foreign',pod=>pod.spec.hostPID=true,
    pod=>pod.spec.runtimeClassName='foreign',pod=>pod.status.containerStatuses.push(pod.status.containerStatuses[0]),
    pod=>pod.status.containerStatuses[0].imageID='sha256:'+'f'.repeat(64),pod=>pod.status.containerStatuses[0].state={running:{}},
    pod=>pod.metadata.ownerReferences[0].uid=uid(99)]){
    const list=structuredClone(original);mutate(list.items[0]);
    assert.throws(()=>verifyCatalogAuthorityJobPod(job,list),/JOB_POD_REJECTED/);
  }
});

test('separate-process restore CLI opens only after fresh106 owner terminal and rejoins without another effect',async t=>{
  const revision=execFileSync('git',['rev-parse','HEAD'],{encoding:'utf8'}).trim();
  const f=await terminalFixture(t,{revision}),bin=join(f.directory,'bin'),statePath=join(f.directory,'synthetic-api.json');mkdirSync(bin,{mode:0o700});
  f.api.spec.replicas=3;f.api.status={observedGeneration:1,replicas:3,updatedReplicas:3,readyReplicas:3,availableReplicas:3};
  const state={resources:Object.fromEntries(f.resources),jobs:Object.fromEntries(f.jobs),jobPods:Object.fromEntries(f.jobPods),logs:Object.fromEntries(f.jobLogs),
    kubeconfig:f.kubeconfig,context:f.profile.context,calls:[],effects:0,lostACK:true};
  writeFileSync(statePath,JSON.stringify(state),{mode:0o600});
  const mock=`#!${process.execPath}
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const statePath=path.join(__dirname,'../synthetic-api.json'),s=JSON.parse(fs.readFileSync(statePath)),args=process.argv.slice(2);
const save=()=>fs.writeFileSync(statePath,JSON.stringify(s)),out=value=>{save();process.stdout.write(JSON.stringify(value));};
assert.equal(args[args.indexOf('--kubeconfig')+1],s.kubeconfig);assert.equal(args[args.indexOf('--context')+1],s.context);
assert.ok(args.includes('--request-timeout=25s'));s.calls.push(args);
const d=s.resources['deployment/control-api-gateway'];
const apiInventory=()=>{
 const rs={apiVersion:'apps/v1',kind:'ReplicaSet',metadata:{name:'api-rs',uid:'00000000-0000-4000-8000-000000000250',namespace:'kodex-system',ownerReferences:[{kind:'Deployment',name:d.metadata.name,uid:d.metadata.uid,controller:true}]}};
 return [rs,...Array.from({length:d.spec.replicas},(_,index)=>({apiVersion:'v1',kind:'Pod',metadata:{name:'api-reader-'+index,uid:'00000000-0000-4000-8000-00000000025'+(index+1),namespace:'kodex-system',
  labels:{'app.kubernetes.io/name':'control-api-gateway'},ownerReferences:[{kind:'ReplicaSet',name:rs.metadata.name,uid:rs.metadata.uid,controller:true}]},spec:d.spec.template.spec,
  status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:d.spec.template.spec.containers.map(c=>({name:c.name,ready:true,imageID:c.image}))}}))];
};
const at=args.indexOf('get'),patch=args.indexOf('patch'),logs=args.indexOf('logs');
if(at>=0){
 const kind=args[at+1],name=args[at+2];
 if(kind==='replicasets,pods,jobs'){save();process.stdout.write(apiInventory().map(r=>[r.kind,r.metadata.name,r.metadata.uid,r.metadata.labels?.['app.kubernetes.io/name']??'',r.metadata.namespace,JSON.stringify(r.metadata.ownerReferences??[])].join('\\t')).join('\\n'));}
 else if(kind==='pod')out(apiInventory().find(r=>r.kind==='Pod'&&r.metadata.name===name));
 else if(kind==='endpointslices')out({items:[]});
 else if(kind==='pods'){const selector=args[args.indexOf('-l')+1];out(selector.startsWith('batch.kubernetes.io/job-name=')?s.jobPods[selector.split('=')[1]]:{items:[]});}
 else if(kind==='job')out(s.jobs[name]);else out(s.resources[kind+'/'+name]);
}else if(logs>=0){save();process.stdout.write(s.logs[args[logs+1].replace('job/','')]);}
else if(patch>=0){
 assert.equal(args[patch+1],'service');assert.equal(args[patch+2],'control-api-gateway');
 const value=s.resources['service/control-api-gateway'],operations=JSON.parse(fs.readFileSync(args[args.indexOf('--patch-file')+1]));
 for(const op of operations.filter(op=>op.op==='test')){let current=value;for(const key of op.path.slice(1).split('/'))current=current[key];assert.deepEqual(current,op.value);}
 if(args.includes('--dry-run=server')){save();process.stdout.write('dry-run');process.exit(0);}
 for(const op of operations.filter(op=>op.op==='add'||op.op==='replace')){const keys=op.path.slice(1).split('/');let parent=value;for(const key of keys.slice(0,-1))parent=parent[key];parent[keys.at(-1)]=op.value;}
 value.metadata.resourceVersion=String(Number(value.metadata.resourceVersion)+1);s.effects++;save();if(s.lostACK)process.exit(9);process.stdout.write('patched');
}else process.exit(10);
`;
  writeFileSync(join(bin,'kubectl'),mock,{mode:0o700});
  const restorePlan=join(f.directory,'restore-plan.json'),restoreEvidence=join(f.directory,'restore.jsonl');
  const flags=['--kubeconfig',f.kubeconfig,'--context',f.profile.context,'--closed-plan',f.admission.paths.planPath,'--closed-evidence',f.admission.paths.evidencePath,
    '--isolation-plan',f.admission.paths.trafficPlanPath,'--isolation-evidence',f.admission.paths.trafficEvidencePath,
    '--authority-plan',f.output,'--authority-evidence',f.evidence,'--authority-status-plan',f.freshOutput,'--authority-status-evidence',f.freshEvidence];
  const run=args=>spawnSync(process.execPath,[join(f.source,'tools/release/component-api-traffic.mjs'),...args],{encoding:'utf8',timeout:30000,
    env:{...process.env,PATH:bin+':'+process.env.PATH}});
  let result=run(['plan',...flags,'--target',f.profilePath,'--action','restore','--plan',restorePlan]);
  assert.equal(result.status,0,result.stderr);
  const plan=JSON.parse(readFileSync(restorePlan,'utf8'));
  result=run(['apply',...flags,'--plan',restorePlan,'--evidence',restoreEvidence,'--approved-plan-sha',fingerprint(plan)]);
  assert.notEqual(result.status,0,'synthetic lost ACK must remain UNKNOWN');
  let observed=JSON.parse(readFileSync(statePath,'utf8'));assert.equal(observed.effects,1);
  assert.deepEqual(observed.resources['service/control-api-gateway'].spec,f.admission.trafficPlan.beforeSpec);
  const priorLogs=observed.calls.filter(args=>args.includes('logs')).length;
  // Исторический RESTORED read path не требует нового owner query или повторного patch.
  result=run(['status',...flags,'--plan',restorePlan,'--evidence',restoreEvidence]);assert.equal(result.status,0,result.stderr);
  result=run(['apply',...flags,'--plan',restorePlan,'--evidence',restoreEvidence,'--approved-plan-sha',fingerprint(plan)]);assert.equal(result.status,0,result.stderr);
  observed=JSON.parse(readFileSync(statePath,'utf8'));assert.equal(observed.effects,1);assert.equal(observed.calls.filter(args=>args.includes('logs')).length,priorLogs);
});
