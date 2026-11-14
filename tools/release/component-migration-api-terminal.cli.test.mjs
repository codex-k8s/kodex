import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {join} from 'node:path';
import {routineMigrationAPIFixture} from './fixtures/routine-migration-api-synthetic.mjs';

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
 assert.equal(plan.authority.sourceCompatibility.executorSHA,f.sourceSHA);
 assert.ok(f.state().calls.some(call=>call.args.includes('--dbname=internal_rpc_authority')));
 assert.ok(f.state().calls.some(call=>call.args.includes('--dbname=control_plane')));
});
