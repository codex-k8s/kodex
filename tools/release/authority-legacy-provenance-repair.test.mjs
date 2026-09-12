import assert from 'node:assert/strict';
import test from 'node:test';
import {verifyRepairJob} from './authority-legacy-provenance-repair.mjs';

test('repair readback preserves proof binding, exact spec and excludes only controller labels',()=>{
 const expected={metadata:{name:'authority-legacy-repair-fixture',namespace:'kodex-system',annotations:{'kodex.dev/legacy-repair-proof':'a'.repeat(64)}},spec:{template:{metadata:{labels:{app:'migrator'}},spec:{containers:[{name:'migrate',image:'registry/cli@sha256:'+'b'.repeat(64),args:['legacy-provenance-repair'],volumeMounts:[{name:'proof',readOnly:true}]}]}}}};
 const actual=structuredClone(expected);actual.metadata.uid='14650000-0000-4000-8000-000000000001';actual.spec.selector={matchLabels:{'controller-uid':actual.metadata.uid}};actual.spec.template.metadata.labels['controller-uid']=actual.metadata.uid;
 verifyRepairJob(actual,expected);
 for(const mutate of [v=>v.metadata.annotations['kodex.dev/legacy-repair-proof']='c'.repeat(64),v=>v.spec.template.spec.containers[0].args=['up'],v=>v.spec.template.spec.containers[0].volumeMounts[0].readOnly=false,v=>v.spec.template.spec.unknown=true,v=>v.metadata.namespace='foreign']){
  const changed=structuredClone(actual);mutate(changed);assert.throws(()=>verifyRepairJob(changed,expected));
 }
});
