import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { renderAuthorityPolicyEnvironment, verifyAuthorityPolicyEnvironment, readAuthorityPolicyEnvironment, verifyAuthorityPolicyEnvironmentReadback } from "./authority-policy-environment.mjs";

import {personalIntegrationsAuthorityTransition, planPersonalIntegrationsAuthorityRegistry, validatePersonalIntegrationsAuthorityPolicy, validatePersonalIntegrationsAuthorityRegistry, verifyPersonalIntegrationsAuthorityPolicy} from "./authority-personal-integrations-source.mjs";
import {authorityPolicyReadbackContract, authorityPolicySource, authorityPolicyTransitionConfiguration, isPortableAuthorityTransition, validateAuthorityPolicy} from "./authority-catalog-transition.mjs";
import {telegramRegistrySource} from "./authority-telegram-transition.mjs";

const source = readFileSync(new URL("../../deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json",import.meta.url),"utf8");
const environment = {oidcIssuer:"https://sso.fixture.test/realms/kodex",oidcAudience:"kodex-control-api"};
test("public CP config is source-bound and drift is rejected",()=>{
  const deployment={kind:"Deployment",metadata:{name:"control-plane",namespace:"kodex-system",uid:"cp-uid",resourceVersion:"15",labels:{"app.kubernetes.io/part-of":"kodex","kodex.dev/environment":"staging"}},spec:{template:{spec:{containers:[{name:"control-plane",env:[{name:"CONTROL_PLANE_OIDC_ISSUER",valueFrom:{configMapKeyRef:{name:"cp-runtime",key:"issuer"}}}]}]}}}};
  const cm={kind:"ConfigMap",metadata:{name:"cp-runtime",namespace:"kodex-system",uid:"cm-uid",resourceVersion:"9"},data:{issuer:environment.oidcIssuer}};
  const expected=readAuthorityPolicyEnvironment(deployment,[cm]);
  assert.equal(expected.oidcIssuer,environment.oidcIssuer);
  assert.equal(expected.oidcAudience,environment.oidcAudience);
  assert.deepEqual(verifyAuthorityPolicyEnvironmentReadback(expected,deployment,[cm]),expected);
  cm.metadata.resourceVersion="10";
  assert.throws(()=>verifyAuthorityPolicyEnvironmentReadback(expected,deployment,[cm]),/CONFIG_DRIFT/);
  deployment.metadata.labels["kodex.dev/environment"]="production";
  deployment.metadata.labels["kodex.dev/local-profile"]="hot-reload";
  assert.throws(()=>readAuthorityPolicyEnvironment(deployment,[cm]),/IDENTITY_REJECTED/);
});
test("render changes only three OIDC issuer fields and preserves permission policy",()=>{
  const result=renderAuthorityPolicyEnvironment(source,environment);
  assert.notEqual(result.sourceSHA256,result.renderedSHA256);
  assert.equal(result.oidcProducers,3);
  const before=JSON.parse(source),after=JSON.parse(result.raw);
  for(const producer of after.policy.authority_proof_producers) if(producer.caller_workload_id==="control-api-gateway") {
    assert.equal(producer.application_credential_issuer,environment.oidcIssuer);
    producer.application_credential_issuer="__KODEX_OIDC_ISSUER__";
  }
  assert.deepEqual(after,before);
  assert.equal(verifyAuthorityPolicyEnvironment(result.raw,environment).sha256,result.renderedSHA256);
  assert.throws(()=>verifyAuthorityPolicyEnvironment(source,environment),/UNRESOLVED_TEMPLATE/);
});
test("foreign issuer, audience and incomplete rendering are rejected",()=>{
  const rendered=renderAuthorityPolicyEnvironment(source,environment).raw;
  assert.throws(()=>verifyAuthorityPolicyEnvironment(rendered,{...environment,oidcIssuer:"https://foreign.fixture.test/realms/kodex"}));
  assert.throws(()=>renderAuthorityPolicyEnvironment(source,{...environment,oidcAudience:"foreign"}));
  assert.throws(()=>renderAuthorityPolicyEnvironment(source,{...environment,oidcIssuer:"http://sso.fixture.test/realms/kodex"}));
  assert.throws(()=>renderAuthorityPolicyEnvironment(source.replace('"control-plane.oidc"','"__KODEX_UNKNOWN__"'),environment),/UNRESOLVED_TEMPLATE/);
});

test("политика личных интеграций закрепляет 107→110 и точные байты каждого окружения",()=>{
  const sources=verifyPersonalIntegrationsAuthorityPolicy();
  assert.equal(sources.addedOperations.length,77);
  for(const [role,revision] of [["predecessor",107],["desired",110]]){
    const materialized=renderAuthorityPolicyEnvironment(sources[role].raw,environment);
    assert.equal(validatePersonalIntegrationsAuthorityPolicy(materialized.raw,role,environment).revision,revision);
    assert.equal(authorityPolicySource(role,personalIntegrationsAuthorityTransition).raw,sources[role].raw);
    assert.equal(validateAuthorityPolicy(materialized.raw,role,{profile:personalIntegrationsAuthorityTransition,environment}).revision,revision);
    assert.throws(()=>validatePersonalIntegrationsAuthorityPolicy(materialized.raw+" ",role,environment),/POLICY_RENDER_CHANGED/);
    assert.throws(()=>validatePersonalIntegrationsAuthorityPolicy(materialized.raw,role,{...environment,oidcIssuer:"https://other.example.invalid/realms/kodex"}),/POLICY_RENDER_CHANGED/);
  }
});

test("реестр личных интеграций добавляет обязательный issuer в следующий revision",()=>{
  const baseline=telegramRegistrySource("desired").raw;
  const planned=planPersonalIntegrationsAuthorityRegistry(baseline);
  assert.equal(planned.revision,planned.predecessorRevision+1);
  assert.equal(planned.registeredReadbackCount,18);
  assert.equal(planned.expectedReadbackCount,16);
  assert.equal(validatePersonalIntegrationsAuthorityRegistry(planned.raw,baseline).sha256,planned.sha256);
  assert.throws(()=>validatePersonalIntegrationsAuthorityRegistry(planned.raw+'\n',baseline),/REGISTRY_RENDER_CHANGED/);
  assert.deepEqual(authorityPolicyReadbackContract(personalIntegrationsAuthorityTransition),{
    registeredReadbackCount:18,expectedReadbackCount:16,requiredReadbackTargetsSHA256:planned.requiredReadbackTargetsSHA256,
  });
  assert.equal(authorityPolicyTransitionConfiguration(personalIntegrationsAuthorityTransition).desiredRevision,110);
  assert.equal(isPortableAuthorityTransition(personalIntegrationsAuthorityTransition),false);
  assert.throws(()=>planPersonalIntegrationsAuthorityRegistry(baseline.replace("interaction-gateway","other-gateway")),/REGISTRY_CLOSURE_REJECTED/);
  assert.throws(()=>planPersonalIntegrationsAuthorityRegistry(baseline+"\nversion: 1\n"),/REGISTRY_PARSE_REJECTED/);
});
