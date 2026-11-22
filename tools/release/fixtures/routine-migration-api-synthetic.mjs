import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {mkdtempSync,mkdirSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import {execFileSync,spawnSync} from 'node:child_process';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {createRequire} from 'node:module';
import {baselineFixture,historicalCatalog106Source,catalog106SourceSHA,catalog106PredecessorSHA} from './catalog-authority-baseline-synthetic.mjs';
import {componentCohortFixture,replaceComponentLock} from './component-cohort-synthetic.mjs';
import {personalIntegrationsStagedInputsFixture} from './personal-integrations-staged-inputs-synthetic.mjs';
import {personalStagedInputsSchema} from '../artifact-retention-issuer-inputs.mjs';
import {backupMaintenanceFixture} from './backup-controller-maintenance-synthetic.mjs';
import {planBackupControllerMaintenance,backupControllerMaintenanceHeader} from '../backup-controller-maintenance.mjs';
import {uid} from './authority-catalog-synthetic.mjs';
import {fingerprint} from '../scoped-release.mjs';
import {readCatalogTarget} from '../authority-catalog-transition.mjs';
import {planComponentAPIMaintenance} from '../component-api-maintenance.mjs';
import {planComponentAPITraffic} from '../component-api-traffic.mjs';
import {planComponentMigrationHandoff} from '../portable-component-migration.mjs';
import {readMigrationAPIAdmission} from '../component-migration-api-admission.mjs';
import {immutableMigrationProfile,planImmutableControlPlaneMigration} from '../control-plane-immutable-migration.mjs';
import {sentryObservabilityCatalog} from '../sentry-observability-catalog.gen.mjs';
import {isPersonalIntegrationsMigrationProfile,personalIntegrationsCatalog,personalIntegrationsDeliveryPrerequisite} from '../personal-integrations-migration-contract.mjs';

const require=createRequire(import.meta.url),transport=require('./routine-migration-kubectl.cjs');

// Настоящие owner factory, JSON files, argv parser и child CLI сохраняются.
// Только external Kubernetes/SQL transport заменён finite metadata fixture.
export function routineMigrationAPIFixture(t,{predecessorSource=false,authoritySquashSource=false,rolloutAnnotations,applicationSource,executorSource,profile='sentry-observability-backup',stagedArtifactCatalog=false,prepareStagedArtifactCatalog}={}){
 const root=fileURLToPath(new URL('../../../',import.meta.url)).replace(/\/$/,'');
 const personal=isPersonalIntegrationsMigrationProfile(profile);
 assert.ok(personal||['sentry-observability','sentry-observability-backup'].includes(profile));
 const backup=profile.endsWith('-backup');
 assert.equal(typeof stagedArtifactCatalog,'boolean');
 assert.ok(!stagedArtifactCatalog||profile==='personal-integrations-backup');
 assert.ok(prepareStagedArtifactCatalog===undefined||stagedArtifactCatalog&&typeof prepareStagedArtifactCatalog==='function');
 assert.ok(!executorSource||personal&&applicationSource);
 const executorRoot=executorSource??root;
 const executorSHA=execFileSync('git',['-C',executorRoot,'rev-parse','HEAD'],{encoding:'utf8'}).trim();
 const selected=personal?(applicationSource??{revision:executorSHA,path:root}):historicalCatalog106Source(t,predecessorSource?catalog106PredecessorSHA:catalog106SourceSHA),sourceSHA=selected.revision,sourceRoot=selected.path;
 const directory=mkdtempSync(join(tmpdir(),'kodex-routine-api-'));
 const git=(...args)=>execFileSync('git',['-C',root,...args],{encoding:'utf8',stdio:'pipe',timeout:30000}).trim();
 const authorityEquivalentSHA=authoritySquashSource?(authoritySquashSource.equivalent??sourceSHA):undefined;
 // Orphan содержит именно опубликованный045/106 tree: текущий107 не выдаётся
 // за старый16-target source и не ослабляет проверку runtime reader.
 const authoritySHA=authoritySquashSource?(authoritySquashSource.original??git('-c','commit.gpgSign=false','commit-tree',git('rev-parse',`${authorityEquivalentSHA}^{tree}`),'-m','Синтетический исходный squash executor2140')):sourceSHA;
 const closedSourceSHA=authoritySquashSource?(authoritySquashSource.close??authorityEquivalentSHA):personal?sourceSHA:executorSHA;
 let authorityRoot=sourceRoot;
 if(authoritySquashSource){authorityRoot=join(directory,'authority-original');git('worktree','add','--detach',authorityRoot,authoritySHA);}
 t.after(()=>{if(authoritySquashSource)git('worktree','remove','--force',authorityRoot);rmSync(directory,{recursive:true,force:true});});
 const bin=join(directory,'bin');mkdirSync(bin,{mode:0o700});
 const kubeconfig=join(directory,'explicit config');writeFileSync(kubeconfig,'synthetic',{mode:0o600});
 const save=(name,value)=>writeFileSync(join(directory,name),JSON.stringify(value,null,2)+'\n',{mode:0o600});
 const journal=(name,events)=>writeFileSync(join(directory,name),events.map(row=>JSON.stringify(row)+'\n').join(''),{mode:0o600});
 const baseline=baselineFixture(t,{revision:personal?catalog106SourceSHA:sourceSHA}),cohort=stagedArtifactCatalog?personalIntegrationsStagedInputsFixture(profile,personalStagedInputsSchema):componentCohortFixture(profile),apps=Object.fromEntries(cohort.deployments.map(app=>[app.metadata.name,app]));
 if(stagedArtifactCatalog){
  for(const value of [cohort.value,cohort.target,cohort.observed])value.context='k3d-synthetic';
  cohort.observed.nodes[0].metadata.name='k3d-synthetic-server-0';
  cohort.observed.nodes[0].status.conditions.push({type:'DiskPressure',status:'False'});
 }
 for(const part of cohort.value.components){
  const lock=JSON.parse(Buffer.from(part.componentLockRawBase64,'base64'));lock.source_sha=sourceSHA;
  if(stagedArtifactCatalog){cohort.value.registry='registry.local.kodex';cohort.value.repositoryPrefix='kodex';
   lock.image_ref=`${cohort.value.registry}/${cohort.value.repositoryPrefix}/${part.component}@${lock.digest}`;}
  replaceComponentLock(part,lock);part.nodeProof.sourceSHA=sourceSHA;
  if(stagedArtifactCatalog){part.nodeProof.imageRef=lock.image_ref;part.nodeProof.nodes.forEach(node=>{node.imageRef=lock.image_ref;node.name='k3d-synthetic-server-0';});}
 }
 const api=apps['control-api-gateway'],cp=apps['control-plane'];
 if(stagedArtifactCatalog){
  for(const input of [cohort.inputs,cohort.inputs.issuerInputs,cohort.inputs.authority,cohort.inputs.cpPolicy])input.sourceSHA=sourceSHA;
  cp.spec.template.spec.containers[0].env.push(...structuredClone(baseline.deployment.spec.template.spec.containers[0].env));
 }else{
 cp.spec.template.spec.containers[0].env=structuredClone(baseline.deployment.spec.template.spec.containers[0].env);
 cp.spec.template.spec.containers[0].volumeMounts=structuredClone(baseline.deployment.spec.template.spec.containers[0].volumeMounts);
 cp.spec.template.spec.volumes=structuredClone(baseline.deployment.spec.template.spec.volumes);
 }
 api.spec.replicas=3;for(const key of ['replicas','updatedReplicas','readyReplicas','availableReplicas'])api.status[key]=3;
 Object.assign(baseline.profile,{...cohort.target,resources:[...Object.values(apps),baseline.publisher,baseline.registry,baseline.service].map(value=>
  ({apiVersion:value.apiVersion,kind:value.kind,name:value.metadata.name,uid:value.metadata.uid}))});
 baseline.resources.set('namespace/kube-system',{metadata:{uid:cohort.target.clusterUID}});
 baseline.resources.set('namespace/kodex-system',{metadata:{uid:cohort.target.namespaceUID}});
 baseline.resources.set('configmap/kodex-installation-profile',cohort.observed.installation);
 for(const app of Object.values(apps))baseline.resources.set('deployment/'+app.metadata.name,app);
 baseline.plan.policyTransition=readCatalogTarget(baseline.profile,baseline.profile.context,baseline.get).transition;
 baseline.plan.context=cohort.target.context;baseline.plan.source=authorityRoot;baseline.plan.revision=authoritySHA;baseline.plan.kubeconfig=kubeconfig;
 baseline.events.at(-1).status='RUNNING';delete baseline.events.at(-1).rotationStatus;
 baseline.events[0].planSHA256=fingerprint(baseline.plan);
 const authority=personal?{postgres:{pod:'kodex-postgresql-0',uid:baseline.pg.metadata.uid,ownerUID:baseline.set.metadata.uid},postgresBackend:baseline.backend}:baseline.read();
 const observed={context:cohort.target.context,clusterUID:cohort.target.clusterUID,namespaceUID:cohort.target.namespaceUID,installation:cohort.observed.installation,resources:{items:[]}};
 if(rolloutAnnotations)api.metadata.annotations={...api.metadata.annotations,...rolloutAnnotations.before};
 const closedPlan=planComponentAPIMaintenance(api,baseline.profile,observed,closedSourceSHA,'close');
 const closed={plan:closedPlan,events:[{type:'HEADER',schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(closedPlan),maintenanceID:closedPlan.maintenanceID},
  {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'CLOSED',deploymentUID:api.metadata.uid,maintenanceID:closedPlan.maintenanceID}]};
 api.spec=structuredClone(closedPlan.afterSpec);api.metadata.annotations=structuredClone(closedPlan.afterAnnotations);
 for(const key of ['replicas','updatedReplicas','readyReplicas','availableReplicas'])api.status[key]=0;
 save('close.json',closedPlan);journal('close.jsonl',closed.events);
 const isolationPlan=planComponentAPITraffic({service:baseline.service,deployment:api,resources:{items:[]},target:baseline.profile,observed,sourceSHA:personal?sourceSHA:executorSHA,action:'isolate',closed,...(personal?{}:{authorityExecutorSHA:authoritySHA,...(authoritySquashSource?{authoritySourceEquivalentSHA:authorityEquivalentSHA}:{}),
  migrationRestore:{profile,closedPlanPath:join(directory,'close.json'),closedEvidencePath:join(directory,'close.jsonl'),authority}})});
 const isolation={plan:isolationPlan,events:[{type:'HEADER',schema:'kodex.component-api-traffic-evidence/v1',planSHA256:fingerprint(isolationPlan),maintenanceID:isolationPlan.maintenanceID},
  {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'ISOLATED',serviceUID:baseline.service.metadata.uid,maintenanceID:isolationPlan.maintenanceID}],
  service:baseline.service,slices:{items:[]},pods:{items:[]}};
 baseline.service.spec=structuredClone(isolationPlan.afterSpec);baseline.service.metadata.annotations=structuredClone(isolationPlan.afterAnnotations);
 save('isolate.json',isolationPlan);journal('isolate.jsonl',isolation.events);
 const configuration=immutableMigrationProfile(profile);
 const base=JSON.parse(execFileSync('yq',['-o=json','.',`${root}/deploy/k8s/base/control-plane/migration-job.yaml`],{encoding:'utf8'}));
 Object.assign(base.metadata,{uid:uid(200),resourceVersion:'1',namespace:'kodex-system'});
 Object.assign(base.metadata.labels,{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'production','kodex.dev/profile':'web-only'});delete base.metadata.labels['kodex.dev/local-profile'];
 const source={path:sourceRoot,revision:sourceSHA};
 const migration=planImmutableControlPlaneMigration(base,source,()=>({revision:sourceSHA}),uid(201),cohort.value,1,profile);
 const hash=name=>createHash('sha256').update(readFileSync(`${root}/${name}`)).digest('hex');
 const bundleSpec=JSON.parse(execFileSync('yq',['-o=json','select(.kind == "Bundle" and .metadata.name == "control-plane-postgresql-ca") | .spec',`${root}/deploy/k8s/base/platform-state/ca-bundles.yaml`],{encoding:'utf8'}));
 const dependencyIDs={serviceaccount:uid(210),secret:uid(211)},caUID=uid(212),bundleUID=uid(213);
 Object.assign(migration,{clusterUID:cohort.target.clusterUID,namespaceUID:cohort.target.namespaceUID,context:cohort.target.context,
  portableTarget:cohort.target,componentHandoff:planComponentMigrationHandoff(cohort.value,Object.values(apps),source,undefined,stagedArtifactCatalog?cohort.inputs:undefined),
  anchor:{kind:'Deployment',name:'control-plane',uid:cp.metadata.uid,resourceVersion:cp.metadata.resourceVersion,specSHA256:fingerprint(cp.spec),
   stoppedSpecSHA256:fingerprint({...cp.spec,replicas:0}),previousImage:cp.spec.template.spec.containers[0].image},
  dependencies:[{kind:'serviceaccount',name:'control-plane-migrator',uid:dependencyIDs.serviceaccount,resourceVersion:'1'},
   {kind:'secret',name:'control-plane-postgres-migration',uid:dependencyIDs.secret,resourceVersion:'1'},
   {kind:'configmap',name:'control-plane-postgresql-ca',uid:caUID,resourceVersion:'1',bundle:{uid:bundleUID,resourceVersion:'1',specSHA256:fingerprint(bundleSpec)}}],
  postgres:authority.postgres,postgresBackend:{...authority.postgresBackend,database:'control_plane',databaseOID:'20001'},
  baseRenderPath:`${root}/deploy/k8s/base/control-plane/migration-job.yaml`,baseRenderSHA256:fingerprint(base),
  migrationFileSHA256:hash('services/internal/control-plane/cmd/cli/migrations/'+configuration.file),
  preconditionSQLSHA256:hash('tools/release/'+configuration.preconditionFile),postconditionSQLSHA256:hash('tools/release/'+configuration.postconditionFile),
  componentDrainSQLSHA256:hash('tools/release/portable-component-migration-drain.sql')});
 const admissionKube=(args,input)=>args.includes('endpointslices')||args[args.indexOf('get')+1]==='pods'?'{"items":[]}':baseline.kube(args,input);
 if(personal){migration.deliveryPrerequisite=personalIntegrationsDeliveryPrerequisite;migration.kubeconfig=kubeconfig;}
 migration.apiMaintenance=readMigrationAPIAdmission({...(personal?{profile,closedPlanPath:join(directory,'close.json'),closedEvidencePath:join(directory,'close.jsonl')}:{}),isolationPlanPath:join(directory,'isolate.json'),isolationEvidencePath:join(directory,'isolate.jsonl')},admissionKube,{stopped:true}).admission;
 let backupPlan,backupEvents,backupController,backupReplicaSet,stagedExtension;
 if(stagedArtifactCatalog){
  migration.personalIntegrationsInputs=cohort.inputs;
  const fixture=backupMaintenanceFixture();
  const target={...cohort.target,resources:[{apiVersion:'apps/v1',kind:'Deployment',name:'backup-controller',uid:fixture.deployment.metadata.uid}]};
  backupPlan=planBackupControllerMaintenance(fixture.deployment,target,{...observed,postgres:migration.postgres},sourceSHA,'stop',undefined,fixture.inventory());
  Object.assign(backupPlan,{planPath:join(directory,'backup-stop.json'),evidencePath:join(directory,'backup-stop.jsonl'),migrationProfile:profile,
   postgresBackend:structuredClone(migration.postgresBackend),ownerSQLSHA256:{before:migration.preconditionSQLSHA256,after:migration.postconditionSQLSHA256}});
  backupController=structuredClone(fixture.deployment);backupController.spec=structuredClone(backupPlan.afterSpec);
  backupController.metadata.annotations=structuredClone(backupPlan.afterAnnotations);backupController.metadata.generation=2;
  backupController.status={observedGeneration:2,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};
  backupReplicaSet=structuredClone(fixture.rs);backupReplicaSet.spec.replicas=0;backupReplicaSet.status.replicas=0;
  backupEvents=[{type:'HEADER',...backupControllerMaintenanceHeader(backupPlan)},{type:'INTENT',step:'apply'},
   {type:'OBSERVED',state:'STOPPED',deploymentUID:backupController.metadata.uid,maintenanceID:backupPlan.maintenanceID}];
  migration.backupMaintenance={planPath:backupPlan.planPath,evidencePath:backupPlan.evidencePath,planSHA256:fingerprint(backupPlan)};
  // Builder расширяет только test fixture до вычисления immutable pins. Без
  // него APPLY должен закрыться на настоящей factory отсутствующих prerequisites.
  stagedExtension=prepareStagedArtifactCatalog?.({directory,root,source,cohort,apps,baseline,migration,backupPlan,backupEvents,save,journal,kubeconfig});
 }
 const finish=stagedExtension=>{
 assert.ok(stagedExtension===undefined||stagedExtension&&typeof stagedExtension==='object'&&!Array.isArray(stagedExtension));
 const planSHA=fingerprint(migration),job=structuredClone(migration.job);
 if(stagedArtifactCatalog){
  backupEvents.push({type:'BACKUP_MIGRATION_BOUND',migrationPlanSHA256:planSHA});
  save('backup-stop.json',backupPlan);journal('backup-stop.jsonl',backupEvents);
 }

 Object.assign(job.metadata,{uid:uid(220),resourceVersion:'1',annotations:{[configuration.annotation]:planSHA}});
 job.status={succeeded:1,conditions:[{type:'Complete',status:'True'}]};
 const jobPod={apiVersion:'v1',kind:'Pod',metadata:{name:job.metadata.name+'-pod',namespace:'kodex-system',uid:uid(221),resourceVersion:'1',
  labels:{...job.spec.template.metadata.labels,'batch.kubernetes.io/job-name':job.metadata.name},
  ownerReferences:[{apiVersion:'batch/v1',kind:'Job',name:job.metadata.name,uid:job.metadata.uid,controller:true}]},spec:structuredClone(job.spec.template.spec),
  status:{phase:'Succeeded',containerStatuses:job.spec.template.spec.containers.map(container=>({name:container.name,restartCount:0,imageID:container.image,state:{terminated:{exitCode:0}}})),
   initContainerStatuses:job.spec.template.spec.initContainers.map(container=>({name:container.name,restartCount:0,state:{terminated:{exitCode:0}}}))}};
 const events=stagedArtifactCatalog?[{type:'HEADER',version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256:planSHA},
  {type:'CREATE_INTENT',jobName:job.metadata.name},{type:'CREATE_ACK',jobUID:job.metadata.uid},
  {type:'READBACK',jobName:job.metadata.name,jobUID:job.metadata.uid,status:'PASS',schemaPostcondition:'PASS',sourceRevision:sourceSHA,migrationVersion:configuration.version},
  {type:'STOP_ACK'},...migration.componentHandoff.components.map(part=>({type:'PAIR_HANDOFF_INTENT',name:part.name,stage:'stagedSpec'})),
  {type:'PAIR_HANDOFF_ACK',publicationSHA256:migration.componentHandoff.publicationSHA256}]:[{type:'HEADER',version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256:planSHA},{type:'CREATE_INTENT'},{type:'CREATE_ACK',jobUID:job.metadata.uid},
  {type:'STOP_ACK'},{type:'PAIR_HANDOFF_ACK',publicationSHA256:migration.componentHandoff.publicationSHA256},{type:'RESUME_INTENT'},{type:'RESUME_ACK'},
  ...Object.keys(apps).filter(name=>!['control-plane','control-api-gateway'].includes(name)).map(name=>({type:'PAIR_RESUME_ACK',name}))];
 for(const part of migration.componentHandoff.components){
  const app=apps[part.name];app.spec=structuredClone(stagedArtifactCatalog||part.maintenance?part.stagedSpec:part.resumedSpec);app.metadata.generation=2;
  app.status={observedGeneration:2,...Object.fromEntries(['replicas','updatedReplicas','readyReplicas','availableReplicas'].map(name=>[name,app.spec.replicas]))};
 }
 cp.metadata.annotations={...(cp.metadata.annotations??{}),[configuration.annotation]:planSHA};
 if(rolloutAnnotations)api.metadata.annotations={...api.metadata.annotations,...rolloutAnnotations.after};
 let openPlan,openEvents;
 if(!stagedArtifactCatalog){
 openPlan=planComponentAPIMaintenance(api,baseline.profile,observed,personal?sourceSHA:executorSHA,'open',closed,isolation);
 api.spec=structuredClone(openPlan.afterSpec);api.metadata.annotations=structuredClone(openPlan.afterAnnotations);
 for(const key of ['replicas','updatedReplicas','readyReplicas','availableReplicas'])api.status[key]=api.spec.replicas;
 openEvents=[{type:'HEADER',schema:'kodex.component-api-maintenance-evidence/v1',planSHA256:fingerprint(openPlan),maintenanceID:openPlan.maintenanceID},
  {type:'INTENT',step:'apply'},{type:'OBSERVED',state:'OPEN',deploymentUID:api.metadata.uid,maintenanceID:openPlan.maintenanceID}];
 save('open.json',openPlan);journal('open.jsonl',openEvents);
 }
 save('migration.json',migration);journal('migration.jsonl',events);
 save('authority.json',baseline.plan);journal('authority.jsonl',baseline.events);save('target.json',baseline.profile);
 const state={...stagedExtension,...(stagedArtifactCatalog?{stagedArtifactCatalog,backupController,backupReplicaSet,backupPlan,backupEvents}:{}),sourceSHA,executorSHA,context:cohort.target.context,clusterUID:cohort.target.clusterUID,namespaceUID:cohort.target.namespaceUID,kubeconfig,
  apps:{...apps,'internal-rpc-authority-publisher':baseline.publisher,...(stagedArtifactCatalog?{'backup-controller':backupController}:{})},nodes:cohort.observed.nodes,service:baseline.service,
  pg:baseline.pg,postgresSet:baseline.set,authorityBackend:baseline.backend,owner:baseline.owner,dependencies:dependencyIDs,caUID,bundleUID,bundleSpec,
  configMaps:{'kodex-installation-profile':cohort.observed.installation,'internal-rpc-authority-publisher-target-registry':baseline.registry,'oidc-public':baseline.oidc,...stagedExtension?.configMaps},
  migration,job,jobPod,postcondition:{profile:(personal?'PERSONAL_INTEGRATIONS_':'SENTRY_OBSERVABILITY_')+(backup?'BACKUP_':'')+'V1',database:'control_plane',readOnly:'on',historyMatches:true,schemaMatches:true,serverVersionNum:180003,gooseVersion:Number(configuration.version),
   backupQualification:backup?'CANONICAL_BACKUP':'NONE',publicSequenceSafe:true,schemaCatalog:(personal?personalIntegrationsCatalog:sentryObservabilityCatalog)[backup?'backup':'normal'].after},effects:[],calls:[]};
 save('state.json',state);
 const fake=`#!${process.execPath}
const fs=require('node:fs'),path=require('node:path'),file=path.join(__dirname,'..','state.json');
const {runCLI}=require(${JSON.stringify(fileURLToPath(new URL('./routine-migration-kubectl.cjs',import.meta.url)))});
const args=process.argv.slice(2);
// Асинхронный readback оставляет stdin открытым. Читаем его только для команд с входом.
const input=args.includes('exec')||args.includes('create')?fs.readFileSync(0,'utf8'):'';
runCLI(file,args,input);
`;
 writeFileSync(join(bin,'kubectl'),fake,{mode:0o700});
 const read=name=>JSON.parse(readFileSync(join(directory,name),'utf8'));
 const edit=fn=>{const value=read('state.json');fn(value);save('state.json',value);};
 const common=['--kubeconfig',kubeconfig,'--context',cohort.target.context,'--closed-plan',join(directory,'close.json'),'--closed-evidence',join(directory,'close.jsonl')];
 const inputs=['--authority-plan',join(directory,'authority.json'),'--authority-evidence',join(directory,'authority.jsonl'),
  '--isolation-plan',join(directory,'isolate.json'),'--isolation-evidence',join(directory,'isolate.jsonl'),
  '--migration-plan',join(directory,'migration.json'),'--migration-evidence',join(directory,'migration.jsonl'),
  '--api-open-plan',join(directory,'open.json'),'--api-open-evidence',join(directory,'open.jsonl')];
 const authoritySourceArgs=['--authority-executor-sha',authoritySHA,...(authoritySquashSource?['--authority-source-equivalent-sha',authorityEquivalentSHA]:[])];
 const run=(phase,extra=[],changeArguments)=>{
  const argv=[fileURLToPath(new URL('../component-api-traffic.mjs',import.meta.url)),phase,
  '--plan',join(directory,'restore.json'),...common,...(phase==='plan'?['--action','restore','--target',join(directory,'target.json'),
   '--migration-profile',profile,'--migration-source-sha',sourceSHA,...authoritySourceArgs,...inputs]:['--evidence',join(directory,'restore.jsonl'),
   ...(phase==='apply'?['--approved-plan-sha',fingerprint(read('restore.json')),...inputs]:[])]),...extra];
  changeArguments?.(argv);
  return spawnSync(process.execPath,argv,{encoding:'utf8',env:{PATH:bin+':'+process.env.PATH,HOME:process.env.HOME,LANG:'C'},timeout:120000});
 };
 const runInspect=(extra=[])=>spawnSync(process.execPath,[join(executorRoot,'tools/release/control-plane-migration.mjs'),'inspect',
  '--profile',profile,'--context',cohort.target.context,'--kubeconfig',kubeconfig,'--plan',join(directory,'migration.json'),
  '--evidence',join(directory,'migration.jsonl'),'--evidence-read-only','true',...(!stagedArtifactCatalog?['--api-open-plan',join(directory,'open.json'),'--api-open-evidence',join(directory,'open.jsonl')]:[]),...extra],
  {encoding:'utf8',env:{PATH:bin+':'+process.env.PATH,HOME:process.env.HOME,LANG:'C'},timeout:120000});
 const runIsolation=(phase,changeArguments)=>{const argv=[fileURLToPath(new URL('../component-api-traffic.mjs',import.meta.url)),phase,
  '--plan',join(directory,'new-isolate.json'),...common,...(phase==='plan'?['--action','isolate','--target',join(directory,'target.json'),'--migration-profile',profile,...authoritySourceArgs]:
   ['--evidence',join(directory,'new-isolate.jsonl'),...(phase==='apply'?['--approved-plan-sha',fingerprint(read('new-isolate.json'))]:[])]),
  ...(['plan','apply'].includes(phase)?['--authority-plan',join(directory,'authority.json'),'--authority-evidence',join(directory,'authority.jsonl')]:[])];
  changeArguments?.(argv);return spawnSync(process.execPath,argv,{encoding:'utf8',env:{PATH:bin+':'+process.env.PATH,HOME:process.env.HOME,LANG:'C'},timeout:120000});};
 let artifactCatalogPlanSHA;
 const runArtifactCatalog=(phase,extra=[],changeArguments)=>{
  assert.ok(stagedArtifactCatalog&&['plan','apply'].includes(phase));
  const argv=[join(executorRoot,'tools/release/control-plane-migration.mjs'),phase+'-artifact-grant-catalog',
   '--profile',profile,'--context',cohort.target.context,'--kubeconfig',kubeconfig,'--plan',join(directory,'migration.json'),
   '--evidence',join(directory,'migration.jsonl'),...(phase==='apply'?['--confirm','APPLY-STAGING-CP-MIGRATION',
    ...(!extra.includes('--artifact-grant-catalog-plan-sha')&&artifactCatalogPlanSHA?['--artifact-grant-catalog-plan-sha',artifactCatalogPlanSHA]:[])]:[]),...extra];
  changeArguments?.(argv);
  const result=spawnSync(process.execPath,argv,{encoding:'utf8',env:{PATH:bin+':'+process.env.PATH,HOME:process.env.HOME,LANG:'C'},timeout:120000});
  if(phase==='plan'&&result.status===0){const output=JSON.parse(result.stdout);assert.equal(output.status,'PLAN_ONLY');artifactCatalogPlanSHA=output.planSHA256;}
  return result;
 };
 const pass=phase=>{const result=run(phase);assert.equal(result.status,0,result.stderr+"\n"+(read('state.json').diagnostic??''));return JSON.parse(result.stdout);};
 return {directory,root,sourceRoot,sourceSHA,executorSHA,authoritySHA,authorityEquivalentSHA,state:()=>read('state.json'),read,edit,run,runInspect,runIsolation,runArtifactCatalog,pass,save,journal,migration,closed,isolation,openPlan,openEvents,
  kube:(args,input='')=>transport.run(state,['--kubeconfig',kubeconfig,'--context',cohort.target.context,...args],String(input))};
 };
 return stagedExtension?.then?stagedExtension.then(finish):finish(stagedExtension);
}
