import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,writeFileSync} from 'node:fs';
import {join} from 'node:path';
import {routineMigrationAPIFixture} from './fixtures/routine-migration-api-synthetic.mjs';

test('native CLI synthetic squash authority: exact orphan source and immutable ancestor close receipt survive full restore',t=>{
 const f=routineMigrationAPIFixture(t,{authoritySquashSource:true,predecessorSource:true});
 const names=['authority.json','authority.jsonl','close.json','close.jsonl','migration.json','migration.jsonl','open.json','open.jsonl'];
 const before=names.map(name=>readFileSync(join(f.directory,name),'utf8'));
 assert.notEqual(f.authoritySHA,f.executorSHA);
 assert.equal(f.pass('plan').status,'PLAN_ONLY');
 assert.equal(f.read('restore.json').authoritySourceEquivalentSHA,f.authorityEquivalentSHA);
 assert.equal(f.pass('apply').state,'RESTORED');assert.equal(f.pass('status').state,'RESTORED');assert.equal(f.pass('apply').state,'RESTORED');
 assert.deepEqual(f.state().effects,['service']);
 assert.deepEqual(names.map(name=>readFileSync(join(f.directory,name),'utf8')),before);
});

test('native CLI squash baseline capture preserves exact old close and rejects missing or mismatched equivalence before effects',t=>{
 const f=routineMigrationAPIFixture(t,{authoritySquashSource:true});
 f.edit(state=>{
  const api=state.apps['control-api-gateway'];api.spec=structuredClone(f.closed.plan.afterSpec);api.metadata.annotations=structuredClone(f.closed.plan.afterAnnotations);
  for(const key of ['replicas','readyReplicas','updatedReplicas','availableReplicas'])api.status[key]=0;
  state.service.spec=structuredClone(f.isolation.plan.beforeSpec);state.service.metadata.annotations=structuredClone(f.isolation.plan.beforeAnnotations);
 });
 for(const change of ['missing-equivalence','wrong-tree','missing-original']){
  const refused=f.runIsolation('plan',argv=>{
   const index=argv.indexOf(change==='missing-original'?'--authority-executor-sha':'--authority-source-equivalent-sha');
   if(change==='wrong-tree')argv[index+1]=f.executorSHA;else argv.splice(index,2);
  });
  assert.notEqual(refused.status,0,change);assert.deepEqual(f.state().effects,[]);
  if(change==='wrong-tree')assert.match(refused.stderr,/EQUIVALENT_TREE_CHANGED/);
  if(change==='missing-original')assert.match(refused.stderr,/EQUIVALENT_INVALID/);
 }
 const planned=f.runIsolation('plan');assert.equal(planned.status,0,planned.stderr);
 assert.equal(f.read('new-isolate.json').authoritySourceEquivalentSHA,f.authorityEquivalentSHA);
 const applied=f.runIsolation('apply');assert.equal(applied.status,0,applied.stderr);
 assert.equal(JSON.parse(applied.stdout).state,'ISOLATED');
 assert.equal(f.runIsolation('status').status,0);assert.equal(f.runIsolation('apply').status,0);
 assert.deepEqual(f.state().effects,['service']);
});

test('native CLI routine045: exact owner/cohort terminal restores Service without rotation or input rewrites',t=>{
 const f=routineMigrationAPIFixture(t);
 const names=['authority.json','authority.jsonl','migration.json','migration.jsonl','open.json','open.jsonl'];
 const before=names.map(name=>readFileSync(join(f.directory,name),'utf8'));
 assert.equal(f.pass('plan').status,'PLAN_ONLY');assert.deepEqual(f.state().effects,[]);
 assert.equal(f.pass('apply').state,'RESTORED');assert.deepEqual(f.state().effects,['service']);
 assert.equal(f.pass('status').state,'RESTORED');assert.equal(f.pass('apply').state,'RESTORED');
 assert.deepEqual(f.state().effects,['service']);
 assert.deepEqual(names.map(name=>readFileSync(join(f.directory,name),'utf8')),before);
 const plan=f.read('restore.json');
 assert.equal(plan.migrationRestoreProfile,'sentry-observability-backup');
 assert.equal(plan.authority.migrationVersion,20261114130000);
 assert.equal(plan.authority.authority.policyRevision,106);
 assert.equal(plan.authority.sourceCompatibility.migrationSourceSHA,f.sourceSHA);
 assert.equal(plan.authority.sourceCompatibility.executorSHA,f.executorSHA);
 assert.ok(f.state().calls.some(call=>call.args.includes('--dbname=internal_rpc_authority')));
 assert.ok(f.state().calls.some(call=>call.args.includes('--dbname=control_plane')));
});

test('native CLI predecessor source: published source/locks/receipts remain exact while compatible new executor restores traffic',t=>{
 const f=routineMigrationAPIFixture(t,{predecessorSource:true});
 assert.notEqual(f.sourceSHA,f.executorSHA);
 const names=['authority.json','authority.jsonl','migration.json','migration.jsonl','open.json','open.jsonl'];
 const before=names.map(name=>readFileSync(join(f.directory,name),'utf8'));
 assert.equal(f.pass('plan').status,'PLAN_ONLY');assert.equal(f.pass('apply').state,'RESTORED');
 assert.deepEqual(f.state().effects,['service']);
 const plan=f.read('restore.json');
 assert.equal(plan.authority.sourceCompatibility.migrationSourceSHA,f.sourceSHA);
 assert.equal(plan.authority.sourceCompatibility.executorSHA,f.executorSHA);
 assert.deepEqual(names.map(name=>readFileSync(join(f.directory,name),'utf8')),before);
});

test('native CLI normal045 uses its exact non-backup SQL/catalog and the same protected restoration boundary',t=>{
 const f=routineMigrationAPIFixture(t,{profile:'sentry-observability'});
 f.pass('plan');assert.equal(f.pass('apply').state,'RESTORED');assert.deepEqual(f.state().effects,['service']);
 assert.equal(f.read('restore.json').migrationRestoreProfile,'sentry-observability');
});

test('native CLI captures immutable authority baseline before routine isolation and refreshes it before its only Service effect',t=>{
 const f=routineMigrationAPIFixture(t);
 f.edit(state=>{
  const api=state.apps['control-api-gateway'];api.spec=structuredClone(f.closed.plan.afterSpec);api.metadata.annotations=structuredClone(f.closed.plan.afterAnnotations);
  for(const key of ['replicas','readyReplicas','updatedReplicas','availableReplicas'])api.status[key]=0;
  state.service.spec=structuredClone(f.isolation.plan.beforeSpec);state.service.metadata.annotations=structuredClone(f.isolation.plan.beforeAnnotations);
 });
 const plan=f.runIsolation('plan');assert.equal(plan.status,0,plan.stderr);assert.deepEqual(f.state().effects,[]);
 const captured=f.read('new-isolate.json');assert.equal(captured.migrationRestore.authority.policyRevision,106);
 assert.equal(captured.migrationRestore.authority.rotationPlanSHA256,f.read('isolate.json').migrationRestore.authority.rotationPlanSHA256);
 const apply=f.runIsolation('apply');assert.equal(apply.status,0,apply.stderr);assert.equal(JSON.parse(apply.stdout).state,'ISOLATED');
 assert.equal(f.runIsolation('status').status,0);assert.equal(f.runIsolation('apply').status,0);assert.deepEqual(f.state().effects,['service']);
});

test('native CLI lost Service acknowledgement is recovered by readback without a second restore or changed input bytes',t=>{
 const f=routineMigrationAPIFixture(t);f.pass('plan');f.edit(state=>state.lostServiceACK=true);
 const unknown=f.run('apply');assert.notEqual(unknown.status,0);assert.doesNotMatch(unknown.stderr,/private synthetic/);
 assert.deepEqual(f.state().effects,['service']);
 assert.equal(f.pass('status').state,'RESTORED');assert.equal(f.pass('apply').state,'RESTORED');assert.deepEqual(f.state().effects,['service']);
});

test('native CLI rejects converged API substitutions after terminal inspect but before final Service gate',t=>{
 for(const change of ['image','env','replicas']){
  const f=routineMigrationAPIFixture(t);f.pass('plan');f.edit(state=>state.apiDriftAfterOwner=change);
  const refused=f.run('apply');assert.notEqual(refused.status,0,change);assert.match(refused.stderr,/MIGRATION_API_SPEC_CHANGED/);
  assert.deepEqual(f.state().effects,[]);
 }
});

test('native CLI rejects stale authority, replaced Job and changed migration/open journals before Service effects',t=>{
 for(const change of ['stale','job','migration-journal','open-journal']){
  const f=routineMigrationAPIFixture(t);f.pass('plan');
  if(change==='stale')f.edit(state=>state.staleOwner='2001-01-01T00:00:00Z');
  if(change==='job')f.edit(state=>state.job.metadata.uid='99999999-9999-4999-8999-999999999999');
  if(change==='migration-journal'){
   const path=join(f.directory,'migration.jsonl');writeFileSync(path,readFileSync(path,'utf8')+'{"type":"INSPECT","stage":"UPGRADED"}\n');
  }
  if(change==='open-journal'){
   const path=join(f.directory,'open.jsonl');writeFileSync(path,readFileSync(path,'utf8')+'{"type":"STATUS_ONLY"}\n');
  }
  const refused=f.run('apply');assert.notEqual(refused.status,0,change);assert.deepEqual(f.state().effects,[]);
 }
});

test('native CLI requires explicit source and rejects unrelated source or changed selected-source SQL',t=>{
 for(const change of ['missing','unrelated','sql']){
  const f=routineMigrationAPIFixture(t,{predecessorSource:true});
  const mutate=args=>{
   const index=args.indexOf('--migration-source-sha');
   if(change==='missing')args.splice(index,2);
   if(change==='unrelated')args[index+1]='f'.repeat(40);
  };
  if(change==='unrelated'){
   const migration=f.read('migration.json');migration.source.revision='f'.repeat(40);f.save('migration.json',migration);
  }
  if(change==='sql'){
   const path=join(f.sourceRoot,'tools/release/sentry-observability-backup-migration-postcondition.sql');
   writeFileSync(path,readFileSync(path,'utf8')+'\n-- synthetic source drift\n');
  }
  const refused=f.run('plan',[],mutate);assert.notEqual(refused.status,0,change);assert.deepEqual(f.state().effects,[]);
 }
});


test('native CLI rollout revision6→7 preserves old close and exact current OPEN while restoring once',t=>{
 const revision='deployment.kubernetes.io/revision';
 const f=routineMigrationAPIFixture(t,{rolloutAnnotations:{before:{[revision]:'6'},after:{[revision]:'7'}}});
 const names=['close.json','close.jsonl','migration.json','migration.jsonl','open.json','open.jsonl'];
 const before=names.map(name=>readFileSync(join(f.directory,name),'utf8'));
 assert.equal(f.pass('plan').status,'PLAN_ONLY');assert.equal(f.pass('apply').state,'RESTORED');
 assert.equal(f.pass('apply').state,'RESTORED');assert.deepEqual(f.state().effects,['service']);
 assert.deepEqual(names.map(name=>readFileSync(join(f.directory,name),'utf8')),before);
});

test('native CLI rejects non-controller annotation drift and invalid or decreasing rollout revision before restore',t=>{
 const revision='deployment.kubernetes.io/revision';
 for(const after of [{[revision]:'5'},{[revision]:'NaN'},{[revision]:''},{[revision]:'07'},{[revision]:'7','kodex.dev/foreign':'changed'}]){
  const f=routineMigrationAPIFixture(t,{rolloutAnnotations:{before:{[revision]:'6'},after}});
  const result=f.run('plan');assert.notEqual(result.status,0);assert.match(result.stderr,/OPEN_STAGE_CHANGED/);
  assert.deepEqual(f.state().effects,[]);
 }
});
