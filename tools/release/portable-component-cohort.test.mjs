import {telegramAuthorityReadbackContract} from './authority-telegram-transition.mjs';
import test from 'node:test';
import {componentCohortFixture as fixture,replaceComponentLock as replaceLock} from './fixtures/component-cohort-synthetic.mjs';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {readFileSync,writeFileSync,mkdtempSync,rmSync,chmodSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,dirname} from 'node:path';
import {spawnSync} from 'node:child_process';
import {fingerprint} from './scoped-release.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {componentMigrationPublication,componentMigrationTarget,componentMigrationNames,
  planComponentMigrationHandoff,componentMigrationLifecycle} from './portable-component-migration.mjs';

const revision='a'.repeat(40);
const uuid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`;

test('044: exact пять компонентов и прежний seal042 не заменяют отдельный predecessor043',()=>{
  const f=fixture(),result=componentMigrationPublication(f.value,{revision},'github-repository-scope');
  assert.equal(result.components.length,5);
  assert.ok(componentMigrationTarget(f.target,f.value,f.observed));
  for(const mutate of [v=>v.components.pop(),v=>v.components.reverse(),v=>v.schema='kodex.portable-component-migration/v1',
    v=>v.schemaHeadsBefore['control-plane']=20261112000100,v=>v.schemaHeadsAfter['control-plane']=20261114130000,
    v=>v.baseSchemaHeads['control-plane']=20261111000200,v=>v.baseSchemaHeads['email-bridge']=4,
    v=>v.profile='sentry',v=>delete v.baseSchemaHeads,v=>v.components[2].nodeProof.nodes[0].repoDigestVerified=false]) {
    const value=structuredClone(f.value);mutate(value);
    assert.throws(()=>componentMigrationPublication(value,{revision},value.profile));
  }
  const wrong=structuredClone(f.observed);wrong.installation.data['schema-heads.json']=JSON.stringify(f.value.schemaHeadsBefore);
  assert.throws(()=>componentMigrationTarget(f.target,f.value,wrong));
  const incomplete=structuredClone(f.target);incomplete.resources.pop();
  assert.throws(()=>componentMigrationTarget(incomplete,f.value,f.observed));
  const duplicate=structuredClone(f.observed);duplicate.deployments[4]=structuredClone(duplicate.deployments[3]);
  assert.throws(()=>componentMigrationTarget(f.target,f.value,duplicate));
});

test('044: неизвестные cohort, foreign source, изменённый ABI и смешанный source отклоняются',()=>{
  for(const mutate of [l=>l.compatibility.verified_cohort.components.pop(),l=>l.compatibility.verified_cohort.contract_change='WORKSPACE_UNBOUND_STOP_UPGRADE',
    l=>l.compatibility.verified_cohort.anchor='services/external/foreign/code.go',l=>l.compatibility.verified_cohort.runner_input_abi='CHANGED',
    l=>l.source_sha='e'.repeat(40)]) {
    const f=fixture(),part=f.value.components[2],lock=JSON.parse(Buffer.from(part.componentLockRawBase64,'base64'));
    mutate(lock);replaceLock(part,lock);
    assert.throws(()=>componentMigrationPublication(f.value,{revision},'github-repository-scope'));
  }
});

test('044: handoff сохраняет env и maintenance API=0 вместо неявного открытия admission',()=>{
  const f=fixture(),plan=planComponentMigrationHandoff(f.value,f.deployments,{revision});
  assert.equal(plan.components.length,5);
  for(const part of plan.components) {
    assert.deepEqual(part.beforeSpec.template.spec.containers[0].env,part.stagedSpec.template.spec.containers[0].env);
    assert.equal(part.stoppedSpec.replicas,0);
    assert.equal(part.resumedSpec.replicas,part.beforeSpec.replicas);
  }
  const gateway=plan.components.find(c=>c.name==='control-api-gateway');
  assert.equal(gateway.maintenance,true);assert.equal(gateway.resumedSpec.replicas,0);
  const wrong=structuredClone(f.deployments);wrong.find(d=>d.metadata.name==='control-api-gateway').spec.replicas=2;
  assert.throws(()=>planComponentMigrationHandoff(f.value,wrong,{revision}));
});

// Исполняется lifecycle с настоящим discovery/join и файловым CAS primitive.
// Только Kubernetes API и PostgreSQL transport заменены ограниченной оснасткой.
function lifecycleFixture(profile='github-repository-scope') {
  const f=fixture(profile),handoff=planComponentMigrationHandoff(f.value,f.deployments,{revision});
  const drain=readFileSync(new URL('./portable-component-migration-drain.sql',import.meta.url));
  const postgres={pod:'kodex-postgresql-0',uid:uuid(30),ownerUID:uuid(31)};
  const plan={source:{revision},portableTarget:f.target,imageEvidence:f.value,componentHandoff:handoff,migrationVersion:String(f.value.schemaHeadsAfter['control-plane']),postgres,
    componentDrainSQLSHA256:createHash('sha256').update(drain).digest('hex')};
  const apps=new Map(f.deployments.map(d=>[d.metadata.name,structuredClone(d)]));
  const events=[],patches=[],faults={blocked:'',lostACK:'',schema:false,postcondition:true,activeEffects:0,claimSessions:0,mutatePod:undefined,authorityDelivery:null,apiReaders:null,activeInteractionDeliveries:0,cpProjection:null,policyReady:false};
  const status=app=>{app.metadata.generation++;app.metadata.resourceVersion=String(Number(app.metadata.resourceVersion)+1);
    app.status={observedGeneration:app.metadata.generation,...Object.fromEntries(['replicas','updatedReplicas','readyReplicas','availableReplicas'].map(k=>[k,app.spec.replicas]))};};
  const inventory=()=>[...apps.values()].flatMap(app=>{
    const name=app.metadata.name,rsUID=app.metadata.uid+'-rs';
    const set={kind:'ReplicaSet',metadata:{name:name+'-rs',uid:rsUID,namespace:'kodex-system',labels:{'app.kubernetes.io/name':name},
      ownerReferences:[{kind:'Deployment',uid:app.metadata.uid,name,controller:true}]}};
    const count=app.spec.replicas || (faults.blocked===name?1:0);
    return [set,...Array.from({length:count},(_,index)=>({kind:'Pod',metadata:{name:name+'-'+index,uid:rsUID+'-'+index,namespace:'kodex-system',
      labels:{'app.kubernetes.io/name':name},ownerReferences:[{kind:'ReplicaSet',uid:rsUID,name:set.metadata.name,controller:true}]},
      spec:structuredClone(app.spec.template.spec),status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],
        containerStatuses:app.spec.template.spec.containers.map(c=>({name:c.name,ready:name!=='control-plane'||!profile.startsWith('telegram')||app.spec.template.spec.containers[0].image===f.value.components[0].expectedImageRef||faults.policyReady,imageID:c.image,state:{running:{}}}))}})).map(p=>{
          if(name==='control-plane')faults.mutatePod?.(p);
          return p;
        })];
  });
  const kube=(args,input)=>{
    if(args.includes('exec')) {
      assert.ok(Buffer.from(input).equals(drain));
      return JSON.stringify({profile:'PORTABLE_COMPONENT_DRAIN_V1',database:'control_plane',readOnly:'on',gooseVersion:f.value.schemaHeadsAfter['control-plane'],
        ...Object.fromEntries(['activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions','pendingPromotions',
          'activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks','activeRuntimeLeases','oldClaimSessions','activeIntegrationInvocations',
          'activeIntegrationConnectionTests','activePersonalCredentialTests','activeRepositoryBundles','activeConfigurationSources','activeConfigurationWritebacks'].map(k=>[k,0])),
        activeInteractionDeliveries:faults.activeInteractionDeliveries,activeIntegrationInvocations:faults.activeEffects,oldClaimSessions:faults.claimSessions});
    }
    if(args.includes('patch')) {
      const name=args[args.indexOf('deployment')+1],app=apps.get(name);
      const patch=JSON.parse(readFileSync(args.find(a=>a.startsWith('--patch-file=')).slice('--patch-file='.length)));
      if(args.includes('--dry-run=server'))faults.beforePatch?.(name);
      assert.equal(patch[0].value,app.metadata.uid);assert.equal(patch[1].value,app.metadata.resourceVersion);assert.deepEqual(patch[2].value,app.spec);
      if(!args.includes('--dry-run=server')) {
        app.spec=structuredClone(patch[3].value);status(app);patches.push(name);
        if(faults.lostACK===name){faults.lostACK='';throw Error('SYNTHETIC_RESPONSE_LOST');}
      }
      return '{}';
    }
    if(args.includes('deployment'))return JSON.stringify(apps.get(args[args.indexOf('deployment')+1]));
    if(args.includes('replicasets,pods,jobs'))return inventory().map(r=>[r.kind,r.metadata.name,r.metadata.uid,r.metadata.labels['app.kubernetes.io/name'],
      r.metadata.namespace,JSON.stringify(r.metadata.ownerReferences)].join('\t')).join('\n');
    if(args.includes('pod'))return JSON.stringify(inventory().find(r=>r.kind==='Pod'&&r.metadata.name===args[args.indexOf('pod')+1]));
    throw Error('SYNTHETIC_COMMAND_REJECTED');
  };
  const observeCP=()=>{const app=apps.get('control-plane'),part=handoff.components[0];
    return {stage:fingerprint(app.spec)===fingerprint(part.stoppedSpec)?'STOPPED':'STAGED',joined:app.spec.replicas===0};};
  const lifecycle=componentMigrationLifecycle({plan,kube,journal:{events,append:e=>events.push(e)},observeCP,
    readJob:()=>({status:faults.schema?'PASS':'UNKNOWN',schemaPostcondition:faults.postcondition?'PASS':'FAIL'}),
    preconditionSQL:()=>{throw Error('PREDECESSOR_MUST_NOT_RUN_AFTER_DDL');},drainSQL:()=>drain,
    readOwnerSQL:sql=>JSON.parse(kube(['exec'],sql)),readAuthorityDelivery:async()=>faults.authorityDelivery,readMaintenanceReaders:()=>faults.apiReaders,readCPProjection:required=>{if(!faults.cpProjection&&required!==false)throw Error('TELEGRAM_CP_PROJECTION_REQUIRED');return faults.cpProjection;},
    postgresIdentity:()=>postgres,validateInputs:()=>{},activePods:activeDeploymentPods,
    wait:async(observe,ready,code)=>{if(!ready(observe()))throw Error(code);},clock:{}});
  return {lifecycle,apps,events,patches,faults,plan,projectCP:()=>{
      const part=handoff.components[0];faults.cpProjection={stagedSpec:structuredClone(part.stagedSpec),resumedSpec:structuredClone(part.resumedSpec)};
      for(const spec of [faults.cpProjection.stagedSpec,faults.cpProjection.resumedSpec])spec.template.spec.containers[0].env.push({name:'POLICY_REVISION',value:'107'});
      apps.get('control-plane').spec=structuredClone(faults.cpProjection.stagedSpec);status(apps.get('control-plane'));faults.policyReady=true;
      const body={state:'PENDING_AUTHORITY',policyRevision:107,...telegramAuthorityReadbackContract(),sourceSHA:revision,
       introducedReceiverSHA256:fingerprint(plan.imageEvidence.introducedReceiver),...Object.fromEntries(['clusterUID','namespaceUID','installationUID','installationSHA256'].map(key=>[key,plan.portableTarget[key]])),
       observedAt:new Date().toISOString(),operationID:uuid(200),snapshotRevision:5};faults.authorityDelivery={...body,deliverySHA256:fingerprint(body)};
    },
    stopCP:()=>{apps.get('control-plane').spec=structuredClone(handoff.components[0].stoppedSpec);status(apps.get('control-plane'));events.push({type:'STOP_ACK'});}};
}

test('044 lifecycle: все workers joined, full postcondition, пять image handoff, API остаётся закрытым',async()=>{
  const f=lifecycleFixture();
  assert.throws(()=>f.lifecycle.requireStopped());
  await f.lifecycle.stopController();f.lifecycle.requireStopped();f.stopCP();
  await assert.rejects(()=>f.lifecycle.handoff(),/POSTCONDITION_REQUIRED/);
  f.faults.schema=true;f.faults.postcondition=false;
  await assert.rejects(()=>f.lifecycle.handoff(),/POSTCONDITION_REQUIRED/);
  f.faults.postcondition=true;f.faults.activeEffects=1;
  await assert.rejects(()=>f.lifecycle.handoff(),/OWNER_PRECONDITION_FAILED/);
  f.faults.activeEffects=0;await f.lifecycle.handoff();
  assert.ok([...f.apps.values()].every(a=>a.spec.replicas===0));
  const resumed=await f.lifecycle.resume();assert.equal(resumed.gatewayMaintenance,true);
  assert.equal(f.apps.get('control-api-gateway').spec.replicas,0);
  assert.equal(f.patches.filter(n=>n==='control-api-gateway').length,1);
  const count=f.patches.length;await f.lifecycle.resume();assert.equal(f.patches.length,count);
  assert.equal(f.lifecycle.inspect().filter(s=>s.maintenance&&s.joined&&s.stage==='stagedSpec').length,1);
});

test('044 lifecycle: оставшийся Pod, потерянный ACK, drift и подмена drain не обходят join/CAS',async()=>{
  const blocked=lifecycleFixture();blocked.faults.blocked='integration-gateway';
  await assert.rejects(()=>blocked.lifecycle.stopController(),/CONTROLLER_STOP_TIMEOUT/);
  assert.throws(()=>blocked.lifecycle.requireStopped(),/CONTROLLER_JOIN_REQUIRED/);
  const f=lifecycleFixture();await f.lifecycle.stopController();f.stopCP();f.faults.schema=true;
  f.faults.lostACK='integration-gateway';await assert.rejects(()=>f.lifecycle.handoff(),/RESPONSE_LOST/);
  const writes=f.patches.filter(n=>n==='integration-gateway').length;
  f.lifecycle.inspect();await f.lifecycle.handoff();assert.equal(f.patches.filter(n=>n==='integration-gateway').length,writes);
  f.plan.componentDrainSQLSHA256='0'.repeat(64);await assert.rejects(()=>f.lifecycle.resume(),/PRECONDITION_SOURCE_CHANGED/);
  f.apps.get('runtime-controller').metadata.uid=uuid(99);assert.throws(()=>f.lifecycle.inspect(),/UID_CHANGED/);
});

test('044 resume: новые CP claims переживают потерянный ACK без повторного patch, старые claims закрыты',async()=>{
  const f=lifecycleFixture();await f.lifecycle.stopController();f.stopCP();f.faults.schema=true;
  f.faults.claimSessions=7;
  await assert.rejects(()=>f.lifecycle.handoff(),/OWNER_PRECONDITION_FAILED/);
  f.faults.claimSessions=0;await f.lifecycle.handoff();
  f.faults.claimSessions=7;
  await assert.rejects(()=>f.lifecycle.resume(),/OWNER_PRECONDITION_FAILED/);
  f.faults.claimSessions=0;f.faults.lostACK='control-plane';
  await assert.rejects(()=>f.lifecycle.resume(),/RESPONSE_LOST/);
  const writes=f.patches.filter(n=>n==='control-plane').length;
  f.faults.claimSessions=7;f.lifecycle.inspect();
  // Spec/image без readiness и durable intent не разрешают живые claims.
  const cp=f.apps.get('control-plane');cp.status.readyReplicas=0;
  await assert.rejects(()=>f.lifecycle.resume(),/OWNER_PRECONDITION_FAILED/);
  cp.status.readyReplicas=3;
  const intent=f.events.find(e=>e.type==='RESUME_INTENT'&&e.name==='control-plane');
  intent.type='UNCONFIRMED';await assert.rejects(()=>f.lifecycle.resume(),/OWNER_PRECONDITION_FAILED/);intent.type='RESUME_INTENT';
  assert.equal((await f.lifecycle.resume()).status,'RESUMED');
  assert.equal(f.patches.filter(n=>n==='control-plane').length,writes);
  const allWrites=f.patches.length;assert.equal((await f.lifecycle.resume()).status,'RESUMED');assert.equal(f.patches.length,allWrites);
  f.faults.activeEffects=1;await assert.rejects(()=>f.lifecycle.resume(),/OWNER_PRECONDITION_FAILED/);
});

test('044 ready: лишний debug/sidecar и изменённые команды Pod не подтверждают нового writer',async()=>{
  const f=lifecycleFixture();await f.lifecycle.stopController();f.stopCP();f.faults.schema=true;await f.lifecycle.handoff();
  f.faults.lostACK='control-plane';await assert.rejects(()=>f.lifecycle.resume(),/RESPONSE_LOST/);
  f.faults.claimSessions=7;
  const oldImage=f.plan.componentHandoff.components[0].beforeSpec.template.spec.containers[0].image;
  const extra={name:'unapproved-writer',image:oldImage};
  for(const mutate of [
    p=>{p.spec.ephemeralContainers=[extra];p.status.ephemeralContainerStatuses=[{name:extra.name,imageID:oldImage,ready:true,state:{running:{}}}];},
    p=>{p.spec.containers.push(extra);p.status.containerStatuses.push({name:extra.name,imageID:oldImage,ready:true,state:{running:{}}});},
    p=>{p.spec.initContainers=[{...extra,restartPolicy:'Always'}];p.status.initContainerStatuses=[{name:extra.name,imageID:oldImage,ready:true,state:{running:{}}}];},
    p=>{p.spec.containers[0].command=['/unapproved-writer'];},
    p=>{p.status.containerStatuses.push(structuredClone(p.status.containerStatuses[0]));},
    p=>{p.status.containerStatuses[0].imageID='unapproved-prefix'+p.status.containerStatuses[0].imageID.split('@')[1];},
  ]) {
    f.faults.mutatePod=mutate;
    assert.equal(f.lifecycle.inspect().find(s=>s.name==='control-plane').ready,false);
    await assert.rejects(()=>f.lifecycle.resume(),/OWNER_PRECONDITION_FAILED/);
  }
  f.faults.mutatePod=undefined;assert.equal((await f.lifecycle.resume()).status,'RESUMED');
});

test('public CLI044/045/046 normal/backup запрещает прежний путь без полного cohort до любой mutation',t=>{
  const directory=mkdtempSync(join(tmpdir(),'kodex-cohort-cli-'));chmodSync(directory,0o700);
  t.after(()=>rmSync(directory,{recursive:true,force:true}));
  const log=join(directory,'commands.jsonl'),binary=join(directory,'kubectl');
  writeFileSync(binary,`#!${process.execPath}\nconst fs=require('node:fs');const args=process.argv.slice(2);fs.appendFileSync(${JSON.stringify(log)},JSON.stringify(args)+'\\n');if(args.includes('get')&&args.includes('kube-system'))process.stdout.write(JSON.stringify({metadata:{uid:${JSON.stringify(uuid(1))}}}));else process.exit(73);\n`,{mode:0o700});
  for(const profile of ['github-repository-scope','github-repository-scope-backup','sentry-observability','sentry-observability-backup','telegram-notifications','telegram-notifications-backup']) {
    const result=spawnSync(process.execPath,[new URL('./control-plane-migration.mjs',import.meta.url).pathname,'plan',
      '--profile',profile,'--context','portable-fixture','--kubeconfig','/synthetic/kubeconfig','--source',directory,'--revision',revision,
      '--plan',join(directory,'plan.json'),'--base-render',join(directory,'render.yaml'),
      '--publication-plan',join(directory,'legacy.json'),'--publication-evidence',join(directory,'legacy.jsonl'),
      ...(profile.endsWith('-backup')?['--backup-maintenance-plan',join(directory,'backup.json'),'--backup-maintenance-evidence',join(directory,'backup.jsonl')]:[])],
    {encoding:'utf8',timeout:10000,env:{PATH:directory+':'+dirname(process.execPath),HOME:directory}});
    assert.equal(result.status,1);assert.match(result.stderr,/COMPONENT_MIGRATION_COHORT_REQUIRED/);
  }
  const calls=readFileSync(log,'utf8').trim().split('\n').map(line=>JSON.parse(line));
  assert.equal(calls.length,6);
  for(const args of calls)assert.deepEqual(args,['--kubeconfig','/synthetic/kubeconfig','--context','portable-fixture','--request-timeout=30s','get','namespace','kube-system','-o','json']);
});

for(const profile of ['sentry-observability','sentry-observability-backup'])test(`045 lifecycle: exact cohort, owner drain, lost ACK rejoin and maintenance API: ${profile}`,async()=>{
 const publication=fixture(profile);
 assert.equal(componentMigrationPublication(publication.value,{revision},profile).components.length,5);
 assert.ok(componentMigrationTarget(publication.target,publication.value,publication.observed));
 assert.equal(publication.value.schemaHeadsBefore['control-plane'],20261114120000);
 assert.equal(publication.value.schemaHeadsAfter['control-plane'],20261114130000);
 const other=profile.endsWith('-backup')?'sentry-observability':'sentry-observability-backup';
 assert.throws(()=>componentMigrationPublication(publication.value,{revision},other));
 const blocked=lifecycleFixture(profile);blocked.faults.blocked='integration-gateway';
 await assert.rejects(()=>blocked.lifecycle.stopController(),/CONTROLLER_STOP_TIMEOUT/);
 assert.throws(()=>blocked.lifecycle.requireStopped(),/CONTROLLER_JOIN_REQUIRED/);
 const f=lifecycleFixture(profile);await f.lifecycle.stopController();f.stopCP();
 await assert.rejects(()=>f.lifecycle.handoff(),/POSTCONDITION_REQUIRED/);
 f.faults.schema=true;f.faults.postcondition=false;
 await assert.rejects(()=>f.lifecycle.handoff(),/POSTCONDITION_REQUIRED/);
 f.faults.postcondition=true;f.faults.activeEffects=1;
 await assert.rejects(()=>f.lifecycle.handoff(),/OWNER_PRECONDITION_FAILED/);
 f.faults.activeEffects=0;f.faults.lostACK='integration-gateway';
 await assert.rejects(()=>f.lifecycle.handoff(),/RESPONSE_LOST/);
 const gatewayWrites=f.patches.filter(n=>n==='integration-gateway').length;
 f.lifecycle.inspect();await f.lifecycle.handoff();
 assert.equal(f.patches.filter(n=>n==='integration-gateway').length,gatewayWrites);
 f.faults.claimSessions=1;await assert.rejects(()=>f.lifecycle.resume(),/OWNER_PRECONDITION_FAILED/);
 f.faults.claimSessions=0;f.faults.lostACK='control-plane';
 await assert.rejects(()=>f.lifecycle.resume(),/RESPONSE_LOST/);
 const writes=f.patches.length,cpWrites=f.patches.filter(n=>n==='control-plane').length;f.lifecycle.inspect();
 assert.equal((await f.lifecycle.resume()).gatewayMaintenance,true);
 assert.equal(f.patches.filter(n=>n==='control-plane').length,cpWrites);
 assert.equal(f.apps.get('control-api-gateway').spec.replicas,0);
 const completed=f.patches.length;await f.lifecycle.resume();assert.equal(f.patches.length,completed);
 assert.ok(completed>=writes);
 f.plan.componentDrainSQLSHA256='0'.repeat(64);
 await assert.rejects(()=>f.lifecycle.resume(),/PRECONDITION_SOURCE_CHANGED/);
});

// Backup-публикация сохраняет собственный discriminator; normal receipt не переименовывается при plan.
test('044 backup: producer profile, полный cohort и handoff имеют одинаковый discriminator',()=>{
 const f=fixture('github-repository-scope-backup');
 assert.equal(componentMigrationPublication(f.value,{revision},'github-repository-scope-backup').components.length,5);
 assert.ok(componentMigrationTarget(f.target,f.value,f.observed));
 assert.throws(()=>componentMigrationPublication(f.value,{revision},'github-repository-scope'));
 const normal=fixture();assert.throws(()=>componentMigrationPublication(normal.value,{revision},'github-repository-scope-backup'));
});

test('046 handoff сохраняет назначенный UID/spec dormant0, schema resume не запускает нового issuer',async()=>{
 for(const profile of ['telegram-notifications','telegram-notifications-backup']){
  const f=lifecycleFixture(profile);await f.lifecycle.stopController();f.lifecycle.requireStopped();f.stopCP();f.faults.schema=true;
  const introduced=f.plan.componentHandoff.components.find(c=>c.name==='interaction-gateway');
  assert.equal(introduced.introduced,true);assert.equal(introduced.replicas,3);assert.equal(introduced.beforeSpec.replicas,0);
  await f.lifecycle.handoff();assert.deepEqual(f.apps.get('interaction-gateway').spec,introduced.beforeSpec);
  assert.equal(f.patches.filter(name=>name==='interaction-gateway').length,0);
  const result=await f.lifecycle.resume();assert.equal(result.status,'STAGED_PENDING_AUTHORITY');assert.equal(result.pairReady,false);
  assert.equal(f.apps.get('interaction-gateway').spec.replicas,0);assert.equal(f.apps.get('control-api-gateway').spec.replicas,0);
  assert.equal(f.apps.get('control-plane').spec.replicas,0);assert.equal(f.apps.get('egress-gateway').spec.replicas,0);
  const writes=f.patches.length;assert.equal((await f.lifecycle.resume()).status,'STAGED_PENDING_AUTHORITY');assert.equal(f.patches.length,writes);
  f.projectCP();await f.lifecycle.resume();assert.equal(f.apps.get('control-plane').spec.replicas,3);assert.equal(f.apps.get('egress-gateway').spec.replicas,2);
 }
 const f=fixture('telegram-notifications');f.deployments.at(-1).metadata.uid=uuid(999);
 assert.throws(()=>planComponentMigrationHandoff(f.value,f.deployments,{revision}),/INTRODUCTION_HANDOFF_CHANGED/);
 const g=fixture('telegram-notifications');g.deployments.at(-1).spec.template.spec.containers[0].env.push({name:'FOREIGN',value:'changed'});
 assert.throws(()=>planComponentMigrationHandoff(g.value,g.deployments,{revision}),/INTRODUCTION_HANDOFF_CHANGED/);
});

test('046 activation требует fresh107 DELIVER, lostACK rejoin не повторяет start и не открывает API',async()=>{
 const f=lifecycleFixture('telegram-notifications');await f.lifecycle.stopController();f.stopCP();f.faults.schema=true;await f.lifecycle.handoff();f.projectCP();await f.lifecycle.resume();f.faults.authorityDelivery=null;
 const writes=f.patches.length;await assert.rejects(()=>f.lifecycle.activateIntroduced(),/COHORT_READERS_REQUIRED/);assert.equal(f.patches.length,writes);
 const api=f.apps.get('control-api-gateway');api.spec.replicas=3;api.metadata.generation++;api.status={observedGeneration:api.metadata.generation,replicas:3,updatedReplicas:3,readyReplicas:3,availableReplicas:3};
 await assert.rejects(()=>f.lifecycle.activateIntroduced(),/API_READERS_REQUIRED/);assert.equal(f.patches.length,writes);
 f.faults.apiReaders={state:'ISOLATED_READERS_READY',sourceSHA:revision,deploymentUID:api.metadata.uid,spec:structuredClone(api.spec),operationID:uuid(200)};
 await assert.rejects(()=>f.lifecycle.activateIntroduced(),/DELIVERY_REQUIRED/);assert.equal(f.patches.length,writes);
 f.faults.activeInteractionDeliveries=1;await assert.rejects(()=>f.lifecycle.activateIntroduced(),/OWNER_PRECONDITION_FAILED/);assert.equal(f.patches.length,writes);f.faults.activeInteractionDeliveries=0;
 const target=f.plan.portableTarget,body={state:'PENDING_AUTHORITY',policyRevision:107,...telegramAuthorityReadbackContract(),sourceSHA:revision,
  introducedReceiverSHA256:fingerprint(f.plan.imageEvidence.introducedReceiver),clusterUID:target.clusterUID,namespaceUID:target.namespaceUID,
  installationUID:target.installationUID,installationSHA256:target.installationSHA256,observedAt:new Date().toISOString(),operationID:uuid(200),snapshotRevision:5};
 const sign=value=>({...value,deliverySHA256:fingerprint(value)});
 for(const mutate of [p=>p.policyRevision=106,p=>p.expectedReadbackCount=16,p=>p.sourceSHA='f'.repeat(40),p=>p.introducedReceiverSHA256='f'.repeat(64),
  p=>p.installationUID=uuid(999),p=>p.observedAt=new Date(Date.now()-30001).toISOString()]){
  const altered=structuredClone(body);mutate(altered);f.faults.authorityDelivery=sign(altered);
  await assert.rejects(()=>f.lifecycle.activateIntroduced(),/DELIVERY_REQUIRED/);assert.equal(f.patches.length,writes);
 }
 f.faults.authorityDelivery=sign(body);f.faults.lostACK='interaction-gateway';await assert.rejects(()=>f.lifecycle.activateIntroduced(),/RESPONSE_LOST/);
 const started=f.patches.length;assert.equal(started,writes+1);assert.equal(f.apps.get('interaction-gateway').spec.replicas,3);
 // Восстановление уже выполненного start читает exact owned spec/CRI и intent;
 // оно не выдаёт новый effect и не требует заново возвращать ротацию в DISTRIBUTE.
 f.faults.authorityDelivery=null;f.lifecycle.inspect();const result=await f.lifecycle.activateIntroduced();
 assert.equal(result.status,'READERS_STARTED_PENDING_RETIRE');assert.equal(f.patches.length,started);assert.equal(f.apps.get('control-api-gateway').spec.replicas,3);assert.equal(result.apiReadersIsolated,true);
 assert.equal((await f.lifecycle.resume()).status,'PENDING_AUTHORITY');assert.equal(f.patches.length,started);
 f.faults.apiReaders.state='RESTORED_READERS_READY';f.faults.activeEffects=1;f.faults.activeInteractionDeliveries=1;
 const terminal=await f.lifecycle.resume();assert.equal(terminal.status,'RESUMED');assert.equal(terminal.serviceRestored,true);assert.equal(f.patches.length,started);
 f.faults.apiReaders.state='ISOLATED_READERS_READY';f.faults.activeEffects=0;f.faults.activeInteractionDeliveries=0;
 const intent=f.events.find(e=>e.type==='PAIR_ACTIVATE_INTENT');intent.type='FOREIGN';
 await assert.rejects(()=>f.lifecycle.activateIntroduced(),/UNOWNED_HANDOFF/);
});

test('046 CP не запускается со106projection; full Pod readiness остаётся обязательной после107pointer',async()=>{
 const f=lifecycleFixture('telegram-notifications');await f.lifecycle.stopController();f.stopCP();f.faults.schema=true;await f.lifecycle.handoff();
 const initial=f.patches.length;assert.equal((await f.lifecycle.resume()).status,'STAGED_PENDING_AUTHORITY');assert.equal(f.patches.length,initial);
 f.projectCP();const valid=f.faults.authorityDelivery;f.faults.authorityDelivery={...valid,sourceSHA:'f'.repeat(40)};
 await assert.rejects(()=>f.lifecycle.resume(),/AUTHORITY_DELIVERY_REQUIRED/);assert.equal(f.apps.get('control-plane').spec.replicas,0);assert.equal(f.patches.length,initial);
 f.faults.authorityDelivery=valid;f.faults.policyReady=false;
 await assert.rejects(()=>f.lifecycle.resume(),/RESUME_TIMEOUT/);assert.equal(f.apps.get('control-plane').spec.replicas,3);
 assert.equal(f.apps.get('staff-control-center').spec.replicas,0);assert.equal(f.apps.get('egress-gateway').spec.replicas,0);
 const started=f.patches.length;f.faults.policyReady=true;f.lifecycle.inspect();assert.equal((await f.lifecycle.resume()).status,'PENDING_AUTHORITY');
 assert.equal(f.patches.filter(name=>name==='control-plane').length,2);assert.equal(f.patches.length,started+2);
});


test('046 DELIVER TTL проверяется после dry-run прямо перед start intent; expiry не оставляет writer intent',async()=>{
 const f=lifecycleFixture('telegram-notifications');await f.lifecycle.stopController();f.stopCP();f.faults.schema=true;await f.lifecycle.handoff();f.projectCP();
 const now=Date.now,before=f.patches.length;f.faults.beforePatch=name=>{if(name==='control-plane')Date.now=()=>now()+40000;};
 try{await assert.rejects(()=>f.lifecycle.resume(),/AUTHORITY_DELIVERY_REQUIRED/);}finally{Date.now=now;}
 assert.equal(f.apps.get('control-plane').spec.replicas,0);assert.equal(f.patches.length,before);
 assert.equal(f.events.some(row=>row.type==='RESUME_INTENT'),false);
 f.faults.beforePatch=undefined;assert.equal((await f.lifecycle.resume()).status,'PENDING_AUTHORITY');
});
