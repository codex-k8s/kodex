import {authorityOwnerPostgresObjects} from './fixtures/authority-owner-postgres-synthetic.mjs';
import {catalogAuthorityBaselineSQL} from './catalog-authority-baseline.mjs';
import {backupMaintenanceFixture} from './fixtures/backup-controller-maintenance-synthetic.mjs';
import {prepareAdmission} from './fixtures/authority-catalog-synthetic.mjs';
import {readCatalogTarget} from './authority-catalog-transition.mjs';
import {telegramPolicyTransition,telegramPolicySource,telegramRegistrySource} from './authority-telegram-transition.mjs';
import {renderAuthorityPolicyEnvironment} from './authority-policy-environment.mjs';
import producerMetadata from './fixtures/telegram-producer-metadata.cjs';
import {telegramIntroductionResources} from './telegram-introduction-inventory.mjs';
import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync,spawnSync} from 'node:child_process';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,chmodSync,rmSync,copyFileSync,existsSync} from 'node:fs';
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
const canonicalSourceResources=decodeTelegramResourceCreateSource(execute('kubectl',['--kubeconfig','/synthetic/unused','--context','synthetic',
 'kustomize','deploy/k8s/profiles/web-only']),execute);
const activities=['activeRuntimeLeases','activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions',
 'pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks','activeIntegrationInvocations','activeIntegrationConnectionTests',
 'activePersonalCredentialTests','activeRepositoryBundles','activeConfigurationSources','activeConfigurationWritebacks','activeInteractionDeliveries','activeBackupSessions','oldClaimSessions'];
const write=(path,value)=>writeFileSync(path,JSON.stringify(value)+'\n',{mode:0o600});
const read=path=>JSON.parse(readFileSync(path,'utf8'));

async function fixture(t,{narrow=false,successor=false,repair=false,backup=false}={}){
 const sourceResources=structuredClone(canonicalSourceResources);
 if(repair)for(const resource of sourceResources){
  if(resource.kind==='Service'&&resource.metadata.name==='egress-gateway')for(const port of resource.spec.ports)if(port.port===8086){port.name='telegram-connect';port.targetPort='telegram-connect';}
  if(resource.kind==='Deployment'&&resource.metadata.name==='egress-gateway')for(const port of resource.spec.template.spec.containers.find(c=>c.name==='egress-gateway').ports)if(port.containerPort===8086)port.name='telegram-connect';
 }
 const directory=mkdtempSync('/tmp/kodex-telegram-deployment-');chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const source=join(directory,'source'),bin=join(directory,'bin');mkdirSync(source,{mode:0o755});mkdirSync(bin,{mode:0o700});
 const tracked=execute('git',['ls-files','-z','--','tools','infra/public-ingress','deploy/k8s/base/internal-rpc-authority-data','deploy/k8s/base/internal-rpc-authority-publisher']).split('\0').filter(Boolean);
 for(const path of [...new Set([...tracked,'tools/release/telegram-cp-policy.mjs','tools/release/telegram-cp-policy.test.mjs'])]){
  mkdirSync(dirname(join(source,path)),{recursive:true,mode:0o755});copyFileSync(join(root,path),join(source,path));chmodSync(join(source,path),0o644);
 }
 for(const path of ['deploy/k8s/base/platform-state/postgresql.yaml','deploy/k8s/base/egress-gateway/deployment.yaml','deploy/k8s/base/egress-gateway/service.yaml','deploy/k8s/base/platform-state/ca-bundles.yaml','services/internal/control-plane/cmd/cli/migrations/20261114140000_telegram_project_subscriptions.sql',
  ...['telegram-notifications-migration-precondition.sql','telegram-notifications-migration-postcondition.sql','portable-component-migration-drain.sql'].map(name=>'tools/release/'+name)]){
  mkdirSync(dirname(join(source,path)),{recursive:true,mode:0o755});copyFileSync(join(root,path),join(source,path));chmodSync(join(source,path),0o644);
 }
 if(repair){
  // Исторический planner не имел пред-DDL проверки. Executor B восстанавливает
  // настоящий новый guard; production code этим fixture не изменяется.
  const path=join(source,'tools/release/control-plane-immutable-migration.mjs');
  writeFileSync(path,readFileSync(path,'utf8').replace('validateKubernetesPortNames(readRuntimeSource(source));',''));
  for(const file of ['deployment.yaml','service.yaml']){const path=join(source,'deploy/k8s/base/egress-gateway',file);writeFileSync(path,readFileSync(path,'utf8').replaceAll('tg-connect','telegram-connect'));}
 }
 writeFileSync(join(source,'source-render.json'),JSON.stringify(sourceResources)+'\n',{mode:0o644});
 const git=(...args)=>execute('git',['-C',source,...args]).trim();git('init','-q');git('remote','add','origin','https://github.com/codex-k8s/kodex.git');git('add','.');
 git('-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Синтетическая закрытая source fixture');
 const revision=git('rev-parse','HEAD');
 let executor=source;
 if(successor){
  executor=join(directory,'executor');git('worktree','add','-q','--detach',executor,revision);
  if(repair){
   copyFileSync(join(root,'tools/release/control-plane-immutable-migration.mjs'),join(executor,'tools/release/control-plane-immutable-migration.mjs'));
   for(const file of ['deployment.yaml','service.yaml'])copyFileSync(join(root,'deploy/k8s/base/egress-gateway',file),join(executor,'deploy/k8s/base/egress-gateway',file));
  }
  writeFileSync(join(executor,'tools/release/synthetic-successor.txt'),'Synthetic qualified executor revision\n',{mode:0o644});
  execute('git',['-C',executor,'add','.']);
  execute('git',['-C',executor,'-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Синтетический successor executor']);
 }
 const applicationFlags=successor?['--application-source',source,'--application-revision',revision]:[];
 const migrationProfile=backup?'telegram-notifications-backup':'telegram-notifications';
 const f=componentCohortFixture(migrationProfile),resources={};
 for(const [index,part] of f.value.components.entries()){
  const lock=JSON.parse(Buffer.from(part.componentLockRawBase64,'base64'));lock.source_sha=revision;replaceComponentLock(part,lock);part.nodeProof.sourceSHA=revision;
  const app=f.deployments[index];app.spec.selector={matchLabels:{'app.kubernetes.io/name':app.metadata.name}};
  app.spec.template.metadata.labels={...app.metadata.labels,...(app.metadata.name==='control-api-gateway'?{'app.kubernetes.io/component':'external-gateway'}:{})};
  resources['deployment/'+app.metadata.name]=structuredClone(app);
 }
 const installation=f.observed.installation;resources['configmap/kodex-installation-profile']=installation;
 const labels={'app.kubernetes.io/part-of':'kodex','kodex.dev/profile':'web-only','kodex.dev/environment':'production'};
 const publisher={apiVersion:'apps/v1',kind:'Deployment',metadata:{name:'internal-rpc-authority-publisher',namespace:'kodex-system',uid:uid(500),resourceVersion:'1',labels},
  spec:{template:{metadata:{},spec:{containers:[{name:'publisher',command:['/usr/local/bin/internal-rpc-authority-publisher'],image:'registry.local.kodex/kodex/internal-rpc-authority@sha256:'+'a'.repeat(64)}]}}}};
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
 const cp=resources['deployment/control-plane'],app=cp.spec.template.spec.containers[0];
 cp.spec.strategy={type:'RollingUpdate',rollingUpdate:{maxUnavailable:0,maxSurge:1}};
 app.env.push({name:'CONTROL_PLANE_OIDC_ISSUER',value:'https://sso.synthetic.invalid/realms/kodex'},{name:'CONTROL_PLANE_OIDC_AUDIENCE',value:'kodex-control-api'});
 const environment={oidcIssuer:'https://sso.synthetic.invalid/realms/kodex',oidcAudience:'kodex-control-api'};
 const previousPolicy=renderAuthorityPolicyEnvironment(telegramPolicySource('predecessor').raw,environment),policyName=`control-plane-policy-projection-${previousPolicy.renderedSHA256.slice(0,16)}`;
 app.env.push({name:'CONTROL_PLANE_AUTHORITY_POLICY_FILE',value:'/var/run/config/kodex/control-plane/environment-policy/policy.json'});
 app.volumeMounts=[{name:'control-plane-environment-policy',mountPath:'/var/run/config/kodex/control-plane/environment-policy',readOnly:true}];
 cp.spec.template.spec.volumes=[{name:'control-plane-environment-policy',configMap:{name:policyName,defaultMode:292}}];
 resources['configmap/'+policyName]={apiVersion:'v1',kind:'ConfigMap',metadata:{name:policyName,namespace:'kodex-system',uid:uid(570),resourceVersion:'1',labels},immutable:true,data:{'policy.json':previousPolicy.raw}};
 const registry={apiVersion:'v1',kind:'ConfigMap',metadata:{name:'internal-rpc-authority-publisher-target-registry',namespace:'kodex-system',uid:uid(571),resourceVersion:'1',labels},
  data:{'authority-policy.json':previousPolicy.raw,'key-delivery-targets.yaml':telegramRegistrySource('predecessor').raw.replace('source_revision: 7','source_revision: 2')}};
 resources['configmap/'+registry.metadata.name]=registry;
 for(const name of ['internal-rpc-authority-migrator','internal-rpc-authority-postgresql-from-migrator'])for(const resource of sourceResources.filter(r=>r.metadata.name===name)){
  if(['ServiceAccount','NetworkPolicy'].includes(resource.kind)){const copy=structuredClone(resource);Object.assign(copy.metadata,{uid:uid(counter++),resourceVersion:'1'});resources[copy.kind.toLowerCase()+'/'+name]=copy;}
 }
 const apiService=structuredClone(sourceResources.find(r=>r.kind==='Service'&&r.metadata.name==='control-api-gateway'));
 Object.assign(apiService.metadata,{uid:uid(572),resourceVersion:'1'});Object.assign(apiService.spec,{type:'ClusterIP',clusterIP:'192.0.2.22'});resources['service/control-api-gateway']=apiService;
 const authorityProfile={...f.target,resources:[cp,publisher,registry,resources['deployment/control-api-gateway'],apiService].map(r=>({apiVersion:r.apiVersion,kind:r.kind,name:r.metadata.name,uid:r.metadata.uid}))};
 readCatalogTarget(authorityProfile,f.target.context,(kind,name)=>kind==='namespace'?{metadata:{uid:name==='kube-system'?f.target.clusterUID:f.target.namespaceUID}}:resources[kind+'/'+name],telegramPolicyTransition);
 const api=resources['deployment/control-api-gateway'];api.spec.replicas=3;api.status={observedGeneration:api.metadata.generation,replicas:3,updatedReplicas:3,readyReplicas:3,availableReplicas:3};
 const apiProfile=narrow?{...authorityProfile,resources:authorityProfile.resources.filter(r=>r.name==='control-api-gateway')}:authorityProfile;
 const admission=prepareAdmission({directory,revision,profile:apiProfile,installation,api,service:apiService,
  state:{slices:{items:[]},isolationPods:{items:[]}},authorityTransition:telegramPolicyTransition});
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
 const precondition={profile:backup?'TELEGRAM_NOTIFICATIONS_BACKUP_PREDECESSOR_V1':'TELEGRAM_NOTIFICATIONS_PREDECESSOR_V1',database:'control_plane',readOnly:'on',gooseVersion:20261114130000,backupQualification:backup?'CANONICAL_BACKUP':'NONE',
  schemaCatalog:telegramNotificationsCatalog[backup?'backup':'normal'].before,telegramNotificationsAbsent:true,workspaceOwnerIdle:true,publicSequenceSafe:true,...Object.fromEntries(activities.map(key=>[key,0]))};
 const postcondition={profile:backup?'TELEGRAM_NOTIFICATIONS_BACKUP_V1':'TELEGRAM_NOTIFICATIONS_V1',database:'control_plane',readOnly:'on',gooseVersion:20261114140000,schemaCatalog:telegramNotificationsCatalog[backup?'backup':'normal'].after,backupQualification:backup?'CANONICAL_BACKUP':'NONE',publicSequenceSafe:true};
 let backupState;const backupTarget=join(directory,'backup-target.json'),backupPlan=join(directory,'backup-stop.json'),backupEvidence=join(directory,'backup-stop.jsonl');
 if(backup){
  const b=backupMaintenanceFixture(),pg=authorityOwnerPostgresObjects({podUID:b.postgres.uid,ownerUID:b.postgres.ownerUID});
  backupState={sourceSHA:revision,context:f.target.context,clusterUID:f.target.clusterUID,namespaceUID:f.target.namespaceUID,deployment:b.deployment,postgres:b.postgres,installation,
   rs:b.rs,pods:b.state.pods,originalPods:b.state.pods,extra:[],effects:0,backend:{systemIdentifier:'18446744073709551615',databaseOID:'1',database:'control_plane',port:'5432',serverAddress:null,serverPort:null},
   pgStatefulSet:pg.set,pgPod:pg.pod};
  write(backupTarget,{...f.target,resources:[{apiVersion:'apps/v1',kind:'Deployment',name:'backup-controller',uid:b.deployment.metadata.uid}]});
 }
 write(state,{ownerPostgres:authorityOwnerPostgresObjects(backupState?{podUID:backupState.postgres.uid,ownerUID:backupState.postgres.ownerUID}:{}),ownerSQL:catalogAuthorityBaselineSQL().toString(),...(backupState?{backup:backupState}:{}),source,kubeconfig,context:f.target.context,target:f.target,resources,sourceRenderPath:join(source,'source-render.json'),nodes:f.observed.nodes,componentNames:f.value.components.map(c=>c.component),
  evidence,policyUID:uid(950),cpOldImage:f.value.components[0].expectedImageRef,precondition,postcondition,jobs:{},jobPods:{},calls:[],effects:[]});
 const backend=fileURLToPath(new URL('./fixtures/telegram-deployment-kube.cjs',import.meta.url));
 writeFileSync(join(bin,'kubectl'),`#!${process.execPath}\nprocess.env.KODEX_TELEGRAM_SYNTHETIC_STATE=${JSON.stringify(state)};\nrequire(${JSON.stringify(backend)});\n`,{mode:0o700});
 const setEvidence=path=>{const value=read(state);value.activeEvidence=path;write(state,value);};
 let activeRepair,activeRepairExecutor,newPlanner=false;const repairPath=join(directory,'port-repair.json');
 const run=(phase,extra=[])=>{setEvidence(evidence);const started=Date.now(),beforeCalls=read(state).calls.length;const result=spawnSync(process.execPath,[join(repair?(phase==='plan'&&!newPlanner?source:executor):root,'tools/release/control-plane-migration.mjs'),phase,...(activeRepair?['--telegram-port-repair-plan',activeRepair,...(activeRepairExecutor?['--telegram-port-repair-executor-sha',activeRepairExecutor]:[])]:[]),'--profile',migrationProfile,'--kubeconfig',kubeconfig,
  '--context',f.target.context,'--plan',plan,...(phase==='plan'?['--source',source,'--revision',revision,'--base-render',render,'--component-publication',component,'--portable-target',target,...(backup?['--backup-maintenance-plan',backupPlan,'--backup-maintenance-evidence',backupEvidence]:[])]:['--evidence',evidence]),
  ...(['check','stop','apply','handoff','resume','activate'].includes(phase)?['--confirm','APPLY-STAGING-CP-MIGRATION']:[]),...extra],
  {encoding:'utf8',timeout:backup?180000:90000,env:{HOME:directory,PATH:bin+':'+process.env.PATH,LANG:'C.UTF-8',KODEX_TELEGRAM_SYNTHETIC_STATE:state}});
  let callCount;try{callCount=read(state).calls.length-beforeCalls;}catch{}
  const observation={phase,elapsedMs:Date.now()-started,status:result.status,signal:result.signal,error:result.error?.code,callCount};
  if(backup&&phase==='handoff')console.log(JSON.stringify({code:'SYNTHETIC_MIGRATION_PHASE_OBSERVED',...observation}));
  if(result.status!==0){
   if(String(result.stderr??'').includes('TELEGRAM_AUTHORITY_DELIVERY_OWNER_REJECTED')){
    const current=read(state),path=extra[extra.indexOf('--authority-status-plan')+1],statusPlan=path?read(path):undefined;
    const selected=Object.values(current.jobs).find(job=>job.metadata.annotations?.['kodex.dev/rotation-intent']===statusPlan?.intentID);
    const raw=selected?JSON.parse(current.ownerLogs[selected.metadata.name]):undefined;
    observation.owner={jobStatus:selected?.status,status:raw?.status,operationStatus:raw?.operationStatus,phase:raw?.phase,
     observedAt:raw?.observedAt,ageMs:Date.now()-Date.parse(raw?.observedAt),expectedReadbackCount:raw?.expectedReadbackCount,actualReadbackCount:raw?.actualReadbackCount};
   }
   result.stderr=JSON.stringify({code:'SYNTHETIC_MIGRATION_PHASE_FAILED',...observation})+'\n'+String(result.stderr??'');
  }
  return result;};
 const authorityTarget=join(directory,'authority-target.json');write(authorityTarget,authorityProfile);
 const apiTarget=join(directory,'api-target.json');write(apiTarget,apiProfile);
 const command=(file,args)=>{setEvidence(args.includes('--evidence')?args[args.indexOf('--evidence')+1]:evidence);return spawnSync(process.execPath,[join(file==='component-api-maintenance.mjs'||file==='backup-controller-maintenance-cli.mjs'&&args.includes('stop')?source:executor,'tools/release',file),...args],{encoding:'utf8',timeout:file==='backup-controller-maintenance-cli.mjs'?400000:180000,env:{HOME:directory,PATH:bin+':'+process.env.PATH,LANG:'C.UTF-8',KODEX_TELEGRAM_SYNTHETIC_STATE:state}});};
 if(backup){
  const flags=['--context',f.target.context,'--kubeconfig',kubeconfig,'--plan',backupPlan,'--evidence',backupEvidence];
  const prepared=command('backup-controller-maintenance-cli.mjs',['plan','--action','stop','--migration-profile',migrationProfile,'--target',backupTarget,...flags]);assert.equal(prepared.status,0,prepared.stderr);
  const applied=spawnSync(process.execPath,[join(source,'tools/release/backup-controller-maintenance-cli.mjs'),'apply',...flags,'--approved-plan-sha',fingerprint(read(backupPlan))],{encoding:'utf8',timeout:90000,env:{HOME:directory,PATH:bin+':'+process.env.PATH,LANG:'C.UTF-8',KODEX_TELEGRAM_SYNTHETIC_STATE:state}});assert.equal(applied.status,0,applied.stderr);
 }
 return {backupPlan,backupEvidence,backupTarget,useCurrentPlanner:()=>{newPlanner=true;},withoutRepair:(phase,extra)=>{const saved=activeRepair;activeRepair=undefined;try{return run(phase,extra);}finally{activeRepair=saved;}},enableRepair:()=>{activeRepair=repairPath;applicationFlags.push('--telegram-port-repair-plan',repairPath);},
  upgradeRepairExecutor:()=>{const config=executor;executor=join(directory,'late-owner-executor');execute('git',['-C',config,'worktree','add','-q','--detach',executor,'HEAD']);
   writeFileSync(join(executor,'tools/release/synthetic-owner-successor.txt'),'Synthetic tools-only late owner successor\n',{mode:0o644});execute('git',['-C',executor,'add','.']);
   execute('git',['-C',executor,'-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Синтетический late owner executor']);
   activeRepairExecutor=execute('git',['-C',executor,'rev-parse','HEAD']).trim();applicationFlags.push('--telegram-port-repair-executor-sha',activeRepairExecutor);return config;},repairPath,run,command,directory,source,get executor(){return executor;},get repairExecutor(){return activeRepairExecutor;},applicationFlags,apiTarget,revision,kubeconfig,context:f.target.context,authorityTarget,admission,creator,publicationPath,read:()=>read(state),edit:fn=>{const value=read(state);fn(value);write(state,value);},plan,evidence};
}

for(const scenario of [{name:'real CLI046/107:',narrow:false,successor:false},{name:'real CLI046/107 frozen:',narrow:true,successor:true},{name:'real CLI046/107 port-repair:',narrow:true,successor:true,repair:true},{name:'real CLI046/107 backup-port-repair:',narrow:true,successor:true,repair:true,backup:true}])
test(scenario.name+' dormant→schema→stagedCP0→DELIVER→projection107→ready→APIreaders→activate→RETIRE→restore/rejoin',async t=>{
 const f=await fixture(t,scenario),frozenPaths=[f.admission.paths.planPath,f.admission.paths.evidencePath,f.admission.paths.trafficPlanPath,f.admission.paths.trafficEvidencePath],
  frozenBytes=frozenPaths.map(path=>readFileSync(path)),pass=(phase,status)=>{const result=f.run(phase);assert.equal(result.status,0,result.stderr);const body=JSON.parse(result.stdout);if(status)assert.equal(body.status,status);return body;};
 pass('plan','PLANNED');const planned=read(f.plan);assert.equal(planned.telegramInputs.patches.length,7);
 pass('check','PASS');pass('stop','STOPPED');pass('apply','PASS');
 if(scenario.repair){
  const original=f.run('handoff');assert.notEqual(original.status,0);assert.match(original.stderr,/KUBERNETES_OPERATION_UNKNOWN/);
  const failedRows=readFileSync(f.evidence,'utf8').trim().split('\n').map(JSON.parse);
  assert.equal(failedRows.some(e=>e.type==='PAIR_HANDOFF_INTENT'||e.type==='TELEGRAM_INPUT_PATCH_INTENT'&&e.resource==='v1|Service|egress-gateway'),false);
  const frozenMigration=readFileSync(f.plan),prefix=readFileSync(f.evidence);
  const result=f.command('telegram-port-repair.mjs',['plan','--context',f.context,'--kubeconfig',f.kubeconfig,'--migration-plan',f.plan,'--migration-evidence',f.evidence,'--output',f.repairPath]);
  assert.equal(result.status,0,result.stderr);f.enableRepair();f.upgradeRepairExecutor();
  assert.deepEqual(readFileSync(f.plan),frozenMigration);assert.deepEqual(readFileSync(f.evidence).subarray(0,prefix.length),prefix);
 }
 f.edit(s=>s.lostACK='internal-rpc-authority-publisher');const failed=f.run('handoff');assert.notEqual(failed.status,0);assert.match(failed.stderr,/KUBERNETES_OPERATION_UNKNOWN/);
 if(scenario.repair){
  const effects=f.read().effects.length,missing=f.withoutRepair('handoff');assert.notEqual(missing.status,0);assert.match(missing.stderr,/TELEGRAM_PORT_REPAIR_PIN_REQUIRED/);assert.equal(existsSync(f.evidence+'.lock'),false);assert.equal(f.read().effects.length,effects);
 }
 const before=f.read().effects.filter(value=>value.endsWith(':internal-rpc-authority-publisher')).length;
 pass('inspect','PASS');pass('handoff','STAGED');assert.equal(f.read().effects.filter(value=>value.endsWith(':internal-rpc-authority-publisher')).length,before);
 pass('resume','STAGED_PENDING_AUTHORITY');const count=f.read().effects.length;pass('resume','STAGED_PENDING_AUTHORITY');assert.equal(f.read().effects.length,count);
 const current=f.read();assert.equal(current.effects.filter(effect=>effect==='DDL_CREATE').length,1);
 assert.equal(current.resources['deployment/interaction-gateway'].spec.replicas,0);assert.equal(current.resources['deployment/control-api-gateway'].spec.replicas,0);
 assert.equal(current.resources['deployment/control-plane'].spec.replicas,0);
 assert.equal(current.resources['deployment/egress-gateway'].spec.replicas,0);
 assert.ok(current.resources['deployment/control-plane'].spec.template.spec.containers[0].env.some(e=>e.name==='INTERACTION_PUBLIC_ORIGIN'));
 assert.ok(current.resources['deployment/egress-gateway'].spec.template.spec.containers[0].ports.some(p=>p.containerPort===8086));
 const activation=f.run('activate');assert.notEqual(activation.status,0);assert.match(activation.stderr,/PHASE_ARGUMENT_INVALID/);assert.equal(f.read().effects.length,count);
 const flags=['--kubeconfig',f.kubeconfig,'--context',f.context],owner=join(f.directory,'owner.json'),ownerEvidence=join(f.directory,'owner.jsonl');
 const checked=(file,args)=>{const result=f.command(file,args);assert.equal(result.status,0,`${file}: ${result.stderr}`);const row=result.stdout.trim().split('\n').findLast(value=>value.startsWith('{'));return row?JSON.parse(row):{message:result.stdout.trim()};};
 checked('authority-rotation-transition.mjs',['plan',...flags,'--policy-transition',telegramPolicyTransition,'--portable-target',f.authorityTarget,
  '--source',f.source,'--revision',f.revision,'--action','rotate','--output',owner,...f.admission.flags,
  '--introduced-plan',f.creator.planPath,'--introduced-evidence',f.creator.evidencePath,'--introduced-publication',f.publicationPath]);
 const rotation=read(owner),now=Date.now();f.edit(s=>s.ownerStatus={observedAt:new Date(now).toISOString(),operationId:rotation.ownerOperationID,status:'DISTRIBUTING',operationStatus:'DISTRIBUTING',phase:'DISTRIBUTE',
  registryRevision:rotation.registry.sourceRevision,registrySourceDigestSHA256:rotation.registry.sourceDigestSHA256,baseRevision:4,baseDigestSHA256:'c'.repeat(64),
  sourceRevision:5,sourceDigestSHA256:'d'.repeat(64),intentId:uid(2100),protocolVersion:2,expectedReadbackCount:15,actualReadbackCount:0,
  deliveryStartedAt:new Date(now-2000).toISOString(),deliveredAt:new Date(now-1000).toISOString()});
 const watched=f.command('authority-rotation-transition.mjs',['apply',...flags,'--plan',owner,'--evidence',ownerEvidence,'--confirm','APPLY-PORTABLE-TELEGRAM-AUTHORITY']);
 assert.equal(watched.status,1);assert.equal(JSON.parse(watched.stdout).phase,'FAILED');
 assert.equal(read(owner).ownerOperationID,rotation.ownerOperationID);
 let statusNumber=0;
 const fresh=()=>{const plan=join(f.directory,`status-${++statusNumber}.json`),evidence=plan+'l';
  checked('authority-rotation-transition.mjs',['plan',...flags,'--policy-transition',telegramPolicyTransition,'--portable-target',f.authorityTarget,
   '--source',f.source,'--revision',f.revision,'--action','status','--output',plan]);
  checked('authority-rotation-transition.mjs',['apply',...flags,'--plan',plan,'--evidence',evidence,'--confirm','APPLY-PORTABLE-TELEGRAM-AUTHORITY']);
  return ['--authority-status-plan',plan,'--authority-status-evidence',evidence];};
 const authority=['--authority-plan',owner,'--authority-evidence',ownerEvidence],projection=join(f.directory,'cp-projection.json'),projectionEvidence=join(f.directory,'cp-projection.jsonl');
 checked('control-plane-policy-projection.mjs',['plan',...flags,...f.applicationFlags,'--policy-transition',telegramPolicyTransition,'--portable-target',f.authorityTarget,
  ...f.admission.flags,'--migration-plan',f.plan,'--migration-evidence',f.evidence,...authority,'--output',projection]);
 assert.equal(f.read().resources['deployment/control-plane'].spec.replicas,0);
 f.edit(s=>{s.ownerStatus.status=s.ownerStatus.operationStatus='DELIVERING';});
 const rejected=f.command('control-plane-policy-projection.mjs',['apply',...flags,'--plan',projection,'--evidence',projectionEvidence,...fresh(),'--confirm','APPLY-PORTABLE-TELEGRAM-PROJECTION']);
 assert.notEqual(rejected.status,0);assert.match(rejected.stderr,/TERMINAL_RECEIPT_REQUIRED|DELIVERY_OWNER_REJECTED/);assert.equal(f.read().resources['deployment/control-plane'].spec.replicas,0);
 assert.equal(f.read().resources['deployment/control-plane'].spec.template.spec.volumes.find(v=>v.name==='control-plane-environment-policy').configMap.name,
  planned.componentHandoff.components[0].stagedSpec.template.spec.volumes.find(v=>v.name==='control-plane-environment-policy').configMap.name);
 f.edit(s=>{s.ownerStatus.status=s.ownerStatus.operationStatus='DISTRIBUTING';});
 const delayedProjectionStatus=fresh(),projectionHistory=structuredClone(f.read().ownerLogs);
 if(scenario.repair)f.edit(s=>s.ownerPreflightDelayMS=35000);
 checked('control-plane-policy-projection.mjs',['resume',...flags,'--plan',projection,'--evidence',projectionEvidence,...delayedProjectionStatus]);
 assert.deepEqual(f.read().ownerLogs,projectionHistory);
 if(scenario.repair)assert.ok(f.read().ownerReadEvidence.at(-1).ageMS<30000);
 assert.equal(f.read().effects.filter(effect=>effect==='POLICY_PROJECTION_CREATE').length,1);
 const projected=f.read();assert.equal(projected.resources['deployment/control-plane'].spec.replicas,0);
 const cpProjection=['--cp-projection-plan',projection,'--cp-projection-evidence',projectionEvidence];
 const resumed=f.run('resume',[...cpProjection,...authority,...fresh()]);assert.equal(resumed.status,0,resumed.stderr);assert.equal(JSON.parse(resumed.stdout).status,'PENDING_AUTHORITY');
 const after=f.read();assert.equal(after.resources['deployment/control-plane'].spec.replicas,3);assert.equal(after.resources['deployment/control-plane'].status.availableReplicas,3);
 assert.equal(after.resources['deployment/control-api-gateway'].spec.replicas,0);assert.equal(after.resources['deployment/interaction-gateway'].spec.replicas,0);
 const again=f.run('resume',cpProjection);assert.equal(again.status,0,again.stderr);assert.equal(f.read().effects.length,after.effects.length);
 const open=join(f.directory,'open.json'),openEvidence=join(f.directory,'open.jsonl'),closed=['--closed-plan',f.admission.paths.planPath,'--closed-evidence',f.admission.paths.evidencePath],isolated=['--isolation-plan',f.admission.paths.trafficPlanPath,'--isolation-evidence',f.admission.paths.trafficEvidencePath];
 checked('component-api-maintenance.mjs',['plan',...flags,'--action','open','--target',f.apiTarget,'--plan',open,...closed,...isolated]);
 checked('component-api-maintenance.mjs',['apply',...flags,'--plan',open,'--evidence',openEvidence,'--approved-plan-sha',fingerprint(read(open)),...closed,...isolated]);
 const apiOpen=['--api-open-plan',open,'--api-open-evidence',openEvidence];
 assert.equal(f.read().resources['service/control-api-gateway'].spec.selector['kodex.dev/component-traffic-isolation'],f.admission.closed.plan.maintenanceID);
 const activated=f.run('activate',[...cpProjection,...apiOpen,...authority,...fresh()]);assert.equal(activated.status,0,activated.stderr);assert.equal(JSON.parse(activated.stdout).status,'READERS_STARTED_PENDING_RETIRE');
 const active=f.read();assert.equal(active.resources['deployment/interaction-gateway'].spec.replicas,3);
 const activationAgain=f.run('activate',[...cpProjection,...apiOpen,...authority,...fresh()]);assert.equal(activationAgain.status,0,activationAgain.stderr);
 assert.equal(f.read().effects.filter(effect=>effect==='PAIR_ACTIVATE_INTENT:interaction-gateway').length,1);
 f.edit(s=>{const now=Date.now();s.ownerStatus={...s.ownerStatus,status:'RETIRED',operationStatus:'RETIRED',phase:'RETIRE',sourceRevision:7,actualReadbackCount:17,
  switchNotBefore:new Date(now-10000).toISOString(),previousNotAfter:new Date(now-2000).toISOString(),completedAt:new Date(now-1000).toISOString()};});
 const restored=join(f.directory,'restored.json'),restoredEvidence=join(f.directory,'restored.jsonl');
 checked('component-api-traffic.mjs',['plan',...flags,...f.applicationFlags,'--policy-transition',telegramPolicyTransition,'--action','restore','--target',f.apiTarget,'--plan',restored,...closed,...isolated,...authority,...fresh()]);
 const restoreStatus=fresh(),restoreHistory=structuredClone(f.read().ownerLogs);if(scenario.repair)f.edit(s=>s.ownerPreflightDelayMS=35000);
 checked('component-api-traffic.mjs',['apply',...flags,'--plan',restored,'--evidence',restoredEvidence,'--approved-plan-sha',fingerprint(read(restored)),...closed,...isolated,...authority,...restoreStatus]);
 assert.deepEqual(f.read().ownerLogs,restoreHistory);
 assert.equal(Object.hasOwn(f.read().resources['service/control-api-gateway'].spec.selector,'kodex.dev/component-traffic-isolation'),false);
 const restoredState=f.read(),selector=restoredState.resources['service/control-api-gateway'].spec.selector,labels=restoredState.resources['deployment/control-api-gateway'].spec.template.metadata.labels;
 assert.ok(Object.entries(selector).every(([key,value])=>labels[key]===value));
 assert.equal(f.read().effects.filter(effect=>effect==='DDL_CREATE').length,1);
 const finalInputs=[...cpProjection,...apiOpen,...authority,...restoreStatus,'--api-restore-plan',restored,'--api-restore-evidence',restoredEvidence];
 const finalCount=f.read().effects.length,inspected=f.run('inspect',finalInputs);assert.equal(inspected.status,0,inspected.stderr);assert.equal(JSON.parse(inspected.stdout).pairReady,true);
 if(scenario.repair){
  assert.equal(f.read().resources['deployment/egress-gateway'].spec.template.spec.containers.find(c=>c.name==='egress-gateway').ports.find(p=>p.containerPort===8086).name,'tg-connect');
  assert.equal(f.read().resources['service/egress-gateway'].spec.ports.find(p=>p.port===8086).targetPort,'tg-connect');
 }
 const finalResume=f.run('resume',finalInputs);assert.equal(finalResume.status,0,finalResume.stderr);assert.equal(JSON.parse(finalResume.stdout).status,'RESUMED');assert.equal(f.read().effects.length,finalCount);
 if(scenario.backup){
  const resumed=join(f.directory,'backup-resume.json'),resumedEvidence=join(f.directory,'backup-resume.jsonl');
  const refs=['--stopped-plan',f.backupPlan,'--stopped-evidence',f.backupEvidence,'--migration-plan',f.plan,'--migration-evidence',f.evidence,
   '--cp-projection-plan',projection,'--cp-projection-evidence',projectionEvidence,...apiOpen,...authority,...restoreStatus,'--api-restore-plan',restored,'--api-restore-evidence',restoredEvidence,'--telegram-port-repair-plan',f.repairPath,...(f.repairExecutor?['--telegram-port-repair-executor-sha',f.repairExecutor]:[])];
  const beforeBackup=f.read().backup.effects,foreign='unrelated-backup-producer';
  f.edit(s=>{s.jobs[foreign]={apiVersion:'batch/v1',kind:'Job',metadata:{name:foreign,namespace:'kodex-system',uid:uid(9901),labels:{}},spec:{template:{spec:{serviceAccountName:'backup-controller',containers:[{name:'foreign',image:'synthetic.invalid/foreign@sha256:'+'e'.repeat(64)}],volumes:[{name:'credentials',secret:{secretName:'backup-controller-credentials'}}]}}},status:{active:1}};
   s.jobPods[foreign]={apiVersion:'v1',kind:'Pod',metadata:{name:foreign+'-pod',namespace:'kodex-system',uid:uid(9902),resourceVersion:'1',labels:{},ownerReferences:[{kind:'Job',uid:uid(9901),controller:true}]},spec:structuredClone(s.jobs[foreign].spec.template.spec),status:{phase:'Running'}};});
  const rejectedBackup=f.command('backup-controller-maintenance-cli.mjs',['plan','--action','resume',...flags,'--target',f.backupTarget,'--plan',resumed,'--evidence',resumedEvidence,'--stopped-source-sha',f.revision,...refs]);
  assert.notEqual(rejectedBackup.status,0);assert.match(rejectedBackup.stderr,/BACKUP_MAINTENANCE_OTHER_PRODUCER_PRESENT/);assert.equal(existsSync(resumed),false);assert.equal(f.read().backup.effects,beforeBackup);
  f.edit(s=>{delete s.jobs[foreign];delete s.jobPods[foreign];});
  checked('backup-controller-maintenance-cli.mjs',['plan','--action','resume',...flags,'--target',f.backupTarget,'--plan',resumed,'--evidence',resumedEvidence,'--stopped-source-sha',f.revision,...refs]);
  checked('backup-controller-maintenance-cli.mjs',['apply',...flags,'--plan',resumed,'--evidence',resumedEvidence,'--approved-plan-sha',fingerprint(read(resumed))]);
  checked('backup-controller-maintenance-cli.mjs',['status',...flags,'--plan',resumed,'--evidence',resumedEvidence]);
  assert.equal(f.read().backup.deployment.spec.replicas,1);assert.equal(f.read().effects.filter(effect=>effect==='DDL_CREATE').length,1);
 }
 for(const [index,path] of frozenPaths.entries())assert.deepEqual(readFileSync(path),frozenBytes[index]);
 assert.equal(read(f.plan).source.revision,f.revision);
 if(scenario.successor){
  const pin=read(projection).sourceCompatibility;assert.equal(pin.source,f.source);assert.equal(pin.executorSource,f.executor);
  assert.equal(pin.revision,f.revision);assert.notEqual(pin.executorRevision,pin.revision);
  assert.deepEqual(read(restored).sourceCompatibility,pin);
  if(scenario.repair){assert.equal(pin.repairExecutor.configurationSource,read(f.repairPath).configuration.source);assert.equal(pin.repairExecutor.executorRevision,f.repairExecutor);assert.notEqual(pin.repairExecutor.configurationRevision,pin.repairExecutor.executorRevision);}
  assert.notEqual(read(restored).authority.targetSHA256,read(restored).authority.authorityTargetSHA256);
 }
});


test('real CLI046/107 invalid-port: новый planner отказывает до plan/DDL',async t=>{
 const f=await fixture(t,{narrow:true,successor:true,repair:true});f.useCurrentPlanner();
 const result=f.run('plan');assert.notEqual(result.status,0);assert.match(result.stderr,/KUBERNETES_PORT_NAME_INVALID/);
 assert.equal(existsSync(f.plan),false);assert.equal(f.read().effects.length,0);
});
