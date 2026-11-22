import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {join} from 'node:path';
import {syntheticPersonalExecutor} from './fixtures/personal-integrations-executor-synthetic.mjs';
import {routineMigrationAPIFixture} from './fixtures/routine-migration-api-synthetic.mjs';
import {componentCohortFixture} from './fixtures/component-cohort-synthetic.mjs';
import {componentMigrationTarget} from './portable-component-migration.mjs';

// Настоящий CLI проходит общий imageScope, затем full Job/SQL/cohort inspect.
// Подменяется только bounded внешний transport; signed/live readiness не заявлена.
for(const profile of ['personal-integrations','personal-integrations-backup'])test(`personal canonical CLI imageScope принимает exact isolated OPEN: ${profile}`,t=>{
 const f=routineMigrationAPIFixture(t,{profile}),result=f.runInspect();
 assert.equal(result.status,0,result.stderr+'\n'+(f.state().diagnostic??''));
 const proof=JSON.parse(result.stdout);assert.equal(proof.status,'PASS');assert.equal(proof.migrationVersion,'20261114170000');
 assert.equal(proof.cpStage,'UPGRADED');assert.equal(proof.cpReady,true);assert.equal(proof.pairReady,true);assert.equal(proof.componentPair.length,9);
 assert.equal(proof.componentPair.find(part=>part.name==='control-api-gateway').stage,'isolatedReadersSpec');
 assert.equal(proof.evidenceReadOnly,true);assert.deepEqual(f.state().effects,[]);
});

test('personal canonical CLI не принимает spoofed OPEN либо раскрытый Service',t=>{
 for(const change of [f=>f.edit(s=>s.service.spec.selector={'app.kubernetes.io/name':'control-api-gateway'}),
  f=>{const open=f.read('open.json');open.sourceSHA='f'.repeat(40);f.save('open.json',open);},
  f=>f.edit(s=>s.apps['control-api-gateway'].spec.template.spec.containers[0].env=[{name:'FOREIGN',value:'changed'}])]){
  const f=routineMigrationAPIFixture(t,{profile:'personal-integrations'});change(f);const result=f.runInspect();
  assert.notEqual(result.status,0);assert.deepEqual(f.state().effects,[]);
  assert.match(result.stderr,/ISOLATION_CHANGED|READERS_SCOPE_CHANGED|READERS_JOURNAL_INVALID|SPEC_CHANGED|GATE_CHANGED/);
 }
});

test('закрытый target reader оставляет legacy CAG0 и не допускает personal RESTORED/foreign proof',()=>{
 for(const profile of ['personal-integrations','personal-integrations-backup','telegram-notifications','telegram-notifications-backup','github-repository-scope','sentry-observability']){
  const f=componentCohortFixture(profile),api=f.deployments.find(d=>d.metadata.name==='control-api-gateway');
  assert.ok(componentMigrationTarget(f.target,f.value,f.observed));api.spec.replicas=3;
  const readers={state:'ISOLATED_READERS_STARTING',sourceSHA:f.observed.sourceRevision,deploymentUID:api.metadata.uid,spec:structuredClone(api.spec)};
  const allowed=profile.startsWith('personal-integrations')||profile.startsWith('telegram-notifications');
  if(allowed)assert.ok(componentMigrationTarget(f.target,f.value,{...f.observed,maintenanceReaders:readers}));
  else assert.throws(()=>componentMigrationTarget(f.target,f.value,{...f.observed,maintenanceReaders:readers}),/MAINTENANCE_REQUIRED/);
  for(const patch of [{sourceSHA:'f'.repeat(40)},{deploymentUID:'foreign'},{spec:{replicas:3}}])assert.throws(()=>componentMigrationTarget(f.target,f.value,{...f.observed,maintenanceReaders:{...readers,...patch}}),/MAINTENANCE_REQUIRED/);
  if(profile.startsWith('personal-integrations'))assert.throws(()=>componentMigrationTarget(f.target,f.value,{...f.observed,maintenanceReaders:{...readers,state:'RESTORED_READERS_READY'}}),/MAINTENANCE_REQUIRED/);
 }
});

// Разрешённая дельта потребителя создаётся в синтетическом checkout текущего
// дерева. Реальный CLI должен сохранить прежний source SHA и журнал.
test('canonical child inspect с предшественником и exact successor сохраняет source и journal bytes',t=>{
 const {source,revision,executor,executorRevision}=syntheticPersonalExecutor(t);
 const f=routineMigrationAPIFixture(t,{profile:'personal-integrations-backup',applicationSource:{path:source,revision},executorSource:executor});
 assert.equal(f.executorSHA,executorRevision);
 const evidence=join(f.directory,'migration.jsonl'),before=readFileSync(evidence);
 const refused=f.runInspect();assert.notEqual(refused.status,0);assert.match(refused.stderr,/EXECUTOR_SOURCE_REJECTED/);
 const result=f.runInspect(['--personal-executor-sha',f.executorSHA]);assert.equal(result.status,0,result.stderr+'\n'+(f.state().diagnostic??''));
 const proof=JSON.parse(result.stdout);assert.equal(proof.status,'PASS');assert.equal(proof.sourceRevision,revision);
 assert.equal(proof.personalExecutor.revision,revision);assert.equal(proof.personalExecutor.executorRevision,f.executorSHA);
 assert.deepEqual(readFileSync(evidence),before);assert.deepEqual(f.state().effects,[]);
});
