import test from 'node:test';
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
const roots={'control-plane':'services/internal/control-plane/','runtime-controller':'services/internal/runtime-controller/',
  'integration-gateway':'services/external/integration-gateway/','control-api-gateway':'services/external/control-api-gateway/',
  'staff-control-center':'services/staff/control-center/'};

// Пять настоящих форматов component-lock с синтетическими pins; ни кластер,
// ни registry, ни пользовательские credentials тест не открывает.
function fixture(migrationProfile='github-repository-scope') {
  const names=componentMigrationNames(migrationProfile);
  const baseHeads={'control-plane':20261112000100,'internal-rpc-authority':2,'email-bridge':3};
  const value={schema:'kodex.portable-component-migration/v2',profile:migrationProfile,context:'portable-fixture',
    clusterUID:uuid(1),namespaceUID:uuid(2),baseSourceSHA:'b'.repeat(40),baseReleaseLockSHA256:'c'.repeat(64),
    baseSchemaHeads:baseHeads,schemaHeadsBefore:{...baseHeads,'control-plane':20261113000100},
    schemaHeadsAfter:{...baseHeads,'control-plane':20261114120000},runtimeABI:5,runnerInput:'v10',
    registry:'registry.example.invalid',repositoryPrefix:'synthetic/upgrade',nodeUIDs:[uuid(3)],components:[]};
  for(const [index,component] of names.entries()) {
    const anchor=roots[component]+'internal/synthetic.go',changed=[anchor];
    const digest='sha256:'+String(index+1).repeat(64);
    const lock={schema:'kodex.component-image/v1',component,source_sha:revision,base_application_source_sha:value.baseSourceSHA,
      base_release_lock_sha256:value.baseReleaseLockSHA256,image_ref:`${value.registry}/${value.repositoryPrefix}/${component}@${digest}`,digest,
      production_file_sha256:'1'.repeat(64),schema_heads:value.schemaHeadsAfter,
      compatibility:{changed_files:changed,production_files:changed,shared_libs_contracts_unchanged:false,
        verified_cohort:{components:names,changed_files:changed,production_files:changed,anchor,
          contract_change:'GITHUB_REPOSITORY_SCOPE_UPGRADE',schema_change:true,runner_input_abi:'UNCHANGED'}},
      build:{dockerfile_sha256:'2'.repeat(64),metadata_sha256:'3'.repeat(64),private_source_sha:'4'.repeat(40),platform:'linux/amd64'},
      qualification:'PUBLISHED_NOT_NODE_QUALIFIED_NOT_DEPLOYED'};
    const part={component,expectedImageRef:`registry.example.invalid/base/${component}@sha256:${'f'.repeat(64)}`};
    replaceLock(part,lock);
    part.nodeProof={schema:'kodex.component-node-proof/v1',component,sourceSHA:revision,componentLockSHA256:part.componentLockSHA256,
      imageRef:lock.image_ref,clusterUID:value.clusterUID,namespaceUID:value.namespaceUID,
      nodes:[{name:'node-0',uid:uuid(3),imageRef:lock.image_ref,repoDigestVerified:true}]};
    value.components.push(part);
  }
  const deployments=names.map((name,index)=>{
    const replicas=name==='control-api-gateway'?0:name==='control-plane'?3:2;
    return {apiVersion:'apps/v1',kind:'Deployment',metadata:{name,namespace:'kodex-system',uid:uuid(10+index),resourceVersion:'1',generation:1,
      labels:{'app.kubernetes.io/name':name,'app.kubernetes.io/part-of':'kodex','kodex.dev/profile':'web-only','kodex.dev/environment':'production'}},
      spec:{replicas,template:{metadata:{annotations:{synthetic:'retained'}},spec:{containers:[{name,image:value.components[index].expectedImageRef,
        env:[{name:'SYNTHETIC',value:'unchanged'}]}]}}},status:{observedGeneration:1,replicas,updatedReplicas:replicas,readyReplicas:replicas,availableReplicas:replicas}};
  });
  const profile={version:1,identity:{mode:'bundled'},public_tls:{mode:'managed'}};
  const installation={apiVersion:'v1',kind:'ConfigMap',metadata:{name:'kodex-installation-profile',namespace:'kodex-system',uid:uuid(20),
    labels:{'app.kubernetes.io/part-of':'kodex','app.kubernetes.io/managed-by':'kodex-install'}},data:{'profile.json':JSON.stringify(profile),
    'profile.sha256':fingerprint(profile),'source.sha':value.baseSourceSHA,'release-lock.sha256':value.baseReleaseLockSHA256,'schema-heads.json':JSON.stringify(baseHeads)}};
  const target={schema:'kodex.portable-runner-target/v1',context:value.context,clusterUID:value.clusterUID,namespaceUID:value.namespaceUID,
    installationUID:installation.metadata.uid,installationSHA256:fingerprint(installation.data),
    resources:deployments.map(d=>({apiVersion:d.apiVersion,kind:d.kind,name:d.metadata.name,uid:d.metadata.uid}))};
  const observed={sourceRevision:revision,context:value.context,clusterUID:value.clusterUID,namespaceUID:value.namespaceUID,installation,deployments,
    nodes:[{metadata:{uid:uuid(3),name:'node-0'},status:{conditions:[{type:'Ready',status:'True'}]}}]};
  return {value,deployments,target,observed};
}

function replaceLock(part,lock) {
  const raw=Buffer.from(JSON.stringify(lock,null,2)+'\n');
  part.componentLockRawBase64=raw.toString('base64');
  part.componentLockSHA256=createHash('sha256').update(raw).digest('hex');
  if(part.nodeProof)part.nodeProof.componentLockSHA256=part.componentLockSHA256;
}

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
function lifecycleFixture() {
  const f=fixture(),handoff=planComponentMigrationHandoff(f.value,f.deployments,{revision});
  const drain=readFileSync(new URL('./portable-component-migration-drain.sql',import.meta.url));
  const postgres={pod:'kodex-postgresql-0',uid:uuid(30),ownerUID:uuid(31)};
  const plan={source:{revision},imageEvidence:f.value,componentHandoff:handoff,migrationVersion:'20261114120000',postgres,
    componentDrainSQLSHA256:createHash('sha256').update(drain).digest('hex')};
  const apps=new Map(f.deployments.map(d=>[d.metadata.name,structuredClone(d)]));
  const events=[],patches=[],faults={blocked:'',lostACK:'',schema:false,postcondition:true,activeEffects:0,claimSessions:0,mutatePod:undefined};
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
        containerStatuses:app.spec.template.spec.containers.map(c=>({name:c.name,ready:true,imageID:c.image,state:{running:{}}}))}})).map(p=>{
          if(name==='control-plane')faults.mutatePod?.(p);
          return p;
        })];
  });
  const kube=(args,input)=>{
    if(args.includes('exec')) {
      assert.ok(Buffer.from(input).equals(drain));
      return JSON.stringify({profile:'PORTABLE_COMPONENT_DRAIN_V1',database:'control_plane',readOnly:'on',gooseVersion:20261114120000,
        ...Object.fromEntries(['activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds','pendingAdmissions','pendingPromotions',
          'activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks','activeRuntimeLeases','oldClaimSessions','activeIntegrationInvocations',
          'activeIntegrationConnectionTests','activePersonalCredentialTests','activeRepositoryBundles','activeConfigurationSources','activeConfigurationWritebacks'].map(k=>[k,0])),
        activeIntegrationInvocations:faults.activeEffects,oldClaimSessions:faults.claimSessions});
    }
    if(args.includes('patch')) {
      const name=args[args.indexOf('deployment')+1],app=apps.get(name);
      const patch=JSON.parse(readFileSync(args.find(a=>a.startsWith('--patch-file=')).slice('--patch-file='.length)));
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
    readOwnerSQL:sql=>JSON.parse(kube(['exec'],sql)),
    postgresIdentity:()=>postgres,validateInputs:()=>{},activePods:activeDeploymentPods,
    wait:async(observe,ready,code)=>{if(!ready(observe()))throw Error(code);},clock:{}});
  return {lifecycle,apps,events,patches,faults,plan,
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

test('public CLI044 normal/backup запрещает прежний путь без полного cohort до любой mutation',t=>{
  const directory=mkdtempSync(join(tmpdir(),'kodex-cohort-cli-'));chmodSync(directory,0o700);
  t.after(()=>rmSync(directory,{recursive:true,force:true}));
  const log=join(directory,'commands.jsonl'),binary=join(directory,'kubectl');
  writeFileSync(binary,`#!${process.execPath}\nconst fs=require('node:fs');const args=process.argv.slice(2);fs.appendFileSync(${JSON.stringify(log)},JSON.stringify(args)+'\\n');if(args.includes('get')&&args.includes('kube-system'))process.stdout.write(JSON.stringify({metadata:{uid:${JSON.stringify(uuid(1))}}}));else process.exit(73);\n`,{mode:0o700});
  for(const profile of ['github-repository-scope','github-repository-scope-backup']) {
    const result=spawnSync(process.execPath,[new URL('./control-plane-migration.mjs',import.meta.url).pathname,'plan',
      '--profile',profile,'--context','portable-fixture','--kubeconfig','/synthetic/kubeconfig','--source',directory,'--revision',revision,
      '--plan',join(directory,'plan.json'),'--base-render',join(directory,'render.yaml'),
      '--publication-plan',join(directory,'legacy.json'),'--publication-evidence',join(directory,'legacy.jsonl'),
      ...(profile.endsWith('-backup')?['--backup-maintenance-plan',join(directory,'backup.json'),'--backup-maintenance-evidence',join(directory,'backup.jsonl')]:[])],
    {encoding:'utf8',timeout:10000,env:{PATH:directory+':'+dirname(process.execPath),HOME:directory}});
    assert.equal(result.status,1);assert.match(result.stderr,/COMPONENT_MIGRATION_COHORT_REQUIRED/);
  }
  const calls=readFileSync(log,'utf8').trim().split('\n').map(line=>JSON.parse(line));
  assert.equal(calls.length,2);
  for(const args of calls)assert.deepEqual(args,['--kubeconfig','/synthetic/kubeconfig','--context','portable-fixture','--request-timeout=30s','get','namespace','kube-system','-o','json']);
});

// Backup-публикация сохраняет собственный discriminator; normal receipt не переименовывается при plan.
test('044 backup: producer profile, полный cohort и handoff имеют одинаковый discriminator',()=>{
 const f=fixture('github-repository-scope-backup');
 assert.equal(componentMigrationPublication(f.value,{revision},'github-repository-scope-backup').components.length,5);
 assert.ok(componentMigrationTarget(f.target,f.value,f.observed));
 assert.throws(()=>componentMigrationPublication(f.value,{revision},'github-repository-scope'));
 const normal=fixture();assert.throws(()=>componentMigrationPublication(normal.value,{revision},'github-repository-scope-backup'));
});
