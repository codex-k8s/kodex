import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {join} from 'node:path';
import {routineMigrationAPIFixture} from './fixtures/routine-migration-api-synthetic.mjs';
import {prepareArtifactCatalogPrerequisites} from './fixtures/artifact-catalog-authority-synthetic.mjs';

function stage(t) {
 return routineMigrationAPIFixture(t,{profile:'personal-integrations-backup',stagedArtifactCatalog:true});
}

function evidence(fixture) {
 return readFileSync(join(fixture.directory,'migration.jsonl'),'utf8');
}

function successful(result,fixture) {
 assert.equal(result.status,0,result.stderr+'\n'+(fixture.state().diagnostic??''));
 return JSON.parse(result.stdout);
}

test('ART050 CLI планирует Job из переданного STAGED cohort, сохраняя журнал',t=>{
 const f=stage(t);
 const before=evidence(f);
 const result=successful(f.runArtifactCatalog('plan'),f);
 assert.equal(result.status,'PLAN_ONLY');
 assert.equal(result.jobName,'control-plane-migrate-20261121000100');
 assert.equal(result.imageRef,f.migration.job.spec.template.spec.containers[0].image);
 assert.match(result.planSHA256,/^[a-f0-9]{64}$/);
 assert.equal(evidence(f),before);
 const observed=f.state();
 assert.equal(observed.calls.filter(({args})=>args.includes('create')&&args.includes('--dry-run=server')).length,1);
 assert.deepEqual(observed.effects,[]);
 assert.equal(observed.artifactJob,undefined);
});

test('ART050 CLI закрывает план при активном backup producer до Job dry-run',t=>{
 const f=stage(t);
 const before=evidence(f);
 f.edit(state=>{state.otherBackupProducer={kind:'CronJob',metadata:{
  name:'backup-controller-unconfirmed',namespace:'kodex-system',uid:'00000000-0000-4000-8000-000000000467',
 },spec:{}};});
 const result=f.runArtifactCatalog('plan');
 assert.notEqual(result.status,0);
 assert.match(result.stderr,/BACKUP_MAINTENANCE_OTHER_PRODUCER_PRESENT/);
 assert.equal(evidence(f),before);
 assert.equal(f.state().calls.some(({args})=>args.includes('create')),false);
});

test('ART050 CLI не создаёт Job без подтверждённых deployment prerequisites',t=>{
 const f=stage(t);
 const plan=successful(f.runArtifactCatalog('plan'),f);
 const result=f.runArtifactCatalog('apply',['--artifact-grant-catalog-plan-sha',plan.planSHA256]);
 assert.notEqual(result.status,0);
 assert.match(result.stderr,/PERSONAL_INTEGRATIONS_RELEASE_REFERENCES_REJECTED/);
 assert.deepEqual(f.state().effects,[]);
 assert.equal(evidence(f).includes('PERSONAL_ARTIFACT_GRANT_CATALOG_CREATE_INTENT'),false);
});

test('ART050 CLI применяет точный план и восстанавливает квитанцию при потерянном ACK',async t=>{
 const f=await routineMigrationAPIFixture(t,{profile:'personal-integrations-backup',stagedArtifactCatalog:true,
  prepareStagedArtifactCatalog:context=>prepareArtifactCatalogPrerequisites(t,context)});
 const prerequisiteWrites=f.state().artifactPrerequisites.writes;
 const planned=successful(f.runArtifactCatalog('plan'),f);
 f.edit(state=>{state.artifactLostACK=true;});
 const lost=f.runArtifactCatalog('apply');
 assert.notEqual(lost.status,0,lost.stderr);
 assert.match(lost.stderr,/PERSONAL_ARTIFACT_GRANT_CATALOG_CREATE_UNKNOWN/,f.state().diagnostic);
 assert.deepEqual(f.state().effects,['artifact-grant-catalog']);
 const applied=successful(f.runArtifactCatalog('apply'),f);
 assert.equal(applied.status,'ARTIFACT_GRANT_CATALOG_APPLIED');
 assert.equal(applied.schemaVersion,20261121000100);
 assert.equal(applied.planSHA256,planned.planSHA256);
 assert.equal(applied.jobUID,f.state().artifactJob.metadata.uid);
 assert.deepEqual(f.state().artifactPrerequisites.writes,prerequisiteWrites);
 const inspected=successful(f.runInspect(),f);
 assert.equal(inspected.cpStage,'STAGED');
 assert.equal(inspected.schemaPostcondition,'PASS');
 assert.equal(inspected.servingSchema.jobUID,applied.jobUID);
 assert.equal(inspected.servingSchema.version,applied.schemaVersion);
 const state=f.state();
 assert.deepEqual(state.effects,['artifact-grant-catalog']);
 assert.equal(state.calls.filter(({args})=>args.includes('create')&&!args.includes('--dry-run=server')).length,1);
 assert.equal(state.apps['backup-controller'].spec.replicas,0);
 assert.equal(state.apps['control-api-gateway'].spec.replicas,0);
});
