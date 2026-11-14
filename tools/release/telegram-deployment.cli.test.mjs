import producerMetadata from './fixtures/telegram-producer-metadata.cjs';
import {telegramIntroductionResources} from './telegram-introduction-inventory.mjs';
import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync,spawnSync} from 'node:child_process';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,chmodSync,rmSync,copyFileSync} from 'node:fs';
import {join,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
import {componentCohortFixture,replaceComponentLock} from './fixtures/component-cohort-synthetic.mjs';
import {fingerprint} from './scoped-release.mjs';
import {telegramNotificationsCatalog} from './telegram-notifications-catalog.gen.mjs';
import {decodeTelegramResourceCreateSource,planTelegramResourceCreate,telegramResourceCreateStep,telegramResourceCreateHeader,telegramResourceCreateKeys} from './telegram-resource-create.mjs';
import {readTelegramIntroducedReceiver} from './telegram-introduced-receiver.mjs';
import {validateStableReceipt} from './stable-image-publication.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url)),uid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const execute=(command,args,input,maxBuffer=32<<20)=>execFileSync(command,args,{cwd:root,input,encoding:'utf8',timeout:30000,maxBuffer,
 env:{PATH:process.env.PATH,LANG:'C.UTF-8'},stdio:['pipe','pipe','pipe']});
const sourceResources=decodeTelegramResourceCreateSource(execute('kubectl',['--kubeconfig','/synthetic/unused','--context','synthetic',
 'kustomize','deploy/k8s/profiles/web-only']),execute);
const activities=['activeRuntimeLeases','activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions',
 'pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks','activeIntegrationInvocations','activeIntegrationConnectionTests',
 'activePersonalCredentialTests','activeRepositoryBundles','activeConfigurationSources','activeConfigurationWritebacks','activeInteractionDeliveries','activeBackupSessions','oldClaimSessions'];
const write=(path,value)=>writeFileSync(path,JSON.stringify(value)+'\n',{mode:0o600});
const read=path=>JSON.parse(readFileSync(path,'utf8'));

async function fixture(t){
 const directory=mkdtempSync('/tmp/kodex-telegram-deployment-');chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const source=join(directory,'source'),bin=join(directory,'bin');mkdirSync(source,{mode:0o755});mkdirSync(bin,{mode:0o700});
 for(const path of ['deploy/k8s/base/platform-state/ca-bundles.yaml','services/internal/control-plane/cmd/cli/migrations/20261114140000_telegram_project_subscriptions.sql',
  ...['telegram-notifications-migration-precondition.sql','telegram-notifications-migration-postcondition.sql','portable-component-migration-drain.sql'].map(name=>'tools/release/'+name)]){
  mkdirSync(dirname(join(source,path)),{recursive:true,mode:0o755});copyFileSync(join(root,path),join(source,path));chmodSync(join(source,path),0o644);
 }
 writeFileSync(join(source,'source-render.json'),JSON.stringify(sourceResources)+'\n',{mode:0o644});
 const git=(...args)=>execute('git',['-C',source,...args]).trim();git('init','-q');git('remote','add','origin','https://github.com/codex-k8s/kodex.git');git('add','.');
 git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Синтетическая закрытая source fixture');
 const revision=git('rev-parse','HEAD'),f=componentCohortFixture('telegram-notifications'),resources={};
 for(const [index,part] of f.value.components.entries()){
  const lock=JSON.parse(Buffer.from(part.componentLockRawBase64,'base64'));lock.source_sha=revision;replaceComponentLock(part,lock);part.nodeProof.sourceSHA=revision;
  const app=f.deployments[index];resources['deployment/'+app.metadata.name]=structuredClone(app);
 }
 const installation=f.observed.installation;resources['configmap/kodex-installation-profile']=installation;
 const labels={'app.kubernetes.io/part-of':'kodex','kodex.dev/profile':'web-only','kodex.dev/environment':'production'};
 const publisher={apiVersion:'apps/v1',kind:'Deployment',metadata:{name:'internal-rpc-authority-publisher',namespace:'kodex-system',uid:uid(500),labels},
  spec:{template:{spec:{containers:[{name:'publisher',image:'registry.local.kodex/kodex/internal-rpc-authority@sha256:'+'a'.repeat(64)}]}}}};
 resources['deployment/internal-rpc-authority-publisher']=publisher;
 const inputNames=new Set(['kodex-platform-endpoints','egress-gateway','egress-gateway-exact-runtime-paths','platform-postgresql-exact-clients',
  'control-plane-exact-runtime-paths','internal-rpc-authority-readback-attestor-exact-paths','internal-rpc-authority-restore-controller-exact-paths','internal-rpc-authority-publisher']);
 let counter=10000;for(const resource of sourceResources){
  if(!inputNames.has(resource.metadata.name))continue;
  if(!['ConfigMap','Service','NetworkPolicy','Role'].includes(resource.kind)||telegramResourceCreateKeys.includes(`${resource.apiVersion}|${resource.kind}|${resource.metadata.name}`)||resource.metadata.name.startsWith('egress-gateway-telegram-policy-'))continue;
  const copy=structuredClone(resource);Object.assign(copy.metadata,{uid:uid(counter++),resourceVersion:'1'});resources[copy.kind.toLowerCase()+'/'+copy.metadata.name]=copy;
 }
 resources['configmap/kodex-platform-endpoints'].data.publicOrigin='https://synthetic.example.invalid';
 const service=resources['service/egress-gateway'];Object.assign(service.spec,{clusterIP:'192.0.2.1',clusterIPs:['192.0.2.1'],ipFamilies:['IPv4'],ipFamilyPolicy:'SingleStack'});service.spec.ports=service.spec.ports.filter(p=>p.port!==8086);
 for(const resource of Object.values(resources).filter(r=>r.kind==='NetworkPolicy'))for(const rule of resource.spec.ingress??[]){
  if(rule.from){rule.from=rule.from.filter(peer=>peer.podSelector?.matchLabels?.['app.kubernetes.io/name']!=='interaction-gateway');
   for(const peer of rule.from)for(const expression of peer.podSelector?.matchExpressions??[])expression.values=expression.values?.filter(value=>value!=='interaction-gateway');}
 }
 const egress=resources['networkpolicy/egress-gateway-exact-runtime-paths'];egress.spec.ingress=egress.spec.ingress.filter(rule=>!rule.ports?.some(port=>port.port===8086));
 const role=resources['role/internal-rpc-authority-publisher'];role.rules[0].resourceNames=role.rules[0].resourceNames.filter(name=>!name.includes('interaction-gateway'));
 const kubeconfig=join(directory,'kubeconfig');writeFileSync(kubeconfig,'synthetic-unused',{mode:0o600});
 const createTarget={schema:'kodex.telegram-resource-create-target/v1',sourceSHA:revision,context:f.target.context,clusterUID:f.target.clusterUID,namespaceUID:f.target.namespaceUID,
  installationUID:installation.metadata.uid,installationSHA256:fingerprint(installation.data),publisherUID:publisher.metadata.uid,authorityImage:publisher.spec.template.spec.containers[0].image};
 const namespace={kind:'Namespace',metadata:{name:'kodex-system',uid:createTarget.namespaceUID},status:{phase:'Active'}};
 const observed={context:createTarget.context,clusterUID:createTarget.clusterUID,namespaceUID:createTarget.namespaceUID,namespace,installation,publisher};
 const get=request=>structuredClone(resources[request.kind.toLowerCase()+'/'+request.metadata.name]??null);
 delete resources['deployment/interaction-gateway'];
 const dryRun=request=>{const result=structuredClone(request);Object.assign(result.metadata,{uid:uid(800),resourceVersion:'1'});
  if(result.kind==='Service')Object.assign(result.spec,{clusterIP:'192.0.2.20',clusterIPs:['192.0.2.20'],ipFamilies:['IPv4'],ipFamilyPolicy:'SingleStack'});
  if(result.kind==='Deployment'){result.metadata.generation=1;result.status={observedGeneration:1,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};}return result;};
 const creator=planTelegramResourceCreate({target:createTarget,observed,resources:sourceResources,interactionImage:f.value.components.at(-1).expectedImageRef,get,dryRun,inventory:()=>({items:[]}),
  planPath:join(directory,'create-plan.json'),evidencePath:join(directory,'create-evidence.jsonl'),createID:uid(700)});
 const createJournal={events:[{type:'HEADER',...telegramResourceCreateHeader(creator)}],append(event){this.events.push(event);}};
 let created=0;await telegramResourceCreateStep({phase:'apply',plan:creator,journal:createJournal,observe:()=>observed,inventory:()=>({items:[]}),get,dryRun,
  create:request=>{const result=dryRun(request);result.metadata.uid=uid(801+created++);resources[result.kind.toLowerCase()+'/'+result.metadata.name]=result;return result;}});
 write(creator.planPath,creator);writeFileSync(creator.evidencePath,createJournal.events.map(e=>JSON.stringify(e)+'\n').join(''),{mode:0o600});
 const receipt={schema:'kodex.stable-local-build/v1',component:'interaction-gateway',sourceSHA:revision,contextSHA256:'a'.repeat(64),archiveSHA256:'b'.repeat(64),
  imageDigest:f.value.components.at(-1).nodeProof.imageRef.split('@')[1],publication:'NOT_RUN',admission:'NOT_RUN',nodePull:'NOT_RUN'};
 const publicationPlan={schema:'kodex.stable-image-publication/v2',artifactProfile:'built',receipt,image:validateStableReceipt(receipt),archiveSHA256:receipt.archiveSHA256,
  context:f.target.context,clusterUID:f.target.clusterUID,namespaceUID:f.target.namespaceUID};
 const publication={publicationPlan,publicationEvidence:[{schema:'kodex.stable-image-publication-attempt/v1',status:'INTENT',mode:'apply',planSHA256:fingerprint(publicationPlan),component:receipt.component,digest:receipt.imageDigest},
  {status:'PASS',phase:'REGISTRY_PUBLICATION_ONLY',nodePull:'NOT_RUN'}]},publicationPath=join(directory,'publication.json');write(publicationPath,publication);
 const introductionKube=args=>{const at=args.indexOf('get'),kind=args[at+1],name=args[at+2];
  if(kind==='namespace')return JSON.stringify(name==='kube-system'?{metadata:{uid:f.target.clusterUID}}:namespace);
  if(kind===telegramIntroductionResources)return producerMetadata(Object.values(resources),kind);if(kind==='replicasets,pods')return JSON.stringify({items:[]});return JSON.stringify(resources[kind+'/'+name]??null);};
 f.value.introducedReceiver=await readTelegramIntroducedReceiver({planPath:creator.planPath,evidencePath:creator.evidencePath,publicationPath,sourceSHA:revision,
  context:f.target.context,target:createTarget,publisherImage:createTarget.authorityImage},introductionKube,()=>sourceResources);
 f.target.resources.find(r=>r.name==='interaction-gateway').uid=f.value.introducedReceiver.deploymentUID;
 const plan=join(directory,'plan.json'),evidence=join(directory,'evidence.jsonl'),state=join(directory,'state.json'),component=join(directory,'component.json'),target=join(directory,'target.json'),render=join(directory,'render.json');
 write(component,f.value);write(target,f.target);
 const base=JSON.parse(execute('yq',['-o=json','.',join(root,'deploy/k8s/base/control-plane/migration-job.yaml')]));
 Object.assign(base.metadata,{namespace:'kodex-system',labels:{...base.metadata.labels,...labels}});write(render,base);
 const precondition={profile:'TELEGRAM_NOTIFICATIONS_PREDECESSOR_V1',database:'control_plane',readOnly:'on',gooseVersion:20261114130000,backupQualification:'NONE',
  schemaCatalog:telegramNotificationsCatalog.normal.before,telegramNotificationsAbsent:true,workspaceOwnerIdle:true,publicSequenceSafe:true,...Object.fromEntries(activities.map(key=>[key,0]))};
 const postcondition={profile:'TELEGRAM_NOTIFICATIONS_V1',database:'control_plane',readOnly:'on',gooseVersion:20261114140000,backupQualification:'NONE',schemaCatalog:telegramNotificationsCatalog.normal.after,publicSequenceSafe:true};
 write(state,{source,kubeconfig,context:f.target.context,target:f.target,resources,sourceRenderPath:join(source,'source-render.json'),nodes:f.observed.nodes,componentNames:f.value.components.map(c=>c.component),
  evidence,policyUID:uid(950),precondition,postcondition,jobs:{},jobPods:{},calls:[],effects:[]});
 const backend=fileURLToPath(new URL('./fixtures/telegram-deployment-kube.cjs',import.meta.url));
 writeFileSync(join(bin,'kubectl'),`#!${process.execPath}\nrequire(${JSON.stringify(backend)});\n`,{mode:0o700});
 const run=(phase,extra=[])=>spawnSync(process.execPath,[join(root,'tools/release/control-plane-migration.mjs'),phase,'--profile','telegram-notifications','--kubeconfig',kubeconfig,
  '--context',f.target.context,'--plan',plan,...(phase==='plan'?['--source',source,'--revision',revision,'--base-render',render,'--component-publication',component,'--portable-target',target]:['--evidence',evidence]),
  ...(['check','stop','apply','handoff','resume','activate'].includes(phase)?['--confirm','APPLY-STAGING-CP-MIGRATION']:[]),...extra],
  {encoding:'utf8',timeout:90000,env:{HOME:directory,PATH:bin+':'+process.env.PATH,LANG:'C.UTF-8',KODEX_TELEGRAM_SYNTHETIC_STATE:state}});
 return {run,read:()=>read(state),edit:fn=>{const value=read(state);fn(value);write(state,value);},plan,evidence};
}

test('real CLI046: creator→check→join→DDL→адресные inputs→cohort→resume, receiver остаётся PENDING_AUTHORITY',async t=>{
 const f=await fixture(t),pass=(phase,status)=>{const result=f.run(phase);assert.equal(result.status,0,result.stderr);const body=JSON.parse(result.stdout);if(status)assert.equal(body.status,status);return body;};
 pass('plan','PLANNED');const planned=read(f.plan);assert.equal(planned.telegramInputs.patches.length,7);
 pass('check','PASS');pass('stop','STOPPED');pass('apply','PASS');
 f.edit(s=>s.lostACK='internal-rpc-authority-publisher');const failed=f.run('handoff');assert.notEqual(failed.status,0);assert.match(failed.stderr,/KUBERNETES_OPERATION_UNKNOWN/);
 const before=f.read().effects.filter(value=>value.endsWith(':internal-rpc-authority-publisher')).length;
 pass('inspect','PASS');pass('handoff','STAGED');assert.equal(f.read().effects.filter(value=>value.endsWith(':internal-rpc-authority-publisher')).length,before);
 pass('resume','PENDING_AUTHORITY');const count=f.read().effects.length;pass('resume','PENDING_AUTHORITY');assert.equal(f.read().effects.length,count);
 const current=f.read();assert.equal(current.effects.filter(effect=>effect==='DDL_CREATE').length,1);
 assert.equal(current.resources['deployment/interaction-gateway'].spec.replicas,0);assert.equal(current.resources['deployment/control-api-gateway'].spec.replicas,0);
 assert.equal(current.resources['deployment/control-plane'].spec.replicas,3);
 assert.ok(current.resources['deployment/control-plane'].spec.template.spec.containers[0].env.some(e=>e.name==='INTERACTION_PUBLIC_ORIGIN'));
 assert.ok(current.resources['deployment/egress-gateway'].spec.template.spec.containers[0].ports.some(p=>p.containerPort===8086));
 const activation=f.run('activate');assert.notEqual(activation.status,0);assert.match(activation.stderr,/PHASE_ARGUMENT_INVALID/);assert.equal(f.read().effects.length,count);
});
