import assert from 'node:assert/strict';
import {mkdtempSync,chmodSync,rmSync} from 'node:fs';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {fingerprint} from '../scoped-release.mjs';
import {planPairedTokenRelease,readPairedTokenReleaseOwner,pairedTokenReleaseNames} from '../paired-token-release-plan.mjs';
import {openPairedTokenEffectJournal,pairedTokenReleaseReceiver} from '../paired-token-release-receiver.mjs';
import {pairedTokenReleaseFixture as fixture,pairedTokenReleaseFixtureUID as uid} from './paired-token-release-synthetic.mjs';

// Исключительно synthetic in-memory Kubernetes. Реального kube/Docker binding
// у receiver нет; actual privateJournal обеспечивает fsync/lock/restart tests.
export function setup(t,recordBinding){
 const f=fixture({broaderAdmission:true}),first=f.current();
 const directory=mkdtempSync(join(tmpdir(),'kodex-paired-receiver-'));chmodSync(directory,0o700);
 const binding=recordBinding?.(f,directory)??{configSHA256:'c'.repeat(64),referencesSHA256:'f'.repeat(64)};
 const record={schema:'kodex.paired-token-release-record/v1',executorSHA:'e'.repeat(40),...binding,
  plan:planPairedTokenRelease(f.manifest,first),componentEvidence:pairedTokenReleaseNames.map(name=>({name,servingSHA256:fingerprint(f.observed.serving[name])}))};
 const path=join(directory,'effects.jsonl');let journal=openPairedTokenEffectJournal(path,f.manifest,record,{createOnly:true});
 t.after(()=>{journal?.close();rmSync(directory,{recursive:true,force:true});});
 const state={effects:0,dryRuns:0,physicalJoin:true,autoStop:true,ready:true,loseBefore:false,loseAfter:false,failRead:false,acks:[]};
 const deployments=Object.fromEntries(f.observed.deployments.map(d=>[d.metadata.name,d]));
 const resources=Object.fromEntries(pairedTokenReleaseNames.map(name=>[name,f.observed.serving[name]]));
 function stopped(name){
  const d=deployments[name];d.status={observedGeneration:d.metadata.generation,replicas:0,updatedReplicas:0,readyReplicas:0,availableReplicas:0};
  for(const p of resources[name].items.filter(i=>i.kind==='Pod')){
   p.status.phase='Succeeded';p.status.containerStatuses=p.spec.containers.map(c=>({name:c.name,state:{terminated:{exitCode:0}}}));
  }
 }
 function started(name){
  const d=deployments[name],replicas=d.spec.replicas;
  d.status={observedGeneration:d.metadata.generation,replicas,updatedReplicas:replicas,readyReplicas:state.ready?replicas:0,availableReplicas:state.ready?replicas:0};
  const rs={kind:'ReplicaSet',metadata:{uid:uid(100+state.effects),ownerReferences:[{kind:'Deployment',uid:d.metadata.uid,controller:true}]}};
  resources[name].items.push(rs);
  for(let i=0;i<replicas;i++)resources[name].items.push({kind:'Pod',metadata:{uid:uid(200+state.effects*8+i),namespace:'kodex-system',
   labels:{'app.kubernetes.io/name':name},ownerReferences:[{kind:'ReplicaSet',uid:rs.metadata.uid,controller:true}]},
   spec:structuredClone(d.spec.template.spec),status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],
    containerStatuses:d.spec.template.spec.containers.map(c=>({name:c.name,ready:state.ready,state:{running:{}},imageID:c.image}))}});
 }
 const ports={
  captureBefore:async()=>({...record,current:f.current()}),
  readComponent:name=>{if(state.failRead){state.failRead=false;throw Error('SYNTHETIC_READ_FAILED');}
   return {deployment:structuredClone(deployments[name]),resources:structuredClone(resources[name])};},
  readOwner:manifest=>readPairedTokenReleaseOwner(manifest,f.ports),
  readAPI:()=>({...f.api,observed:f.observed,deployment:deployments['control-api-gateway']}),
  readSource:()=>({sourceSHA:f.manifest.sourceSHA,executorSHA:record.executorSHA}),
  readJoin:request=>({schema:'kodex.paired-component-join/v1',name:request.name,uid:request.uid,podUIDs:request.podUIDs,
   nodeBindingSHA256:fingerprint(request.nodeBinding),nativeState:state.physicalJoin?'JOINED':'UNKNOWN',observedAt:Date.now()}),
  readRollbackCompatibility:expected=>({...expected,sourceReviewSHA256:'a'.repeat(64)}),
  dryRunComponent:async request=>{state.dryRuns++;assert.ok(pairedTokenReleaseNames.includes(request.name));
   assert.equal(request.uid,deployments[request.name].metadata.uid);assert.equal(request.resourceVersion,deployments[request.name].metadata.resourceVersion);
   assert.deepEqual(request.beforeSpec,deployments[request.name].spec);},
  replaceFrozenComponentSpec:async request=>{
   state.effects++;
   if(state.loseBefore){state.loseBefore=false;throw Error('SYNTHETIC_LOST_BEFORE');}
   const d=deployments[request.name];assert.equal(d.metadata.uid,request.uid);assert.equal(d.metadata.resourceVersion,request.resourceVersion);
   assert.deepEqual(d.spec,request.beforeSpec);d.spec=structuredClone(request.afterSpec);d.metadata.resourceVersion=String(Number(d.metadata.resourceVersion)+1);d.metadata.generation++;
   if(d.spec.replicas===0){if(state.autoStop)stopped(request.name);}else started(request.name);
   if(state.loseAfter){state.loseAfter=false;throw Error('SYNTHETIC_LOST_AFTER');}
   const ack={uid:d.metadata.uid,resourceVersion:d.metadata.resourceVersion,specSHA256:fingerprint(d.spec)};state.acks.push(ack);return ack;
  },
 };
 let receiver=pairedTokenReleaseReceiver({manifest:f.manifest,record,journal,ports});
 const reopen=()=>{journal.close();journal=openPairedTokenEffectJournal(path,f.manifest,record);receiver=pairedTokenReleaseReceiver({manifest:f.manifest,record,journal,ports});return receiver;};
 return {f,record,directory,path,state,deployments,resources,ports,get journal(){return journal;},get receiver(){return receiver;},reopen,stopped,started};
}

export async function finish(s,direction='forward'){
 for(let i=0;i<6;i++)await s.receiver.advance(direction);
 return s.receiver.status(direction);
}
