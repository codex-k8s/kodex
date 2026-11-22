import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync,spawnSync} from 'node:child_process';
import {readFileSync,writeFileSync,mkdtempSync,mkdirSync,rmSync,existsSync,copyFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,dirname} from 'node:path';
import {fileURLToPath} from 'node:url';
import {componentCohortFixture} from './fixtures/component-cohort-synthetic.mjs';
import {componentMigrationConfiguration,componentMigrationPublication,componentMigrationTarget,
  planComponentMigrationHandoff,componentMigrationLifecycle,componentServingInventory} from './portable-component-migration.mjs';
import {immutableMigrationProfile,planImmutableControlPlaneMigration,runImmutableControlPlaneMigration,
  validPersonalIntegrationsMigrationPrecondition,validImmutableMigrationPostcondition} from './control-plane-immutable-migration.mjs';
import {fingerprint} from './scoped-release.mjs';
import {backupMigrationProfile,requiresBackupMaintenance} from './backup-migration-registry.mjs';
import {isRoutineMigrationAPIRestoreProfile,readOpenedMigrationAPI,verifyMigrationAPIAdmission} from './component-migration-api-admission.mjs';
import {backupControllerMaintenanceStep} from './backup-controller-maintenance.mjs';
import {isPersonalIntegrationsMigrationProfile,requirePersonalIntegrationsDeliveryPrerequisite,
  personalIntegrationsDeliveryPrerequisite,verifyPersonalIntegrationsMigrationSources,
  validPersonalIntegrationsMigrationProof} from './personal-integrations-migration-contract.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url)),revision='a'.repeat(40);
const manifest=JSON.parse(readFileSync(new URL('./personal-integrations-schema-manifest.json',import.meta.url)));
const components=['control-plane','runtime-controller','integration-gateway','control-api-gateway',
  'staff-control-center','secret-broker','email-bridge','egress-gateway','artifact-retention'];
const profiles=['personal-integrations','personal-integrations-backup'];
const blocked=/PERSONAL_INTEGRATIONS_DELIVERY_PHASE_REJECTED/;

// Копируются actual capture metadata aaf, а не придуманные catalog fingerprints.
function proof(backup,before) {
  const value={profile:'PERSONAL_INTEGRATIONS_'+(backup?'BACKUP_':'')+(before?'PREDECESSOR_':'')+'V1',
    database:'control_plane',readOnly:'on',gooseVersion:before?20261114140000:20261114170000,
    serverVersionNum:180003,historyMatches:true,schemaMatches:true,publicSequenceSafe:true,
    backupQualification:backup?'CANONICAL_BACKUP':'NONE',
    schemaCatalog:structuredClone(manifest.snapshots[backup?'backup':'normal'][before?'20261114140000':'20261114170000'].catalog)};
  if(before)Object.assign(value,{workspaceOwnerIdle:true,oldClaimSessions:0,...Object.fromEntries([
    'activeRuntimeLeases','activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases','openBuilds',
    'pendingAdmissions','pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks',
    'activeIntegrationInvocations','activeIntegrationConnectionTests','activePersonalCredentialTests',
    'activeRepositoryBundles','activeConfigurationSources','activeConfigurationWritebacks',
    'activeInteractionDeliveries','activeBackupSessions'].map(key=>[key,0]))});
  return value;
}

for(const profile of profiles)test(`046→049: точные девять image pins и один конечный handoff ${profile}`,()=>{
  const f=componentCohortFixture(profile),config=immutableMigrationProfile(profile);
  assert.deepEqual(componentMigrationConfiguration(profile).components,components);
  assert.equal(config.previousVersion,20261114140000);assert.equal(config.version,'20261114170000');
  assert.equal(config.requiredMigrations.length,3);
  assert.equal(config.executionCommand,profile.endsWith('-backup')?'up-personal-integrations-backup-maintenance':'up-personal-integrations');
  assert.equal(componentMigrationPublication(f.value,{revision},profile).components.length,9);
  assert.ok(componentMigrationTarget(f.target,f.value,f.observed));
  const before=structuredClone(f.deployments),handoff=planComponentMigrationHandoff(f.value,f.deployments,{revision});
  assert.deepEqual(f.deployments,before);
  for(const part of handoff.components){
    assert.equal(part.stoppedSpec.replicas,0);assert.equal(part.stagedSpec.replicas,0);
    const old=structuredClone(part.beforeSpec.template.spec),next=structuredClone(part.stagedSpec.template.spec);
    next.containers[0].image=old.containers[0].image;assert.deepEqual(next,old);
  }
  assert.equal(handoff.components.find(part=>part.name==='control-api-gateway').resumedSpec.replicas,0);
  for(const change of [v=>v.components.pop(),v=>v.components.reverse(),v=>v.schemaHeadsAfter['control-plane']=20261114160000,
    v=>v.schemaHeadsBefore['control-plane']=20261114150000,v=>v.components[5].nodeProof.nodes[0].repoDigestVerified=false]){
    const changed=structuredClone(f.value);change(changed);assert.throws(()=>componentMigrationPublication(changed,{revision},profile));
  }
  const foreign=structuredClone(f.target);foreign.resources[5].uid='10000000-0000-4000-8000-999999999999';
  assert.throws(()=>componentMigrationTarget(foreign,f.value,f.observed));
});

test('план использует actual migrator boundary и закрытые cohort CLI, без host source',()=>{
  for(const profile of profiles){
    const f=componentCohortFixture(profile);
    const before=JSON.parse(execFileSync('yq',['-o=json','.',join(root,'deploy/k8s/base/control-plane/migration-job.yaml')],{encoding:'utf8'}));
    Object.assign(before.metadata,{uid:'synthetic-anchor',resourceVersion:'7',namespace:'kodex-system'});
    Object.assign(before.metadata.labels,{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'production','kodex.dev/profile':'web-only'});
    const plan=planImmutableControlPlaneMigration(before,{path:'/synthetic/source',revision},()=>({revision}),'synthetic',f.value,1,profile);
    assert.deepEqual(plan.job.spec.template.spec.containers[0].args,[immutableMigrationProfile(profile).executionCommand]);
    assert.deepEqual(plan.checkJob.spec.template.spec.containers[0].args,['check']);
    assert.equal(plan.migrationVersion,'20261114170000');assert.equal(plan.job.spec.backoffLimit,0);
    assert.equal(plan.job.spec.template.spec.volumes.some(value=>value.hostPath),false);
    const job=structuredClone(plan.job);job.metadata.uid='synthetic-job';
    job.status={succeeded:1,conditions:[{type:'Complete',status:'True'}]};
    const terminated=items=>items.map(item=>({name:item.name,restartCount:0,state:{terminated:{exitCode:0}}}));
    const pod={kind:'Pod',metadata:{uid:'synthetic-pod',namespace:'kodex-system',labels:job.metadata.labels,
      ownerReferences:[{kind:'Job',name:job.metadata.name,uid:job.metadata.uid,controller:true}]},
      spec:structuredClone(job.spec.template.spec),status:{phase:'Succeeded',
        initContainerStatuses:terminated(job.spec.template.spec.initContainers),containerStatuses:terminated(job.spec.template.spec.containers)}};
    assert.equal(componentServingInventory({items:[job,pod]},'control-plane','synthetic-deployment').items.length,0);
    const wrong=structuredClone(job);wrong.metadata.name='control-plane-migrate-20261114160000';
    assert.equal(componentServingInventory({items:[wrong,pod]},'control-plane','synthetic-deployment').items.length,1);

  }
});

test('normal и canonical backup требуют exact captured catalog/history; table-only и промежуточный head закрыты',()=>{
  for(const backup of [false,true]){
    const profile=profiles[Number(backup)],before=proof(backup,true),after=proof(backup,false);
    assert.equal(validPersonalIntegrationsMigrationProof(before,backup,true),true);
    assert.equal(validImmutableMigrationPostcondition(profile,after),true);
    for(const change of [p=>p.schemaCatalog.catalogDigest='0'.repeat(64),p=>p.schemaCatalog.tableCount++,p=>p.historyMatches=false,
      p=>p.schemaMatches=false,p=>p.serverVersionNum=180002,p=>p.readOnly='off',p=>p.gooseVersion=20261114160000,
      p=>p.backupQualification=backup?'TABLE_ONLY_BACKUP':'CANONICAL_BACKUP']){
      const value=structuredClone(after);change(value);assert.equal(validImmutableMigrationPostcondition(profile,value),false);
    }
    for(const key of Object.keys(before).filter(key=>key.startsWith('active')||key==='oldClaimSessions'))
      assert.equal(validPersonalIntegrationsMigrationProof({...before,[key]:1},backup,true),false,key);
    assert.equal(validPersonalIntegrationsMigrationProof({...before,oldClaimSessions:1},backup,true,false),true);
    assert.equal(validPersonalIntegrationsMigrationPrecondition(proof(true,true)),false);
  }
  const backup=backupMigrationProfile('personal-integrations-backup');
  assert.equal(requiresBackupMaintenance('personal-integrations'),false);
  assert.equal(requiresBackupMaintenance('personal-integrations-backup'),true);
  assert.equal(backup.receipt,'Personal integrations backup migration completed at version 20261114170000');
  assert.deepEqual(backup.catalog.backup.before,proof(true,true).schemaCatalog);
  assert.deepEqual(backup.catalog.backup.after,proof(true,false).schemaCatalog);
  for(const name of ['personal-integrations-table-only-backup','constructor','__proto__'])assert.throws(()=>immutableMigrationProfile(name));
});

test('миграционные bytes и proof readers совпадают с действительным baseline manifest',()=>{
  const result=verifyPersonalIntegrationsMigrationSources({path:root});
  assert.equal(result.captureIndexSHA256,manifest.captureIndexSHA256);
  assert.equal(result.captureSourceSHA,manifest.sourceSHA);
});

test('cohort допускает только фазы signed owner protocol и сохраняет запрет старого API reader',async()=>{
  assert.equal(personalIntegrationsDeliveryPrerequisite.protocol,'PHASE_BOUND_OWNER_READBACK_V1');
  for(const profile of profiles){
    assert.equal(isRoutineMigrationAPIRestoreProfile(profile),true);
    for(const phase of ['plan','inspect','check','stop','stopController','apply','retry','handoff','start-readers','resume','restore-api-traffic'])
      assert.doesNotThrow(()=>requirePersonalIntegrationsDeliveryPrerequisite(profile,phase));
    for(const phase of ['activate','plan-observability-repair','apply-egress-revision-repair','force'])
      await assert.rejects(runImmutableControlPlaneMigration(phase,{'--profile':profile},{}),blocked);
    assert.throws(()=>readOpenedMigrationAPI({profile},{},()=>assert.fail('unexpected API read')),blocked);
    const f=componentCohortFixture(profile),plan={imageEvidence:f.value,source:{revision},
      componentHandoff:planComponentMigrationHandoff(f.value,f.deployments,{revision})};
    const lifecycle=componentMigrationLifecycle({plan,journal:{events:[]}});
    for(const phase of ['startReaders','resume'])await assert.rejects(lifecycle[phase](),/PERSONAL_INTEGRATIONS_AUTHORITY_READER_REQUIRED/);
  }
  await assert.rejects(backupControllerMaintenanceStep({phase:'apply',plan:{migrationProfile:profiles[0]}}),blocked);
  assert.doesNotThrow(()=>requirePersonalIntegrationsDeliveryPrerequisite(profiles[1],'backup-maintenance'));
  for(const profile of ['sentry-observability','telegram-notifications']){
    assert.equal(isPersonalIntegrationsMigrationProfile(profile),false);
    assert.doesNotThrow(()=>requirePersonalIntegrationsDeliveryPrerequisite(profile,'apply'));
  }
});

test('cohort отклоняет изменение протокола в сохранённом plan до journal и owner transport',async()=>{
  for(const profile of profiles)for(const phase of ['check','stop','apply','retry','handoff','start-readers','resume']){
    for(const deliveryPrerequisite of [undefined,{...personalIntegrationsDeliveryPrerequisite,authorityPolicyRevision:107}])
      await assert.rejects(runImmutableControlPlaneMigration(phase,{'--profile':profile},{
        privateJSON:()=>({deliveryPrerequisite}),kube:()=>assert.fail('unexpected API read'),
      }),/PERSONAL_INTEGRATIONS_DELIVERY_PROTOCOL_CHANGED/);
  }
});

test('actual CLI отклоняет чужие flags API и ремонт Telegram до transport и journal',t=>{
  const directory=mkdtempSync(join(tmpdir(),'kodex-personal-cohort-cli-'));
  t.after(()=>rmSync(directory,{recursive:true,force:true}));mkdirSync(join(directory,'bin'),{mode:0o700});
  const trace=join(directory,'trace'),fake=join(directory,'bin','kubectl');
  writeFileSync(fake,`#!${process.execPath}\nrequire('node:fs').appendFileSync(${JSON.stringify(trace)},'unexpected transport');process.exit(2);\n`,{mode:0o700});
  for(const profile of profiles)for(const phase of ['activate','apply-egress-revision-repair']){
    const result=spawnSync(process.execPath,[join(root,'tools/release/control-plane-migration.mjs'),phase,
      '--profile',profile,'--context','k3d-synthetic','--kubeconfig',join(directory,'synthetic-config'),
      '--plan',join(directory,'synthetic-plan.json'),'--evidence',join(directory,'synthetic-evidence.jsonl'),
      '--confirm','APPLY-STAGING-CP-MIGRATION'],{encoding:'utf8',timeout:10000,env:{PATH:join(directory,'bin')}});
    assert.notEqual(result.status,0);assert.match(result.stderr,/PHASE_ARGUMENT_INVALID|PERSONAL_INTEGRATIONS_DELIVERY_PHASE_REJECTED/);assert.equal(existsSync(trace),false);
    assert.equal(existsSync(join(directory,'synthetic-evidence.jsonl')),false);
  }
});

// Меняется только приватная schema fixture: настоящий captured baseline и DDL
// остаются неизменными, новый expected fingerprint не генерируется.
test('новый DDL/reader либо proof byte не наследует старый captured manifest',t=>{
  const directory=mkdtempSync(join(tmpdir(),'kodex-personal-cohort-source-'));
  t.after(()=>rmSync(directory,{recursive:true,force:true}));
  const files=['tools/release/personal-integrations-schema-manifest.json','tools/release/managed-kubernetes-catalog.sql',
    ...manifest.migrationPins.map(pin=>'services/internal/control-plane/cmd/cli/migrations/'+pin.file),
    ...['personal-integrations','personal-integrations-backup'].flatMap(mode=>['precondition','postcondition','recovery'].map(phase=>'tools/release/'+mode+'-migration-'+phase+'.sql'))];
  for(const file of files){mkdirSync(dirname(join(directory,file)),{recursive:true,mode:0o700});copyFileSync(join(root,file),join(directory,file));}
  assert.ok(verifyPersonalIntegrationsMigrationSources({path:directory}));
  for(const file of ['services/internal/control-plane/cmd/cli/migrations/20261114150000_managed_kubernetes.sql',
    'tools/release/managed-kubernetes-catalog.sql','tools/release/personal-integrations-migration-postcondition.sql']){
    const raw=readFileSync(join(directory,file));writeFileSync(join(directory,file),Buffer.concat([raw,Buffer.from('\n')]));
    assert.throws(()=>verifyPersonalIntegrationsMigrationSources({path:directory}),/PERSONAL_INTEGRATIONS_.*_CHANGED/);
    writeFileSync(join(directory,file),raw);
  }
});

// Это scope join уже проверенной API изоляции; он не является signed authority
// proof и не разрешает effect через закрытый delivery prerequisite.
test('API isolation metadata связывает exact профиль049 с source, target и Deployment без выдуманного DB proof',()=>{
 for(const profile of profiles){
  const f=componentCohortFixture(profile),api=f.deployments.find(value=>value.metadata.name==='control-api-gateway');
  const admission={schema:'kodex.personal-integrations-api-admission/v1',profile,sourceSHA:revision,deploymentUID:api.metadata.uid};
  const postgres={pod:'synthetic-postgres',uid:'synthetic-pod',ownerUID:'synthetic-owner'},postgresBackend={systemIdentifier:'1234567890'};
  const current={admission,isolation:{plan:{target:f.target,migrationRestore:{authority:{postgres,postgresBackend}}}}};
  const migration={version:2,kind:'CONTROL_PLANE_MIGRATION',profile,migrationVersion:'20261114170000',source:{revision},apiMaintenance:admission,portableTarget:f.target,postgres,postgresBackend};
  assert.equal(verifyMigrationAPIAdmission(migration,current),current);
  for(const change of [value=>value.migrationVersion='20261114130000',value=>value.migrationVersion='20261114160000',
    value=>value.portableTarget.clusterUID='foreign',value=>value.source.revision='b'.repeat(40),
    value=>value.apiMaintenance.schema='foreign',value=>value.portableTarget.resources.find(item=>item.name==='control-api-gateway').uid='foreign']){
   const changed=structuredClone(migration);change(changed);assert.throws(()=>verifyMigrationAPIAdmission(changed,current),/ADMISSION_CHANGED/);
  }
 }
});

test('base046 допускается только новым cohort и требует полного фактического installation seal',()=>{
 for(const profile of profiles){
  const f=componentCohortFixture(profile);f.value.baseSchemaHeads['control-plane']=20261114140000;
  assert.equal(componentMigrationPublication(f.value,{revision:'a'.repeat(40)},profile).components.length,9);
  assert.throws(()=>componentMigrationTarget(f.target,f.value,f.observed),/INSTALLATION_PINS_MISMATCH/);
  f.observed.installation.data['schema-heads.json']=JSON.stringify(f.value.baseSchemaHeads);
  assert.throws(()=>componentMigrationTarget(f.target,f.value,f.observed),/TARGET_REJECTED/);
  f.target.installationSHA256=fingerprint(f.observed.installation.data);
  assert.ok(componentMigrationTarget(f.target,f.value,f.observed));
  for(const change of [value=>delete value.installation.data['source.sha'],value=>value.installation.data['release-lock.sha256']='f'.repeat(64),
    value=>value.installation.data['profile.sha256']='f'.repeat(64),value=>value.installation.metadata.uid='foreign']){
   const observed=structuredClone(f.observed);change(observed);assert.throws(()=>componentMigrationTarget(f.target,f.value,observed));
  }
  const wrong=structuredClone(f.value);wrong.schemaHeadsBefore['control-plane']=20261114150000;
  assert.throws(()=>componentMigrationPublication(wrong,{revision:'a'.repeat(40)},profile),/SCHEMA_INVALID/);
 }
 for(const profile of ['github-repository-scope','sentry-observability','telegram-notifications']){
  const f=componentCohortFixture(profile);f.value.baseSchemaHeads['control-plane']=20261114140000;
  assert.throws(()=>componentMigrationPublication(f.value,{revision:'a'.repeat(40)},profile),/BASE_SCHEMA_INVALID/);
 }
});
