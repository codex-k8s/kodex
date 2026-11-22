import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {mkdtempSync,rmSync,readFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {fingerprint} from './scoped-release.mjs';
import {createCanonicalRotationTemplate} from './authority-rotation-transition.mjs';
import {canonicalPostgresBackupSpec} from './backup-postgres-boundary.mjs';
import {authorityOwnerDatabaseArgs} from './migration-owner-database.mjs';
import {privateJournal} from '../dev/role-image-acceptance.mjs';
import {planIssuerMigration,issuerMigrationSQL,validateIssuerMigrationState,renderIssuerMigrationJob,verifyIssuerMigrationJob,assertNoOtherAuthorityWork,
 issuerMigrationJournalHeader,validateIssuerMigrationJournal,executeIssuerMigration,issuerMigrationIO,mainIssuerMigration} from './artifact-retention-issuer-migration.mjs';

import {root,uid,before,after,backend,publication,fixture,ownerFixture} from './fixtures/issuer-migration-synthetic.mjs';

test('Scoped immutable Job сохраняет canonical TLS и не допускает обычный up/source fallback',t=>{
 const f=fixture(t),container=f.expected.spec.template.spec.containers[0];
 assert.deepEqual(container.command,['/usr/local/bin/internal-rpc-authority-cli']);
 assert.deepEqual(container.args,['up-artifact-retention-issuer']);
 assert.equal(f.expected.spec.backoffLimit,0);assert.equal(f.expected.spec.activeDeadlineSeconds,300);
 assert.equal(f.expected.spec.ttlSecondsAfterFinished,undefined);
 assert.equal(f.expected.spec.template.spec.automountServiceAccountToken,false);
 assert(container.volumeMounts.some(m=>m.name==='postgresql-credentials'&&m.readOnly));
 assert.equal(container.env.find(e=>e.name==='INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME').value,'internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local');
 for(const mutate of [j=>j.spec.template.spec.containers[0].args=['up'],j=>j.spec.template.spec.containers[0].command=['sh'],j=>j.spec.backoffLimit=1,j=>j.metadata.uid=uid(99)]) {
  const changed=structuredClone(f.actual);mutate(changed);assert.throws(()=>verifyIssuerMigrationJob(changed,f.expected,uid(6)),/ISSUER_MIGRATION_JOB_/);
 }
});

test('Exact Go SQL/readback и active rotation/migrator guards закрывают дрейф',()=>{
 const sql=issuerMigrationSQL(root);
 const role=readFileSync(join(root,'services/internal/internal-rpc-authority/cmd/cli/sql/artifact_retention_issuer_readback.sql'),'utf8').trim().replace(/;$/,'');
 assert(sql.includes(role));assert(sql.includes('BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY'));
 assert(sql.includes("usename='internal_rpc_authority_migrator'"));
 for(const patch of [{head:20260912000200},{roleExists:true},{roleValid:true},{activeRotationCount:1},{migratorSessions:1},{observedAt:'bad'},{observedAt:new Date(Date.now()-60000).toISOString()},{unknown:true}])assert.throws(()=>validateIssuerMigrationState({...before(),...patch}),/OWNER_STATE_REJECTED/);
 validateIssuerMigrationState(after(),true);
 for(const patch of [{head:20261115000100},{roleExists:false},{roleValid:false}])assert.throws(()=>validateIssuerMigrationState({...after(),...patch},true),/OWNER_STATE_REJECTED/);
});

test('CREATE intent устойчив до неизвестного ACK; повтор apply/status не создаёт вторую Job',async t=>{
 const f=fixture(t);f.state.loseACK=true;
 let journal=f.journal();
 try{await assert.rejects(executeIssuerMigration('apply',f.plan,journal,f.io),/UNCONFIRMED/);}finally{journal.close();}
 assert.equal(f.state.creates,1);assert(readFileSync(f.path,'utf8').includes('CREATE_INTENT'));
 journal=f.journal();try{assert.equal((await executeIssuerMigration('status',f.plan,journal,f.io)).status,'PASS');}finally{journal.close();}
 journal=f.journal();try{assert.equal((await executeIssuerMigration('apply',f.plan,journal,f.io)).status,'PASS');}finally{journal.close();}
 assert.equal(f.state.creates,1);
 assert.equal(readFileSync(f.path,'utf8').split('\n').filter(s=>s.includes('CREATE_INTENT')).length,1);
});

test('Отсутствие Job после неизвестного ACK не разрешает create даже при повторе apply',async t=>{
 const f=fixture(t);f.state.loseACK=true;f.state.absentAfterUnknown=true;
 let journal=f.journal();try{await assert.rejects(executeIssuerMigration('apply',f.plan,journal,f.io));}finally{journal.close();}
 for(const phase of ['apply','status']) {journal=f.journal();try{await assert.rejects(executeIssuerMigration(phase,f.plan,journal,f.io),/UNKNOWN_READBACK_REQUIRED/);}finally{journal.close();}}
 assert.equal(f.state.creates,1);
});

test('Поздний owner head и уже существующая чужая Job не принимаются как новый effect',async t=>{
 for(const kind of ['head-after','foreign-job','role-before','active-rotation']) {
  const f=fixture(t),journal=f.journal();
  if(kind==='foreign-job')f.state.job=structuredClone(f.actual);
  else f.io.readState=()=>kind==='head-after'?after():{...before(),...(kind==='role-before'?{roleExists:true}:{activeRotationCount:1})};
  try{await assert.rejects(executeIssuerMigration('apply',f.plan,journal,f.io));}finally{journal.close();}
  assert.equal(f.state.creates,0);
 }
});

test('Replacement UID после первого ACK либо owner read не стирает original custody',async t=>{
 for(const when of ['after-ACK','after-owner-read']) {
  const f=fixture(t),journal=f.journal();
  try {
   if(when==='after-ACK') {
    f.state.job=structuredClone(f.actual);
    journal.append({type:'CREATE_INTENT',jobName:f.expected.metadata.name,jobSpecSHA256:fingerprint(f.expected.spec)});
    journal.append({type:'CREATE_ACK',jobUID:uid(9)});
   } else {
    f.io.readState=()=>{if(f.state.job){f.state.job.metadata.uid=uid(9);return after();}return before();};
   }
   await assert.rejects(executeIssuerMigration('apply',f.plan,journal,f.io),/JOB_IDENTITY_CHANGED/);
  } finally {journal.close();}
 }
});

test('Нет PASS без positive exact Pod join и фактического immutable imageID',async t=>{
 const mutations=[p=>p.status.phase='Running',p=>p.status.containerStatuses[0].state={running:{}},p=>p.status.containerStatuses[0].imageID='sha256:'+'e'.repeat(64),p=>p.spec.containers[0].args=['up'],p=>p.metadata.ownerReferences[0].uid=uid(99),p=>p.status.containerStatuses[0].restartCount=1,p=>p.status.conditions=[{type:'Ready',status:'Unknown'}]];
 for(const mutate of mutations) {const f=fixture(t),journal=f.journal();mutate(f.state.pod);try{await assert.rejects(executeIssuerMigration('apply',f.plan,journal,f.io),/JOB_POD_REJECTED/);}finally{journal.close();}}
});

test('Journal закрыто отклоняет лишние записи, чужой plan и усечённую историю',t=>{
 const f=fixture(t),journal=f.journal();
 try {
  const events=structuredClone(journal.events);
  assert.throws(()=>validateIssuerMigrationJournal([...events,{at:new Date().toISOString(),type:'PASS'}],f.plan,f.expected),/JOURNAL_REJECTED/);
  events[0].planSHA256='f'.repeat(64);
  assert.throws(()=>validateIssuerMigrationJournal(events,f.plan,f.expected),/JOURNAL_REJECTED/);
 }finally{journal.close();}
});

test('Чужая privileged migration/rotation и orphan с тем же именем не скрываются',()=>{
 const name='authority-artifact-retention-issuer-20261115000200';
 const job={metadata:{name:'other',namespace:'kodex-system',uid:uid(10)},spec:{template:{spec:{serviceAccountName:'internal-rpc-authority-migrator'}}},status:{active:1}};
 assert.throws(()=>assertNoOtherAuthorityWork([job],[],name),/OTHER_JOB_ACTIVE/);
 const pod={metadata:{uid:uid(11),namespace:'kodex-system',ownerReferences:[{kind:'Job',name,uid:uid(12),controller:true}]},spec:{serviceAccountName:'internal-rpc-authority-migrator',containers:[{name:'migrate'}]},status:{phase:'Running',containerStatuses:[{name:'migrate',state:{running:{}}}]}};
 assert.throws(()=>assertNoOtherAuthorityWork([],[pod],name),/OTHER_POD_ACTIVE/);
});

test('Неверная publication и database identity отклоняются до kubectl IO',t=>{
 for(const mutate of [plan=>plan.publication.publicationEvidence[1].status='UNKNOWN',plan=>plan.backend.database='control_plane',plan=>plan.sqlSHA256='f'.repeat(64)]) {
  const f=fixture(t);f.plan.sqlSHA256=createHash('sha256').update(issuerMigrationSQL(root)).digest('hex');mutate(f.plan);
  let calls=0;assert.throws(()=>issuerMigrationIO(f.plan,()=>{calls++;throw Error('unexpected');}),/PLAN_REJECTED|PUBLICATION_UNCONFIRMED|IDENTITY_INVALID/);assert.equal(calls,0);
 }
});

// Factory fixture проходит реальные owner DB/target helpers через synthetic kubectl,
// не заменяет production IO набором готовых callbacks и не открывает соединений.

test('Actual factory связывает canonical target, local owner DB и Service endpoint с Pod UID',t=>{
 const f=ownerFixture(),local=fixture(t);
 const plan=planIssuerMigration({target:{context:'fixture',clusterUID:uid(2),namespaceUID:uid(3)},source:{path:root,revision:'a'.repeat(40)},publication:publication(),intentID:uid(1),evidencePath:local.path},f.kube);
 assert.equal(plan.backend.database,'internal_rpc_authority');assert.equal(plan.backend.systemIdentifier,'18446744073709551615');
 assert.equal(plan.target.endpoints[0].uid,uid(9));assert.equal(plan.postgres.uid,uid(4));
 assert.equal(plan.target.dependencies[1].bundle.uid,uid(12));
 const io=issuerMigrationIO(plan,f.kube);io.preflight();validateIssuerMigrationState(io.readState());
 for(const query of f.state.queries.filter(sql=>!sql.includes('pg_control_system()'))) {
  assert(query.startsWith('BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;'));
  assert(!query.includes('PASSWORD'));assert(query.includes("current_database()='internal_rpc_authority'"));
 }
 for(const mutate of [()=>f.state.endpoint.endpoints[0].addresses=['10.42.9.9'],()=>f.state.backend.databaseOID='9999',()=>f.state.pod.metadata.uid=uid(55)]) {
  const old=structuredClone(f.state);mutate();assert.throws(()=>io.preflight(),/ENDPOINT_CHANGED|BOUNDARY_CHANGED|POSTGRES_CHANGED|INSTANCE_CHANGED/);Object.assign(f.state,old);
 }
 const validCA=structuredClone(f.state.ca);
 f.state.ca.metadata.ownerReferences[0].uid=uid(66);
 assert.throws(()=>io.preflight(),/DEPENDENCY_CHANGED/);
 f.state.ca=validCA;
 f.state.ca.data['ca.pem']+='\n';
 assert.throws(()=>io.preflight(),/BOUNDARY_CHANGED/);
});

test('Failed joined Job сохраняет отдельно post-COMMIT owner факт без PASS или нового create',async t=>{
 for(const applied of [false,true]) {
  const f=fixture(t);
  f.actual.status={failed:1,conditions:[{type:'Failed',status:'True'}]};
  f.state.pod.status.phase='Failed';f.state.pod.status.containerStatuses[0].state.terminated.exitCode=1;
  f.io.readState=()=>f.state.job?(applied?after():before()):before();
  let journal=f.journal();
  try {const result=await executeIssuerMigration('apply',f.plan,journal,f.io);assert.equal(result.status,'FAILED');assert.equal(result.ownerAfter,applied);assert.equal(result.roleReady,applied);}finally{journal.close();}
  journal=f.journal();try{assert.equal((await executeIssuerMigration('status',f.plan,journal,f.io)).status,'FAILED');}finally{journal.close();}
  assert.equal(f.state.creates,1);assert.equal(readFileSync(f.path,'utf8').includes('"type":"PASS"'),false);
 }
});

test('Actual factory apply/status проходит один scoped create и lost ACK owner readback',async t=>{
 const f=ownerFixture(),local=fixture(t);
 const plan=planIssuerMigration({target:{context:'fixture',clusterUID:uid(2),namespaceUID:uid(3)},source:{path:root,revision:'a'.repeat(40)},publication:publication(),intentID:uid(1),evidencePath:local.path},f.kube);
 const io=issuerMigrationIO(plan,f.kube);f.state.loseACK=true;
 let journal=privateJournal(local.path,issuerMigrationJournalHeader(plan));
 try{await assert.rejects(executeIssuerMigration('apply',plan,journal,io),/UNCONFIRMED/);}finally{journal.close();}
 journal=privateJournal(local.path,issuerMigrationJournalHeader(plan));
 try{assert.equal((await executeIssuerMigration('status',plan,journal,io)).status,'PASS');}finally{journal.close();}
 assert.equal(f.state.creates,1);assert.deepEqual(f.state.job.spec.template.spec.containers[0].args,['up-artifact-retention-issuer']);
 journal=privateJournal(local.path,issuerMigrationJournalHeader(plan));
 try{assert.equal((await executeIssuerMigration('apply',plan,journal,io)).status,'PASS');}finally{journal.close();}
 assert.equal(f.state.creates,1);
});

test('Terminating Job не выдаёт PASS по прежнему succeeded Pod',async t=>{
 const f=fixture(t),journal=f.journal();f.actual.status.terminating=1;
 try{await assert.rejects(executeIssuerMigration('apply',f.plan,journal,f.io),/JOB_NOT_JOINED/);}finally{journal.close();}
});

test('Render закрывает DSN/env/mount drift и явно фиксирует mode0440 из canonical YAML',t=>{
 const f=fixture(t),base=createCanonicalRotationTemplate(root,f.plan.image,true);
 assert.equal(renderIssuerMigrationJob(f.plan,base).spec.template.spec.volumes.find(v=>v.name==='postgresql-ca').configMap.defaultMode,288);
 const changes=[value=>value.spec.template.spec.containers[0].env[1].value='foreign.example',value=>value.spec.template.spec.containers[0].env.push({name:'PGOPTIONS',value:'-c role=postgres'}),
  value=>value.spec.template.spec.volumes[0].secret.secretName='foreign',value=>value.spec.template.spec.volumes[2].configMap.defaultMode=444,value=>value.spec.template.spec.hostNetwork=true,
  value=>value.spec.template.spec.containers[0].args=['up']];
 for(const change of changes){const changed=structuredClone(base);change(changed);assert.throws(()=>renderIssuerMigrationJob(f.plan,changed),/TEMPLATE_REJECTED/);}
});


test('План не принимает прежний journal как новое намерение; CLI не допускает unscoped up',async t=>{
 const f=fixture(t),journal=f.journal();journal.close();
 let calls=0;
 assert.throws(()=>planIssuerMigration({target:{context:'fixture',clusterUID:uid(2),namespaceUID:uid(3)},source:f.plan.source,publication:publication(),evidencePath:f.path},()=>{calls++;throw Error('unexpected');}),/JOURNAL_REJECTED/);
 assert.equal(calls,0);
 await assert.rejects(mainIssuerMigration(['up']),/ARGUMENT_REJECTED/);
 await assert.rejects(mainIssuerMigration(['apply','--context','fixture','--force','1']),/ARGUMENT_REJECTED/);
});
