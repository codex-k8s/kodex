// Отдельный subprocess заменяет только Kubernetes/PostgreSQL transport.
// Никакого обращения к kubeconfig, sockets, реальной сети и credentials нет.
const fs=require('node:fs'),assert=require('node:assert/strict'),producerMetadata=require('./telegram-producer-metadata.cjs');
const statePath=process.env.KODEX_TELEGRAM_SYNTHETIC_STATE,s=JSON.parse(fs.readFileSync(statePath)),argv=process.argv.slice(2);
const prefix=['--kubeconfig',s.kubeconfig,'--context',s.context,'--request-timeout=30s'];
assert.deepEqual(argv.slice(0,prefix.length),prefix);
const args=argv.slice(prefix.length);s.calls.push(args);
const save=()=>fs.writeFileSync(statePath,JSON.stringify(s));
const out=value=>{save();process.stdout.write(typeof value==='string'?value:JSON.stringify(value));};
const fail=()=>{save();process.exit(71);};
const key=(kind,name)=>kind.toLowerCase()+'/'+name;
const get=(kind,name)=>s.resources[key(kind,name)];
const input=()=>fs.readFileSync(0,'utf8');
const events=()=>fs.existsSync(s.evidence)?fs.readFileSync(s.evidence,'utf8').trim().split('\n').map(JSON.parse):[];
const sync=app=>{app.metadata.resourceVersion=String(Number(app.metadata.resourceVersion)+1);app.metadata.generation=(app.metadata.generation??1)+1;
 app.status={observedGeneration:app.metadata.generation,...Object.fromEntries(['replicas','updatedReplicas','readyReplicas','availableReplicas'].map(field=>[field,app.spec.replicas]))};};
const inventory=()=>{
 const result=[];
 for(const name of s.componentNames){const app=get('deployment',name),rs=name+'-rs',rsUID=app.metadata.uid+'-rs';
  result.push({kind:'ReplicaSet',metadata:{name:rs,uid:rsUID,namespace:'kodex-system',generation:1,labels:{'app.kubernetes.io/name':name},ownerReferences:[{kind:'Deployment',name,uid:app.metadata.uid,controller:true}]},spec:{replicas:app.spec.replicas},status:{replicas:app.spec.replicas,observedGeneration:1}});
  for(let index=0;index<app.spec.replicas;index++)result.push({kind:'Pod',metadata:{name:name+'-'+index,uid:rsUID+'-'+index,namespace:'kodex-system',
   labels:{'app.kubernetes.io/name':name},ownerReferences:[{kind:'ReplicaSet',name:rs,uid:rsUID,controller:true}]},spec:app.spec.template.spec,
   status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:app.spec.template.spec.containers.map(c=>({name:c.name,ready:true,restartCount:0,imageID:c.image,state:{running:{}}})),
    initContainerStatuses:(app.spec.template.spec.initContainers??[]).map(c=>({name:c.name,ready:c.restartPolicy==='Always',restartCount:0,imageID:c.image,state:c.restartPolicy==='Always'?{running:{}}:{terminated:{exitCode:0}}}))}});
 }
 for(const job of Object.values(s.jobs))result.push(job,s.jobPods[job.metadata.name]);
 return [...result,...(s.extraInventory??[])];
};
const run=()=>{
 if(args[0]==='kustomize'){assert.equal(args[1],s.source+'/deploy/k8s/profiles/web-only');return out(JSON.parse(fs.readFileSync(s.sourceRenderPath,'utf8')).map(r=>JSON.stringify(r)).join('\n---\n'));}
 if(args.includes('get')){
  const at=args.indexOf('get'),kind=args[at+1],name=args[at+2],format=args[args.indexOf('-o')+1];
  if(kind==='namespace')return out(format==='json'?{apiVersion:'v1',kind:'Namespace',metadata:{name,uid:name==='kube-system'?s.target.clusterUID:s.target.namespaceUID},status:{phase:'Active'}}:s.target.namespaceUID);
  if(kind==='nodes')return out({items:s.nodes});
  if(kind==='replicasets,pods,jobs')return out(inventory().map(r=>[r.kind,r.metadata.name,r.metadata.uid,r.metadata.labels?.['app.kubernetes.io/name']??'',r.metadata.namespace,JSON.stringify(r.metadata.ownerReferences??[])].join('\t')).join('\n')+'\n');
  if(kind==='deployments,statefulsets,daemonsets,replicasets,replicationcontrollers,jobs,cronjobs,pods')return out(producerMetadata([...Object.values(s.resources),...inventory()],kind));
  if(kind==='replicasets,pods')return out({items:inventory().filter(r=>['ReplicaSet','Pod'].includes(r.kind))});
  if(kind==='jobs')return out({items:Object.values(s.jobs)});
  if(kind==='pods'){
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
  return out(job.spec.template.spec.containers[0].args[0]==='check'?'PostgreSQL connection check completed\n':'goose: current version: 20261114140000\n');
 }
 if(args.includes('exec')){
  assert.ok(args.includes('--host=/var/run/postgresql')&&args.includes('--port=5432')&&args.includes('--no-password')&&args[args.indexOf('env')+1]==='-i');
  const sql=input();
  if(sql.includes('pg_catalog.pg_control_system()'))return out({systemIdentifier:'18446744073709551615',databaseOID:'1',database:'control_plane',port:'5432',serverAddress:null,serverPort:null});
  assert.ok(sql.includes('BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;')&&sql.includes('ROLLBACK;'));
  if(sql.includes('PORTABLE_COMPONENT_DRAIN_V1'))return out({...s.precondition,profile:'PORTABLE_COMPONENT_DRAIN_V1',gooseVersion:s.schemaMigrated?20261114140000:20261114130000,
   oldClaimSessions:get('deployment','control-plane').spec.replicas?3:0,...s.drainOverrides});
  if(sql.includes('TELEGRAM_NOTIFICATIONS_PREDECESSOR_V1'))return out({...s.precondition,...s.preconditionOverrides});
  if(sql.includes('TELEGRAM_NOTIFICATIONS_V1'))return out({...s.postcondition,...s.postconditionOverrides});
  throw Error('SYNTHETIC_SQL_NOT_REGISTERED');
 }
 if(args.includes('create')){
  const object=JSON.parse(input());
  if(object.kind==='ConfigMap'){
   const result=structuredClone(object);Object.assign(result.metadata,{uid:s.policyUID,resourceVersion:'1'});
   if(!args.includes('--dry-run=server')){assert.equal(events().at(-1).type,'TELEGRAM_INPUT_CREATE_INTENT');assert.ok(!get('configmap',object.metadata.name));s.resources[key('configmap',object.metadata.name)]=result;s.effects.push('INPUT_CREATE');}
   return out(result);
  }
  assert.equal(object.kind,'Job');assert.ok(!s.jobs[object.metadata.name]);
  const check=object.spec.template.spec.containers[0].args[0]==='check';assert.equal(events().at(-1).type,check?'CHECK_CREATE_INTENT':'CREATE_INTENT');
  object.metadata.uid='job-'+(s.effects.length+1);object.status={succeeded:1,completionTime:new Date().toISOString(),conditions:[{type:'Complete',status:'True'}]};
  s.jobs[object.metadata.name]=object;s.jobPods[object.metadata.name]={kind:'Pod',metadata:{name:object.metadata.name+'-pod',namespace:'kodex-system',uid:'pod-'+object.metadata.uid,
   labels:object.spec.template.metadata.labels,ownerReferences:[{kind:'Job',name:object.metadata.name,uid:object.metadata.uid,controller:true}]},spec:object.spec.template.spec,
   status:{phase:'Succeeded',initContainerStatuses:[{name:'wait-for-postgresql',restartCount:0,state:{terminated:{exitCode:0}}}],
    containerStatuses:[{name:'migrate',restartCount:0,imageID:object.spec.template.spec.containers[0].image,state:{terminated:{exitCode:0}}}]}};
  s.effects.push(check?'CHECK_CREATE':'DDL_CREATE');if(!check)s.schemaMigrated=true;return out(object);
 }
 if(args.includes('patch')){
  const at=args.indexOf('patch'),kind=args[at+1],name=args[at+2],resource=get(kind,name);assert.ok(resource);
  const path=args.find(value=>value.startsWith('--patch-file='));
  const patch=JSON.parse(path?fs.readFileSync(path.slice(13),'utf8'):args[args.indexOf('-p')+1]);
  assert.equal(patch[0].value,resource.metadata.uid);assert.equal(patch[1].value,resource.metadata.resourceVersion);
  if(patch[2].path==='/spec/replicas')assert.equal(patch[2].value,resource.spec.replicas);else assert.deepEqual(patch[2].value,resource[patch[2].path.slice(1)]);
  if(!args.includes('--dry-run=server')){
   const intent=events().at(-1);assert.ok(['TELEGRAM_INPUT_PATCH_INTENT','STOP_INTENT','PAIR_STOP_INTENT','PAIR_HANDOFF_INTENT','RESUME_INTENT','PAIR_RESUME_INTENT','PAIR_ACTIVATE_INTENT'].includes(intent.type));
   if(s.lostBefore===name)fail();
   if(patch[3].path==='/spec/replicas'){resource.spec.replicas=patch[3].value;resource.metadata.annotations=patch[4].value;}
   else resource[patch[3].path.slice(1)]=patch[3].value;
   if(kind==='deployment')sync(resource);else resource.metadata.resourceVersion=String(Number(resource.metadata.resourceVersion)+1);
   s.effects.push(intent.type+':'+name);
   if(s.lostACK===name){s.lostACK='';fail();}
  }
  return out(resource);
 }
 throw Error('SYNTHETIC_COMMAND_NOT_REGISTERED');
};
try{run();}catch(error){save();process.stderr.write(error.code??error.message);process.exitCode=1;}
