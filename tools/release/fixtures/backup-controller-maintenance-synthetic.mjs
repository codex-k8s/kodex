import assert from 'node:assert/strict';
import {fixture,metadata,uid} from './authority-catalog-synthetic.mjs';
import {fingerprint} from '../scoped-release.mjs';
import {backupControllerMaintenanceHeader,planBackupControllerMaintenance,backupControllerMaintenanceStep} from '../backup-controller-maintenance.mjs';

export function backupMaintenanceFixture(){
 const base=fixture(),sourceSHA='a'.repeat(40),deployment=structuredClone(base.api);
 deployment.metadata={...metadata('backup-controller',300),generation:1,labels:{...deployment.metadata.labels,'app.kubernetes.io/name':'backup-controller'}};
 deployment.spec.replicas=1;deployment.spec.selector.matchLabels={'app.kubernetes.io/name':'backup-controller'};
 deployment.spec.template.spec.containers[0].name='backup-controller';deployment.spec.template.spec.serviceAccountName='backup-controller';
 deployment.spec.template.spec.volumes=[{name:'credentials',secret:{secretName:'backup-controller-credentials'}}];
 deployment.status={observedGeneration:1,replicas:1,updatedReplicas:1,readyReplicas:1,availableReplicas:1};
 const target={...base.profile,resources:[{apiVersion:'apps/v1',kind:'Deployment',name:'backup-controller',uid:deployment.metadata.uid}]};
 const postgres={pod:'kodex-postgresql-0',uid:uid(310),ownerUID:uid(311)};
 const observed={context:target.context,clusterUID:target.clusterUID,namespaceUID:target.namespaceUID,installation:base.installation,postgres,resources:{items:[]}};
 const rs={kind:'ReplicaSet',metadata:{...metadata('backup-controller-rs',301),generation:1,ownerReferences:[{kind:'Deployment',uid:deployment.metadata.uid,controller:true}]},spec:{replicas:1},status:{replicas:1,observedGeneration:1}};
 let serial=320;
 const pod=()=>({kind:'Pod',metadata:{...metadata('backup-controller-pod',serial++),ownerReferences:[{kind:'ReplicaSet',uid:rs.metadata.uid,controller:true}]},
  spec:{...structuredClone(deployment.spec.template.spec),nodeName:'synthetic-node',enableServiceLinks:true},status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:deployment.spec.template.spec.containers.map(c=>({name:c.name,ready:true,imageID:c.image,state:{running:{}}}))}});
 const state={pods:[pod()],extra:[],effects:0,loseACK:false,gateValid:true};
 const projected=item=>({kind:item.kind,metadata:structuredClone(item.metadata),serviceAccounts:item.kind==='Deployment'||item.kind==='Pod'?['backup-controller']:[],secretRefs:[],replicas:item.spec?.replicas,statusReplicas:item.status?.replicas,observedGeneration:item.status?.observedGeneration});
 const inventory=()=>({schema:'kodex.backup-producer-inventory/v1',namespace:'kodex-system',kinds:['Deployment','StatefulSet','DaemonSet','CronJob','Job','ReplicaSet','Pod'],items:[deployment,rs,...state.pods].map(projected).concat(state.extra)});
 const resources=()=>({items:[rs,...state.pods]});
 const stop=planBackupControllerMaintenance(deployment,target,observed,sourceSHA,'stop',undefined,inventory());
 const journal=plan=>({events:[{type:'HEADER',...backupControllerMaintenanceHeader(plan)}],append(value){this.events.push(value);}});
 const patch=(operations,dryRun)=>{
  for(const op of operations.filter(v=>v.op==='test')){let current=deployment;for(const key of op.path.slice(1).split('/'))current=current[key];assert.deepEqual(current,op.value);}
  if(dryRun)return;
  for(const op of operations.filter(v=>v.op==='add'||v.op==='replace')){const keys=op.path.slice(1).split('/');let current=deployment;for(const key of keys.slice(0,-1))current=current[key];current[keys.at(-1)]=structuredClone(op.value);}
  deployment.metadata.resourceVersion=String(Number(deployment.metadata.resourceVersion)+1);deployment.metadata.generation++;state.effects++;
  if(state.loseACK)throw Error('LOST_ACK');
 };
 const gate=plan=>({targetSHA256:fingerprint(target),deploymentUID:deployment.metadata.uid,sourceSHA,planSHA256:plan.stopPlanSHA256,canonicalBackup:state.gateValid,cleanupTerminal:state.gateValid});
 const step=(phase,plan,journal,extra={})=>backupControllerMaintenanceStep({phase,plan,journal,get:()=>structuredClone(deployment),resources,producerInventory:inventory,patch,gate:async()=>gate(plan),...extra});
 const converge=()=>{
  const n=deployment.spec.replicas;deployment.status={observedGeneration:deployment.metadata.generation,replicas:n,updatedReplicas:n,readyReplicas:n,availableReplicas:n};
  rs.spec.replicas=n;rs.status.replicas=n;state.pods=n?[pod()]:[];observed.resources=resources();
 };
 return {deployment,target,postgres,observed,sourceSHA,rs,state,inventory,resources,stop,journal,step,converge};
}


// Synthetic backend обслуживает только закрытые команды нового CLI; неизвестный
// argv приводит к отказу, а effects учитываются лишь для actual CAS patch.
export function backupMaintenanceKube(state,args,input){
 const out=value=>JSON.stringify(value),namespace='kodex-system';
 const replicaCount=state.deployment.spec.replicas;
 const resources=[state.rs,...state.pods];
 const projected=item=>({kind:item.kind,metadata:structuredClone(item.metadata),serviceAccounts:item.kind==='Deployment'||item.kind==='Pod'?['backup-controller']:[],secretRefs:[],replicas:item.spec?.replicas,statusReplicas:item.status?.replicas,observedGeneration:item.status?.observedGeneration});
 if(args.includes('patch')){
  const patch=JSON.parse(input);for(const op of patch.filter(v=>v.op==='test')){let value=state.deployment;for(const key of op.path.slice(1).split('/'))value=value[key];assert.deepEqual(value,op.value);}
  if(args.includes('--dry-run=server'))return 'deployment.apps/backup-controller\n';
  for(const op of patch.filter(v=>v.op==='add'||v.op==='replace')){const keys=op.path.slice(1).split('/');let value=state.deployment;for(const key of keys.slice(0,-1))value=value[key];value[keys.at(-1)]=op.value;}
  state.effects++;state.deployment.metadata.resourceVersion=String(Number(state.deployment.metadata.resourceVersion)+1);state.deployment.metadata.generation++;
  const n=state.deployment.spec.replicas;state.deployment.status={observedGeneration:state.deployment.metadata.generation,replicas:n,updatedReplicas:n,readyReplicas:n,availableReplicas:n};
  state.rs.spec.replicas=n;state.rs.status.replicas=n;state.rs.status.observedGeneration=state.rs.metadata.generation;
  state.pods=n?structuredClone(state.originalPods):[];
  if(state.loseACK)throw Error('LOST_ACK');return 'deployment.apps/backup-controller\n';
 }
 if(args.includes('exec')){
  assert.ok(args.includes('env')&&args.includes('-i')&&args.includes('--host=/var/run/postgresql')&&args.includes('--no-password'));
  if(input.includes('pg_control_system'))return out(state.backend);
  if(input.includes("json_build_object('version'"))return out({version:state.ownerVersion??20261113000100,backupSessions:state.backupSessions??0});
  return out(state.ownerProof);
 }
 const get=args.indexOf('get');assert.ok(get>=0);
 const [kind,name]=args.slice(get+1),format=args.at(-1);
 if(kind==='namespace')return out({metadata:{uid:name==='kube-system'?state.clusterUID:state.namespaceUID}});
 if(kind==='configmap'&&name==='kodex-installation-profile')return out(state.installation);
 if(kind==='deployment'&&name==='backup-controller')return out(state.deployment);
 if(kind==='pod'&&name==='kodex-postgresql-0')return `${state.postgres.uid}|StatefulSet|kodex-postgresql|${state.postgres.ownerUID}|kodex-postgresql|Running|`;
 if(kind==='pod')return out(state.pods.find(p=>p.metadata.name===name));
 if(kind==='replicasets,pods,jobs')return resources.map(item=>[item.kind,item.metadata.name,item.metadata.uid,item.metadata.labels?.['app.kubernetes.io/name']??'',namespace,JSON.stringify(item.metadata.ownerReferences??[])].join('\t')).join('\n')+'\n';
 if(kind==='deployments,statefulsets,daemonsets,cronjobs,jobs,replicasets,pods'){
  const items=[state.deployment,...resources].map(projected).concat(state.extra);
  return items.map(item=>[item.kind,item.metadata.name,item.metadata.uid,namespace,item.metadata.labels?.['app.kubernetes.io/name']??'',JSON.stringify(item.metadata.ownerReferences??[]),item.serviceAccounts?.join(' ')??'',item.secretRefs?.join(' ')??'','','','',item.replicas??'',item.statusReplicas??'',item.metadata.generation??'',item.observedGeneration??''].join('\t')).join('\n')+'\n';
 }
 throw Error('UNREGISTERED_SYNTHETIC_COMMAND');
}
