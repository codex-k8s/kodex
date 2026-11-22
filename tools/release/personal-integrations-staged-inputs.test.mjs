import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {componentCohortFixture,replaceComponentLock} from './fixtures/component-cohort-synthetic.mjs';
import {planComponentMigrationHandoff,componentMigrationPublication,componentMigrationConfiguration} from './portable-component-migration.mjs';
import {validateComponentPublicationConfiguration} from './component-publication.mjs';

const source={revision:'a'.repeat(40)},authorityImage='registry.example.invalid/kodex/internal-rpc-authority@sha256:'+'b'.repeat(64);
import {personalIntegrationsStagedInputsFixture as fixture} from './fixtures/personal-integrations-staged-inputs-synthetic.mjs';

// Это чистая derivation двух явных template дельт. Источники inputs и authority
// image при effect проверяет production reader, а не этот synthetic объект.


test('cohort передаёт CP/ART template delta и штатный shutdown budget integration gateway',()=>{
 const f=fixture(),saved=structuredClone(f.deployments),handoff=f.read();assert.deepEqual(f.deployments,saved);
 const cp=handoff.components.find(p=>p.name==='control-plane'),art=handoff.components.find(p=>p.name==='artifact-retention');
 assert.ok(cp.stagedSpec.template.spec.containers[0].env.some(e=>e.name==='CONTROL_PLANE_ARTIFACT_RETENTION_GRANT_TRUST_FILE'));
 assert.equal(cp.stagedSpec.template.spec.volumes.at(-1).emptyDir.sizeLimit,'512Mi');
 assert.deepEqual(art.stagedSpec.template.spec.containers.map(c=>c.name),['artifact-retention','internal-rpc-authority-issuer','platform-worker-grant-agent']);
 assert.equal(art.stagedSpec.template.spec.containers[1].image,authorityImage);assert.equal(art.stagedSpec.replicas,0);
 assert.equal(art.stoppedSpec.template.spec.containers.length,1);assert.equal(art.resumedSpec.replicas,2);
 for(const part of handoff.components.filter(p=>!['control-plane','artifact-retention'].includes(p.name))){
  const before=structuredClone(part.beforeSpec.template.spec),staged=structuredClone(part.stagedSpec.template.spec);
  if(part.name==='integration-gateway'){
   const lifecycle=readFileSync(new URL('../../deploy/k8s/base/integration-gateway/lifecycle-patch.yaml',import.meta.url),'utf8');
   const grace=Number(lifecycle.match(/^\s*terminationGracePeriodSeconds:\s+(\d+)$/m)?.[1]);
   assert.equal(grace,60);
   before.terminationGracePeriodSeconds=Math.max(grace,before.terminationGracePeriodSeconds??30);
   assert.equal(part.stoppedSpec.template.spec.terminationGracePeriodSeconds,part.beforeSpec.template.spec.terminationGracePeriodSeconds);
  }
  staged.containers[0].image=before.containers[0].image;assert.deepEqual(staged,before);
 }
 for(const mutate of [f=>f.inputs.sourceSHA='e'.repeat(40),f=>f.inputs.authority.sourceSHA='e'.repeat(40),
  f=>f.inputs.authority.imageRef='registry.example.invalid/mutable:latest',f=>f.inputs.authority.publicationSHA256='bad']){
  const f=fixture();mutate(f);assert.throws(f.read);
 }
});

test('cohort сохраняет ранее увеличенный grace integration gateway',()=>{
 const f=fixture();
 const gateway=f.deployments.find(part=>part.metadata.name==='integration-gateway');
 gateway.spec.template.spec.terminationGracePeriodSeconds=90;
 const part=f.read().components.find(value=>value.name===gateway.metadata.name);
 assert.equal(part.stagedSpec.template.spec.terminationGracePeriodSeconds,90);
});

test('historical install authority head и actual00200 сохраняются раздельно в producer/consumer',()=>{
 for(const profile of ['personal-integrations','personal-integrations-backup']){
  const f=componentCohortFixture(profile),cfg=componentMigrationConfiguration(profile);
  const config={schema:'kodex.component-publication-config/v1',profile,sourceSHA:source.revision,sourceCheckout:'/synthetic/source',context:'k3d-synthetic',
   clusterUID:f.value.clusterUID,namespaceUID:f.value.namespaceUID,baseSourceSHA:f.value.baseSourceSHA,baseReleaseLockSHA256:f.value.baseReleaseLockSHA256,
   baseReleaseLock:'/synthetic/base-lock',baseSchemaHeads:f.value.baseSchemaHeads,schemaHeadsBefore:f.value.schemaHeadsBefore,schemaHeadsAfter:f.value.schemaHeadsAfter,
   nodes:[{name:'k3d-synthetic-server-0',uid:f.value.nodeUIDs[0]}],components:cfg.components.map(name=>({name,
    expectedImageRef:`registry.local.kodex/kodex/${name}@sha256:${'f'.repeat(64)}`,anchor:cfg.sources[name]+'internal/synthetic.go',
    buildReceipt:'/synthetic/'+name+'/build',publicationPlan:'/synthetic/'+name+'/publication',publicationEvidence:'/synthetic/'+name+'/events'}))};
  assert.equal(validateComponentPublicationConfiguration(config).components.length,9);
  assert.equal(componentMigrationPublication(f.value,source,profile).components.length,9);
  const newBase=structuredClone(config);newBase.baseSchemaHeads['control-plane']=20261114140000;assert.equal(validateComponentPublicationConfiguration(newBase).components.length,9);
  for(const mutate of [x=>x.schemaHeadsBefore['internal-rpc-authority']=20261115000100,
   x=>x.schemaHeadsAfter['internal-rpc-authority']=20261115000100,x=>x.baseSchemaHeads['internal-rpc-authority']=123,
   x=>x.schemaHeadsAfter['email-bridge']++]){
   const bad=structuredClone(config);mutate(bad);assert.throws(()=>validateComponentPublicationConfiguration(bad));
   const badValue=structuredClone(f.value);mutate(badValue);
   for(const part of badValue.components){const lock=JSON.parse(Buffer.from(part.componentLockRawBase64,'base64'));lock.schema_heads=badValue.schemaHeadsAfter;replaceComponentLock(part,lock);}
   assert.throws(()=>componentMigrationPublication(badValue,source,profile));
  }
 }
});
