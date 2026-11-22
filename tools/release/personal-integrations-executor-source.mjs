import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {resolve} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {inspectSource,sourceGitArguments} from './application-source.mjs';
import {fingerprint} from './scoped-release.mjs';
import {isPersonalIntegrationsMigrationProfile} from './personal-integrations-migration-contract.mjs';

const fail=ok=>{if(!ok)throw Error('PERSONAL_INTEGRATIONS_EXECUTOR_SOURCE_REJECTED');};
const same=(a,b)=>fingerprint(a)===fingerprint(b);
const sha=raw=>createHash('sha256').update(raw).digest('hex');
const root=fileURLToPath(new URL('../../',import.meta.url)).replace(/\/$/,'');
const protocol='kodex.personal-integrations-executor-source/v1';
// Только проверенные поздние Node consumers. Ни production inputs, ни SQL,
// ни registry/policy/generated proofs не входят в этот закрытый перечень.
export const personalIntegrationsExecutorPaths=Object.freeze([
 'Makefile','docs/operations/personal-email-2168.md',
 'tools/release/control-plane-migration.mjs','tools/release/control-plane-immutable-migration.mjs',
 'tools/release/portable-component-migration.mjs','tools/release/component-api-traffic.mjs',
 'tools/release/backup-controller-maintenance-cli.mjs','tools/release/backup-controller-maintenance-cli.test.mjs',
 'tools/release/component-migration-api-terminal.mjs','tools/release/personal-integrations-api-readers.mjs',
 'tools/release/personal-integrations-staged-policy.mjs',
 'tools/release/personal-integrations-executor-source.mjs','tools/release/personal-integrations-executor-source.test.mjs',
 'tools/release/personal-integrations-original-owner.mjs',
 'tools/release/personal-integrations-artifact-spec.mjs','tools/release/personal-integrations-artifact-spec.test.mjs',
 'tools/release/personal-integrations-artifact-socket-repair.mjs','tools/release/personal-integrations-artifact-socket-repair.test.mjs',
 'tools/release/personal-integrations-artifact-grant-repair.mjs','tools/release/personal-integrations-artifact-grant-repair.test.mjs',
 'tools/release/artifact-retention-grant-catalog.mjs','tools/release/artifact-retention-grant-catalog-release.mjs',
 'tools/release/artifact-retention-grant-catalog.test.mjs',
 'tools/release/personal-integrations-deployment-inputs.mjs',
 'tools/release/personal-integrations-staged-admission.mjs','tools/release/personal-integrations-staged-admission.test.mjs',
 'tools/release/personal-integrations-reader-scope.test.mjs','tools/release/portable-component-cohort.test.mjs',
 'tools/release/personal-integrations-terminal-freshness.test.mjs',
 'tools/release/fixtures/routine-migration-api-synthetic.mjs','tools/release/fixtures/routine-migration-kubectl.cjs',
 'tools/release/artifact-retention-issuer-assets.mjs','tools/release/artifact-retention-issuer-assets.test.mjs',
 'tools/release/personal-integrations-installed-custody.test.mjs',
]);
export const personalIntegrationsExecutorPhasePaths=Object.freeze([
 'tools/release/personal-integrations-migration-contract.mjs','tools/release/personal-integrations-schema-manifest.json',
 'tools/release/portable-component-migration-drain.sql',
 ...['personal-integrations','personal-integrations-backup'].flatMap(mode=>['precondition','postcondition','recovery'].map(phase=>`tools/release/${mode}-migration-${phase}.sql`)),
 ...['20261114150000_managed_kubernetes.sql','20261114160000_managed_ssh.sql','20261114170000_personal_email_mailboxes.sql'].map(name=>'services/internal/control-plane/cmd/cli/migrations/'+name),
 'deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml',
]);

// Application source остаётся прежним immutable deliverable. Новый executor
// не заменяет OCI, DDL или signed authority; его допуск только post-handoff.
export function resolvePersonalIntegrationsExecutorSource(executorSource,{source,revision,saved}={}){
 if(saved){
  fail(source===undefined&&revision===undefined&&saved.schema===protocol&&
   Object.keys(saved).sort().join(',')==='changedPathsSHA256,executorRevision,executorSource,phaseSourceSHA256,revision,schema,source');
  ({source,revision}=saved);
 }
 fail(typeof source==='string'&&source===resolve(source)&&/^[a-f0-9]{40}$/.test(revision??''));
 executorSource=resolve(executorSource);const executorRevision=inspectSource(executorSource).revision;
 fail(inspectSource(source).revision===revision&&executorRevision!==revision);
 const git=(...args)=>execFileSync('git',sourceGitArguments(executorSource,...args),{timeout:15000,maxBuffer:4<<20,stdio:['ignore','pipe','pipe']});
 let paths,phaseSources;
 try{
  git('merge-base','--is-ancestor',revision,executorRevision);
  paths=git('diff','--no-ext-diff','--no-textconv','--no-renames','--name-only','-z',revision,executorRevision).toString().split('\0').filter(Boolean).sort();
  fail(paths.length>0&&paths.length<=personalIntegrationsExecutorPaths.length&&paths.every(path=>personalIntegrationsExecutorPaths.includes(path)));
  phaseSources=personalIntegrationsExecutorPhasePaths.map(path=>{
   const original=git('show',`${revision}:${path}`),current=git('show',`${executorRevision}:${path}`);fail(original.equals(current));return {path,sha256:sha(original)};
  });
 }catch{throw Error('PERSONAL_INTEGRATIONS_EXECUTOR_SOURCE_REJECTED');}
 fail(inspectSource(source).revision===revision&&inspectSource(executorSource).revision===executorRevision);
 const proof={schema:protocol,source,revision,executorSource,executorRevision,changedPathsSHA256:fingerprint(paths),phaseSourceSHA256:fingerprint(phaseSources)};
 if(saved)fail(same(proof,saved));return Object.freeze(proof);
}

// Перед mutation нужен явный точный executor SHA. Child inspect получает тот
// же аргумент от проверенного родителя, а не назначает себе старый app SHA.
export function readPersonalIntegrationsMigrationExecutor(plan,selectedSHA,executorSource=root){
 const current=inspectSource(executorSource).revision;
 if(current===plan.source.revision){fail(selectedSHA===undefined);return undefined;}
 fail(selectedSHA===current);
 return resolvePersonalIntegrationsExecutorSource(executorSource,{source:plan.source.path,revision:plan.source.revision});
}

// Durable binding добавляется только владеющей mutation. Read-only child
// сверяет прежнюю binding при её наличии и никогда не дописывает journal.
export function bindPersonalIntegrationsMigrationExecutor(journal,proof,{readOnly=false}={}){
 const rows=journal.events.filter(row=>row.type==='PERSONAL_EXECUTOR_BOUND');
 fail(rows.length<=1&&(!rows.length||proof&&same(rows[0].compatibility,proof)));
 if(proof&&!rows.length&&!readOnly)journal.append({type:'PERSONAL_EXECUTOR_BOUND',compatibility:proof});
 return proof;
}

// Уже связанный executor остаётся источником предшествующих записей. Узкое
// продолжение после ART init fail может читать его журнал и фиксирует свою
// собственную совместимость перед ровно одним адресным изменением.
export function bindPersonalArtifactSocketExecutor(journal,proof,{repair=false,grantRepair=false,schemaRepair=false,terminalRepair=false,readOnly=false}={}){
 const rows=journal.events.filter(row=>row.type==='PERSONAL_EXECUTOR_BOUND');
 const repairs=journal.events.filter(row=>row.type==='PERSONAL_ARTIFACT_SOCKET_EXECUTOR_BOUND');
 fail(rows.length<=1&&repairs.length<=1);
 const grantBindings=journal.events.filter(row=>row.type==='PERSONAL_ARTIFACT_GRANT_EXECUTOR_BOUND');
 const schemaBindings=journal.events.filter(row=>row.type==='PERSONAL_ARTIFACT_GRANT_CATALOG_EXECUTOR_BOUND');
 const terminalBindings=journal.events.filter(row=>row.type==='PERSONAL_INTEGRATIONS_TERMINAL_EXECUTOR_BOUND');
 const previousSchema=schemaBindings[0]?.compatibility;
 if(terminalBindings.length||terminalRepair&&previousSchema&&proof?.executorRevision!==previousSchema.executorRevision){
  fail(terminalBindings.length<=1&&schemaBindings.length===1&&proof&&previousSchema.executorRevision!==proof.executorRevision&&
   previousSchema.source===proof.source&&previousSchema.revision===proof.revision&&previousSchema.phaseSourceSHA256===proof.phaseSourceSHA256);
  const previousEvents=journal.events.filter(row=>row.type!=='PERSONAL_INTEGRATIONS_TERMINAL_EXECUTOR_BOUND');
  bindPersonalArtifactSocketExecutor({events:previousEvents},previousSchema,{readOnly:true});
  const paths=['tools/release/control-plane-immutable-migration.mjs','tools/release/personal-integrations-executor-source.mjs',
   'tools/release/personal-integrations-terminal-freshness.test.mjs','tools/release/portable-component-migration.mjs'];
  const git=(...args)=>execFileSync('git',sourceGitArguments(proof.executorSource,...args),
   {timeout:15000,maxBuffer:1<<20,stdio:['ignore','pipe','pipe']});
  try{
   git('merge-base','--is-ancestor',previousSchema.executorRevision,proof.executorRevision);
   const changed=git('diff','--no-ext-diff','--no-renames','--name-only','-z',previousSchema.executorRevision,proof.executorRevision)
    .toString().split('\0').filter(Boolean);
   fail(changed.includes('tools/release/portable-component-migration.mjs')&&changed.every(path=>paths.includes(path)));
  }catch{fail(false);}
  const record={type:'PERSONAL_INTEGRATIONS_TERMINAL_EXECUTOR_BOUND',priorExecutorSHA256:fingerprint(previousSchema),compatibility:proof};
  if(terminalBindings.length){
   const {at,...persisted}=terminalBindings[0];
   fail(same(record,persisted)&&journal.events.indexOf(schemaBindings[0])<journal.events.indexOf(terminalBindings[0]));
  }else if(!readOnly)journal.append(record);
  return proof;
 }
 if(schemaRepair||schemaBindings.length){
  fail(schemaBindings.length<=1&&grantBindings.length===1&&proof);
  const previous=grantBindings[0].compatibility;
  const previousEvents=journal.events.filter(row=>row.type!=='PERSONAL_ARTIFACT_GRANT_CATALOG_EXECUTOR_BOUND');
  bindPersonalArtifactSocketExecutor({events:previousEvents},previous,{readOnly:true});
  fail(previous.executorRevision!==proof.executorRevision&&previous.source===proof.source&&previous.revision===proof.revision&&
   previous.phaseSourceSHA256===proof.phaseSourceSHA256);
  const paths=['tools/release/control-plane-migration.mjs','tools/release/control-plane-immutable-migration.mjs',
   'tools/release/personal-integrations-executor-source.mjs','tools/release/artifact-retention-grant-catalog.mjs',
   'tools/release/artifact-retention-grant-catalog-release.mjs','tools/release/artifact-retention-grant-catalog.test.mjs',
   'tools/release/portable-component-migration.mjs','tools/release/backup-controller-maintenance-cli.mjs'];
  const git=(...args)=>execFileSync('git',sourceGitArguments(proof.executorSource,...args),
   {timeout:15000,maxBuffer:1<<20,stdio:['ignore','pipe','pipe']});
  try{
   git('merge-base','--is-ancestor',previous.executorRevision,proof.executorRevision);
   const changed=git('diff','--no-ext-diff','--no-renames','--name-only','-z',previous.executorRevision,proof.executorRevision)
    .toString().split('\0').filter(Boolean);
   fail(changed.includes('tools/release/artifact-retention-grant-catalog-release.mjs')&&changed.every(path=>paths.includes(path)));
  }catch{fail(false);}
  const record={type:'PERSONAL_ARTIFACT_GRANT_CATALOG_EXECUTOR_BOUND',priorExecutorSHA256:fingerprint(previous),compatibility:proof};
  if(schemaBindings.length){
   const {at,...persisted}=schemaBindings[0];
   fail(same(record,persisted)&&journal.events.indexOf(grantBindings[0])<journal.events.indexOf(schemaBindings[0]));
  }else if(!readOnly)journal.append(record);
  return proof;
 }
 if(grantRepair||grantBindings.length){
  fail(grantBindings.length<=1&&repairs.length===1&&proof);
  const previous=repairs[0].compatibility;
  const previousEvents=journal.events.filter(row=>row.type!=='PERSONAL_ARTIFACT_GRANT_EXECUTOR_BOUND');
  bindPersonalArtifactSocketExecutor({events:previousEvents},previous,{readOnly:true});
  fail(previous.executorRevision!==proof.executorRevision&&previous.source===proof.source&&previous.revision===proof.revision&&
   previous.phaseSourceSHA256===proof.phaseSourceSHA256);
  const paths=['tools/release/control-plane-migration.mjs','tools/release/control-plane-immutable-migration.mjs',
   'tools/release/personal-integrations-executor-source.mjs','tools/release/personal-integrations-artifact-grant-repair.mjs',
   'tools/release/personal-integrations-artifact-grant-repair.test.mjs','tools/release/portable-component-migration.mjs'];
  const git=(...args)=>execFileSync('git',sourceGitArguments(proof.executorSource,...args),
   {timeout:15000,maxBuffer:1<<20,stdio:['ignore','pipe','pipe']});
  try{
   git('merge-base','--is-ancestor',previous.executorRevision,proof.executorRevision);
   const changed=git('diff','--no-ext-diff','--no-renames','--name-only','-z',previous.executorRevision,proof.executorRevision)
    .toString().split('\0').filter(Boolean);
   fail(changed.includes('tools/release/personal-integrations-artifact-grant-repair.mjs')&&changed.every(path=>paths.includes(path)));
  }catch{fail(false);}
  const record={type:'PERSONAL_ARTIFACT_GRANT_EXECUTOR_BOUND',priorExecutorSHA256:fingerprint(previous),compatibility:proof};
  if(grantBindings.length){
   const {at,...persisted}=grantBindings[0];
   fail(same(record,persisted)&&journal.events.indexOf(repairs[0])<journal.events.indexOf(grantBindings[0]));
  }else if(!readOnly)journal.append(record);
  return proof;
 }
 const original=rows[0]?.compatibility;
 if(!repairs.length&&!repair)return bindPersonalIntegrationsMigrationExecutor(journal,proof,{readOnly});
 fail(original&&proof&&original.source===proof.source&&original.revision===proof.revision&&
  original.phaseSourceSHA256===proof.phaseSourceSHA256&&original.executorRevision!==proof.executorRevision);
 resolvePersonalIntegrationsExecutorSource(original.executorSource,{saved:original});
 const paths=['tools/release/control-plane-migration.mjs','tools/release/control-plane-immutable-migration.mjs',
  'tools/release/personal-integrations-executor-source.mjs','tools/release/personal-integrations-artifact-socket-repair.mjs','tools/release/component-api-traffic.mjs',
  'tools/release/personal-integrations-artifact-socket-repair.test.mjs','tools/release/portable-component-migration.mjs'];
 const git=(...args)=>execFileSync('git',sourceGitArguments(proof.executorSource,...args),
  {timeout:15000,maxBuffer:1<<20,stdio:['ignore','pipe','pipe']});
 try {
  git('merge-base','--is-ancestor',original.executorRevision,proof.executorRevision);
  const changed=git('diff','--no-ext-diff','--no-renames','--name-only','-z',original.executorRevision,proof.executorRevision)
   .toString().split('\0').filter(Boolean);
  fail(changed.includes('tools/release/personal-integrations-artifact-socket-repair.mjs')&&changed.every(path=>paths.includes(path)));
 }catch{fail(false);}
 const record={type:'PERSONAL_ARTIFACT_SOCKET_EXECUTOR_BOUND',priorExecutorSHA256:fingerprint(original),compatibility:proof};
 if(repairs.length){
  const {at,...persisted}=repairs[0];
  fail(same(record,persisted)&&journal.events.indexOf(rows[0])<journal.events.indexOf(repairs[0]));
 }else if(!readOnly)journal.append(record);
 return proof;
}

// Свежие статусы читаются тем же authority executor, который зафиксирован
// в исходной status Job и durable migration binding. Ремонт Deployment
// не назначает нового владельца DB и не ослабляет scope исходного reader.
export async function readPersonalIntegrationsPinnedAuthority(input,kube,{purpose,executor,grantRepair=false,schemaRepair=false,terminalRepair=false}){
 fail(['delivered','terminal'].includes(purpose));
 const scope=input.freshPlan?.ownerScope;
 let authoritySource=root;
 if(scope?.executorSource!==root){
  const {plan,events}=input.migration;
  bindPersonalArtifactSocketExecutor({events},executor,{repair:true,grantRepair,schemaRepair,terminalRepair,readOnly:true});
  const original=events.find(row=>row.type==='PERSONAL_EXECUTOR_BOUND').compatibility;
  fail(original.revision===plan.source.revision&&original.source===plan.source.path&&
    scope?.executorSource===original.executorSource&&scope.executorRevision===original.executorRevision);
  authoritySource=original.executorSource;
 }
 const original=await import(pathToFileURL(resolve(authoritySource,'tools/release/authority-rotation-transition.mjs')).href);
 const read=purpose==='terminal'?original.readPersonalIntegrationsAuthorityTerminal:original.readPersonalIntegrationsAuthorityDelivered;
 fail(typeof read==='function');
 return read(input,kube);
}

// Child budget включает тот же full049 SQL и девять serving inventories.
// Старые профили сохраняют120s, Telegram backup — прежние300s.
export function migrationInspectTimeoutMs(profile){
 return isPersonalIntegrationsMigrationProfile(profile)?240000:profile==='telegram-notifications-backup'?300000:120000;
}
