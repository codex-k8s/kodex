import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { projectWorkload, workloadPods, verifyManifest, validateCompatibility } from './component-manifest.mjs';
const hash = 'a'.repeat(64), image = `registry.example/app@sha256:${hash}`;
function fixture() {
  const container = { name: 'app', image };
  const workload = { kind: 'Deployment', metadata: { name: 'app', uid: 'deployment', generation: 2 }, spec: { replicas: 1, template: { spec: { containers: [container] } } }, status: { observedGeneration: 2 } };
  const rs = { kind: 'ReplicaSet', metadata: { uid: 'rs', ownerReferences: [{ uid: 'deployment', controller: true }] } };
  const pod = { kind: 'Pod', metadata: { name: 'app-pod', uid: 'pod', ownerReferences: [{ uid: 'rs', controller: true }] }, spec: { containers: [container] }, status: { phase: 'Running', conditions: [{ type: 'Ready', status: 'True' }], containerStatuses: [{ name: 'app', imageID: image, ready: true, state: { running: {} } }] } };
  return { workload, rs, pod };
}
function observed() {
  const { workload, rs, pod } = fixture();
  return { version: 1, profile: 'component-revisions', clusterUID: 'cluster', namespace: 'kodex-system', namespaceUID: 'namespace', components: [projectWorkload(workload, [rs,pod])], compatibility: { contract: 'v2' } };
}
test('Deployment ownership excludes same-label bootstrap Jobs', () => {
  const { workload, rs, pod } = fixture();
  const alien = structuredClone(pod); alien.metadata.uid = 'job-pod'; alien.metadata.ownerReferences[0].uid = 'job';
  assert.deepEqual(workloadPods(workload, [rs,pod,alien]), [pod]);
});
test('replacement Pod with same actual binary is allowed', () => {
  const expected = observed(), actual = structuredClone(expected); actual.components[0].pods[0].uid = 'replacement';
  assert.equal(verifyManifest(expected, actual), true);
});
for (const [name, change] of [
  ['cluster', value => value.clusterUID = 'other'], ['namespace', value => value.namespaceUID = 'other'],
  ['missing component', value => value.components = []], ['UID', value => value.components[0].uid = 'other'],
  ['spec', value => value.components[0].specSHA256 = 'b'.repeat(64)], ['imageID', value => value.components[0].images[0].imageID = 'other'],
  ['source', value => value.components[0].sources.push({ revision: 'b'.repeat(40) })], ['compatibility', value => value.compatibility.contract = 'v1'],
]) test(`rejects changed ${name}`, () => { const expected = observed(), actual = structuredClone(expected); change(actual); assert.throws(() => verifyManifest(expected,actual)); });
for (const [name, change] of [
  ['unobserved generation', f => f.workload.status.observedGeneration = 1], ['unready Pod', f => f.pod.status.conditions[0].status = 'False'],
  ['image mismatch', f => f.pod.spec.containers = [{ name: 'app', image: 'other' }]], ['unknown image ID', f => f.pod.status.containerStatuses[0].imageID = ''],
  ['missing replica', f => f.workload.spec.replicas = 2],
]) test(`rejects ${name}`, () => { const f = fixture(); change(f); assert.throws(() => projectWorkload(f.workload,[f.rs,f.pod])); });
test('hot-reload requires the running executable and exact read-only source', () => {
  const f=fixture(); Object.assign(f.workload.spec.template.spec.containers[0], { command: ['/workspace/tools/dev/run-go-hot-reload.sh'], args: ['services/internal/app','./cmd/app','app'], volumeMounts: [{ name:'source',mountPath:'/workspace',readOnly:true }] });
  f.workload.spec.template.spec.volumes = [{ name:'source',hostPath:{path:'/source'} }];
  f.pod.spec.volumes = structuredClone(f.workload.spec.template.spec.volumes);
  const inspect = () => ({ path:'/source',revision:'a'.repeat(40) });
  assert.throws(() => projectWorkload(f.workload,[f.rs,f.pod],inspect), /RUNNING_EXECUTABLE_REQUIRED/);
  assert.equal(projectWorkload(f.workload,[f.rs,f.pod],inspect,()=>hash).images[0].binarySHA256,hash);
  f.workload.spec.template.spec.containers[0].volumeMounts[0].readOnly = false;
  assert.throws(() => projectWorkload(f.workload,[f.rs,f.pod],inspect,()=>hash), /SOURCE_MOUNT_NOT_READONLY/);
});
function compatibility() {
  return { version:1, components:[{component:'Deployment/app',revisions:{source:[],imageIDs:[image]},provides:{rpc:hash},requires:[{component:'Deployment/app',contract:'rpc',acceptedSHA256:[hash]}]}],evidence:[{path:'/proof.md',sha256:createHash('sha256').update('proof').digest('hex')}] };
}
test('explicit compatible contract matrix and evidence are checked', () => assert.ok(validateCompatibility(compatibility(),observed().components,()=>Buffer.from('proof'))));
test('unsupported producer contract fails', () => { const value=compatibility(); value.components[0].requires[0].acceptedSHA256=['b'.repeat(64)]; assert.throws(()=>validateCompatibility(value,observed().components,()=>Buffer.from('proof')),/CONTRACT_INCOMPATIBLE/); });
test('missing matrix component fails', () => { const value=compatibility(); value.components=[]; assert.throws(()=>validateCompatibility(value,observed().components,()=>Buffer.from('proof')),/COMPATIBILITY_COMPONENT_SET_MISMATCH/); });
test('changed compatibility evidence fails', () => assert.throws(()=>validateCompatibility(compatibility(),observed().components,()=>Buffer.from('changed')),/COMPATIBILITY_EVIDENCE_CHANGED/));

test('old Pod source volume is rejected despite identical image', () => {
  const f=fixture(); f.workload.spec.template.spec.volumes=[{name:'source',hostPath:{path:'/new'}}]; f.pod.spec.volumes=[{name:'source',hostPath:{path:'/old'}}];
  assert.throws(()=>projectWorkload(f.workload,[f.rs,f.pod]),/POD_VOLUME_MISMATCH/);
});
test('old Pod application arguments are rejected', () => {
  const f=fixture(); f.pod.spec.containers=structuredClone(f.pod.spec.containers); f.pod.spec.containers[0].args=['old'];
  assert.throws(()=>projectWorkload(f.workload,[f.rs,f.pod]),/POD_CONTAINER_SPEC_MISMATCH/);
});

import { mkdtempSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
test('CLI captures and verifies without mutation, refuses overwrite and spec drift', () => {
  const directory=mkdtempSync(join(tmpdir(),'kodex-manifest-'));
  try {
    const f=fixture(), snapshot=join(directory,'snapshot.json'), proof=join(directory,'proof.md');
    writeFileSync(snapshot,JSON.stringify([f.workload,f.rs,f.pod])); writeFileSync(proof,'proof');
    const matrix=compatibility(); matrix.evidence[0].path=proof;
    const matrixPath=join(directory,'compatibility.json'); writeFileSync(matrixPath,JSON.stringify(matrix));
    writeFileSync(join(directory,'kubectl'), `#!${process.execPath}\nconst fs=require('node:fs'); const args=process.argv.slice(2); if(args[0]==='config') process.stdout.write('fixture'); else { if(args[0]!=='--context'||args[1]!=='fixture'||args[2]!=='get')process.exit(9); const resource=args[3]; if(resource==='namespace')process.stdout.write(JSON.stringify({metadata:{uid:args[4],labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/environment':'staging'}}})); else process.stdout.write(JSON.stringify({items:JSON.parse(fs.readFileSync(process.env.SNAPSHOT,'utf8'))})); }`,{mode:0o755});
    const manifest=join(directory,'manifest.json'), evidence=join(directory,'evidence.json');
    const execute=(...args)=>spawnSync(process.execPath,['tools/dev/component-manifest.mjs',...args],{env:{...process.env,PATH:`${directory}:${process.env.PATH}`,SNAPSHOT:snapshot},encoding:'utf8'});
    let result=execute('capture','--context','fixture','--compatibility',matrixPath,'--output',manifest); assert.equal(result.status,0,result.stderr);
    assert.equal(JSON.parse(readFileSync(manifest,'utf8')).status,'CAPTURED');
    result=execute('verify','--context','fixture','--manifest',manifest,'--output',evidence); assert.equal(result.status,0,result.stderr);
    assert.equal(JSON.parse(readFileSync(evidence,'utf8')).status,'PASS');
    assert.equal(JSON.parse(readFileSync(evidence,'utf8')).manifestSHA256,JSON.parse(readFileSync(manifest,'utf8')).manifestSHA256);
    assert.notEqual(execute('verify','--context','fixture','--manifest',manifest,'--output',evidence).status,0);
    f.workload.spec.revisionHistoryLimit=4; writeFileSync(snapshot,JSON.stringify([f.workload,f.rs,f.pod]));
    result=execute('verify','--context','fixture','--manifest',manifest,'--output',join(directory,'drift.json')); assert.notEqual(result.status,0); assert.match(result.stderr,/COMPONENT_SPECSHA/);
    assert.notEqual(execute('capture','--context','production','--compatibility',matrixPath,'--output',join(directory,'prod.json')).status,0);
  } finally { rmSync(directory,{recursive:true,force:true}); }
});

test('compatibility facts cannot be reused for another component revision', () => { const value=compatibility(); value.components[0].revisions.source=['b'.repeat(40)]; assert.throws(()=>validateCompatibility(value,observed().components,()=>Buffer.from('proof')),/COMPATIBILITY_REVISION_MISMATCH/); });
test('application and authority may use distinct exact source revisions', () => {
  const f=fixture(); const app=f.workload.spec.template.spec.containers[0];
  app.volumeMounts=[{name:'app-source',mountPath:'/workspace',readOnly:true}];
  const sidecar={...structuredClone(app),name:'authority',volumeMounts:[{name:'authority-source',mountPath:'/workspace',readOnly:true}]};
  f.workload.spec.template.spec.containers.push(sidecar);
  f.pod.spec.containers=structuredClone(f.workload.spec.template.spec.containers);
  f.workload.spec.template.spec.volumes=[{name:'app-source',hostPath:{path:'/new'}},{name:'authority-source',hostPath:{path:'/old'}}];
  f.pod.spec.volumes=structuredClone(f.workload.spec.template.spec.volumes);
  f.pod.status.containerStatuses.push({...structuredClone(f.pod.status.containerStatuses[0]),name:'authority'});
  const result=projectWorkload(f.workload,[f.rs,f.pod],path=>({path,revision:(path==='/new'?'a':'b').repeat(40)}));
  assert.deepEqual(result.sources.map(item=>item.revision),['a'.repeat(40),'b'.repeat(40)]);
});
