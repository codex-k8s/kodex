import test from 'node:test';
import assert from 'node:assert/strict';
import {fingerprint} from './scoped-release.mjs';
import {verifyPersonalIntegrationsMigrationWitness} from './personal-integrations-migration-witness.mjs';

function fixture(){
 const jobName='control-plane-migrate-20261114170000';
 const plan={source:{path:'/synthetic/source',revision:'a'.repeat(40)},context:'synthetic',kubeconfig:'/synthetic/kubeconfig',profile:'personal-integrations-backup',migrationVersion:'20261114170000',job:{metadata:{name:jobName}},componentHandoff:{publicationSHA256:'b'.repeat(64)}};
 const rotation={source:plan.source.path,context:plan.context,kubeconfig:plan.kubeconfig,revision:plan.source.revision,maintenance:{cohort049:{phase:'STAGED',migrationPlanSHA256:fingerprint(plan),jobUID:'synthetic-job'}}};
 const events=[{type:'HEADER',version:1,kind:'CONTROL_PLANE_MIGRATION',planSHA256:fingerprint(plan)},
  {type:'CREATE_INTENT',jobName},{type:'CREATE_ACK',jobUID:'synthetic-job'},
  {type:'READBACK',jobName,jobUID:'synthetic-job',status:'PASS',schemaPostcondition:'PASS',sourceRevision:plan.source.revision,migrationVersion:plan.migrationVersion},
  {type:'PAIR_HANDOFF_ACK',publicationSHA256:plan.componentHandoff.publicationSHA256}];
 return {plan,rotation,events};
}

test('owned witness допускает partial readers без повторного inspect/снятия lock',()=>{
 const f=fixture();f.events.push({type:'PAIR_RESUME_INTENT',name:'artifact-retention'});
 assert.equal(verifyPersonalIntegrationsMigrationWitness(f.rotation,f).plan,f.plan);
});

test('непринятый plan/header/schema/job или missing handoff не заменяется saved READY',()=>{
 for(const mutate of [f=>f.plan.source.revision='b'.repeat(40),f=>f.events[0].planSHA256='c'.repeat(64),
  f=>f.events[3].schemaPostcondition='FAIL',f=>f.events[2].jobUID='foreign',f=>f.events.pop(),f=>f.events[0].version=2]){
  const f=fixture();mutate(f);assert.throws(()=>verifyPersonalIntegrationsMigrationWitness(f.rotation,f));
 }
});
