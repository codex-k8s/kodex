#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { lstatSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { fingerprint } from './scoped-release.mjs';
import { inspectSource, planSourceChange } from './application-source.mjs';
import { migrationReadback } from './control-plane-migration.mjs';
import { privateJournal } from '../dev/role-image-acceptance.mjs';
const ensure = (ok,code) => { if (!ok) throw new Error(code); };
const ns = 'kodex-system';
export function mailboxMaintenancePlan(before, source, inspect = inspectSource) {
  ensure(before?.kind === 'Deployment' && before.metadata?.name === 'control-plane' && before.metadata.namespace === ns && before.metadata.uid && before.metadata.resourceVersion && before.metadata.labels?.['kodex.dev/environment'] === 'staging' && before.metadata.labels?.['kodex.dev/local-profile'] === 'hot-reload', 'CP_IDENTITY_INVALID');
  ensure(Number.isInteger(before.spec.replicas) && before.spec.replicas > 0 && before.spec.replicas <= 8 && before.status?.availableReplicas === before.spec.replicas, 'CP_HEALTH_REQUIRED');
  const index = before.spec.template.spec.containers.findIndex((c) => c.name === 'control-plane'); ensure(index >= 0,'CP_CONTAINER_REQUIRED');
  const stopped = structuredClone(before.spec); stopped.replicas = 0;
  const replaced = structuredClone(stopped); planSourceChange(before,replaced,index,source,inspect);
  const resumed = structuredClone(replaced); resumed.replicas = before.spec.replicas;
  return {version:1,kind:'CP_MAILBOX_MAINTENANCE',beforeUID:before.metadata.uid,beforeResourceVersion:before.metadata.resourceVersion,source,specs:{before:before.spec,stopped,replaced,resumed}};
}
function privateJSON(path) { path=resolve(path); const st=lstatSync(path); ensure(realpathSync(path)===path && st.isFile() && st.nlink===1 && !(st.mode&0o077) && st.size<(4<<20),'PRIVATE_INPUT_INVALID'); return JSON.parse(readFileSync(path)); }
function writePrivate(path,value) { path=resolve(path); ensure(realpathSync(dirname(path))===dirname(path) && !(lstatSync(dirname(path)).mode&0o077),'PRIVATE_DIRECTORY_REQUIRED'); writeFileSync(path,JSON.stringify(value)+'\n',{flag:'wx',mode:0o600}); }
export async function mailboxMaintenanceStep({phase,plan,journal,get,pods,patch,migration}) {
  const actual=get(); ensure(actual.metadata.uid===plan.beforeUID,'CP_UID_CHANGED');
  const stages=['before','stopped','replaced','resumed']; const stage=stages.find((s)=>fingerprint(actual.spec)===fingerprint(plan.specs[s])); ensure(stage,'CP_SPEC_CHANGED');
  const gone=()=>pods().every((p)=>['Succeeded','Failed'].includes(p.status?.phase));
  if(phase==='inspect') { const result={status:'OBSERVED',stage,podsGone:gone(),availableReplicas:actual.status?.availableReplicas??0,sourceRevision:plan.source.revision,resourceVersion:actual.metadata.resourceVersion}; journal.append({type:'INSPECT',...result}); return result; }
  const edges={stop:['before','stopped'],replace:['stopped','replaced'],resume:['replaced','resumed']}; const [from,to]=edges[phase]??[]; ensure(from,'PHASE_INVALID');
  const intent=journal.events.find((e)=>e.type==='INTENT'&&e.step===phase);
  if(stage===to) { ensure(intent,'UNOWNED_CP_TRANSITION'); journal.append({type:'READBACK',step:phase,stage,resourceVersion:actual.metadata.resourceVersion}); return {status:phase==='stop'&&!gone()?'DRAINING':'ACKNOWLEDGED',stage}; }
  ensure(stage===from,'CP_PHASE_ORDER_INVALID');
  if(phase==='stop') ensure(actual.metadata.resourceVersion===plan.beforeResourceVersion,'CP_RESOURCE_VERSION_CHANGED');
  if(phase!=='stop') { ensure(gone(),'CP_PODS_REMAIN'); ensure(journal.events.some((e)=>e.type==='INTENT'&&e.step===(phase==='replace'?'stop':'replace')),'CP_PREDECESSOR_REQUIRED'); }
  if(phase==='replace'||phase==='resume') ensure(inspectSource(plan.source.path).revision===plan.source.revision,'SOURCE_REVISION_CHANGED');
  if(phase==='resume') ensure(migration().status==='PASS','MIGRATION_READBACK_REQUIRED');
  // После UNKNOWN нужен отдельный durable inspect того же before и RV.
  // Оба возможных PATCH имеют exact before-spec CAS: примениться может только один.
  if(intent) {
    const lastIntent=journal.events.findLastIndex((e)=>e.type==='INTENT'&&e.step===phase);
    const observed=journal.events.findLastIndex((e)=>e.type==='INSPECT'&&e.stage===from&&e.resourceVersion===actual.metadata.resourceVersion);
    ensure(observed>lastIntent,'CP_PATCH_OUTCOME_UNKNOWN');
  }
  const operations=[{op:'test',path:'/metadata/uid',value:plan.beforeUID},{op:'test',path:'/metadata/resourceVersion',value:actual.metadata.resourceVersion},{op:'test',path:'/spec',value:plan.specs[from]},{op:'replace',path:'/spec',value:plan.specs[to]}];
  journal.append({type:'INTENT',step:phase,patchSHA256:fingerprint(operations)});
  try { patch(operations); } catch { journal.append({type:'UNKNOWN',step:phase}); throw new Error('CP_PATCH_OUTCOME_UNKNOWN'); }
  const after=get(); ensure(after.metadata.uid===plan.beforeUID&&fingerprint(after.spec)===fingerprint(plan.specs[to]),'CP_READBACK_MISMATCH');
  journal.append({type:'ACK',step:phase,stage:to,resourceVersion:after.metadata.resourceVersion});
  return {status:phase==='stop'&&!gone()?'DRAINING':'ACKNOWLEDGED',stage:to};
}
async function main() {
  const args=process.argv.slice(2),phase=args.shift(),o={}; while(args.length){const k=args.shift();ensure(/^--[a-z-]+$/.test(k??'')&&args.length&&!(k in o),'ARGUMENT_INVALID');o[k]=args.shift();}
  ensure(['plan','stop','replace','resume','inspect'].includes(phase)&&Object.keys(o).every((k)=>['--context','--source','--revision','--plan','--evidence','--migration-plan','--confirm'].includes(k))&&o['--plan'],'ARGUMENT_INVALID');
  const context=o['--context'];ensure(/^[A-Za-z0-9_.:@/-]{1,160}$/.test(context??'')&&!/prod(?:uction)?/i.test(context),'STAGING_CONTEXT_REQUIRED');
  ensure(o['--confirm']===(['stop','replace','resume'].includes(phase)?'APPLY-STAGING-CP-MAILBOX-MAINTENANCE':undefined),'CONFIRMATION_INVALID');
  ensure(phase==='plan'?o['--source']&&o['--revision']&&!o['--evidence']&&!o['--migration-plan']:o['--evidence']&&!o['--source']&&!o['--revision']&&(phase==='resume'?!!o['--migration-plan']:!o['--migration-plan']),'PHASE_ARGUMENT_INVALID');
  const kube=(a,input)=>{try{return execFileSync('kubectl',['--context',context,'--request-timeout=30s',...a],{input,encoding:'utf8',timeout:35000,maxBuffer:4<<20,stdio:['pipe','pipe','pipe']});}catch{throw new Error('KUBERNETES_OPERATION_UNKNOWN');}};
  const get=()=>JSON.parse(kube(['-n',ns,'get','deployment','control-plane','-o','json'])); const clusterUID=JSON.parse(kube(['get','namespace','kube-system','-o','json'])).metadata.uid;
  if(phase==='plan'){const plan={...mailboxMaintenancePlan(get(),{path:o['--source'],revision:o['--revision']}),context,clusterUID};writePrivate(o['--plan'],plan);process.stdout.write(JSON.stringify({status:'PLANNED',planSHA256:fingerprint(plan)})+'\n');return;}
  const plan=privateJSON(o['--plan']);ensure(plan.kind==='CP_MAILBOX_MAINTENANCE'&&plan.context===context&&plan.clusterUID===clusterUID,'PLAN_SCOPE_MISMATCH');
  const journal=privateJournal(o['--evidence'],{version:1,kind:plan.kind,planSHA256:fingerprint(plan)});
  try{const result=await mailboxMaintenanceStep({phase,plan,journal,get,pods:()=>JSON.parse(kube(['-n',ns,'get','pods','-l','app.kubernetes.io/name=control-plane','-o','json'])).items,patch:(ops)=>kube(['-n',ns,'patch','deployment','control-plane','--type=json','--patch-file=/dev/stdin'],JSON.stringify(ops)),migration:()=>{
    const p=privateJSON(o['--migration-plan']);ensure(p.profile==='mailbox-observation'&&p.context===context&&p.clusterUID===clusterUID&&fingerprint(p.source)===fingerprint(plan.source),'MIGRATION_SCOPE_CHANGED');
    const job=JSON.parse(kube(['-n',ns,'get','job',p.job.metadata.name,'-o','json']));const logs=kube(['-n',ns,'logs',`job/${p.job.metadata.name}`,'-c','migrate','--tail=50']);return migrationReadback(job,p,logs);
  }});process.stdout.write(JSON.stringify(result)+'\n');}finally{journal.close();}
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))main().catch((error)=>{process.stderr.write((/^[A-Z][A-Z0-9_]{2,90}$/.test(error.message)?error.message:'CP_MAILBOX_MAINTENANCE_FAILED')+'\n');process.exitCode=1;});
