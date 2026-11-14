// Отдельный subprocess заменяет только Kubernetes/PostgreSQL transport.
// Никакого обращения к kubeconfig, sockets, реальной сети и credentials нет.
const fs=require('node:fs'),crypto=require('node:crypto'),path=require('node:path'),assert=require('node:assert/strict'),producerMetadata=require('./telegram-producer-metadata.cjs');
const statePath=process.env.KODEX_TELEGRAM_SYNTHETIC_STATE,s=JSON.parse(fs.readFileSync(statePath)),argv=process.argv.slice(2);
const prefix=['--kubeconfig',s.kubeconfig,'--context',s.context];
assert.deepEqual(argv.slice(0,prefix.length),prefix);assert.ok(['--request-timeout=25s','--request-timeout=30s'].includes(argv[prefix.length]));prefix.push(argv[prefix.length]);
const args=argv.slice(prefix.length);s.calls.push(args.map(value=>value.length>512?'<bounded-fixture-argument>':value));
for(const resource of Object.values(s.resources))for(const [key,value]of Object.entries(resource.kind==='ConfigMap'?resource.data??{}:{}))
 if(value?.fixtureBodySHA256)resource.data[key]=fs.readFileSync(path.join(path.dirname(statePath),'fixture-body-'+value.fixtureBodySHA256),'utf8');
const save=()=>{
 const stored=structuredClone(s);
 for(const resource of Object.values(stored.resources))for(const [key,value]of Object.entries(resource.kind==='ConfigMap'?resource.data??{}:{}))if(typeof value==='string'&&value.length>8192){
  const digest=crypto.createHash('sha256').update(value).digest('hex'),file=path.join(path.dirname(statePath),'fixture-body-'+digest);
  if(!fs.existsSync(file))fs.writeFileSync(file,value,{mode:0o600,flag:'wx'});resource.data[key]={fixtureBodySHA256:digest};
 }
 fs.writeFileSync(statePath,JSON.stringify(stored));
};
const out=value=>{save();process.stdout.write(typeof value==='string'?value:JSON.stringify(value));};
const fail=()=>{save();process.exit(71);};
const key=(kind,name)=>kind.toLowerCase()+'/'+name;
const syntheticUID=value=>{const hex=crypto.createHash('sha256').update(value).digest('hex').slice(0,32);return [hex.slice(0,8),hex.slice(8,12),hex.slice(12,16),hex.slice(16,20),hex.slice(20)].join('-');};
const get=(kind,name)=>s.resources[key(kind,name)];
let inputBytes;const input=()=>inputBytes??=(fs.readFileSync(0,'utf8'));
const evidence=s.activeEvidence??s.evidence;
const events=()=>fs.existsSync(evidence)?fs.readFileSync(evidence,'utf8').trim().split('\n').map(JSON.parse):[];
const cpReady=app=>app.metadata.name!=='control-plane'||app.spec.template.spec.containers[0].image===s.cpOldImage||app.spec.template.spec.volumes?.some(volume=>volume.name==='control-plane-environment-policy'&&JSON.parse(get('configmap',volume.configMap.name)?.data?.['policy.json']??'{}').policy_revision===107);
const sync=app=>{app.metadata.resourceVersion=String(Number(app.metadata.resourceVersion)+1);app.metadata.generation=(app.metadata.generation??1)+1;
 app.status={observedGeneration:app.metadata.generation,...Object.fromEntries(['replicas','updatedReplicas','readyReplicas','availableReplicas'].map(field=>[field,['readyReplicas','availableReplicas'].includes(field)&&!cpReady(app)?0:app.spec.replicas]))};};
const inventory=()=>{
 const result=[];
 for(const name of s.componentNames){const app=get('deployment',name),rs=name+'-rs',rsUID=s.backup?syntheticUID('rs/'+app.metadata.uid):app.metadata.uid+'-rs';
  result.push({kind:'ReplicaSet',metadata:{name:rs,uid:rsUID,namespace:'kodex-system',generation:1,labels:{'app.kubernetes.io/name':name},ownerReferences:[{kind:'Deployment',name,uid:app.metadata.uid,controller:true}]},spec:{replicas:app.spec.replicas},status:{replicas:app.spec.replicas,observedGeneration:1}});
  for(let index=0;index<app.spec.replicas;index++)result.push({kind:'Pod',metadata:{name:name+'-'+index,uid:s.backup?syntheticUID('pod/'+rsUID+'/'+index):rsUID+'-'+index,namespace:'kodex-system',
   labels:structuredClone(app.spec.template.metadata.labels??{'app.kubernetes.io/name':name}),ownerReferences:[{kind:'ReplicaSet',name:rs,uid:rsUID,controller:true}]},spec:app.spec.template.spec,
   status:{phase:'Running',conditions:[{type:'Ready',status:cpReady(app)?'True':'False'}],containerStatuses:app.spec.template.spec.containers.map(c=>({name:c.name,ready:cpReady(app),restartCount:0,imageID:c.image,state:{running:{}}})),
    initContainerStatuses:(app.spec.template.spec.initContainers??[]).map(c=>({name:c.name,ready:c.restartPolicy==='Always',restartCount:0,imageID:c.image,state:c.restartPolicy==='Always'?{running:{}}:{terminated:{exitCode:0}}}))}});
 }
 for(const job of Object.values(s.jobs))result.push(job,s.jobPods[job.metadata.name]);
 if(s.backup)result.push(s.backup.rs,...s.backup.pods);
 return [...result,...(s.extraInventory??[])];
};
let backupMaintenanceKube;
const backupRun=(args,bytes='')=>{
 const b=s.backup;b.ownerVersion=s.schemaMigrated?20261114140000:20261114130000;b.ownerProof=s.schemaMigrated?s.postcondition:s.precondition;
 const owned=new Set([b.deployment.metadata.uid,b.rs.metadata.uid,...b.pods.map(p=>p.metadata.uid),b.pgStatefulSet.metadata.uid,b.pgPod.metadata.uid]);
 b.foreignControllers=[...new Map([...Object.values(s.resources),...inventory()].filter(r=>r?.metadata?.uid&&!owned.has(r.metadata.uid)).map(r=>[r.kind+'/'+r.metadata.name,r])).values()];const effects=b.effects;
 const result=backupMaintenanceKube(b,args,bytes);if(b.effects!==effects)s.effects.push('BACKUP_PATCH');return out(result);
};
// Один закрытый routing predicate управляет и импортом, и исполнением.
// Unrelated GET/logs/kustomize остаются тем же лёгким CJS transport, что normal.
const backupRequest=()=>{
 if(!s.backup)return;
 const at=args.indexOf('get'),kind=args[at+1],name=args[at+2];
 if(args.includes('patch')&&args[args.indexOf('patch')+2]==='backup-controller'){
  const p=args.find(v=>v.startsWith('--patch-file='))?.slice(13)??args[args.indexOf('--patch-file')+1];return {bytes:fs.readFileSync(p,'utf8')};
 }
 if(kind==='deployment'&&name==='backup-controller'||kind==='pod'&&name==='kodex-postgresql-0'||kind==='statefulset'&&name==='kodex-postgresql'||
  kind==='deployments,statefulsets,daemonsets,cronjobs,jobs,replicasets,replicationcontrollers,pods')return {bytes:''};
 if(args.includes('exec')&&input().includes("json_build_object('version'"))return {bytes:input()};
};
const run=request=>{
 if(request)return backupRun(args,request.bytes);
 if(s.backup){const at=args.indexOf('get'),kind=args[at+1],name=args[at+2];
  if(kind==='pod'&&s.backup.pods.some(p=>p.metadata.name===name))return out(s.backup.pods.find(p=>p.metadata.name===name));
 }
 if(args[0]==='auth')return out('yes\n');
 if(args[0]==='kustomize'){assert.equal(args[1],s.source+'/deploy/k8s/profiles/web-only');return out(JSON.parse(fs.readFileSync(s.sourceRenderPath,'utf8')).map(r=>JSON.stringify(r)).join('\n---\n'));}
 if(args.includes('get')){
  const at=args.indexOf('get'),kind=args[at+1],name=args[at+2],format=args[args.indexOf('-o')+1];
  if(format==='name'&&['secret','configmap'].includes(kind))return out(kind+'/'+name);
  if(kind==='namespace')return out(format==='json'?{apiVersion:'v1',kind:'Namespace',metadata:{name,uid:name==='kube-system'?s.target.clusterUID:s.target.namespaceUID},status:{phase:'Active'}}:s.target.namespaceUID);
  if(kind==='endpointslices')return out({items:[]});
  if(kind==='nodes')return out({items:s.nodes});
  if(kind==='replicasets,pods,jobs')return out(inventory().map(r=>[r.kind,r.metadata.name,r.metadata.uid,r.metadata.labels?.['app.kubernetes.io/name']??'',r.metadata.namespace,JSON.stringify(r.metadata.ownerReferences??[])].join('\t')).join('\n')+'\n');
  if(kind==='deployments,statefulsets,daemonsets,replicasets,replicationcontrollers,jobs,cronjobs,pods')return out(producerMetadata([...Object.values(s.resources),...inventory()],kind));
  if(kind==='replicasets,pods')return out({items:inventory().filter(r=>['ReplicaSet','Pod'].includes(r.kind))});
  if(kind==='jobs')return out({items:Object.values(s.jobs)});
  if(kind==='pods'){
   if(format?.startsWith('jsonpath='))return out(inventory().filter(r=>r.kind==='Pod').map(p=>[p.metadata.name,p.metadata.uid,p.metadata.resourceVersion??'1',p.metadata.labels?.['batch.kubernetes.io/job-name']??'',JSON.stringify(p.metadata.ownerReferences??[])].join('|')).join('\n')+'\n');
   const selector=args[args.indexOf('-l')+1];
   if(selector?.startsWith('batch.kubernetes.io/job-name=')){const pod=s.jobPods[selector.split('=')[1]];return out({items:pod?[pod]:[]});}
   return out({items:[]});
  }
  if(kind==='pod'&&name==='kodex-postgresql-0')return out('pg-uid|StatefulSet|kodex-postgresql|pg-owner|kodex-postgresql|Running|');
  if(kind==='bundles.trust.cert-manager.io')return out('bundle-uid|1|'+JSON.stringify({sources:[{secret:{name:'kodex-installation-ca',key:'tls.crt'}}],target:{namespaceSelector:{matchLabels:{'kubernetes.io/metadata.name':'kodex-system'}},configMap:{key:'ca.pem'}}}));
  if(kind==='configmap'&&name==='control-plane-postgresql-ca')return out('ca-uid|1|control-plane-postgresql-ca|'+JSON.stringify([{apiVersion:'trust.cert-manager.io/v1alpha1',kind:'Bundle',name,uid:'bundle-uid',controller:true}]));
  if(kind==='secret'||kind==='serviceaccount'&&name==='control-plane-migrator'){
   assert.ok(format.startsWith('jsonpath='));return out(kind+'-'+name+'|1|kodex');
  }
  let resource=kind==='pod'?inventory().find(r=>r.kind==='Pod'&&r.metadata.name===name):kind==='job'?s.jobs[name]:get(kind,name);
  if(!resource){assert.ok(args.some(value=>value.startsWith('--ignore-not-found')));return out('');}
  return out(resource);
 }
 if(args.includes('logs')){
  const name=args.find(value=>value.startsWith('job/')).slice(4),job=s.jobs[name];assert.ok(job);
  if(job.metadata.annotations?.['kodex.dev/rotation-intent'])return out(s.ownerLogs[job.metadata.name]);
  return out(job.spec.template.spec.containers[0].args[0]==='check'?'PostgreSQL connection check completed\n':s.backup?'Telegram notifications backup migration completed at version 20261114140000\n':'goose: current version: 20261114140000\n');
 }
 if(args.includes('exec')){
  assert.ok(args.includes('--host=/var/run/postgresql')&&args.includes('--port=5432')&&args.includes('--no-password')&&args[args.indexOf('env')+1]==='-i');
  const sql=input();
  if(sql.includes('pg_catalog.pg_control_system()'))return out({systemIdentifier:'18446744073709551615',databaseOID:'1',database:'control_plane',port:'5432',serverAddress:null,serverPort:null});
  assert.ok(sql.includes('BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;')&&sql.includes('ROLLBACK;'));
  if(sql.includes('PORTABLE_COMPONENT_DRAIN_V1'))return out({...s.precondition,profile:'PORTABLE_COMPONENT_DRAIN_V1',gooseVersion:s.schemaMigrated?20261114140000:20261114130000,
   oldClaimSessions:get('deployment','control-plane').spec.replicas?3:0,...s.drainOverrides});
  if(sql.includes('TELEGRAM_NOTIFICATIONS_PREDECESSOR_V1')||sql.includes('TELEGRAM_NOTIFICATIONS_BACKUP_PREDECESSOR_V1'))return out({...s.precondition,...s.preconditionOverrides});
  if(sql.includes('TELEGRAM_NOTIFICATIONS_V1')||sql.includes('TELEGRAM_NOTIFICATIONS_BACKUP_V1'))return out({...s.postcondition,...s.postconditionOverrides});
  throw Error('SYNTHETIC_SQL_NOT_REGISTERED');
 }
 if(args.includes('create')){
  const object=JSON.parse(input());
  if(object.kind==='ConfigMap'){
   const result=structuredClone(object);Object.assign(result.metadata,{uid:'10000000-0000-4000-8000-'+String(4000+s.effects.length).padStart(12,'0'),resourceVersion:'1'});
   if(!args.includes('--dry-run=server')){assert.ok(events().at(-1).type==='TELEGRAM_INPUT_CREATE_INTENT'||events().at(-1).operation==='CREATE_PROJECTION');assert.ok(!get('configmap',object.metadata.name));s.resources[key('configmap',object.metadata.name)]=result;s.effects.push(object.metadata.name.startsWith('control-plane-policy-projection-')?'POLICY_PROJECTION_CREATE':'INPUT_CREATE');}
   return out(result);
  }
  assert.equal(object.kind,'Job');assert.ok(!s.jobs[object.metadata.name]);
  if(object.metadata.annotations?.['kodex.dev/rotation-intent']){
   object.metadata.uid='10000000-0000-4000-8000-'+String(2000+s.effects.length).padStart(12,'0');
   if(args.includes('--dry-run=server'))return out(object);
   const watcher=object.spec.template.spec.containers[0].args[0]==='rotation-watch';
   object.status=watcher?{failed:1,conditions:[{type:'Failed',status:'True',reason:'DeadlineExceeded'}]}:{succeeded:1,conditions:[{type:'Complete',status:'True'}]};s.ownerLogs??={};s.ownerLogs[object.metadata.name]=JSON.stringify({...s.ownerStatus,observedAt:new Date().toISOString()})+'\n';s.jobs[object.metadata.name]=object;
   const statuses=containers=>(containers??[]).map(c=>({name:c.name,restartCount:0,imageID:c.image,state:{terminated:{exitCode:0}}}));
   s.jobPods[object.metadata.name]={apiVersion:'v1',kind:'Pod',metadata:{name:object.metadata.name+'-pod',namespace:'kodex-system',uid:'10000000-0000-4000-8000-'+String(3000+s.effects.length).padStart(12,'0'),ownerReferences:[{apiVersion:'batch/v1',kind:'Job',name:object.metadata.name,uid:object.metadata.uid,controller:true}]},
    spec:structuredClone(object.spec.template.spec),status:{phase:'Succeeded',containerStatuses:statuses(object.spec.template.spec.containers),initContainerStatuses:statuses(object.spec.template.spec.initContainers)}};
   s.effects.push('AUTHORITY_JOB');return out(object);
  }
  const check=object.spec.template.spec.containers[0].args[0]==='check';assert.equal(events().at(-1).type,check?'CHECK_CREATE_INTENT':'CREATE_INTENT');
  object.metadata.uid=s.backup?syntheticUID('migration-job/'+object.metadata.name):'job-'+(s.effects.length+1);object.status={succeeded:1,completionTime:new Date().toISOString(),conditions:[{type:'Complete',status:'True'}]};
  s.jobs[object.metadata.name]=object;s.jobPods[object.metadata.name]={kind:'Pod',metadata:{name:object.metadata.name+'-pod',namespace:'kodex-system',uid:s.backup?syntheticUID('migration-pod/'+object.metadata.uid):'pod-'+object.metadata.uid,resourceVersion:'1',
   labels:object.spec.template.metadata.labels,ownerReferences:[{kind:'Job',name:object.metadata.name,uid:object.metadata.uid,controller:true}]},spec:object.spec.template.spec,
   status:{phase:'Succeeded',initContainerStatuses:[{name:'wait-for-postgresql',restartCount:0,state:{terminated:{exitCode:0}}}],
    containerStatuses:[{name:'migrate',restartCount:0,imageID:object.spec.template.spec.containers[0].image,state:{terminated:{exitCode:0}}}]}};
  s.effects.push(check?'CHECK_CREATE':'DDL_CREATE');if(!check)s.schemaMigrated=true;return out(object);
 }
 if(args[0]==='replace'){
  const desired=JSON.parse(input()),current=get(desired.kind,desired.metadata.name);assert.ok(current);assert.equal(current.metadata.uid,desired.metadata.uid);assert.equal(current.metadata.resourceVersion,desired.metadata.resourceVersion);
  desired.metadata.resourceVersion=String(Number(desired.metadata.resourceVersion)+1);s.resources[key(desired.kind,desired.metadata.name)]=desired;s.effects.push('REPLACE:'+desired.metadata.name);return out(desired);
 }
 if(args.includes('patch')){
  const at=args.indexOf('patch'),kind=args[at+1],name=args[at+2],resource=get(kind,name);assert.ok(resource);
  const path=args.find(value=>value.startsWith('--patch-file='))?.slice(13)??(args.includes('--patch-file')?args[args.indexOf('--patch-file')+1]:null);
  const patch=JSON.parse(path?fs.readFileSync(path,'utf8'):args[args.indexOf('-p')+1]);
  assert.equal(patch[0].value,resource.metadata.uid);assert.equal(patch[1].value,resource.metadata.resourceVersion);
  const candidate=structuredClone(resource);
  for(const operation of patch){const keys=operation.path.slice(1).split('/');let parent=candidate;for(const key of keys.slice(0,-1))parent=parent[key];
   if(operation.op==='test')assert.deepEqual(parent[keys.at(-1)],operation.value);else parent[keys.at(-1)]=operation.value;}
  // API dry-run обязан отвергать именно тот named port, который отверг installed apiserver.
  const validName=value=>typeof value==='string'&&value.length>0&&value.length<=15&&/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(value)&&/[a-z]/.test(value);
  if(candidate.kind==='Service')for(const port of candidate.spec.ports??[])if(typeof port.targetPort==='string'&&!validName(port.targetPort))fail();
  for(const container of candidate.spec?.template?.spec?.containers??[])for(const port of container.ports??[])if(port.name!==undefined&&!validName(port.name))fail();
  if(!args.includes('--dry-run=server')){
   const intent=events().at(-1);assert.ok(['TELEGRAM_INPUT_PATCH_INTENT','STOP_INTENT','PAIR_STOP_INTENT','PAIR_HANDOFF_INTENT','RESUME_INTENT','PAIR_RESUME_INTENT','PAIR_ACTIVATE_INTENT','INTENT'].includes(intent.type)||intent.operation==='CP_POINTER_CAS');
   if(s.lostBefore===name)fail();
   Object.assign(resource,candidate);
   if(kind==='deployment')sync(resource);else resource.metadata.resourceVersion=String(Number(resource.metadata.resourceVersion)+1);
   s.effects.push((intent.type??intent.operation)+':'+name);
   if(s.lostACK===name){s.lostACK='';fail();}
  }
  return out(resource);
 }
 throw Error('SYNTHETIC_COMMAND_NOT_REGISTERED');
};
(async()=>{try{const request=backupRequest();if(request)({backupMaintenanceKube}=await import('./backup-controller-maintenance-synthetic.mjs'));run(request);}catch(error){save();process.stderr.write(error.code??error.message);process.exitCode=1;}})();
