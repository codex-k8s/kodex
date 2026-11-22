import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,writeFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {personalIntegrationsReleaseReferences,readPersonalIntegrationsReleaseReferences,bindPersonalIntegrationsDeploymentPlan} from './personal-integrations-deployment-inputs.mjs';

// Только private metadata contract planner, без выдачи actual owner grant.
function fixture(t){
 const dir=mkdtempSync(join(tmpdir(),'kodex-deployment-inputs-'));t.after(()=>rmSync(dir,{recursive:true,force:true}));
 const sourceSHA='a'.repeat(40),target={context:'k3d-synthetic',clusterUID:'synthetic-cluster',namespaceUID:'synthetic-namespace',installationUID:'synthetic-installation',installationSHA256:'b'.repeat(64)};
 const owner=(prefix,sourceSHA)=>({planPath:join(dir,prefix+'-plan'),evidencePath:join(dir,prefix+'-events'),sourceSHA});
 const references=personalIntegrationsReleaseReferences({authorityTarget:{...target,schema:'kodex.portable-runner-target/v1',resources:[]},
  material:owner('material','c'.repeat(40)),issuerMigration:owner('role','d'.repeat(40)),
  authorityImage:{publicationPlanPath:join(dir,'image-plan'),publicationEvidencePath:join(dir,'image-events'),nodeProofPath:join(dir,'nodes')},
  assets:{planPath:join(dir,'assets-plan'),evidencePath:join(dir,'assets-events')}});
 const deployment={releaseReferences:references,inputs:{schema:'kodex.personal-integrations-staged-inputs/v1',sourceSHA,cpPolicy:{sourceSHA,targetSHA256:'1'.repeat(64)}},
  prerequisites:{schema:'kodex.personal-integrations-deployment-prerequisites/v1',sourceSHA,
   owner107:{targetSHA256:'1'.repeat(64)},materialReady:{sourceSHA:references.material.sourceSHA,planSHA256:'e'.repeat(64)},
   issuerMigrationReady:{sourceSHA:references.issuerMigration.sourceSHA,evidenceSHA256:'f'.repeat(64)}}};
 const plan={version:2,kind:'CONTROL_PLANE_MIGRATION',profile:'personal-integrations-backup',migrationVersion:'20261114170000',source:{revision:sourceSHA},portableTarget:target};
 return {dir,references,deployment,plan};
}

test('исходный parent plan сохраняет все3 части до hash и не держит mutable aliases',t=>{
 const f=fixture(t);assert.equal(bindPersonalIntegrationsDeploymentPlan(f.plan,f.deployment),f.plan);
 const before=fingerprint(f.plan);
 assert.deepEqual(f.plan.personalIntegrationsPrerequisites,f.deployment.prerequisites);
 assert.deepEqual(f.plan.personalIntegrationsRelease,f.references);
 f.deployment.prerequisites.materialReady.planSHA256='changed';f.references.assets.planPath='/foreign';
 assert.equal(fingerprint(f.plan),before);
});

test('missing source/receipts/refs или другой parent profile закрывают binding',t=>{
 for(const mutate of [f=>delete f.deployment.releaseReferences,f=>delete f.deployment.prerequisites,
  f=>f.deployment.inputs.sourceSHA='b'.repeat(40),f=>f.deployment.prerequisites.materialReady.sourceSHA='b'.repeat(40),
  f=>f.plan.profile='telegram-notifications',f=>f.plan.migrationVersion='20261114160000',f=>f.plan.portableTarget.namespaceUID='foreign']){
  const f=fixture(t);mutate(f);assert.throws(()=>bindPersonalIntegrationsDeploymentPlan(f.plan,f.deployment));
  assert.equal(Object.hasOwn(f.plan,'personalIntegrationsPrerequisites'),false);
 }
});

test('private config принимает только закрытые paths/source и не переносит готовые grants',t=>{
 const f=fixture(t),targetPath=join(f.dir,'authority-target'),path=join(f.dir,'release-config');
 writeFileSync(targetPath,JSON.stringify(f.references.authorityTarget),{mode:0o600});
 const {authorityTarget,...fields}=f.references;const config={...fields,authorityTargetPath:targetPath};
 const save=()=>writeFileSync(path,JSON.stringify(config),{mode:0o600});save();
 assert.deepEqual(readPersonalIntegrationsReleaseReferences(path),f.references);
 config.ready=true;save();assert.throws(()=>readPersonalIntegrationsReleaseReferences(path),/CONFIGURATION_REJECTED/);delete config.ready;
 config.material={...config.material,planPath:join(f.dir,'x','..','material-plan')};save();
 // join нормализует заранее; только необработанный alias должен быть отвергнут.
 config.material.planPath=f.dir+'/x/../material-plan';save();assert.throws(()=>readPersonalIntegrationsReleaseReferences(path),/REFERENCES_REJECTED/);
});

test('историческое подтверждение привязано к контексту и исходной роли',t=>{
 const f=fixture(t),targetPath=join(f.dir,'authority-target'),path=join(f.dir,'release-config');
 writeFileSync(targetPath,JSON.stringify(f.references.authorityTarget),{mode:0o600});
 const {authorityTarget,...fields}=f.references;
 const ref={contextPath:join(f.dir,'historical-context'),contextSHA256:'2'.repeat(64)};
 const config={...fields,authorityTargetPath:targetPath,issuerMigrationReadiness:ref};
 const save=()=>writeFileSync(path,JSON.stringify(config),{mode:0o600});save();
 f.deployment.releaseReferences=readPersonalIntegrationsReleaseReferences(path);
 assert.deepEqual(f.deployment.releaseReferences.issuerMigrationReadiness,ref);
 const role=f.deployment.prerequisites.issuerMigrationReady;
 const proof={schema:'kodex.artifact-retention-issuer-historical-readiness/v1',purpose:'HISTORICAL_ROLE_ONLY',
  contextSHA256:ref.contextSHA256,ready:structuredClone(role)};
 assert.throws(()=>bindPersonalIntegrationsDeploymentPlan(f.plan,f.deployment),/BINDING_REJECTED/);
 f.deployment.prerequisites.issuerMigrationHistoricalReadiness=proof;
 assert.throws(()=>bindPersonalIntegrationsDeploymentPlan(f.plan,{...f.deployment,releaseReferences:f.references}),/BINDING_REJECTED/);
 proof.contextSHA256='3'.repeat(64);
 assert.throws(()=>bindPersonalIntegrationsDeploymentPlan(f.plan,f.deployment),/BINDING_REJECTED/);
 proof.contextSHA256=ref.contextSHA256;proof.ready.sourceSHA='1'.repeat(40);
 assert.throws(()=>bindPersonalIntegrationsDeploymentPlan(f.plan,f.deployment),/BINDING_REJECTED/);
 proof.ready.sourceSHA=role.sourceSHA;
 assert.equal(bindPersonalIntegrationsDeploymentPlan(f.plan,f.deployment),f.plan);
 config.issuerMigrationReadiness={...ref,ready:role};save();
 assert.throws(()=>readPersonalIntegrationsReleaseReferences(path),/HISTORICAL_REFERENCE_REJECTED/);
});
