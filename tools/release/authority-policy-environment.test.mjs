import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { renderAuthorityPolicyEnvironment, verifyAuthorityPolicyEnvironment, readAuthorityPolicyEnvironment, verifyAuthorityPolicyEnvironmentReadback } from "./authority-policy-environment.mjs";

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
