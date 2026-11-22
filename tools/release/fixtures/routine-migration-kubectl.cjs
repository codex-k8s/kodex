// Metadata-only Kubernetes/SQL transport для настоящих CLI subprocesses.
// Никакой команды к кластеру, сети или пользовательским данным здесь нет.
const fs=require('node:fs');
const assert=require('node:assert/strict');
const artifactVersion=20261121000100,artifactJobName='control-plane-migrate-'+artifactVersion;
const syntheticUID=number=>'00000000-0000-4000-8000-'+String(number).padStart(12,'0');
const drainCounters=['activeRuntimeLeases','activeArchiveTasks','activeRuntimeRuns','activeRunNodes','activeSessionTurns',
 'claimedRuntimeLeases','openBuilds','pendingAdmissions','pendingPromotions','activeModelCatalogTasks','activeRenewalTasks',
 'oldClaimSessions','activeIntegrationInvocations','activeIntegrationConnectionTests','activePersonalCredentialTests',
 'activeRepositoryBundles','activeConfigurationSources','activeConfigurationWritebacks','activeInteractionDeliveries'];

function inventory(state){
 const values=Object.values(state.apps).flatMap((app,appIndex)=>{
  const rs=app.metadata.name==='backup-controller'&&state.backupReplicaSet||{apiVersion:'apps/v1',kind:'ReplicaSet',metadata:{name:app.metadata.name+'-rs',namespace:'kodex-system',uid:app.metadata.uid+'-rs',
   ownerReferences:[{kind:'Deployment',name:app.metadata.name,uid:app.metadata.uid,controller:true}]}};
  if(state.stagedArtifactCatalog&&app.metadata.name!=='backup-controller')rs.metadata.uid=syntheticUID(501+appIndex);
  return [rs,...Array.from({length:app.spec.replicas},(_,index)=>({apiVersion:'v1',kind:'Pod',metadata:{name:app.metadata.name+'-'+index,namespace:'kodex-system',
   uid:state.stagedArtifactCatalog?syntheticUID(610+appIndex*10+index):app.metadata.uid+'-pod-'+index,
   resourceVersion:'1',labels:{'app.kubernetes.io/name':app.metadata.name},ownerReferences:[{kind:'ReplicaSet',name:rs.metadata.name,uid:rs.metadata.uid,controller:true}]},
   spec:structuredClone(app.spec.template.spec),status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:app.spec.template.spec.containers.map(container=>
    ({name:container.name,ready:true,state:{running:{}},imageID:container.image}))}}))];
 });
 return [...values,state.job,state.jobPod,state.artifactJob,state.artifactPod].filter(Boolean);
}

function backupInventory(state){
 const postgresSet=structuredClone(state.postgresSet);
 postgresSet.metadata.labels={'app.kubernetes.io/name':'kodex-postgresql'};
 const resources=[...Object.values(state.apps),postgresSet,state.pg,...inventory(state),state.otherBackupProducer].filter(Boolean);
 return resources.map(resource=>{
  const {kind,metadata,status}=resource;
  const spec=kind==='Pod'?resource.spec:resource.spec?.template?.spec;
  return [kind,metadata.name,metadata.uid,metadata.namespace,metadata.labels?.['app.kubernetes.io/name']??'',
   JSON.stringify(metadata.ownerReferences??[]),spec?.serviceAccountName??'',
   (spec?.volumes??[]).flatMap(volume=>volume.secret?.secretName?[volume.secret.secretName]:[]).join(' '),'','','',
   resource.spec?.replicas??'',status?.replicas??'',metadata.generation??'',status?.observedGeneration??''].join('\t');
 }).join('\n')+'\n';
}

function createArtifactJob(state,args,input){
 const job=JSON.parse(input);
 assert.equal(job.kind,'Job');
 assert.equal(job.metadata.name,artifactJobName);
 assert.equal(job.metadata.namespace,'kodex-system');
 assert.deepEqual(job.spec.template.spec.containers[0].args,['up-artifact-retention-grant-catalog']);
 assert.match(job.metadata.annotations['kodex.dev/artifact-grant-schema-release'],/^[a-f0-9]{64}$/);
 assert.equal(job.spec.template.spec.containers[0].image,state.migration.job.spec.template.spec.containers[0].image);
 if(args.includes('--dry-run=server'))return JSON.stringify(job);
 assert.equal(state.artifactJob,undefined,'repeat ART050 create');
 Object.assign(job.metadata,{uid:'00000000-0000-4000-8000-000000000450',resourceVersion:'1'});
 job.status={succeeded:1,conditions:[{type:'Complete',status:'True'}]};
 const template=job.spec.template;
 const terminated=container=>({name:container.name,restartCount:0,imageID:container.image,state:{terminated:{exitCode:0}}});
 state.artifactJob=job;
 state.artifactPod={apiVersion:'v1',kind:'Pod',metadata:{name:artifactJobName+'-pod',namespace:'kodex-system',
  uid:'00000000-0000-4000-8000-000000000451',resourceVersion:'1',
  labels:{...template.metadata.labels,'batch.kubernetes.io/job-name':artifactJobName},
  ownerReferences:[{apiVersion:'batch/v1',kind:'Job',uid:job.metadata.uid,name:artifactJobName,controller:true}]},
  spec:structuredClone(template.spec),status:{phase:'Succeeded',containerStatuses:template.spec.containers.map(terminated),
   initContainerStatuses:(template.spec.initContainers??[]).map(terminated)}};
 state.effects.push('artifact-grant-catalog');
 return state.artifactLostACK?{lost:true}:JSON.stringify(job);
}

function run(state,args,input,prerequisite=()=>undefined){
 state.calls.push({args});
 assert.equal(args[args.indexOf('--kubeconfig')+1],state.kubeconfig);assert.equal(args[args.indexOf('--context')+1],state.context);
 if(state.artifactIssuerRenderPath&&args.includes('kustomize')){
  assert.equal(args.at(-1),state.migration.source.path+'/deploy/k8s/profiles/web-only');
  return fs.readFileSync(state.artifactIssuerRenderPath,'utf8');
 }
 if(args.includes('exec')){
  assert.ok(args.includes('env')&&args[args.indexOf('env')+1]==='-i'&&args.includes('--host=/var/run/postgresql')&&args.includes('--no-password'));
  assert.equal(args[args.indexOf('-c')+1],'postgresql');
  const authority=args.includes('--dbname=internal_rpc_authority'),backend=authority?state.authorityBackend:state.migration.postgresBackend;
  if(input.includes('pg_catalog.pg_control_system()'))return JSON.stringify(backend);
  assert.ok(input.includes('READ ONLY')&&input.includes('ROLLBACK;'));
  if(authority){
   const reply=prerequisite(args,input);
   if(reply!==undefined)return reply;
   assert.ok(input.includes('internal_rpc_authority.authority_rotation_status()'));
   state.owner.observedAt=state.staleOwner??new Date().toISOString();
   if(state.apiDriftAfterOwner){
    const api=state.apps['control-api-gateway'];
    if(state.apiDriftAfterOwner==='image')api.spec.template.spec.containers[0].image='example.invalid/foreign@sha256:'+'f'.repeat(64);
    if(state.apiDriftAfterOwner==='env')api.spec.template.spec.containers[0].env=[{name:'FOREIGN',value:'changed'}];
    if(state.apiDriftAfterOwner==='replicas'){api.spec.replicas=2;for(const name of ['replicas','readyReplicas','updatedReplicas','availableReplicas'])api.status[name]=2;}
   }
   return JSON.stringify(state.owner);
  }
  if(state.stagedArtifactCatalog){
   const gooseVersion=state.artifactJob?artifactVersion:state.postcondition.gooseVersion;
   if(input.includes('PORTABLE_COMPONENT_DRAIN_V1'))return JSON.stringify({
    profile:'PORTABLE_COMPONENT_DRAIN_V1',database:'control_plane',readOnly:'on',gooseVersion,
    ...Object.fromEntries(drainCounters.map(name=>[name,0])),...state.drainPatch});
   if(input.includes('artifactGrantCatalogMatches'))return JSON.stringify({
    ...state.postcondition,gooseVersion,artifactGrantCatalogMatches:Boolean(state.artifactJob),...state.artifactPostconditionPatch});
  }
  assert.ok(input.includes(state.migration.profile.startsWith('personal-integrations')?'PERSONAL_INTEGRATIONS':'SENTRY_OBSERVABILITY'));
  return JSON.stringify({...state.postcondition,...state.postconditionPatch});
 }
 const get=args.indexOf('get');
 if(get>=0){
  const kind=args[get+1],name=args[get+2],jsonpath=args.find(value=>value.startsWith('jsonpath='));
  if(kind==='namespace'){
   if(jsonpath)return state.namespaceUID;
   const metadata=name==='kube-system'?{uid:state.clusterUID}:{uid:state.namespaceUID,labels:{'app.kubernetes.io/part-of':'kodex'}};
   return JSON.stringify({metadata});
  }
  if(kind==='nodes')return JSON.stringify({items:state.nodes});
  if(state.stagedArtifactCatalog&&kind==='deployments,statefulsets,daemonsets,cronjobs,jobs,replicasets,replicationcontrollers,pods')
   return backupInventory(state);
  if(!jsonpath||!['statefulset','pod'].includes(kind)){
   const reply=prerequisite(args,input);
   if(reply!==undefined)return reply;
  }
  if(kind==='replicasets,pods,jobs')return inventory(state).map(resource=>[resource.kind,resource.metadata.name,resource.metadata.uid,
   resource.metadata.labels?.['app.kubernetes.io/name']??'',resource.metadata.namespace,JSON.stringify(resource.metadata.ownerReferences??[])].join('\t')).join('\n')+'\n';
  if(kind==='pods'){
   if(jsonpath)return inventory(state).filter(resource=>resource.kind==='Pod').map(pod=>[pod.metadata.name,pod.metadata.uid,pod.metadata.resourceVersion??'1',
    pod.metadata.labels?.['batch.kubernetes.io/job-name']??'',JSON.stringify(pod.metadata.ownerReferences??[])].join('|')).join('\n')+'\n';
   if(args.some(value=>value.startsWith('batch.kubernetes.io/job-name=')))return JSON.stringify({items:[state.jobPod]});
   return '{"items":[]}';
  }
  if(kind==='deployment')return JSON.stringify(state.apps[name]);
  if(kind==='service')return JSON.stringify(state.service);
  if(kind==='endpointslices')return '{"items":[]}';
  if(kind==='statefulset')return JSON.stringify(state.postgresSet);
  if(kind==='pod'){
   if(name==='kodex-postgresql-0')return jsonpath?[state.pg.metadata.uid,'StatefulSet','kodex-postgresql',state.postgresSet.metadata.uid,'kodex-postgresql','Running',''].join('|'):JSON.stringify(state.pg);
   return JSON.stringify(inventory(state).find(resource=>resource.kind==='Pod'&&resource.metadata.name===name));
  }
  if(kind==='job')return state.stagedArtifactCatalog&&name===artifactJobName?(state.artifactJob?JSON.stringify(state.artifactJob):''):JSON.stringify(state.job);
  if(kind==='jobs'&&state.stagedArtifactCatalog)return JSON.stringify({items:[state.job,state.artifactJob].filter(Boolean)});
  if(kind==='bundles.trust.cert-manager.io')return [state.bundleUID,'1',JSON.stringify(state.bundleSpec)].join('|');
  if(kind==='configmap'&&name==='control-plane-postgresql-ca')return [state.caUID,'1','control-plane-postgresql-ca',JSON.stringify([{apiVersion:'trust.cert-manager.io/v1alpha1',kind:'Bundle',name,uid:state.bundleUID,controller:true}])].join('|');
  if(kind==='serviceaccount'||kind==='secret')return [state.dependencies[kind],'1','kodex'].join('|');
  if(kind==='configmap')return JSON.stringify(state.configMaps[name]);
  throw Error('unexpected synthetic GET');
 }
 if(state.stagedArtifactCatalog&&args.includes('create'))return createArtifactJob(state,args,input);
 if(state.stagedArtifactCatalog&&args.includes('logs')&&args.includes('job/'+artifactJobName))
  return 'Artifact retention grant catalog migration completed at version '+artifactVersion+'\n';
 if(args.includes('logs')&&state.migration.profile.startsWith('personal-integrations'))return (state.migration.profile.endsWith('-backup')?'Personal integrations backup migration completed at version ':'migrated database to version: ')+state.migration.migrationVersion+'\n';
 if(args.includes('logs'))return state.migration.profile==='sentry-observability-backup'?
  'Sentry observability backup migration completed at version 20261114130000\n':'migrated database to version: 20261114130000\n';
 const patch=args.indexOf('patch');
 if(patch>=0){
  assert.equal(args[patch+1],'service');assert.equal(args[patch+2],'control-api-gateway');
  const operations=JSON.parse(fs.readFileSync(args[args.indexOf('--patch-file')+1],'utf8'));
  for(const operation of operations.filter(value=>value.op==='test')){
   let value=state.service;for(const key of operation.path.slice(1).split('/'))value=value[key];assert.deepEqual(value,operation.value);
  }
  if(args.includes('--dry-run=server'))return 'dry run';
  state.effects.push('service');state.service.metadata.annotations=operations[3].value;state.service.spec=operations[4].value;
  state.service.metadata.resourceVersion=String(Number(state.service.metadata.resourceVersion)+1);
  if(state.lostServiceACK)return {lost:true};
  return 'patched';
 }
 throw Error('unexpected synthetic operation');
}
async function runCLI(path,args,input){
 const state=JSON.parse(fs.readFileSync(path));
 try{
  const {artifactCatalogPrerequisiteTransport}=state.artifactPrerequisites?
   await import('./artifact-catalog-prerequisites-synthetic.mjs'):{};
  const result=run(state,args,input,artifactCatalogPrerequisiteTransport?
   (argv,body)=>artifactCatalogPrerequisiteTransport(state.artifactPrerequisites,argv,body):undefined);
  if(result?.lost){process.exitCode=9;return;}
  process.stdout.write(result);
 }catch(error){
  state.diagnostic=String(error.stack).slice(0,4000);
  process.stderr.write('Synthetic transport refused\n');
  process.exitCode=7;
 }finally{
  fs.writeFileSync(path,JSON.stringify(state));
 }
}

module.exports={run,inventory,runCLI};
