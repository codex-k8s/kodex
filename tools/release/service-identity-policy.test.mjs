import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { buildServicePolicy, bindingDigest } from "./service-identity-policy.mjs";

const source = JSON.parse(readFileSync(new URL("../../deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json",import.meta.url),"utf8"));
const classification = JSON.parse(readFileSync(new URL("../../services/internal/control-plane/internal/app/service-identity-classification.json",import.meta.url),"utf8"));
test("workflow catalog preserves leased source binding in local service registry",() => {
  const operation="platform.runtime.execution.workflow.catalog";
  const originals=source.policy.operation_bindings.filter(binding=>binding.operation_id===operation);
  assert.equal(originals.length,1);
  const original=originals[0];
  assert.equal(original.full_method,"/controlplane.v1.RuntimeWorkService/GetExecutionWorkflowCatalog");
  assert.equal(original.permission,operation);
  assert.deepEqual(original.authority_sources,["DOMAIN_STATE"]);
  const records=classification.operations.filter(record=>record.operation_id===operation);
  assert.equal(records.length,1);
  assert.equal(records[0].actor_mode,"SERVICE_OWNER_RESOLVED");
  assert.equal(records[0].source_binding_sha256,bindingDigest(original));
  const local=buildServicePolicy(source,classification).bindings.filter(binding=>binding.operation_id===operation);
  assert.deepEqual(local,[{
    caller_spiffe_id:"spiffe://kodex.local/ns/kodex-system/sa/runtime-controller",
    full_method:original.full_method,operation_id:operation,permission:operation,
    actor_mode:"SERVICE_OWNER_RESOLVED",project_required:false,
  }]);
  const generated=JSON.parse(readFileSync(new URL("../../services/internal/control-plane/internal/app/service-identity-policy.json",import.meta.url),"utf8"));
  assert.deepEqual(generated.bindings.filter(binding=>binding.operation_id===operation),local);
  const missing=structuredClone(classification);
  missing.operations=missing.operations.filter(record=>record.operation_id!==operation);
  assert.throws(()=>buildServicePolicy(source,missing),/service operation classification drift/);
  const changed=structuredClone(source);
  changed.policy.operation_bindings.find(binding=>binding.operation_id===operation).request_profile.version="REQUIRED";
  assert.throws(()=>buildServicePolicy(changed,classification),/service operation classification drift/);
});
test("control-plane policy preserves exact bindings and excludes STT continuation",() => {
  const policy=buildServicePolicy(source,classification);
  assert.equal(policy.bindings.length,412);
  for (const operation of ["platform.role-images.admission.recovery-terminal.get", "platform.role-images.supply-work.get"]) {
    const bindings=policy.bindings.filter(value=>value.operation_id===operation);
    assert.equal(bindings.length,1);assert.equal(bindings[0].caller_spiffe_id,"spiffe://kodex.local/ns/kodex-system/sa/image-admission-controller");
    assert.equal(bindings[0].actor_mode,"SERVICE_OWNER_RESOLVED");assert.equal(bindings[0].project_required,false);
  }
  for (const operation of ["platform.role-images.admission.fail","platform.role-images.admission.expire","platform.role-images.admission.terminal.get"]) {
    const bindings = policy.bindings.filter((value) => value.operation_id === operation);
    assert.equal(bindings.length,1);
    assert.equal(bindings[0].caller_spiffe_id,"spiffe://kodex.local/ns/kodex-system/sa/image-admission");
    assert.equal(bindings[0].actor_mode,"SERVICE_OWNER_RESOLVED");
    assert.equal(bindings[0].project_required,false);
  }
  assert.equal(policy.bindings.filter(b=>b.actor_mode==="USER_CREDENTIAL_REQUIRED").length,319);
  for (const operation of [
    "platform.organization.role-images.recipes.list",
    "platform.organization.role-images.recipes.get",
    "platform.organization.role-images.recipe-revisions.list",
    "platform.organization.role-images.recipes.manage",
    "platform.command.organization.role-images.promote",
    "platform.query.organization.runtime-secrets.list",
    "platform.command.organization.runtime-secret-drafts.create",
    "platform.command.organization.runtime-environment-drafts.create",
    "platform.query.organization.role-images.vulnerability-report.get",
    "platform.query.role-images.vulnerability-report.get",
    "platform.command.organization.role-images.risk.decide",
    "platform.command.role-images.risk.decide",
  ]) {
    const bindings=policy.bindings.filter(binding=>binding.operation_id===operation);
    assert.equal(bindings.length,1);
    assert.equal(bindings[0].actor_mode,"USER_CREDENTIAL_REQUIRED");
    assert.equal(bindings[0].project_required,false);
  }
  for (const operation of ["platform.assistant.turns.add", "platform.assistant.turns.cancel"]) {
    assert.equal(policy.bindings.filter(binding=>binding.operation_id===operation&&binding.actor_mode==="USER_CREDENTIAL_REQUIRED").length,1);
  }
  for (const operation of ["platform.command.projects.trash", "platform.command.projects.restore", "platform.command.projects.purge", "platform.query.projects.trash.list"]) {
    assert.equal(policy.bindings.filter(binding=>binding.operation_id===operation&&binding.actor_mode==="USER_CREDENTIAL_REQUIRED").length,1);
  }
  assert.equal(policy.bindings.find(binding=>binding.operation_id==="platform.command.projects.trash").project_required,true);
  for (const operation of ["platform.command.projects.restore", "platform.command.projects.purge"]) {
    assert.equal(policy.bindings.find(binding=>binding.operation_id===operation).project_required,false);
  }
  assert.equal(policy.bindings.some(b=>b.operation_id==="platform.stt.policy.resolve"),false);
  for(const binding of policy.bindings) {
    const original=source.policy.operation_bindings.find(b=>b.operation_id===binding.operation_id);
    for(const key of ["caller_spiffe_id","full_method","permission","project_required"]) assert.deepEqual(binding[key],original[key]);
  }
});
test("permission changes and new methods require explicit classification",()=>{
  for(const mutate of [
    s=>{s.policy.operation_bindings.find(b=>b.target_workload_id==="control-plane").permission="expanded.permission";},
    s=>{const added=structuredClone(s.policy.operation_bindings.find(b=>b.target_workload_id==="control-plane"));added.operation_id="new.operation";s.policy.operation_bindings.push(added);},
    s=>{s.policy.operation_bindings=s.policy.operation_bindings.filter(b=>b.operation_id!==classification.operations[0].operation_id);},
  ]){const copy=structuredClone(source);mutate(copy);assert.throws(()=>buildServicePolicy(copy,classification));}
});
test("delegation cannot be downgraded even with updated source digest",()=>{
  const copy=structuredClone(classification);
  const record=copy.operations.find(r=>r.actor_mode==="PRESERVE_DELEGATION");
  record.actor_mode="SERVICE_OWNER_RESOLVED";
  record.source_binding_sha256=bindingDigest(source.policy.operation_bindings.find(b=>b.operation_id===record.operation_id));
  assert.throws(()=>buildServicePolicy(source,copy));
});
