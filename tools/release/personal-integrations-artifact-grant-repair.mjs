import {execFileSync} from 'node:child_process';
import {resolve} from 'node:path';
import {inspectSource,sourceGitArguments} from './application-source.mjs';
import {readAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {readPersonalIntegrationsAuthorityImage} from './personal-integrations-authority-image.mjs';
import {sourceArchiveHash} from './stable-image-publication.mjs';
import {personalArtifactSocketRepairSpec} from './personal-integrations-artifact-socket-repair.mjs';
import {fingerprint,withPrivatePatchFile} from './scoped-release.mjs';

const name='artifact-retention',namespace='kodex-system',worker='platform-worker-grant-agent';
const marker='kodex.dev/artifact-grant-image-repair';
const prefix='PERSONAL_ARTIFACT_GRANT_REPAIR';
const reject=()=>{throw Error(prefix+'_REJECTED');};
const requireValue=ok=>{if(!ok)reject();};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const sha=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);
const rv=value=>typeof value==='string'&&/^[1-9][0-9]*$/.test(value);
const image=value=>typeof value==='string'&&/^registry\.local\.kodex\/kodex\/internal-rpc-authority@sha256:[a-f0-9]{64}$/.test(value);
const type=(step,kind)=>prefix+'_'+step.toUpperCase()+'_'+kind;

export async function readArtifactGrantRepairCandidate(path,migration) {
 const input=readAuthorityPrivateJSON(path);
 requireValue(input.schema==='kodex.artifact-grant-repair-inputs/v1'&&
  Object.keys(input).sort().join(',')==='nodeProofPath,publicationEvidencePath,publicationPlanPath,schema,source,sourceSHA'&&
  typeof input.source==='string'&&resolve(input.source)===input.source&&
  /^[a-f0-9]{40}$/.test(input.sourceSHA??'')&&input.sourceSHA!==migration.source.revision&&
  inspectSource(input.source).revision===input.sourceSHA);
 const git=(...args)=>execFileSync('git',sourceGitArguments(input.source,...args),
  {timeout:15000,maxBuffer:1<<20,stdio:['ignore','pipe','pipe']});
 const expectedPaths=['app.go','app_test.go'].map(file=>'services/internal/internal-rpc-authority/internal/platformworkergrant/'+file);
 try{
  git('merge-base','--is-ancestor',migration.source.revision,input.sourceSHA);
  requireValue(same(git('diff','--no-ext-diff','--no-renames','--name-only','-z',migration.source.revision,input.sourceSHA)
   .toString().split('\0').filter(Boolean).sort(),expectedPaths));
 }catch{reject();}
 const publication=readAuthorityPrivateJSON(input.publicationPlanPath);
 const nodes=readAuthorityPrivateJSON(input.nodeProofPath);
 requireValue(publication.source===input.sourceSHA&&publication.receipt?.dockerfile==='services/internal/internal-rpc-authority/Dockerfile'&&
  publication.receipt.contextSHA256===await sourceArchiveHash(input.sourceSHA,input.source)&&
  same(nodes.binding?.map(node=>node.uid).sort(),[...migration.imageEvidence.nodeUIDs].sort()));
 const candidate=await readPersonalIntegrationsAuthorityImage({...input,target:migration.portableTarget,kubeconfig:migration.kubeconfig});
 requireValue(inspectSource(input.source).revision===input.sourceSHA);
 return candidate;
}

function repairContext(migration,events,actual,imageRef) {
 const before=personalArtifactSocketRepairSpec(migration,events,actual);
 const artifact=migration.componentHandoff.components.find(part=>part.name===name);
 requireValue(before?.replicas>0&&artifact?.uid===actual.metadata?.uid&&actual.kind==='Deployment'&&actual.apiVersion==='apps/v1'&&
  actual.metadata.name===name&&actual.metadata.namespace===namespace&&rv(actual.metadata.resourceVersion)&&!actual.metadata.deletionTimestamp&&
  image(imageRef)&&imageRef!==migration.personalIntegrationsInputs.authority.imageRef);
 const matches=before.template.spec.containers.map((container,index)=>container.name===worker?index:-1).filter(index=>index>=0);
 requireValue(matches.length===1&&before.template.spec.containers[matches[0]].image===migration.personalIntegrationsInputs.authority.imageRef);
 const stopped={...before,replicas:0},after=structuredClone(before);
 after.template.spec.containers[matches[0]].image=imageRef;
 return {before,stopped,after,uid:artifact.uid};
}

function repairPins(migration,ctx,candidate) {
 return {migrationPlanSHA256:fingerprint(migration),deploymentUID:ctx.uid,
  beforeSHA256:fingerprint(ctx.before),stoppedSHA256:fingerprint(ctx.stopped),afterSHA256:fingerprint(ctx.after),
  imageRef:candidate.imageRef,candidateSHA256:fingerprint(candidate)};
}

// Ранее принятый исходный SHA и ART socket repair остаются в журнале.
// Поздние читатели допускают строго одноконтейнерный delta по двум exact ACK.
export function personalArtifactGrantRepairSpec(migration,events,actual) {
 const stopped=events.filter(row=>row.type===type('stop','ACK'));
 const resumed=events.filter(row=>row.type===type('resume','ACK'));
 const intents=events.filter(row=>[type('stop','INTENT'),type('resume','INTENT')].includes(row.type));
 if(!stopped.length&&!resumed.length&&!intents.length)return undefined;
 requireValue(stopped.length===1&&resumed.length===1&&intents.length===2);
 const ctx=repairContext(migration,events,actual,resumed[0].imageRef);
 const {imageRef,candidateSHA256}=resumed[0];
 const pins={migrationPlanSHA256:fingerprint(migration),deploymentUID:ctx.uid,beforeSHA256:fingerprint(ctx.before),
  stoppedSHA256:fingerprint(ctx.stopped),afterSHA256:fingerprint(ctx.after),imageRef,candidateSHA256};
 const history=[intents[0],stopped[0],intents[1],resumed[0]];
 const expectedTypes=[type('stop','INTENT'),type('stop','ACK'),type('resume','INTENT'),type('resume','ACK')];
 const repairSHA256=fingerprint(pins);
 requireValue(sha(candidateSHA256)&&actual.metadata.annotations?.[marker]===repairSHA256+':resume'&&
  history.every((row,index)=>row.type===expectedTypes[index]&&row.repairSHA256===repairSHA256&&rv(row.resourceVersion)&&
   Object.entries(pins).every(([key,value])=>row[key]===value)&&(!index||events.indexOf(row)>events.indexOf(history[index-1])))&&
  stopped[0].resourceVersion!==intents[0].resourceVersion&&resumed[0].resourceVersion!==intents[1].resourceVersion);
 return ctx.after;
}

export async function repairPersonalArtifactGrant({migration,journal,kube,readPods,wait,clock,authority,readAPI,checkMigration,
 bindExecutor,loadCandidate,approvedSHA256}) {
 checkMigration();
 const initial=await loadCandidate(),get=()=>JSON.parse(kube(['-n',namespace,'get','deployment',name,'-o','json']));
 const first=get(),ctx=repairContext(migration,journal.events,first,initial.imageRef);
 const pins=repairPins(migration,ctx,initial),repairSHA256=fingerprint(pins);
 const binding=journal.events.find(row=>row.type==='PERSONAL_INTEGRATIONS_READERS_BOUND');
 requireValue(binding&&binding.migrationPlanSHA256===pins.migrationPlanSHA256&&
  !journal.events.some(row=>['PERSONAL_INTEGRATIONS_TERMINAL_ACCEPTED','BACKUP_RESUME_INTENT'].includes(row.type))&&
  readAPI().openPlanSHA256===binding.openPlanSHA256);
 const read=()=>{
  const actual=get();
  repairContext(migration,journal.events,actual,initial.imageRef);
  const states=[
   [ctx.before,undefined],[ctx.stopped,repairSHA256+':stop'],[ctx.after,repairSHA256+':resume'],
  ];
  const stage=states.findIndex(([spec,annotation])=>same(spec,actual.spec)&&actual.metadata.annotations?.[marker]===annotation);
  requireValue(stage>=0);
  return {actual,stage};
 };
 const draft=(actual,wanted,step)=>{
  const patch=[{op:'test',path:'/metadata/uid',value:ctx.uid},{op:'test',path:'/metadata/resourceVersion',value:actual.metadata.resourceVersion},
   {op:'test',path:'/spec',value:actual.spec},{op:'replace',path:'/spec',value:wanted},
   {op:'add',path:'/metadata/annotations',value:{...actual.metadata.annotations,[marker]:repairSHA256+':'+step}}];
  const args=path=>['-n',namespace,'patch','deployment',name,'--type=json','--patch-file='+path,'-o','json'];
  const predicted=withPrivatePatchFile(patch,path=>JSON.parse(kube([...args(path),'--dry-run=server'])));
  requireValue(predicted.metadata.uid===ctx.uid&&same(predicted.spec,wanted)&&predicted.metadata.annotations?.[marker]===repairSHA256+':'+step);
  return {patch,args};
 };
 if(approvedSHA256===undefined) {
  requireValue(!journal.events.some(row=>row.type?.startsWith(prefix))&&read().stage===0);
  draft(first,ctx.stopped,'stop');
  return {status:'PLAN_ONLY',repairSHA256,...pins};
 }
 requireValue(approvedSHA256===repairSHA256);
 for(const [index,step,wanted] of [[1,'stop',ctx.stopped],[2,'resume',ctx.after]]) {
  const intents=journal.events.filter(row=>row.type===type(step,'INTENT'));
  const acks=journal.events.filter(row=>row.type===type(step,'ACK'));
  requireValue(intents.length<=1&&acks.length<=1&&(!acks.length||intents.length)&&
   [...intents,...acks].every(row=>row.repairSHA256===repairSHA256&&rv(row.resourceVersion)&&
    Object.entries(pins).every(([key,value])=>row[key]===value)));
  let state=read();
  if(state.stage>=index) {
   requireValue(intents.length===1);
   if(!acks.length){
    requireValue(state.stage===index&&state.actual.metadata.resourceVersion!==intents[0].resourceVersion);
    journal.append({type:type(step,'ACK'),...pins,repairSHA256,resourceVersion:state.actual.metadata.resourceVersion});
   }
   continue;
  }
  requireValue(!intents.length&&state.stage===index-1);
  if(step==='resume'){
   await wait(()=>{const current=read();return {...current,joined:current.stage===1&&
    current.actual.status?.observedGeneration>=current.actual.metadata.generation&&!(current.actual.status.replicas??0)&&readPods(ctx.uid).length===0};
   },verify=>verify.joined,prefix+'_STOP_NOT_JOINED',clock);
  }
  requireValue(same(await loadCandidate(),initial));
  const proof=await authority();
  requireValue(proof.state==='RETIRED'&&proof.policyRevision===110&&proof.operationID===binding.operationID&&
   proof.bindingSHA256===binding.bindingSHA256&&readAPI().openPlanSHA256===binding.openPlanSHA256);
  state=read();
  requireValue(state.stage===index-1&&(step==='stop'||state.actual.status?.observedGeneration>=state.actual.metadata.generation&&
   !(state.actual.status.replicas??0)&&readPods(ctx.uid).length===0));
  const {patch,args}=draft(state.actual,wanted,step),resourceVersion=state.actual.metadata.resourceVersion;
  const observed=Date.parse(proof.observedAt);
  requireValue(observed<=Date.now()+5000&&Date.now()-observed<=30000);
  bindExecutor();
  journal.append({type:type(step,'INTENT'),...pins,repairSHA256,resourceVersion});
  try{withPrivatePatchFile(patch,path=>kube(args(path)));}catch{throw Error(prefix+'_PATCH_UNKNOWN');}
  const updated=read();
  requireValue(updated.stage===index&&updated.actual.metadata.resourceVersion!==resourceVersion);
  journal.append({type:type(step,'ACK'),...pins,repairSHA256,resourceVersion:updated.actual.metadata.resourceVersion});
 }
 requireValue(same(personalArtifactGrantRepairSpec(migration,journal.events,get()),ctx.after));
 return {status:'ARTIFACT_GRANT_REPAIRED',repairSHA256,imageRef:initial.imageRef};
}
