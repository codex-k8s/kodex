import assert from 'node:assert/strict';
import test from 'node:test';
import {readFileSync,mkdtempSync,writeFileSync,rmSync,symlinkSync,linkSync,chmodSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {catalogPolicySource,catalogPolicyTransition,validateCatalogPolicy,readCatalogTarget,verifyCatalogTransition,parseAuthorityPrivateJSON,readAuthorityPrivateJSON,openCatalogProjectionJournal,validateCatalogProjectionEvents} from './authority-catalog-transition.mjs';
import {renderAuthorityPolicyEnvironment} from './authority-policy-environment.mjs';
import {createRegistryCAS,deriveCanonicalRegistryAdvance,validateRotationPolicy,validatePredecessorPolicy,createCanonicalRotationTemplate,createRotationJob} from './authority-rotation-transition.mjs';
import {planCatalogPolicyProjection,projectionPatch,projectionMatches} from './control-plane-policy-projection.mjs';
import {fingerprint} from './scoped-release.mjs';

import {fixture,uid,image,metadata} from './fixtures/authority-catalog-synthetic.mjs';

test('105→106 opt-in validates exact rendered bytes while legacy77 stays closed',()=>{
  const f=fixture(),before=structuredClone(f.deployment);
  assert.equal(validatePredecessorPolicy(f.previous.raw,f.target.transition).revision,105);
  assert.equal(validateRotationPolicy(f.desired.raw,f.target.transition).revision,106);
  assert.throws(()=>validateRotationPolicy(f.desired.raw),/EXACT_ROTATION_POLICY_REQUIRED/);
  assert.throws(()=>validatePredecessorPolicy(f.previous.raw),/ROTATION_PREDECESSOR_POLICY_REJECTED/);
  for(const [role,raw] of [['predecessor',f.previous.raw],['desired',f.desired.raw]]){
    for(const changed of [raw+'\n',raw.replace('"policy_revision":','"unknown":true,"policy_revision":'),catalogPolicySource(role).raw]){
      assert.throws(()=>validateCatalogPolicy(changed,role,f.target.transition),/CATALOG_POLICY_ENVIRONMENT_BYTES_REJECTED/);
    }
  }
  assert.throws(()=>validateCatalogPolicy(f.desired.raw,'desired',{...f.target.transition,profile:'ARBITRARY'}),/TRANSITION_REQUIRED/);
  assert.deepEqual(f.deployment,before);
});

test('portable authority target pins installation bytes, cluster/ns/CP/publisher/registry identities',()=>{
  for(const mutate of [
    f=>{f.profile.clusterUID=uid(99);},f=>{f.profile.namespaceUID=uid(99);},f=>{f.profile.context='foreign';},
    f=>{f.installation.metadata.uid=uid(99);},f=>{f.installation.data['profile.json']='foreign';},
    f=>{f.deployment.metadata.uid=uid(99);},f=>{f.publisher.metadata.uid=uid(99);},f=>{f.registry.metadata.uid=uid(99);},
    f=>{f.deployment.metadata.labels['kodex.dev/environment']='staging';},f=>{f.profile.extra=true;},
  ]){
    const f=fixture();mutate(f);
    assert.throws(()=>readCatalogTarget(f.profile,'synthetic-production',f.get));
  }
  const f=fixture();f.deployment.metadata.resourceVersion='2';
  assert.doesNotThrow(()=>verifyCatalogTransition(f.target.transition,readCatalogTarget(f.profile,f.profile.context,f.get).transition));
  f.oidc.metadata.resourceVersion='2';
  assert.throws(()=>verifyCatalogTransition(f.target.transition,readCatalogTarget(f.profile,f.profile.context,f.get).transition),/TARGET_DRIFT/);
});

test('catalog CAS advances live delivery1→2 atomically without using policy106 as source revision',()=>{
  const f=fixture(),canonical=readFileSync(new URL('../../deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml',import.meta.url),'utf8');
  const next=deriveCanonicalRegistryAdvance(f.registry.data['key-delivery-targets.yaml'],canonical);
  const desiredData={...f.registry.data,'key-delivery-targets.yaml':next.raw,'authority-policy.json':f.desired.raw};
  const plan={policyTransition:f.target.transition,intentID:uid(8),ownerOperationID:next.identity.ownerOperationID,registry:{
    uid:f.registry.metadata.uid,resourceVersion:f.registry.metadata.resourceVersion,currentDataSHA256:fingerprint(f.registry.data),
    previousPolicySHA256:f.previous.renderedSHA256,previousPolicyRevision:105,previousSourceRevision:1,
    previousSourceDigestSHA256:next.previous.sourceDigestSHA256,policySHA256:f.desired.renderedSHA256,desiredDataSHA256:fingerprint(desiredData),desiredData,
  }};
  const before=structuredClone(f.registry),actual=createRegistryCAS(f.registry,plan);
  assert.deepEqual(f.registry,before);assert.equal(next.identity.sourceRevision,2);
  assert.equal(actual.data.unrelated,'unchanged');assert.equal(JSON.parse(actual.data['authority-policy.json']).policy_revision,106);
  assert.equal(actual.metadata.annotations['kodex.dev/rotation-operation'],next.identity.ownerOperationID);
  for(const mutate of [v=>v.metadata.uid=uid(99),v=>v.metadata.resourceVersion='2',v=>v.data.unrelated='changed',v=>v.data['authority-policy.json']+='\n']){
    const candidate=structuredClone(f.registry);mutate(candidate);assert.throws(()=>createRegistryCAS(candidate,plan));
  }
});

test('portable immutable watcher uses exact already-serving publisher image and caller checkout',()=>{
  const f=fixture(),source=new URL('../..',import.meta.url).pathname.replace(/\/$/,'');
  const template=createCanonicalRotationTemplate(source,image,true);
  const plan={version:3,intentID:uid(8),ownerOperationID:uid(9),action:'rotate',revision:'a'.repeat(40),source,policyTransition:f.target.transition};
  const job=createRotationJob(template,plan);
  assert.equal(job.spec.template.spec.containers[0].image,image);
  assert.deepEqual(job.spec.template.spec.containers[0].args,['rotation-watch','--operation-id',uid(9)]);
  assert.equal(job.metadata.labels['kodex.dev/environment'],'production');
  assert.throws(()=>createCanonicalRotationTemplate(source,image),/EXACT_CLI_IMAGE_REQUIRED/);
});

function applyPatch(resource,patch){
  const changed=structuredClone(resource);
  for(const item of patch.filter(p=>p.op==='add')){const keys=item.path.slice(1).split('/');let parent=changed;for(const key of keys.slice(0,-1))parent=parent[key];parent[keys.at(-1)]=item.value;}
  return changed;
}

test('106 projection changes only pointer arrays and preserves images, sidecars and registry',()=>{
  const f=fixture();f.registry.data['authority-policy.json']=f.desired.raw;
  const before=structuredClone({deployment:f.deployment,registry:f.registry});
  const plan=planCatalogPolicyProjection(f.deployment,f.registry,f.target.configMaps,{transition:f.target.transition,target:f.target.target});
  const changed=applyPatch(f.deployment,projectionPatch(f.deployment,plan));
  assert.equal(fingerprint(changed.spec),plan.afterSpecSHA256);
  assert.equal(changed.spec.template.spec.containers[0].image,'unchanged-application');
  assert.deepEqual(changed.spec.template.spec.containers[1],before.deployment.spec.template.spec.containers[1]);
  assert.deepEqual({deployment:f.deployment,registry:f.registry},before);
  assert.equal(projectionMatches(plan.projection,plan.projection),true);
  assert.equal(plan.projection.data['policy.json'],f.desired.raw);
  f.registry.data['authority-policy.json']=f.previous.raw;
  assert.throws(()=>planCatalogPolicyProjection(f.deployment,f.registry,f.target.configMaps,{transition:f.target.transition,target:f.target.target}),/ENVIRONMENT_BYTES_REJECTED/);
});

function existingProjection(f){
  const name=`control-plane-policy-projection-${f.previous.renderedSHA256.slice(0,16)}`;
  const old={apiVersion:'v1',kind:'ConfigMap',metadata:metadata(name,10),immutable:true,data:{'policy.json':f.previous.raw}};
  const app=f.deployment.spec.template.spec.containers[0];
  app.env.push({name:'CONTROL_PLANE_AUTHORITY_POLICY_FILE',value:'/var/run/config/kodex/control-plane/environment-policy/policy.json'});
  app.volumeMounts.push({name:'control-plane-environment-policy',mountPath:'/var/run/config/kodex/control-plane/environment-policy',readOnly:true});
  f.deployment.spec.template.spec.volumes.push({name:'control-plane-environment-policy',configMap:{name,defaultMode:292}});
  return old;
}

test('existing immutable105 projection is preserved while pointer moves forward exactly once',()=>{
  const f=fixture(),old=existingProjection(f),before=structuredClone(old);f.registry.data['authority-policy.json']=f.desired.raw;
  const plan=planCatalogPolicyProjection(f.deployment,f.registry,f.target.configMaps,{transition:f.target.transition,target:f.target.target,existingProjection:old});
  const changed=applyPatch(f.deployment,projectionPatch(f.deployment,plan));
  assert.equal(changed.spec.template.spec.volumes.filter(v=>v.name==='control-plane-environment-policy').length,1);
  assert.equal(plan.predecessor.uid,old.metadata.uid);assert.deepEqual(old,before);
  assert.notEqual(plan.projection.metadata.name,old.metadata.name);
  for(const mutate of [v=>v.immutable=false,v=>v.data['policy.json']+='\n',v=>v.metadata.name='foreign',v=>v.data.extra='foreign']){
    const candidate=structuredClone(old);mutate(candidate);
    assert.throws(()=>planCatalogPolicyProjection(f.deployment,f.registry,f.target.configMaps,{transition:f.target.transition,target:f.target.target,existingProjection:candidate}));
  }
});

test('private operator inputs reject duplicate JSON keys, links and public permissions',()=>{
  for(const raw of ['{"context":"first","context":"second"}','{"context":"first","cont\\u0065xt":"second"}','{"a":[{"x":1,"x":2}]}','{"a":1,}']){
    assert.throws(()=>parseAuthorityPrivateJSON(raw),/AUTHORITY_PRIVATE_JSON_REJECTED/);
  }
  assert.deepEqual(parseAuthorityPrivateJSON('{"a":[{"x":"}\\\"{"}],"b":true}'),{a:[{x:'}"{'}],b:true});
  const directory=mkdtempSync(join(tmpdir(),'catalog-authority-private-'));
  try {
    const path=join(directory,'profile.json');writeFileSync(path,'{"scope":"synthetic"}',{mode:0o600});
    assert.deepEqual(readAuthorityPrivateJSON(path),{scope:'synthetic'});
    const symbolic=join(directory,'symbolic');symlinkSync(path,symbolic);assert.throws(()=>readAuthorityPrivateJSON(symbolic));
    const hard=join(directory,'hard');linkSync(path,hard);assert.throws(()=>readAuthorityPrivateJSON(path),/PRIVATE_INPUT_REQUIRED/);
    rmSync(hard);chmodSync(path,0o644);assert.throws(()=>readAuthorityPrivateJSON(path),/PRIVATE_INPUT_REQUIRED/);
  } finally {rmSync(directory,{recursive:true,force:true});}
});

test('projection journal survives two UNKNOWN ACKs and binds every resumed record to one plan and UID',()=>{
  const directory=mkdtempSync(join(tmpdir(),'catalog-authority-journal-')),path=join(directory,'receipt.jsonl'),plan={kind:'synthetic-plan',nonce:uid(15)};
  try {
    let journal=openCatalogProjectionJournal(path,plan);
    journal.append({status:'INTENT',operation:'APPLY'});
    journal.append({status:'INTENT',operation:'CREATE_PROJECTION'});
    journal.append({status:'UNKNOWN',operation:'CREATE_PROJECTION'});journal.close();
    journal=openCatalogProjectionJournal(path,plan,true);
    assert.equal(journal.events.length,3);
    journal.append({status:'INTENT',operation:'RESUME'});
    journal.append({status:'ACK',operation:'CREATE_PROJECTION',uid:uid(20)});
    journal.append({status:'INTENT',operation:'CP_POINTER_CAS'});
    journal.append({status:'UNKNOWN',operation:'CP_POINTER_CAS'});journal.close();
    journal=openCatalogProjectionJournal(path,plan,true);
    journal.append({status:'ACK',operation:'CP_POINTER_CAS',uid:uid(3)});
    const events=structuredClone(journal.events);journal.close();
    assert.equal(validateCatalogProjectionEvents(events,plan).get('CREATE_PROJECTION'),uid(20));
    assert.equal(validateCatalogProjectionEvents(events,plan).get('CP_POINTER_CAS'),uid(3));
    assert.throws(()=>openCatalogProjectionJournal(path,{...plan,nonce:uid(99)},true),/JOURNAL_DRIFT/);
    for(const change of [rows=>rows[1].planSHA256='0'.repeat(64),rows=>rows[1].status='PASS',rows=>rows[1].operation='ARBITRARY',rows=>rows[1].extra=true,
      rows=>rows.push({...rows[4],uid:uid(99)}),rows=>rows.unshift(rows.pop())]) {
      const changed=structuredClone(events);change(changed);assert.throws(()=>validateCatalogProjectionEvents(changed,plan),/JOURNAL_DRIFT/);
    }
  } finally {rmSync(directory,{recursive:true,force:true});}
});
