import test from 'node:test';
import assert from 'node:assert/strict';
import { portableRunnerTarget } from './portable-runner-target.mjs';
import { requireStaging } from './runner-policy-model.mjs';
import { fingerprint } from './scoped-release.mjs';
const uid = '11111111-1111-4111-8111-111111111111';
function fixture() {
  const installation = {apiVersion:'v1',kind:'ConfigMap',metadata:{name:'kodex-installation-profile',namespace:'kodex-system',uid,labels:{'app.kubernetes.io/part-of':'kodex','app.kubernetes.io/managed-by':'kodex-install'}},data:{'profile.json':'synthetic'}};
  const resource = {apiVersion:'apps/v1',kind:'Deployment',metadata:{name:'control-plane',namespace:'kodex-system',uid,resourceVersion:'1',labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/profile':'web-only','kodex.dev/environment':'production'}}};
  const profile = {schema:'kodex.portable-runner-target/v1',context:'synthetic',clusterUID:uid,namespaceUID:uid,installationUID:uid,installationSHA256:fingerprint(installation.data),resources:[{apiVersion:resource.apiVersion,kind:resource.kind,name:resource.metadata.name,uid}]};
  return {profile,resource,live:{context:'synthetic',clusterUID:uid,namespaceUID:uid,installation}};
}
test('явный portable scope допускает только закреплённый UID без изменения labels',()=>{
  const {profile,resource,live}=fixture(),before=structuredClone(resource),target=portableRunnerTarget(profile,live);
  assert.throws(()=>requireStaging(resource)); assert.doesNotThrow(()=>requireStaging(resource,target));assert.deepEqual(resource,before);
  for(const edit of [r=>r.metadata.uid='22222222-2222-4222-8222-222222222222',r=>r.metadata.namespace='foreign',r=>r.metadata.name='other',r=>r.metadata.deletionTimestamp='now',r=>r.metadata.labels['app.kubernetes.io/part-of']='foreign',r=>r.metadata.labels['kodex.dev/environment']='staging']){
    const changed=structuredClone(resource);edit(changed);assert.throws(()=>requireStaging(changed,target));
  }
});
test('scope закрывает подмену cluster, namespace, установки, повторные ресурсы и лишние поля',()=>{
  for(const edit of [p=>p.clusterUID='22222222-2222-4222-8222-222222222222',p=>p.namespaceUID='22222222-2222-4222-8222-222222222222',p=>p.context='other',p=>p.installationSHA256='0'.repeat(64),p=>p.resources.push(p.resources[0]),p=>p.extra=true]){
    const {profile,live}=fixture();edit(profile);assert.throws(()=>portableRunnerTarget(profile,live));
  }
});
