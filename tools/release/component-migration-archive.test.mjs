import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {existsSync,mkdtempSync,readFileSync,rmSync,writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import test from 'node:test';
import {fileURLToPath} from 'node:url';
import {readMigrationArchiveState,readMigrationArchiveContinuity,observeMigrationContinuity,
  verifyArchivedMigrationRestoration} from './component-migration-archive.mjs';
import {canonicalPostgresBackupSpec} from './backup-postgres-boundary.mjs';
import {syntheticMigrationBaseline, syntheticMigrationTerminal} from './fixtures/migration-api-terminal-synthetic.mjs';
import {fingerprint} from './scoped-release.mjs';
import {workflowFileProvenanceMigration} from './workflow-file-provenance-migration-contract.mjs';

const uid = '11111111-1111-4111-8111-111111111111';
const other = '22222222-2222-4222-8222-222222222222';
const source = 'a'.repeat(40);
const profile = workflowFileProvenanceMigration.profile;
const version = workflowFileProvenanceMigration.after;
const iso = seconds => new Date(Date.UTC(2024,0,1,0,0,seconds)).toISOString();

function reseal(value, key) {
  const {[key]:old, ...body} = value;
  return {...body,[key]:fingerprint(body)};
}

function fixture() {
  const target = {context:'synthetic',clusterUID:uid,namespaceUID:uid,installationUID:uid,installationSHA256:'f'.repeat(64)};
  const jobName = 'control-plane-migrate-' + version;
  const migration = {version:2,kind:'CONTROL_PLANE_MIGRATION',profile,migrationVersion:String(version),
    context:target.context,clusterUID:uid,namespaceUID:uid,portableTarget:target,source:{revision:source},
    job:{metadata:{name:jobName}},postconditionSQLSHA256:'b'.repeat(64),postgres:{uid,ownerUID:uid},
    postgresBackend:{database:'control_plane'},imageEvidence:{source},componentHandoff:{publicationSHA256:'c'.repeat(64)}};
  const history = [{type:'HEADER',kind:migration.kind,version:1,planSHA256:fingerprint(migration)},
    {type:'CREATE_INTENT',jobName},{type:'CREATE_ACK',jobUID:uid},
    {type:'READBACK',status:'PASS',schemaPostcondition:'PASS',
      jobName,jobUID:uid,sourceRevision:source,migrationVersion:String(version)},
    {type:'TERMINAL_READBACK',jobUID:uid}];
  const authority = reseal({...syntheticMigrationBaseline(target),policyRevision:110,observedAt:iso(0)},'baselineSHA256');
  const isolation = {target,serviceUID:uid,maintenanceID:other,migrationRestore:{profile,authority}};
  const closed = {kind:'synthetic'};
  const terminal = syntheticMigrationTerminal({isolation,closed,deployment:{metadata:{uid},spec:{}},sourceSHA:source});
  const planned = reseal({...terminal,jobName,podUID:other,migrationVersion:version,
    migrationPlanSHA256:fingerprint(migration),migrationEvidenceSHA256:fingerprint(history),
    migrationTargetSHA256:fingerprint(migration.portableTarget),publicationSHA256:fingerprint(migration.imageEvidence),
    handoffSHA256:fingerprint(migration.componentHandoff),
    sourceCompatibility:{...terminal.sourceCompatibility,postconditionSQLSHA256:migration.postconditionSQLSHA256},
    postcondition:{...terminal.postcondition,observedAt:iso(0),sqlSHA256:migration.postconditionSQLSHA256,
      postgresSHA256:fingerprint(migration.postgres),backendSHA256:fingerprint(migration.postgresBackend)}},
    'terminalSHA256');
  const restore = {action:'restore',schema:'kodex.component-api-traffic-plan/v1',migrationRestoreProfile:profile,
    migrationSourceSHA:source,sourceSHA:source,target,previousIsolationSHA256:fingerprint(isolation),closePlanSHA256:fingerprint(closed),
    maintenanceID:other,serviceUID:uid,authority:planned};
  const applied = reseal({...planned,observedAt:iso(12),postcondition:{...planned.postcondition,observedAt:iso(11)},
    authority:reseal({...authority,observedAt:iso(12)},'baselineSHA256')},'terminalSHA256');
  const events = [{type:'HEADER',schema:'kodex.component-api-traffic-evidence/v1',at:iso(5),
    planSHA256:fingerprint(restore),maintenanceID:other},
    {type:'MIGRATION_CONFIRMED',at:iso(13),plannedTerminalSHA256:planned.terminalSHA256,authority:applied},
    {type:'INTENT',step:'apply',at:iso(14)},
    {type:'OBSERVED',state:'RESTORED',at:iso(15),maintenanceID:other,serviceUID:uid}];
  return {migration,history,restore,events};
}

const verify = f => verifyArchivedMigrationRestoration(f.migration,f.history,f.restore,f.events);

// Синтетический transport исполняет настоящие guards Pod/backend/catalog.
// Archive terminal отдельно квалифицирован предыдущими тестами этого файла.
function continuityFixture() {
  const labels = {'app.kubernetes.io/part-of':'kodex','kodex.dev/profile':'web-only','kodex.dev/environment':'production'};
  const meta = name => ({name,namespace:'kodex-system',uid,resourceVersion:'10',labels:{...labels}});
  const installation = {apiVersion:'v1',kind:'ConfigMap',metadata:meta('kodex-installation-profile'),data:{
    'source.sha':'c'.repeat(40),'release-lock.sha256':'d'.repeat(64),'schema-heads.json':JSON.stringify({
      'control-plane':20261121000200,'internal-rpc-authority':20261115000200,'email-bridge':20260905000200})}};
  installation.metadata.labels['app.kubernetes.io/managed-by'] = 'kodex-install';
  const service = {apiVersion:'v1',kind:'Service',metadata:meta('control-api-gateway'),spec:{selector:{app:'current'}}};
  const deployment = {apiVersion:'apps/v1',kind:'Deployment',metadata:meta('control-api-gateway'),spec:{replicas:2,template:{spec:{containers:[]}}}};
  const target = {context:'synthetic',clusterUID:uid,namespaceUID:uid,installationUID:uid,installationSHA256:'f'.repeat(64),
    resources:[service,deployment].map(row=>({apiVersion:row.apiVersion,kind:row.kind,name:row.metadata.name,uid:row.metadata.uid}))};
  const sql = readFileSync(new URL('./workflow-file-provenance-backup-migration.sql',import.meta.url));
  const backend = {systemIdentifier:'9007199254740993',databaseOID:'20000',database:'control_plane',port:'5432',serverAddress:null,serverPort:null};
  const migration = {version:2,profile,context:'synthetic',
    portableTarget:{...target,resources:target.resources.filter(row=>row.kind==='Deployment')},source:{revision:source},
    postgres:{uid:other,ownerUID:uid},postgresBackend:structuredClone(backend),
    postconditionSQLSHA256:createHash('sha256').update(sql).digest('hex'),
    job:{metadata:{name:'control-plane-migrate-'+version},spec:{backoffLimit:0,activeDeadlineSeconds:300,template:{spec:{containers:[]}}}}};
  const job = {apiVersion:'batch/v1',kind:'Job',metadata:{...meta(migration.job.metadata.name),
    annotations:{'kodex.dev/cp-workflow-file-provenance-migration-plan':fingerprint(migration)}},spec:structuredClone(migration.job.spec),
    status:{succeeded:1,conditions:[{type:'Complete',status:'True'}]}};
  const pgSpec = canonicalPostgresBackupSpec();
  const postgres = {apiVersion:'v1',kind:'Pod',metadata:{...meta('kodex-postgresql-0'),uid:other,
    labels:{'app.kubernetes.io/name':'kodex-postgresql'},
    ownerReferences:[{apiVersion:'apps/v1',kind:'StatefulSet',name:'kodex-postgresql',uid,controller:true}]},
    spec:{...structuredClone(pgSpec),volumes:[...structuredClone(pgSpec.volumes),{name:'data',persistentVolumeClaim:{claimName:'data-kodex-postgresql-0'}}]},
    status:{phase:'Running',conditions:[{type:'Ready',status:'True'}],containerStatuses:[{
      name:'postgresql',ready:true,state:{running:{}},imageID:pgSpec.containers[0].image}]}};
  const set = {apiVersion:'apps/v1',kind:'StatefulSet',metadata:meta('kodex-postgresql'),spec:{template:{spec:pgSpec}}};
  const proof = {profile:'WORKFLOW_FILE_PROVENANCE_BACKUP_051',database:'control_plane',readOnly:'on',gooseVersion:20261121000200,
    serverVersionNum:180003,backupQualification:'CANONICAL_BACKUP',publicSequenceSafe:true,historyMatches:true,schemaMatches:true,
    artifactPredecessorApplied:true,schemaCatalog:{catalogDigest:'31d4205868c2e96b508f8c1ce3572a55477407580486053a23c16044227d80ad'}};
  const terminal = {jobName:job.metadata.name,jobUID:uid,deploymentUID:uid,apiSpecSHA256:fingerprint({replicas:1}),
    postcondition:{schemaCatalogSHA256:fingerprint(proof.schemaCatalog)}};
  const restore = {target,serviceUID:uid,afterSpec:{selector:{app:'before'}},afterAnnotations:{origin:'before'}};
  const resources = new Map([["namespace/kube-system",{apiVersion:'v1',kind:'Namespace',metadata:{name:'kube-system',uid}}],
    ['namespace/kodex-system',{apiVersion:'v1',kind:'Namespace',metadata:{name:'kodex-system',uid}}],
    ['configmap/kodex-installation-profile',installation],['service/control-api-gateway',service],
    ['deployment/control-api-gateway',deployment],['job/'+job.metadata.name,job],
    ['pod/kodex-postgresql-0',postgres],['statefulset/kodex-postgresql',set]]);
  const calls = [],faults = {};
  const kube = (args,input) => {
    calls.push({args,input});
    if (args.includes('exec')) {
      assert.ok(args.includes('--dbname=control_plane'));
      if (String(input).includes('pg_catalog.pg_control_system()')) return JSON.stringify(backend);
      assert.ok(Buffer.from(input).equals(sql));faults.afterSQL?.();return JSON.stringify(proof);
    }
    const index=args.indexOf('get'),key=args[index+1]==='-n'?args.slice(index+3,index+5).join('/'):args.slice(index+1,index+3).join('/');
    assert.ok(resources.has(key),key);return JSON.stringify(resources.get(key));
  };
  return {migration,restore,terminal,installation,service,deployment,job,postgres,backend,proof,resources,calls,faults,kube,
    read:()=>readMigrationArchiveContinuity(migration,restore,terminal,'synthetic',kube)};
}

test('архивная связка принимает два ранее проверенных terminal receipt для одного Job и Pod', () => {
  const f = fixture();
  assert.equal(verify(f).terminalSHA256,f.events[1].authority.terminalSHA256);
  assert.equal(verify(f).podUID,other);
});

test('архив отклоняет чужой scope, незавершённую цепочку, подмену журнала и устаревший на момент принятия proof', () => {
  for (const edit of [
    f => { f.migration.namespaceUID = other; },
    f => { f.events[3].state = 'BEFORE'; },
    f => { f.events[3].serviceUID = other; },
    f => { f.events[1].at = iso(59); f.events[2].at = iso(60); f.events[3].at = iso(61); },
    f => { f.events[1].authority = reseal({...f.events[1].authority,podUID:uid},'terminalSHA256'); },
    f => { f.history[2].jobUID = other; },
    f => { f.history[3].status = 'FAIL'; },
  ]) {
    const f = fixture();
    edit(f);
    assert.throws(() => verify(f), /MIGRATION_ARCHIVE_RESTORATION_REJECTED|CATALOG_BASELINE_PROOF_REJECTED/);
  }
});

test('сверка живого кластера отклоняет чужую установку до запроса PostgreSQL', () => {
  const f = fixture(), calls = [];
  const kube = args => {
    calls.push(args);
    return JSON.stringify({apiVersion:'v1',kind:'Namespace',metadata:{uid:other}});
  };
  assert.throws(() => readMigrationArchiveState(f.migration,f.restore,verify(f),'synthetic',kube),/PORTABLE_RUNNER_TARGET_REJECTED/);
  assert.deepEqual(calls.map(args => args[0]),['get','get','get']);
});

test('CLI сверяет хеш прочитанных байтов до обращения к kubeconfig или Kubernetes', t => {
  const directory = mkdtempSync(join(tmpdir(),'kodex-archive-test-'));
  t.after(() => rmSync(directory,{recursive:true,force:true}));
  const input = join(directory,'input.json');
  const marker = join(directory,'calls');
  writeFileSync(input,'{}\n',{mode:0o600});
  writeFileSync(join(directory,'kubectl'),'#!/bin/sh\nprintf invoked >"$test_marker"\n',{mode:0o700});
  const cli = fileURLToPath(new URL('./component-migration-archive.mjs',import.meta.url));
  const result = spawnSync(process.execPath,[cli,'synthetic','/synthetic/not-opened',
    ...Array.from({length:4},() => [input,'a'.repeat(64)]).flat()],{
    env:{PATH:directory,test_marker:marker},encoding:'utf8',timeout:5000,
  });
  assert.equal(result.status,1);
  assert.match(result.stderr,/^MIGRATION_ARCHIVE_INPUT_PIN_MISMATCH\s*$/);
  assert.equal(existsSync(marker),false);
});

test('continuity наблюдает новый installation/CAG spec прежних UID без ослабления strict archive', () => {
  const f=continuityFixture(),before=JSON.stringify([f.migration,f.restore,f.terminal]);
  const value=f.read();
  assert.equal(value.installationSHA256,fingerprint(f.installation.data));
  assert.equal(value.serviceSpecSHA256,fingerprint(f.service.spec));
  assert.equal(value.deploymentSpecSHA256,fingerprint(f.deployment.spec));
  assert.equal(value.beforeApiDeploymentSpecSHA256,f.terminal.apiSpecSHA256);
  assert.equal(value.beforeApiServiceSpecSHA256,fingerprint(f.restore.afterSpec));
  assert.equal(value.beforeApiServiceAnnotationsSHA256,fingerprint(f.restore.afterAnnotations));
  assert.equal(value.postgresPodUID,other);
  assert.equal(JSON.stringify([f.migration,f.restore,f.terminal]),before);
  assert.throws(()=>readMigrationArchiveState(f.migration,f.restore,f.terminal,'synthetic',f.kube),/PORTABLE_RUNNER_TARGET_REJECTED/);
  assert.ok(f.calls.every(row=>row.args.includes('get')||row.args.includes('exec')));
});

test('continuity закрывает замену cluster/namespace/installation/Service/CAG/Job до SQL', () => {
  for (const mutate of [
    f=>f.resources.get('namespace/kube-system').metadata.uid=other,
    f=>f.resources.get('namespace/kodex-system').metadata.uid=other,
    f=>f.installation.metadata.uid=other,f=>f.service.metadata.uid=other,f=>f.deployment.metadata.uid=other,
    f=>f.service.metadata.deletionTimestamp=iso(0),f=>f.deployment.metadata.labels['kodex.dev/environment']='other',
    f=>f.job.metadata.uid=other,f=>f.job.status.active=1,f=>f.installation.data['schema-heads.json']='{}',
    f=>{f.restore.target.resources=f.restore.target.resources.filter(row=>row.kind!=='Service');},
  ]) {
    const f=continuityFixture();mutate(f);assert.throws(f.read);
    assert.equal(f.calls.some(row=>row.args.includes('exec')),false);
  }
});

test('continuity проверяет backend/catalog и закрывает позднее изменение serving bracket', () => {
  for (const mutate of [
    f=>f.backend.databaseOID='20001',f=>f.proof.artifactPredecessorApplied=false,
    f=>f.proof.schemaCatalog.catalogDigest='0'.repeat(64),
    f=>f.faults.afterSQL=()=>{f.installation.metadata.resourceVersion='11';},
    f=>f.faults.afterSQL=()=>{f.service.spec.selector.app='raced';},
    f=>f.faults.afterSQL=()=>{f.deployment.metadata.uid=other;},
  ]) {const f=continuityFixture();mutate(f);assert.throws(f.read);}
  const f=continuityFixture(),before=observeMigrationContinuity(f.migration,f.restore,f.terminal,'synthetic',f.kube);
  f.service.metadata.resourceVersion='12';
  const after=observeMigrationContinuity(f.migration,f.restore,f.terminal,'synthetic',f.kube);
  assert.notDeepEqual(after,before);assert.equal(after.serviceResourceVersion,'12');
});
