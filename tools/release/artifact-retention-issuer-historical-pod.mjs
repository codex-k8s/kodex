import {fingerprint} from './scoped-release.mjs';
import {parseAuthorityPrivateJSON} from './authority-catalog-transition.mjs';
import {verifyIssuerMigrationJob,validateIssuerMigrationState} from './artifact-retention-issuer-migration.mjs';

const fail=(ok,code)=>{if(!ok)throw Error('ISSUER_HISTORICAL_'+code);};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const namespace='kodex-system';
const podQuery='go-template={{with .metadata.continue}}{{.}}{{end}}{{"\\n"}}{{range .items}}{{.metadata.uid}}{{"\\t"}}{{.metadata.namespace}}{{"\\t"}}{{.metadata.name}}{{"\\t"}}{{range .metadata.ownerReferences}}{{.uid}}{{","}}{{end}}{{"\\t"}}{{with .metadata.labels}}{{with index . "batch.kubernetes.io/controller-uid"}}{{.}}{{end}}{{end}}{{"\\t"}}{{with .metadata.labels}}{{with index . "controller-uid"}}{{.}}{{end}}{{end}}{{"\\t"}}live{{"\\n"}}{{end}}';
const nodeQuery='go-template={{with .metadata.continue}}{{.}}{{end}}{{"\\n"}}{{range .items}}{{.metadata.name}}{{"\\t"}}{{.metadata.uid}}{{"\\t"}}{{range .status.conditions}}{{if eq .type "Ready"}}{{.status}}{{end}}{{end}}{{"\\t"}}{{range .status.conditions}}{{if eq .status "Unknown"}}unknown{{end}}{{end}}{{"\\t"}}{{if .metadata.deletionTimestamp}}deleting{{else}}live{{end}}{{"\\n"}}{{end}}';

function complete(job,pass){
 const status=job.status,completed=Date.parse(status?.completionTime),accepted=Date.parse(pass.at);
 fail(status?.succeeded===1&&[status.active,status.failed,status.terminating,status.ready].every(n=>n===undefined||n===0)&&
  !job.metadata.deletionTimestamp&&Array.isArray(status.conditions)&&
  status.conditions.filter(c=>c.type==='Complete'&&c.status==='True').length===1&&
  status.conditions.length<=2&&new Set(status.conditions.map(c=>c.type)).size===status.conditions.length&&
  status.conditions.every(c=>['Complete','SuccessCriteriaMet'].includes(c.type)&&c.status==='True')&&
  !(status.uncountedTerminatedPods?.succeeded?.length)&&!(status.uncountedTerminatedPods?.failed?.length)&&
  Number.isFinite(completed)&&completed<=accepted&&Number.isFinite(accepted),'RETAINED_JOB_NOT_COMPLETE');
}

// Это дополнительное отсутствие текущего исполнения; исторический positive
// join доказывает прежний accepted PASS. Отсутствие API Pod отдельно не даёт
// новый join, не позволяет запустить Job и не заменяет role/schema READ.
export function assertHistoricalIssuerPodAbsent(prior,job,pass,kube){
 const raw=kube(['-n',namespace,'get','pods','--chunk-size=0','-o',podQuery]);
 fail(typeof raw==='string'&&raw.startsWith('\n')&&raw.endsWith('\n')&&Buffer.byteLength(raw)<=1<<20,'POD_INVENTORY_REJECTED');
 const rows=raw.split('\n');fail(rows.shift()===''&&rows.length<=4097,'POD_INVENTORY_REJECTED');
 const items=rows.filter(Boolean).map(row=>row.split('\t'));
 fail(items.every(row=>row.length===7&&row[0]&&row[1]===namespace&&row[2]&&row[6]==='live')&&
  new Set(items.map(row=>row[0])).size===items.length,'POD_INVENTORY_REJECTED');
 fail(items.every(row=>row[0]!==pass.podUID&&!row[2].startsWith(job.metadata.name+'-')&&!row[3].split(',').includes(pass.jobUID)&&row[4]!==pass.jobUID&&row[5]!==pass.jobUID),
  'OLD_POD_STILL_PRESENT');
 const nodesRaw=kube(['get','nodes','--chunk-size=0','-o',nodeQuery]);
 fail(Buffer.byteLength(nodesRaw)<=65536,'RUNTIME_NODES_CHANGED');
 const nodeRows=nodesRaw.split('\n');fail(nodeRows.shift()==='','RUNTIME_NODES_CHANGED');
 const nodes=nodeRows.filter(Boolean).map(row=>{const [name,uid,ready,unknown,state,...extra]=row.split('\t');
  fail(!extra.length&&ready==='True'&&unknown===''&&state==='live'&&/^[a-z0-9][a-z0-9.-]{0,252}$/.test(name)&&/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(uid??''),'RUNTIME_NODES_CHANGED');return {name,uid};});
 const expected=prior.entries.map(({name,uid})=>({name,uid})),sort=values=>[...values].sort((a,b)=>a.name.localeCompare(b.name));
 fail(nodes.length>0&&nodes.length<=16&&new Set(nodes.map(n=>n.name)).size===nodes.length&&same(sort(nodes),sort(expected)),'RUNTIME_NODES_CHANGED');
 for(const node of nodes){
  // Upstream kubelet /runningpods/ обращается к CRI cache, в отличие от desired
  // /pods. Недоступный/debug-disabled endpoint закрывает этот READ branch.
  const runtimeRaw=kube(['get','--raw',`/api/v1/nodes/${node.name}/proxy/runningpods/`]);
  fail(Buffer.byteLength(runtimeRaw)<=1<<20,'RUNTIME_INVENTORY_REJECTED');
  const runtime=parseAuthorityPrivateJSON(runtimeRaw),pods=runtime.items;
  fail(runtime.kind==='PodList'&&runtime.apiVersion==='v1'&&!runtime.metadata?.continue&&Array.isArray(pods)&&pods.length<=4096&&
   pods.every(p=>typeof p.metadata?.uid==='string'&&p.metadata.uid&&typeof p.metadata.name==='string'&&p.metadata.name&&
    typeof p.metadata.namespace==='string'&&p.metadata.namespace)&&new Set(pods.map(p=>p.metadata.uid)).size===pods.length,'RUNTIME_INVENTORY_REJECTED');
  fail(pods.every(p=>p.metadata.uid!==pass.podUID&&!(p.metadata.namespace===namespace&&p.metadata.name.startsWith(job.metadata.name+'-'))),
   'OLD_RUNTIME_POD_STILL_PRESENT');
 }
}

// Только прежний terminal PASS с тем же retained complete Job позволяет
// исторический read после GC Pod. Present/invalid Pod никогда не попадает в
// fallback; обычный positive join остаётся у существующего status consumer.
export async function readIssuerHistoricalMissingPodStatus(plan,journal,io,prior,kube){
 const expected=io.expectedJob(),pass=journal.events.find(row=>row.type==='PASS');
 fail(pass&&pass.jobUID&&pass.podUID,'ORIGINAL_PASS_REQUIRED');
 await io.preflight();
 const job=await io.getJob();fail(job,'RETAINED_JOB_REQUIRED');
 verifyIssuerMigrationJob(job,expected,pass.jobUID);complete(job,pass);
 const pods=await io.getPods(job);
 fail(Array.isArray(pods?.items)&&!pods.metadata?.continue&&pods.items.length===0,'POD_ABSENCE_CHANGED');
 assertHistoricalIssuerPodAbsent(prior,job,pass,kube);
 await io.preflight();
 const state=await io.readState();validateIssuerMigrationState(state,true);
 const final=await io.getJob();fail(final,'RETAINED_JOB_REQUIRED');
 verifyIssuerMigrationJob(final,expected,pass.jobUID);complete(final,pass);
 const finalPods=await io.getPods(final);
 fail(Array.isArray(finalPods?.items)&&!finalPods.metadata?.continue&&finalPods.items.length===0,'POD_ABSENCE_CHANGED');
 assertHistoricalIssuerPodAbsent(prior,final,pass,kube);
 return {status:'PASS',jobName:final.metadata.name,jobUID:pass.jobUID,podUID:pass.podUID,migrationVersion:state.head,forwardOnly:true};
}
