import test from 'node:test';
import assert from 'node:assert/strict';
import {fingerprint} from './scoped-release.mjs';
import {execFileSync} from 'node:child_process';
import {mkdtemp,mkdir,writeFile,readFile,rm} from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {fileURLToPath} from 'node:url';
import {telegramResourceCreateKeys,telegramResourceCreateHeader,telegramResourceCreateRequests,telegramResourceCreateProjection,
 requireTelegramDormantInventory,requireTelegramCreateAdmission,planTelegramResourceCreate,telegramResourceCreateStep} from './telegram-resource-create.mjs';

const uid=n=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const metadata=name=>({name,namespace:'kodex-system',labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'production','kodex.dev/profile':'web-only'}});
function fixture(){
 const installation={apiVersion:'v1',kind:'ConfigMap',metadata:{...metadata('kodex-installation-profile'),uid:uid(3),labels:{...metadata('').labels,'app.kubernetes.io/managed-by':'kodex-install'}},data:{'source.sha':'a'.repeat(40)}};
 const target={schema:'kodex.telegram-resource-create-target/v1',sourceSHA:'b'.repeat(40),context:'k3d-kodex',clusterUID:uid(1),namespaceUID:uid(2),installationUID:uid(3),installationSHA256:fingerprint(installation.data),publisherUID:uid(4),authorityImage:'registry.local.kodex/kodex/internal-rpc-authority@sha256:'+'c'.repeat(64)};
 const publisher={apiVersion:'apps/v1',kind:'Deployment',metadata:{...metadata('internal-rpc-authority-publisher'),uid:uid(4)},spec:{template:{spec:{containers:[{name:'publisher',image:target.authorityImage}]}}}};
 const observed={context:target.context,clusterUID:target.clusterUID,namespaceUID:target.namespaceUID,namespace:{kind:'Namespace',metadata:{name:'kodex-system',uid:target.namespaceUID},status:{phase:'Active'}},installation,publisher};
 const resources=telegramResourceCreateKeys.map(identity=>{const [apiVersion,kind,name]=identity.split('|');return {apiVersion,kind,metadata:metadata(name),...(kind==='ConfigMap'?{data:{INTERACTION_GATEWAY_ALLOWED_HOSTS:''}}:kind==='ServiceAccount'?{}:{spec:{}})};});
 const deployment=resources.find(r=>r.kind==='Deployment');
 deployment.spec={replicas:3,selector:{matchLabels:{'app.kubernetes.io/name':'interaction-gateway'}},template:{metadata:{labels:{'app.kubernetes.io/name':'interaction-gateway'}},spec:{serviceAccountName:'interaction-gateway',containers:[{name:'interaction-gateway'},{name:'internal-rpc-authority-issuer'},{name:'platform-worker-grant-agent'}],initContainers:[{name:'internal-rpc-authority-socket-init'}]}}};
 resources.find(r=>r.kind==='Service').spec={type:'ClusterIP',selector:{'app.kubernetes.io/name':'interaction-gateway'},ports:[{name:'metrics',port:9090,targetPort:'metrics',protocol:'TCP'}]};
 const interactionImage='registry.local.kodex/kodex/interaction-gateway@sha256:'+'d'.repeat(64),stored=new Map(),state={effects:0,lostACK:false,extra:[],allocated:'10.43.1.2',gateDrift:false};
 const key=r=>`${r.kind}/${r.metadata.name}`,get=r=>structuredClone(stored.get(key(r))??null);
 const dryRun=request=>{const r=structuredClone(request);r.metadata.uid=uid(800);r.metadata.resourceVersion='1';r.metadata.creationTimestamp='2026-11-14T00:00:00Z';
  if(r.kind==='Service')Object.assign(r.spec,{clusterIP:state.allocated,clusterIPs:[state.allocated],ipFamilies:['IPv4'],ipFamilyPolicy:'SingleStack'});
  return r;};
 const create=request=>{assert.equal(get(request),null);const r=dryRun(request);r.metadata.uid=uid(100+state.effects);stored.set(key(r),r);state.effects++;if(state.lostACK)throw Error('LOST_ACK');return r;};
 const inventory=()=>({items:structuredClone(state.extra)}),observe=()=>{const r=structuredClone(observed);if(state.gateDrift)r.namespaceUID=uid(999);return r;};
 const plan=()=>planTelegramResourceCreate({target,observed:observe(),resources,interactionImage,get,dryRun,inventory,planPath:'/synthetic/plan.json',evidencePath:'/synthetic/evidence.jsonl',createID:uid(5)});
 const journal=plan=>({events:[{type:'HEADER',...telegramResourceCreateHeader(plan)}],append(e){this.events.push(e);}});
 const step=(phase,p,j,extra={})=>telegramResourceCreateStep({phase,plan:p,journal:j,observe,inventory,get,dryRun,create,...extra});
 return {target,observed,resources,interactionImage,stored,state,get,dryRun,create,inventory,observe,plan,journal,step};
}

test('fixed dormant create сохраняет server UID и allocation, никогда не запускает worker',async()=>{
 const f=fixture(),plan=f.plan(),journal=f.journal(plan);
 assert.equal(plan.objects.length,10);assert.equal(plan.intendedReplicas,3);assert.equal(plan.objects.at(-1).request.spec.replicas,0);
 const result=await f.step('apply',plan,journal);assert.equal(result.state,'CREATED_DORMANT');assert.equal(result.qualification,'NOT_ACTIVATED_NOT_INSTALLED');assert.equal(f.state.effects,10);
 const uids=result.resources.map(r=>r.uid);assert.equal(new Set(uids).size,10);
 f.state.allocated='10.43.1.3';assert.equal((await f.step('apply',plan,journal)).state,'CREATED_DORMANT');assert.equal(f.state.effects,10);
 assert.deepEqual((await f.step('status',plan,journal)).resources.map(r=>r.uid),uids);
});

test('lost ACK восстанавливается по exact nonce/spec без повторного create',async()=>{
 const f=fixture(),plan=f.plan(),journal=f.journal(plan);f.state.lostACK=true;
 await assert.rejects(f.step('apply',plan,journal),/OPERATION_FAILED_OR_UNKNOWN/);assert.equal(f.state.effects,1);
 const result=await f.step('status',plan,journal);assert.equal(result.state,'INCOMPLETE');assert.equal(result.resources[0].state,'CREATED_DORMANT');
 f.state.lostACK=false;assert.equal((await f.step('apply',plan,journal)).state,'CREATED_DORMANT');assert.equal(f.state.effects,10);assert.ok(journal.events.some(e=>e.type==='UNKNOWN'));
});

test('absent после INTENT либо удаление/replacement после readback не создаёт новый UID',async()=>{
 const f=fixture(),plan=f.plan(),journal=f.journal(plan);
 await assert.rejects(f.step('apply',plan,journal,{create:()=>{throw Error('TIMEOUT');}}),/UNKNOWN/);
 await assert.rejects(f.step('apply',plan,journal),/OUTCOME_UNKNOWN_OR_RESOURCE_REMOVED/);assert.equal(f.state.effects,0);
 const g=fixture(),p=g.plan(),j=g.journal(p);await g.step('apply',p,j);
 const first=p.objects[0].request,identity=`${first.kind}/${first.metadata.name}`;
 const old=g.stored.get(identity);g.stored.delete(identity);await assert.rejects(g.step('apply',p,j),/OUTCOME_UNKNOWN_OR_RESOURCE_REMOVED/);assert.equal(g.state.effects,10);
 old.metadata.uid=uid(999);g.stored.set(identity,old);await assert.rejects(g.step('status',p,j),/UID_OR_ALLOCATION_CHANGED/);
});

test('foreign existing resource нельзя усыновить ни при plan, ни после его записи',async()=>{
 const f=fixture();f.stored.set('ServiceAccount/interaction-gateway',{apiVersion:'v1',kind:'ServiceAccount',metadata:{...metadata('interaction-gateway'),uid:uid(999),resourceVersion:'1'}});
 assert.throws(f.plan,/FOREIGN_RESOURCE_PRESENT/);
 const g=fixture(),p=g.plan(),j=g.journal(p);const foreign=g.dryRun(p.objects[0].request);g.stored.set('ServiceAccount/interaction-gateway',foreign);
 await assert.rejects(g.step('apply',p,j),/FOREIGN_OR_DRIFTED_RESOURCE/);assert.equal(g.state.effects,0);
});

test('namespace/installation/publisher drift и admission change закрывают effect до INTENT',async()=>{
 for(const mutate of [f=>f.state.gateDrift=true,f=>f.observed.namespace.status.phase='Terminating',f=>f.observed.installation.data.extra='drift',f=>f.observed.publisher.spec.template.spec.containers[0].image+='foreign']){
  const f=fixture(),p=f.plan(),j=f.journal(p);mutate(f);await assert.rejects(f.step('apply',p,j),/TARGET_CHANGED/);assert.equal(f.state.effects,0);
 }
 const f=fixture(),p=f.plan(),j=f.journal(p);
 await assert.rejects(f.step('apply',p,j,{dryRun:r=>{const value=f.dryRun(r);value.metadata.labels.injected='foreign';return value;}}),/ADMISSION_CHANGED/);
 assert.equal(f.state.effects,0);assert.equal(j.events.length,1);
});

test('orphan/foreign/debug workloads запрещены, только exact owned ReplicaSet0 допускается',async()=>{
 const f=fixture(),p=f.plan(),j=f.journal(p);f.state.extra=[{kind:'ReplicaSet',metadata:{name:'interaction-gateway-old',uid:uid(71)},spec:{replicas:0},status:{replicas:0}}];
 await assert.rejects(f.step('apply',p,j),/ORPHAN_WORKLOAD/);assert.equal(f.state.effects,0);
 f.state.extra=[];await f.step('apply',p,j);const deployment=f.get(p.objects.at(-1).request);
 const rs={kind:'ReplicaSet',metadata:{name:'label-drift',generation:1,uid:uid(72),ownerReferences:[{kind:'Deployment',controller:true,uid:deployment.metadata.uid}]},spec:{replicas:0},status:{replicas:0,observedGeneration:1}};
 f.state.extra=[rs];assert.equal((await f.step('status',p,j)).state,'CREATED_DORMANT');
 rs.spec.replicas=1;await assert.rejects(f.step('status',p,j),/ORPHAN_WORKLOAD/);rs.spec.replicas=0;
 f.state.extra.push({kind:'Pod',metadata:{name:'interaction-gateway-debug'},spec:{serviceAccountName:'interaction-gateway'}});await assert.rejects(f.step('status',p,j),/ORPHAN_WORKLOAD/);
});

test('закрытые source objects/image/containers и service allocation не расширяются',()=>{
 const f=fixture();for(const mutate of [r=>r.pop(),r=>r.push(structuredClone(r[0])),r=>r.find(v=>v.kind==='Deployment').spec.template.spec.containers.push({name:'extra'}),r=>r[0].metadata.namespace='foreign']){
  const r=structuredClone(f.resources);mutate(r);assert.throws(()=>telegramResourceCreateRequests(r,f.target,uid(5),f.interactionImage));
 }
 const p=f.plan(),service=f.dryRun(p.objects.find(p=>p.request.kind==='Service').request);service.spec.externalIPs=['203.0.113.1'];assert.throws(()=>telegramResourceCreateProjection(service),/SERVICE_INVALID/);
 service.spec.externalIPs=[];service.spec.clusterIP='None';assert.throws(()=>telegramResourceCreateProjection(service),/ALLOCATION_INVALID/);
 assert.throws(()=>requireTelegramDormantInventory({items:[{kind:'Pod',metadata:{name:'foreign'},spec:{serviceAccountName:'interaction-gateway'}}]}),/ORPHAN_WORKLOAD/);
});


const cli=fileURLToPath(new URL('./telegram-resource-create.mjs',import.meta.url));
const backend=String.raw`#!${process.execPath}
const fs=require('node:fs'),path=require('node:path'),args=process.argv.slice(2),base=path.join(__dirname,'..');
const statePath=path.join(base,'state.json'),state=JSON.parse(fs.readFileSync(statePath,'utf8'));
const save=()=>fs.writeFileSync(statePath,JSON.stringify(state));
const output=value=>process.stdout.write(JSON.stringify(value)+'\n');
if(path.basename(process.argv[1])==='git'){
 if(args.join(' ')==='rev-parse HEAD'){console.log('b'.repeat(40));process.exit(0);}
 if(args.join(' ')==='status --porcelain --untracked-files=normal'){if(state.dirty)console.log(' M changed');process.exit(0);}
 throw Error('unexpected git call');
}
if(args[0]!=='--kubeconfig'||args[1]!==path.join(base,'kubeconfig')||args[2]!=='--context'||args[3]!=='k3d-kodex')throw Error('implicit target');
const command=args.slice(5);
if(command[0]==='kustomize'){
 if(command[1]!=='deploy/k8s/profiles/web-only')throw Error('foreign source');
 console.log(state.resources.map(r=>JSON.stringify(r)).join('\n---\n'));process.exit(0);
}
if(command[0]==='get'&&command[1]==='namespace'){
 output({apiVersion:'v1',kind:'Namespace',metadata:{name:command[2],uid:command[2]==='kube-system'?state.target.clusterUID:state.target.namespaceUID},status:{phase:'Active'}});process.exit(0);
}
if(command[0]!=='-n'||command[1]!=='kodex-system')throw Error('foreign namespace');
const call=command.slice(2);
if(call[0]==='get'){
 if(call[1]==='replicasets,pods'){output({items:state.extra});process.exit(0);}
 if(call[1]==='configmap'&&call[2]==='kodex-installation-profile'){const value=state.observed.installation;if(state.targetDrift)value.metadata.uid='00000000-0000-4000-8000-000000009999';output(value);process.exit(0);}
 if(call[1]==='deployment'&&call[2]==='internal-rpc-authority-publisher'){output(state.observed.publisher);process.exit(0);}
 const resource=state.stored[call[1]+'/'+call[2]];if(resource)output(resource);else if(!call.includes('--ignore-not-found'))process.exit(1);process.exit(0);
}
if(call[0]==='create'){
 if(!call.includes('--validate=strict'))throw Error('missing strict validation');
 const request=JSON.parse(fs.readFileSync(call[call.indexOf('-f')+1],'utf8')),key=request.kind.toLowerCase()+'/'+request.metadata.name;
 if(state.stored[key])throw Error('duplicate create');
 const value=structuredClone(request);Object.assign(value.metadata,{uid:'00000000-0000-4000-8000-'+String(100+state.effects).padStart(12,'0'),resourceVersion:'1',creationTimestamp:'2026-11-14T00:00:00Z'});
 if(value.kind==='Service')Object.assign(value.spec,{clusterIP:'10.43.1.2',clusterIPs:['10.43.1.2'],ipFamilies:['IPv4'],ipFamilyPolicy:'SingleStack'});
 if(!call.includes('--dry-run=server')){state.stored[key]=value;state.effects++;save();if(state.lostACK){process.stderr.write('PRIVATE_PROVIDER_OR_URL_MUST_NOT_ESCAPE');process.exit(1);}}
 output(value);process.exit(0);
}
throw Error('unrecognized operation');
`;

async function cliFixture(){
 const f=fixture(),directory=await mkdtemp(join(tmpdir(),'kodex-telegram-create-cli-'));
 const bin=join(directory,'bin');await mkdir(bin,{mode:0o700});
 for(const name of ['git','kubectl'])await writeFile(join(bin,name),backend,{mode:0o700});
 const state={target:f.target,observed:f.observed,resources:f.resources,stored:{},extra:[],effects:0};
 const statePath=join(directory,'state.json'),targetPath=join(directory,'target.json'),publicationPath=join(directory,'publication.json'),planPath=join(directory,'plan.json'),evidencePath=join(directory,'evidence.jsonl');
 const save=()=>writeFile(statePath,JSON.stringify(state),{mode:0o600});await save();
 await writeFile(targetPath,JSON.stringify(f.target)+'\n',{mode:0o600});await writeFile(join(directory,'kubeconfig'),'synthetic-not-a-real-credential',{mode:0o600});
 const receipt={schema:'kodex.stable-local-build/v1',component:'interaction-gateway',sourceSHA:'b'.repeat(40),contextSHA256:'a'.repeat(64),archiveSHA256:'b'.repeat(64),imageDigest:'sha256:'+'d'.repeat(64),publication:'NOT_RUN',admission:'NOT_RUN',nodePull:'NOT_RUN'};
 const image={component:receipt.component,sourceSHA:receipt.sourceSHA,digest:receipt.imageDigest,tag:'stable-'+receipt.sourceSHA+'-'+'d'.repeat(12)};
 const publicationPlan={schema:'kodex.stable-image-publication/v2',artifactProfile:'built',receipt,image,archiveSHA256:receipt.archiveSHA256,context:f.target.context,clusterUID:f.target.clusterUID,namespaceUID:f.target.namespaceUID};
 const publication={publicationPlan,publicationEvidence:[{schema:'kodex.stable-image-publication-attempt/v1',status:'INTENT',mode:'apply',planSHA256:fingerprint(publicationPlan),component:receipt.component,digest:receipt.imageDigest},{status:'PASS',phase:'REGISTRY_PUBLICATION_ONLY',nodePull:'NOT_RUN'}]};
 await writeFile(publicationPath,JSON.stringify(publication)+'\n',{mode:0o600});
 const run=(mode,extra=[])=>{
  const args=[cli,mode,'--kubeconfig',join(directory,'kubeconfig'),'--context','k3d-kodex','--publication',publicationPath,'--plan',planPath,'--evidence',evidencePath,...(mode==='plan'?['--target',targetPath]:[]),...extra];
  try{return {status:0,output:execFileSync(process.execPath,args,{encoding:'utf8',timeout:30000,env:{...process.env,PATH:bin+':'+process.env.PATH}})};}catch(error){return {status:error.status??1,output:String(error.stdout??'')+String(error.stderr??'')};}
 };
 return {directory,state,save,run,planPath,evidencePath,publicationPath,statePath};
}

test('real CLI synthetic backend держит explicit target, source render,10creates и immutable lostACK history',async()=>{
 const f=await cliFixture();
 try{
  const planned=f.run('plan');assert.equal(planned.status,0,planned.output);
  const plan=JSON.parse(await readFile(f.planPath,'utf8'));
  const denied=f.run('apply');assert.notEqual(denied.status,0);assert.match(denied.output,/EXACT_PLAN_REQUIRED/);
  const copied=join(f.directory,'copied-plan.json');await writeFile(copied,JSON.stringify(plan)+'\n',{mode:0o600});
  const env={...process.env,PATH:join(f.directory,'bin')+':'+process.env.PATH};
  for(const change of [['--plan',copied],['--evidence',join(f.directory,'other.jsonl')]]){
   const args=[cli,'apply','--kubeconfig',join(f.directory,'kubeconfig'),'--context','k3d-kodex','--publication',f.publicationPath,'--plan',f.planPath,'--evidence',f.evidencePath,'--approved-plan-sha',fingerprint(plan)];
   args[args.indexOf(change[0])+1]=change[1];
   assert.throws(()=>execFileSync(process.execPath,args,{encoding:'utf8',timeout:30000,env}),error=>String(error.stderr).includes('TELEGRAM_CREATE_JOURNAL_PATH_CHANGED'));
  }
  f.state.lostACK=true;await f.save();const failed=f.run('apply',['--approved-plan-sha',fingerprint(plan)]);assert.notEqual(failed.status,0);assert.match(failed.output,/OPERATION_FAILED_OR_UNKNOWN/);assert.doesNotMatch(failed.output,/PRIVATE_PROVIDER_OR_URL/);
  const observed=f.run('status');assert.equal(observed.status,0,observed.output);assert.equal(JSON.parse(observed.output).state,'INCOMPLETE');
  const state=JSON.parse(await readFile(f.statePath,'utf8'));assert.equal(state.effects,1);state.lostACK=false;await writeFile(f.statePath,JSON.stringify(state));
  const applied=f.run('apply',['--approved-plan-sha',fingerprint(plan)]);assert.equal(applied.status,0,applied.output);assert.equal(JSON.parse(applied.output).state,'CREATED_DORMANT');
  const readback=f.run('status');assert.equal(readback.status,0,readback.output);assert.equal(JSON.parse(readback.output).qualification,'NOT_ACTIVATED_NOT_INSTALLED');
  const final=JSON.parse(await readFile(f.statePath,'utf8'));assert.equal(final.effects,10);assert.equal(final.stored['deployment/interaction-gateway'].spec.replicas,0);
  assert.match(await readFile(f.evidencePath,'utf8'),/"type":"UNKNOWN"/);
 }finally{await rm(f.directory,{recursive:true,force:true});}
});

test('real CLI отказывает wrong publication target, dirty source, orphan и duplicate private JSON без create',async()=>{
 for(const scenario of ['publication-target','dirty','orphan','duplicate']){
  const f=await cliFixture();
  try{
   if(scenario==='publication-target'){
    const p=JSON.parse(await readFile(f.publicationPath,'utf8'));p.publicationPlan.clusterUID=uid(999);p.publicationEvidence[0].planSHA256=fingerprint(p.publicationPlan);await writeFile(f.publicationPath,JSON.stringify(p)+'\n');
   }else if(scenario==='dirty'){f.state.dirty=true;await f.save();}
   else if(scenario==='orphan'){f.state.extra=[{kind:'Pod',metadata:{name:'interaction-gateway-stale'},spec:{serviceAccountName:'interaction-gateway'}}];await f.save();}
   else{const p=await readFile(f.publicationPath,'utf8');await writeFile(f.publicationPath,'{"publicationPlan":{},'+p.slice(1));}
   const result=f.run('plan');assert.notEqual(result.status,0,scenario);assert.equal(JSON.parse(await readFile(f.statePath,'utf8')).effects,0);
  }finally{await rm(f.directory,{recursive:true,force:true});}
 }
});


test('admission defaults не скрывают новый sidecar/env/volume/endpoint или изменённый source',()=>{
 const f=fixture(),p=f.plan(),request=p.objects.at(-1).request,defaulted=structuredClone(request);
 Object.assign(defaulted.spec,{progressDeadlineSeconds:600});
 Object.assign(defaulted.spec.template.spec,{dnsPolicy:'ClusterFirst',restartPolicy:'Always',schedulerName:'default-scheduler'});
 for(const container of [...defaulted.spec.template.spec.containers,...defaulted.spec.template.spec.initContainers])Object.assign(container,{imagePullPolicy:'IfNotPresent',terminationMessagePath:'/dev/termination-log',terminationMessagePolicy:'File'});
 assert.doesNotThrow(()=>requireTelegramCreateAdmission(request,defaulted));
 for(const mutate of [r=>r.spec.template.spec.containers.push({name:'injected'}),r=>r.spec.template.spec.containers[0].env=[{name:'FOREIGN',value:'value'}],r=>r.spec.template.spec.volumes=[{name:'extra',hostPath:{path:'/host'}}],r=>r.spec.template.spec.containers[0].image='foreign',r=>r.spec.template.spec.hostNetwork=true]){
  const value=structuredClone(defaulted);mutate(value);assert.throws(()=>requireTelegramCreateAdmission(request,value),/EXPANDED_SOURCE/);
 }
 const live=f.dryRun(request);live.metadata.annotations['deployment.kubernetes.io/revision']='1';assert.deepEqual(telegramResourceCreateProjection(live).value,request);
 live.metadata.annotations['deployment.kubernetes.io/revision']='2';assert.throws(()=>telegramResourceCreateProjection(live),/REVISION_CHANGED/);
});
