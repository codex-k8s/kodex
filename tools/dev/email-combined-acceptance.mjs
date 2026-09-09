import {createHash} from 'node:crypto';
import {existingFixture, prepareRuntimePlan} from './role-image-runtime-proof.mjs';
import {workspaceAcceptanceTask, verifyWorkspaceAcceptance} from './runtime-workspace-acceptance.mjs';
import {validateRuntimeObservation} from '../release/runtime-pod-observe.mjs';
const check=(value,code)=>{if(!value)throw new Error(code);};
const sha=value=>createHash('sha256').update(typeof value==='string'||Buffer.isBuffer(value)?value:JSON.stringify(value)).digest('hex');
const digest=value=>/^[a-f0-9]{64}$/.test(value??'');
const saved=(journal,step)=>journal.events.findLast(e=>e.step===step&&['ACK','CHECKPOINT'].includes(e.type))?.result;

export function loadCombinedProfile(profile,origin,readPrivate,manifest) {
 if(!profile.combined)return undefined;
 const c=profile.combined;
 check(c.version===1&&Object.keys(c).sort().join()==='fixtureSHA256,fixtureState,runnerProvenance,runnerProvenanceSHA256,version'&&profile.agentRef&&digest(c.fixtureSHA256)&&digest(c.runnerProvenanceSHA256),'COMBINED_PROFILE_INVALID');
 const fixture=existingFixture(c.fixtureState,c.fixtureSHA256,origin);
 check(fixture.projectRef===profile.projectRef&&fixture.agentRef===profile.agentRef,'COMBINED_FIXTURE_SCOPE_CHANGED');
 const header=JSON.parse(readPrivate(c.fixtureState).toString('utf8').split('\n')[0]);
 const bytes=readPrivate(c.runnerProvenance);check(sha(bytes)===c.runnerProvenanceSHA256,'RUNNER_PROVENANCE_CHANGED');const p=JSON.parse(bytes);
 check(p.version===1&&p.kind==='RUNNER_BINARY_PROVENANCE'&&/^[a-f0-9]{40}$/.test(p.sourceRevision??'')&&p.binaryPath==='/usr/local/bin/kodex-agent-runner'&&digest(p.binarySHA256)&&/^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$/.test(p.baseImage??'')&&p.baseImage.endsWith(`@${header.runnerDigest}`)&&/^sha256:[a-f0-9]{64}$/.test(header.runnerDigest??''),'RUNNER_PROVENANCE_INVALID');
 check(manifest?.clusterUID&&manifest.namespaceUID,'COMBINED_CLUSTER_REQUIRED');
 return {clusterUID:manifest.clusterUID,namespaceUID:manifest.namespaceUID,fixture,provenance:{sourceRevision:p.sourceRevision,baseImage:p.baseImage,binaryPath:p.binaryPath,binarySHA256:p.binarySHA256,bytesSHA256:c.runnerProvenanceSHA256},fixtureSHA256:c.fixtureSHA256};
}
export async function combinedPlan(profile,context,nonce,get) {
 check(/^[a-f0-9]{32}$/.test(nonce??''),'WORKSPACE_NONCE_INVALID');
 const runtime=await prepareRuntimePlan(context.fixture,get);
 check(runtime.status==='READY'&&runtime.accountPin.accountRef===profile.accountRef,'COMBINED_RUNTIME_NOT_READY');
 return {version:1,...context,nonce,runtime};
}
export function combinedTask(plan,emailTask) {
 return `Выполни две части в одном turn. Финальный ответ дай только после обеих частей.\n${workspaceAcceptanceTask(plan.nonce)}\nЭто первая часть единого задания. Если workspace probe не выполнен полностью, остановись без почтового вызова. После успешного probe выполни вторую часть ниже ровно один раз; ограничения второй части относятся к почтовому действию. Не повторяй probe, не меняй его код и не исправляй отказ. Не запускай shell после начала почтового действия.\n${emailTask}`;
}
export async function combinedRuntimeBinding(profile,plan,launched,get) {
 check(plan.combined,'COMBINED_PROFILE_REQUIRED');
 const run=await get(`/api/v1/runs/${encodeURIComponent(launched.runRef)}`);
 check(run.ref===launched.runRef&&run.projectRef===profile.projectRef&&run.sessionRef===launched.sessionRef&&run.target?.ref===profile.agentRef&&run.attempt===launched.attempt,'COMBINED_RUN_CHANGED');
 if(run.state==='QUEUED')return {status:'PENDING',runRef:run.ref};
 const diff=await get(`/api/v1/runs/${encodeURIComponent(run.ref)}/runtime-revision-diff`),revision=diff.current;
 check(revision?.runRef===run.ref&&revision.sessionRef===run.sessionRef&&revision.attempt===run.attempt&&/^[A-Za-z0-9_-]{8,128}$/.test(revision.turnRef??'')&&digest(revision.revisionDigest),'COMBINED_REVISION_CHANGED');
 const image=diff.changes?.find(c=>c.component==='IMAGE')?.current;
 check(image?.digest?.replace(/^sha256:/,'')===plan.combined.fixture.manifestDigest.slice(7),'COMBINED_IMAGE_CHANGED');
 return {version:1,kind:'COMBINED_RUNTIME_BINDING',status:'BOUND',clusterUID:plan.combined.clusterUID,namespaceUID:plan.combined.namespaceUID,planSHA256:plan.planSHA256,runRef:run.ref,projectRef:profile.projectRef,agentRef:profile.agentRef,sessionRef:run.sessionRef,turnRef:revision.turnRef,attempt:run.attempt,revisionRef:revision.ref,revisionVersion:revision.version,revisionDigest:revision.revisionDigest,projectHash:sha(profile.projectRef).slice(0,16),sessionHash:sha(run.sessionRef).slice(0,16),turnHash:sha(revision.turnRef).slice(0,16),image:plan.combined.fixture.promotedReference,imageManifestDigest:plan.combined.fixture.manifestDigest,runnerProvenance:plan.combined.provenance};
}
export async function finishCombinedCapture({profile,plan,journal,captured,get,getContent,observation}) {
 if(!plan.combined)return captured;
 if(captured.invocationState!=='SUCCEEDED')return captured;
 check(captured.runTerminal&&captured.runState==='SUCCEEDED','COMBINED_RUN_NOT_SUCCESSFUL');
 const binding=await combinedRuntimeBinding(profile,plan,captured,get);
 check(binding.revisionRef===captured.runtimeRevisionRef&&binding.revisionDigest===captured.runtimeRevisionDigest&&binding.turnRef===captured.turnRef,'COMBINED_CAPTURE_CHANGED');
 check(observation,'RUNTIME_OBSERVATION_REQUIRED');validateRuntimeObservation(observation,binding);
 let workspace;try{workspace=await verifyWorkspaceAcceptance({getJSON:get,getContent,runRef:captured.runRef,projectRef:profile.projectRef,agentRef:profile.agentRef,nonce:plan.combined.nonce});}catch(e){throw new Error(e.code==='ARTIFACT_SCAN_PENDING'?'WORKSPACE_SCAN_PENDING':'WORKSPACE_PROOF_FAILED');}
 check(workspace.runtimeRevisionDigest===captured.runtimeRevisionDigest&&workspace.attempt===captured.attempt&&workspace.executionBindingDigest===observation.executionBindingDigest,'WORKSPACE_CAPTURE_CHANGED');
 const result={status:'CAPTURED',runRef:captured.runRef,invocationRef:captured.invocationRef,runtimeRevisionRef:captured.runtimeRevisionRef,runtimeRevisionDigest:captured.runtimeRevisionDigest,attempt:captured.attempt,observationSHA256:sha(observation),podUID:observation.podUID,nodeName:observation.nodeName,imageID:observation.imageID,runnerBinarySHA256:observation.process.binarySHA256,workspace,receipt:'NOT_RUN'};
 const old=saved(journal,'combined-capture');check(!old||old.observationSHA256===result.observationSHA256,'RUNTIME_OBSERVATION_CHANGED');
 journal.append({type:'CHECKPOINT',step:'combined-capture',result});return result;
}
