// Metadata-only Kubernetes/SQL transport для настоящих CLI subprocesses.
// Никакой команды к кластеру, сети или пользовательским данным здесь нет.
const fs=require('node:fs');
const assert=require('node:assert/strict');

function inventory(state){
 const values=Object.values(state.apps).flatMap(app=>{
  const rs={apiVersion:'apps/v1',kind:'ReplicaSet',metadata:{name:app.metadata.name+'-rs',namespace:'kodex-system',uid:app.metadata.uid+'-rs',
   ownerReferences:[{kind:'Deployment',name:app.metadata.name,uid:app.metadata.uid,controller:true}]}};
  return [rs,...Array.from({length:app.spec.replicas},(_,index)=>({apiVersion:'v1',kind:'Pod',metadata:{name:app.metadata.name+'-'+index,namespace:'kodex-system',uid:app.metadata.uid+'-pod-'+index,
   resourceVersion:'1',labels:{'app.kubernetes.io/name':app.metadata.name},ownerReferences:[{kind:'ReplicaSet',name:rs.metadata.name,uid:rs.metadata.uid,controller:true}]},
   spec:structuredClone(app.spec.template.spec),status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:app.spec.template.spec.containers.map(container=>
    ({name:container.name,ready:true,state:{running:{}},imageID:container.image}))}}))];
 });
 return [...values,state.job,state.jobPod];
}

function run(state,args,input){
 state.calls.push({args});
 assert.equal(args[args.indexOf('--kubeconfig')+1],state.kubeconfig);assert.equal(args[args.indexOf('--context')+1],state.context);
 if(args.includes('exec')){
  assert.ok(args.includes('env')&&args[args.indexOf('env')+1]==='-i'&&args.includes('--host=/var/run/postgresql')&&args.includes('--no-password'));
  assert.equal(args[args.indexOf('-c')+1],'postgresql');
  const authority=args.includes('--dbname=internal_rpc_authority'),backend=authority?state.authorityBackend:state.migration.postgresBackend;
  if(input.includes('pg_catalog.pg_control_system()'))return JSON.stringify(backend);
  assert.ok(input.includes('READ ONLY')&&input.includes('ROLLBACK;'));
  if(authority){
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
  assert.ok(input.includes('SENTRY_OBSERVABILITY'));
  return JSON.stringify({...state.postcondition,...state.postconditionPatch});
 }
 const get=args.indexOf('get');
 if(get>=0){
  const kind=args[get+1],name=args[get+2],jsonpath=args.find(value=>value.startsWith('jsonpath='));
  if(kind==='namespace')return jsonpath?state.namespaceUID:JSON.stringify({metadata:{uid:name==='kube-system'?state.clusterUID:state.namespaceUID}});
  if(kind==='nodes')return JSON.stringify({items:state.nodes});
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
  if(kind==='job')return JSON.stringify(state.job);
  if(kind==='bundles.trust.cert-manager.io')return [state.bundleUID,'1',JSON.stringify(state.bundleSpec)].join('|');
  if(kind==='configmap'&&name==='control-plane-postgresql-ca')return [state.caUID,'1','control-plane-postgresql-ca',JSON.stringify([{apiVersion:'trust.cert-manager.io/v1alpha1',kind:'Bundle',name,uid:state.bundleUID,controller:true}])].join('|');
  if(kind==='serviceaccount'||kind==='secret')return [state.dependencies[kind],'1','kodex'].join('|');
  if(kind==='configmap')return JSON.stringify(state.configMaps[name]);
  throw Error('unexpected synthetic GET');
 }
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
module.exports={run,inventory};
