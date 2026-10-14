// Узкое обслуживание SQL claim: очередь сохраняется, controller выключен до
// FF и exact serving readback. Этот путь не заменяет idle gate runner/policy.
import { readFileSync, realpathSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';

const hash = bytes => createHash('sha256').update(bytes).digest('hex');
const check = (condition, code) => { if (!condition) throw Error(code); };
export function verifyIdleTurns(state, now = Date.now()) {
  check(Number.isFinite(Date.parse(state?.at)) && Math.abs(now-Date.parse(state.at)) < 30000 &&
    ['runningNodes','claimedLeases','openBuilds','pendingAdmissions','pendingPromotions'].every(k=>state[k]===0) &&
    /^[a-f0-9]{64}$/.test(state.pins??''), 'IDLE_TURNS_REQUIRED');
}
export function verifyDelta(paths) {
  const allowed = new Set([
    'services/internal/control-plane/internal/repository/postgres/platform/sql/runtime_files_capture_entries.sql',
    'services/internal/control-plane/internal/repository/postgres/platform/runtime_files_capture_plan_test.go',
    'services/internal/control-plane/internal/repository/postgres/platform/provider_renewal_component_test.go',
    'services/internal/control-plane/internal/maintenance/providermodelcatalog/renewal_worker_test.go',
    'tools/dev/runtime-queue-state-readback.sql', 'tools/dev/queued-runtime-source-update.sql',
    'tools/dev/queued-runtime-source-update.mjs', 'tools/dev/queued-runtime-source-update.test.mjs',
    'docs/operations/self-development-handoff.md', 'docs/operations/self-development-dogfooding.md',
    'docs/qa/bootstrap-1797-evidence-ledger.md',
  ]);
  check(paths.length>0 && paths.every(p=>allowed.has(p)), 'SQL_ONLY_DELTA_REQUIRED');
}

async function main() {
  const [source,hot,previous,target,binary] = process.argv.slice(2);
  let paused=false, deployment, originalTemplate, stage='preflight';
  const run=(cmd,args,options={})=>execFileSync(cmd,args,{encoding:'utf8',timeout:20000,maxBuffer:2<<20,stdio:['pipe','pipe','pipe'],...options}).trim();
  const git=(root,...args)=>run('git',['-C',root,...args]);
  const kube=(...args)=>run('kubectl',['--context=k3d-kodex','--request-timeout=15s','-n','kodex-system',...args]);
  const state=()=>JSON.parse(run('kubectl',['--context=k3d-kodex','--request-timeout=15s','-n','kodex-system','exec','-i','kodex-postgresql-0','--','psql','-X','-qAt','-v','ON_ERROR_STOP=1','-U','postgres','-d','control_plane'],{input:readFileSync(source+'/tools/dev/queued-runtime-source-update.sql')}));
  const readDeployment=()=>JSON.parse(kube('get','deployment/runtime-controller','-o','json'));
  const restore=()=>{
    const fresh=readDeployment();
    check(fresh.metadata.uid===deployment.metadata.uid && hash(JSON.stringify(fresh.spec.template))===originalTemplate && fresh.spec.replicas===0,'CONTROLLER_RESTORE_CONFLICT');
    kube('scale','deployment/runtime-controller','--replicas=1','--current-replicas=0','--resource-version='+fresh.metadata.resourceVersion);
    paused=false;
  };
  try {
    check(process.argv.length===7 && realpathSync(source)===source && realpathSync(hot)===hot && source.startsWith(hot+'/.agents/') &&
      /^[a-f0-9]{40}$/.test(previous??'') && /^[a-f0-9]{40}$/.test(target??''),'EXACT_SOURCE_REQUIRED');
    check(git(source,'rev-parse','HEAD')===target && git(hot,'rev-parse','HEAD')===previous &&
      git(source,'status','--porcelain')==='' && git(hot,'status','--porcelain')==='' &&
      git(hot,'branch','--show-current')==='kodex-agent/issue-1797-full21-v7','SOURCE_PRECONDITION_FAILED');
    git(hot,'merge-base','--is-ancestor',previous,target);
    verifyDelta(git(source,'diff','--name-only',previous,target).split('\n'));
    const expectedELF=hash(readFileSync(binary));
    const before=state(); verifyIdleTurns(before);
    deployment=readDeployment(); originalTemplate=hash(JSON.stringify(deployment.spec.template));
    check(deployment.spec.replicas===1 && deployment.spec.template.metadata.labels?.['kodex.dev/security-profile']==='trusted-cluster' &&
      deployment.spec.template.metadata.labels?.['kodex.dev/local-profile']==='hot-reload','TRUSTED_CONTROLLER_REQUIRED');
    stage='quiesce';
    kube('scale','deployment/runtime-controller','--replicas=0','--current-replicas=1','--resource-version='+deployment.metadata.resourceVersion);paused=true;
    const until=Date.now()+90000;
    while(true){
      const pods=JSON.parse(kube('get','pods','-l','app.kubernetes.io/name=runtime-controller','-o','json')).items;
      if(!pods.some(p=>!['Succeeded','Failed'].includes(p.status.phase)))break;
      check(Date.now()<until,'CONTROLLER_DRAIN_TIMEOUT'); await new Promise(r=>setTimeout(r,1000));
    }
    const drained=state();verifyIdleTurns(drained);check(drained.pins===before.pins,'PUBLISHED_PINS_CHANGED');
    stage='source-ff';
    check(git(hot,'rev-parse','HEAD')===previous && git(hot,'status','--porcelain')==='','HOT_CHANGED');
    git(hot,'merge','--ff-only',target);
    check(git(hot,'rev-parse','HEAD')===target && git(hot,'status','--porcelain')==='','SOURCE_READBACK_FAILED');
    stage='serving';const deadline=Date.now()+180000;
    while(true){
      const pods=JSON.parse(kube('get','pods','-l','app.kubernetes.io/name=control-plane','-o','json')).items.filter(p=>p.status.phase==='Running'&&!p.metadata.deletionTimestamp && p.status.containerStatuses?.some(c=>c.name==='control-plane'&&c.ready));
      if(pods.length===1){
        let actual='';try{actual=kube('exec',pods[0].metadata.name,'-c','control-plane','--','sh','-c',"for pid in $(ps -o pid,comm | awk '$2==\"main\" {print $1}'); do sha256sum /proc/$pid/exe; done").split(/\s+/)[0];}catch{}
        if(actual===expectedELF)break;
      }
      check(Date.now()<deadline,'SERVING_ELF_MISMATCH');await new Promise(r=>setTimeout(r,1000));
    }
    const final=state();verifyIdleTurns(final);check(final.pins===before.pins,'PUBLISHED_PINS_CHANGED');
    restore();
    console.log(JSON.stringify({status:'PASS',at:new Date().toISOString(),target,servingELF:expectedELF,controllerRestored:true,publishedPins:final.pins}));
  } catch(error) {
    let restored=!paused;if(paused)try{restore();restored=true;}catch{}
    console.log(JSON.stringify({status:'FAIL',stage,code:/^[A-Z_]+$/.test(error.message)?error.message:'OPERATION_FAILED',controllerRestored:restored,sourceReadbackRequired:true}));process.exitCode=1;
  }
}
if(process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href)await main();
