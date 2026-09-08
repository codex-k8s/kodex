#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { createHash, randomUUID } from 'node:crypto';
import { lstatSync, readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { fingerprint } from './scoped-release.mjs';
import { inspectSource, planSourceChange } from './application-source.mjs';
import { privateJournal } from '../dev/role-image-acceptance.mjs';
const ensure = (ok, code) => { if (!ok) throw new Error(code); };
const namespace = 'kodex-system';
const migrationVersion = '20260908000300';
const migrationPath = 'services/internal/control-plane/cmd/cli/migrations/20260908000300_email_legacy_package_admission.sql';
const annotation = 'kodex.dev/cp-migration-plan';
const fileDigest = (path) => createHash('sha256').update(readFileSync(path)).digest('hex');

// Узкая фаза только существующего CP migrator в disposable hot-reload профиле.
export function planControlPlaneMigration(before, source, inspect = inspectSource, id = randomUUID()) {
  ensure(before?.kind === 'Job' && before.apiVersion === 'batch/v1' && before.metadata?.name === 'control-plane-migrate' && before.metadata.namespace === namespace && before.metadata.uid && before.metadata.resourceVersion && before.metadata.labels?.['app.kubernetes.io/part-of'] === 'kodex' && before.metadata.labels?.['kodex.dev/environment'] === 'staging' && before.metadata.labels?.['kodex.dev/local-profile'] === 'hot-reload', 'MIGRATOR_IDENTITY_INVALID');
  ensure(before.status?.succeeded === 1 && !before.status.active && before.status.conditions?.some((c) => c.type === 'Complete' && c.status === 'True'), 'PREVIOUS_MIGRATION_NOT_COMPLETE');
  const pod = before.spec.template.spec; const container = pod.containers?.[0];
  ensure(pod.serviceAccountName === 'control-plane-migrator' && pod.automountServiceAccountToken === false && pod.restartPolicy === 'Never' && pod.containers.length === 1 && container?.name === 'migrate' && JSON.stringify(container.command) === JSON.stringify(['/workspace/tools/dev/run-go-command.sh']) && JSON.stringify(container.args) === JSON.stringify(['services/internal/control-plane', './cmd/cli', 'up']) && container.securityContext?.readOnlyRootFilesystem === true && container.securityContext?.allowPrivilegeEscalation === false && /@sha256:[a-f0-9]{64}$/.test(container.image), 'MIGRATOR_COMMAND_INVALID');
  ensure(container.env?.find((e) => e.name === 'CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE')?.value === '/var/run/secrets/kodex/control-plane/postgres-migration/dsn' && pod.volumes?.find((v) => v.name === 'postgres-migration')?.secret?.secretName === 'control-plane-postgres-migration', 'MIGRATOR_CREDENTIAL_BOUNDARY_INVALID');
  const next = structuredClone(before.spec);
  planSourceChange(before, next, 0, source, inspect);
  delete next.selector; delete next.manualSelector;
  next.backoffLimit = 0; next.activeDeadlineSeconds = 300;
  next.ttlSecondsAfterFinished = 86400;
  next.template.metadata ??= {};
  next.template.metadata.labels = Object.fromEntries(Object.entries(next.template.metadata.labels ?? {}).filter(([key]) => !['controller-uid', 'job-name', 'batch.kubernetes.io/controller-uid', 'batch.kubernetes.io/job-name'].includes(key)));
  next.template.metadata.annotations = { ...(next.template.metadata.annotations ?? {}), 'kodex.dev/source-revision': source.revision };
  delete next.template.metadata.annotations['kodex.dev/source-content-sha256'];
  const job = { apiVersion: 'batch/v1', kind: 'Job', metadata: { name: `control-plane-migrate-${migrationVersion}`, namespace, labels: before.metadata.labels }, spec: next };
  return { version: 1, kind: 'CONTROL_PLANE_MIGRATION', id, source, migrationVersion, beforeUID: before.metadata.uid, beforeResourceVersion: before.metadata.resourceVersion, beforeSpecSHA256: fingerprint(before.spec), job };
}
function privateJSON(path) {
  path = resolve(path); const stat = lstatSync(path);
  ensure(realpathSync(path) === path && realpathSync(dirname(path)) === dirname(path) && (lstatSync(dirname(path)).mode & 0o077) === 0 && stat.isFile() && stat.nlink === 1 && (stat.mode & 0o077) === 0 && stat.size < (4 << 20), 'PRIVATE_INPUT_INVALID');
  return JSON.parse(readFileSync(path, 'utf8'));
}
function writePrivate(path, value) {
  path = resolve(path); ensure(realpathSync(dirname(path)) === dirname(path) && (lstatSync(dirname(path)).mode & 0o077) === 0, 'PRIVATE_DIRECTORY_REQUIRED');
  writeFileSync(path, `${JSON.stringify(value)}\n`, { flag: 'wx', mode: 0o600 });
}
export function migrationReadback(job, plan, logs) {
  ensure(job?.metadata?.name === plan.job.metadata.name && job.metadata.namespace === namespace && job.metadata.annotations?.[annotation] === fingerprint(plan) && job.metadata.uid, 'MIGRATION_JOB_SCOPE_MISMATCH');
  ensure(fingerprint(job.spec?.template?.spec) === fingerprint(plan.job.spec.template.spec), 'MIGRATION_JOB_SPEC_CHANGED');
  const completed = job.status?.conditions?.some((c) => c.type === 'Complete' && c.status === 'True');
  const failed = job.status?.conditions?.some((c) => c.type === 'Failed' && c.status === 'True');
  ensure(!(completed && failed), 'MIGRATION_STATUS_INVALID');
  const exact = new RegExp(`(?:migrated database to version:|current version:)\\s*${migrationVersion}(?:\\s|$)`).test(logs ?? '');
  return { status: completed ? exact ? 'PASS' : 'READBACK_UNCONFIRMED' : failed ? 'FAIL' : 'RUNNING', jobName: job.metadata.name, jobUID: job.metadata.uid, sourceRevision: plan.source.revision, migrationVersion: completed && exact ? migrationVersion : undefined };
}
async function main() {
  const args = process.argv.slice(2); const phase = args.shift(); const options = {};
  while (args.length) { const key = args.shift(); ensure(/^--[a-z-]+$/.test(key ?? '') && args.length && !(key in options), 'ARGUMENT_INVALID'); options[key] = args.shift(); }
  ensure(['plan','apply','inspect'].includes(phase) && Object.keys(options).every((k) => ['--context','--source','--revision','--plan','--evidence','--confirm'].includes(k)) && options['--plan'], 'ARGUMENT_INVALID');
  const context = options['--context']; ensure(/^[A-Za-z0-9_.:@/-]{1,160}$/.test(context ?? '') && !/prod(?:uction)?/i.test(context), 'STAGING_CONTEXT_REQUIRED');
  ensure(phase === 'plan' ? options['--source'] && options['--revision'] && !options['--confirm'] && !options['--evidence'] : !options['--source'] && !options['--revision'] && options['--evidence'] && options['--confirm'] === (phase === 'apply' ? 'APPLY-STAGING-CP-MIGRATION' : undefined), 'PHASE_ARGUMENT_INVALID');
  const kube = (argv, input) => { try { return execFileSync('kubectl', ['--context',context,'--request-timeout=30s',...argv], { input, encoding:'utf8', timeout:35000, maxBuffer:4<<20, stdio:['pipe','pipe','pipe'] }); } catch { throw new Error('KUBERNETES_OPERATION_UNKNOWN'); } };
  const get = (kind,name) => JSON.parse(kube(['-n',namespace,'get',kind,name,'-o','json']));
  const clusterUID = JSON.parse(kube(['get','namespace','kube-system','-o','json'])).metadata.uid;
  const base = () => get('job','control-plane-migrate');
  if (phase === 'plan') {
    const plan = planControlPlaneMigration(base(), { path: options['--source'], revision: options['--revision'] });
    plan.clusterUID = clusterUID; plan.context = context; plan.migrationFileSHA256 = fileDigest(`${plan.source.path}/${migrationPath}`);
    writePrivate(options['--plan'],plan); process.stdout.write(`${JSON.stringify({ status:'PLANNED', jobName:plan.job.metadata.name, sourceRevision:plan.source.revision, planSHA256:fingerprint(plan) })}\n`); return;
  }
  const plan = privateJSON(options['--plan']); ensure(plan.kind === 'CONTROL_PLANE_MIGRATION' && plan.clusterUID === clusterUID && plan.context === context && plan.migrationVersion === migrationVersion, 'PLAN_SCOPE_MISMATCH');
  const journal = privateJournal(options['--evidence'], { version:1, kind:'CONTROL_PLANE_MIGRATION', planSHA256:fingerprint(plan) });
  try {
    const attempts = journal.events.filter((e) => e.type === 'CREATE_INTENT');
    const reservations = journal.events.filter((e) => e.type === 'RESERVE_INTENT');
    const optionalJob = () => { const raw = kube(['-n',namespace,'get','job',plan.job.metadata.name,'--ignore-not-found=true','-o','json']); return raw.trim() ? JSON.parse(raw) : undefined; };
    const inspectReservation = (actual) => {
      ensure(actual.metadata.uid === plan.beforeUID && fingerprint(actual.spec) === plan.beforeSpecSHA256, 'MIGRATION_RESERVATION_SCOPE_CHANGED');
      const marker = actual.metadata.annotations?.[annotation];
      ensure(!marker || marker === fingerprint(plan), 'MIGRATION_RESERVATION_SCOPE_CHANGED');
      const existing = optionalJob();
      if (existing) migrationReadback(existing,plan,'');
      const result = {status:existing ? 'JOB_EXISTS_WITHOUT_CREATE_INTENT' : marker ? 'RESERVED_NOT_CREATED' : 'RESERVATION_NOT_APPLIED', beforeUID:actual.metadata.uid, resourceVersion:actual.metadata.resourceVersion, jobName:plan.job.metadata.name, ...(existing ? {jobUID:existing.metadata.uid} : {})};
      journal.append({type:'RESERVATION_READBACK',...result}); return result;
    };
    if (phase === 'inspect' && attempts.length === 0) {
      ensure(reservations.length === 1, 'MIGRATION_RESERVATION_INTENT_REQUIRED');
      process.stdout.write(`${JSON.stringify(inspectReservation(base()))}\n`); return;
    }
    if (phase === 'apply' && attempts.length === 0) {
      const actual = base(); const rebuilt = planControlPlaneMigration(actual,plan.source,inspectSource,plan.id);
      ensure(actual.metadata.uid === plan.beforeUID && fingerprint(actual.spec) === plan.beforeSpecSHA256 && fingerprint(rebuilt.job) === fingerprint(plan.job) && fileDigest(`${plan.source.path}/${migrationPath}`) === plan.migrationFileSHA256, 'MIGRATION_PLAN_DRIFT');
      if (reservations.length) {
        ensure(reservations.length === 1 && inspectReservation(actual).status === 'RESERVED_NOT_CREATED', 'MIGRATION_RESERVATION_NOT_RESUMABLE');
      } else {
        ensure(actual.metadata.resourceVersion === plan.beforeResourceVersion && !actual.metadata.annotations?.[annotation], 'MIGRATION_PLAN_DRIFT');
        ensure(!optionalJob(), 'MIGRATION_JOB_ALREADY_EXISTS');
      }
      const jobs = JSON.parse(kube(['-n',namespace,'get','jobs','-l','app.kubernetes.io/name=control-plane,app.kubernetes.io/component=migration','-o','json'])).items;
      ensure(jobs.every((j) => j.status?.conditions?.some((c) => ['Complete','Failed'].includes(c.type) && c.status === 'True')), 'MIGRATION_ALREADY_RUNNING');
      if (!reservations.length) {
        const patch = [ {op:'test',path:'/metadata/uid',value:plan.beforeUID}, {op:'test',path:'/metadata/resourceVersion',value:plan.beforeResourceVersion}, {op:'add',path:'/metadata/annotations',value:{...(actual.metadata.annotations ?? {}),[annotation]:fingerprint(plan)}} ];
        journal.append({type:'RESERVE_INTENT', beforeUID:plan.beforeUID, beforeResourceVersion:plan.beforeResourceVersion});
        try { const reserved = JSON.parse(kube(['-n',namespace,'patch','job','control-plane-migrate','--type=json','-p',JSON.stringify(patch),'-o','json'])); ensure(reserved.metadata.annotations?.[annotation] === fingerprint(plan), 'MIGRATION_RESERVATION_UNCONFIRMED'); journal.append({type:'RESERVE_ACK',resourceVersion:reserved.metadata.resourceVersion}); } catch(error) { journal.append({type:'UNKNOWN',code:error.message}); throw error; }
      }
      const job = structuredClone(plan.job); job.metadata.annotations = { [annotation]:fingerprint(plan) };
      journal.append({ type:'CREATE_INTENT', jobName:job.metadata.name });
      try { const created = JSON.parse(kube(['create','-f','-','-o','json'],JSON.stringify(job))); journal.append({type:'CREATE_ACK', jobUID:created.metadata.uid}); } catch(error) { journal.append({type:'UNKNOWN', code:error.message}); throw error; }
    }
    ensure(journal.events.some((e) => e.type === 'CREATE_INTENT'), 'MIGRATION_INTENT_REQUIRED');
    const actual = get('job',plan.job.metadata.name);
    const ack = journal.events.find((e) => e.type === 'CREATE_ACK');
    ensure(!ack || ack.jobUID === actual.metadata.uid, 'MIGRATION_JOB_REPLACED');
    let logs = ''; if (actual.status?.succeeded === 1) logs = kube(['-n',namespace,'logs',`job/${plan.job.metadata.name}`,'-c','migrate','--limit-bytes=262144']);
    const result = migrationReadback(actual,plan,logs); journal.append({type:'READBACK',...result}); process.stdout.write(`${JSON.stringify(result)}\n`);
    if (['FAIL','READBACK_UNCONFIRMED'].includes(result.status)) process.exitCode = 1;
  } finally { journal.close(); }
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main().catch((error) => { process.stderr.write(`${JSON.stringify({status:'FAIL',code:/^[A-Z_]+$/.test(error.message)?error.message:'CP_MIGRATION_FAILED'})}\n`); process.exitCode=1; });
