import test from 'node:test';
import assert from 'node:assert/strict';
import { planOutage, policyPatch, selectorMatches } from './authority-freshness-outage.mjs';
const pod={metadata:{name:'api-1',uid:'pod1',namespace:'kodex-system',labels:{'app.kubernetes.io/name':'control-api-gateway','kodex.dev/internal-rpc-authority-issuer':'enabled'}}};
const ns={metadata:{name:'kodex-system',uid:'ns1',labels:{'kodex.dev/environment':'staging','kubernetes.io/metadata.name':'kodex-system'}}};
const peer=name=>({podSelector:{matchLabels:{'app.kubernetes.io/name':name}}});
function policy(name='exact'){return {metadata:{name,uid:name,resourceVersion:'1'},spec:{podSelector:{matchLabels:{'app.kubernetes.io/name':'control-api-gateway'}},policyTypes:['Egress'],egress:[{ports:[{port:8443,protocol:'TCP'}],to:[peer('control-plane'),peer('internal-rpc-authority-readback-attestor')]},{ports:[{port:5432,protocol:'TCP'}],to:[peer('kodex-postgresql')]}]}};}
test('remove every additive target allow while preserving unrelated paths and immutable before',()=>{
 const policies=[policy('one'),policy('two')],before=structuredClone(policies),plan=planOutage(policies,pod,ns,'attestor');
 assert.equal(plan.changes.length,2);assert.deepEqual(policies,before);
 for(const change of plan.changes){assert.deepEqual(change.after.egress[0].to,[peer('control-plane')]);assert.deepEqual(change.after.egress[1],change.before.egress[1]);}
 const db=planOutage(policies,pod,ns,'database');assert.equal(db.changes[0].after.egress.length,1);
 assert.ok(db.changes.every(change=>change.after.egress.every(rule=>rule.to.length>0)));
});
test('broad/alternative egress and shared owner fail closed',()=>{
 for(const mutate of [p=>p.spec.egress.push({}),p=>p.spec.egress[0].to.push({}),p=>p.spec.egress[0].to.push({ipBlock:{cidr:'0.0.0.0/0'}}),p=>p.spec.podSelector={}]) {
  const p=policy();mutate(p);assert.throws(()=>planOutage([p],pod,ns,'attestor'));
 }
 const foreign=structuredClone(ns);foreign.metadata.labels['kodex.dev/environment']='production';assert.throws(()=>planOutage([policy()],pod,foreign,'attestor'));
});
test('CAS patch and restoration reject identity/spec drift; no inverse overwrite',()=>{
 const p=policy(),change=planOutage([p],pod,ns,'attestor').changes[0],patch=policyPatch(p,change,'apply');
 assert.deepEqual(patch.slice(0,2),[{op:'test',path:'/metadata/uid',value:'exact'},{op:'test',path:'/metadata/resourceVersion',value:'1'}]);
 const mutated={...p,metadata:{...p.metadata,resourceVersion:'2'},spec:change.after};
 assert.deepEqual(policyPatch(mutated,change,'restore').at(-1).value,p.spec);
 assert.throws(()=>policyPatch({...mutated,metadata:{...mutated.metadata,uid:'replacement'}},change,'restore'));
 assert.throws(()=>policyPatch({...mutated,spec:{...mutated.spec,ingress:[]}},change,'restore'));
});
test('Kubernetes selector expression semantics and foreign namespaces remain exact',()=>{
 assert.equal(selectorMatches({matchExpressions:[{key:'absent',operator:'NotIn',values:['x']}]},{}),true);
 assert.equal(selectorMatches({matchExpressions:[{key:'absent',operator:'Exists'}]},{}),false);
 assert.throws(()=>selectorMatches({matchExpressions:[{key:'x',operator:'GreaterThan',values:['1']}]},{}));
 const p=policy();p.spec.egress[0].to.push({namespaceSelector:{matchLabels:{'kubernetes.io/metadata.name':'other'}},podSelector:{}});
 assert.equal(planOutage([p],pod,ns,'attestor').changes.length,1);
});
