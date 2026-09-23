import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { buildServicePolicy, bindingDigest } from "./service-identity-policy.mjs";

const source = JSON.parse(readFileSync(new URL("../../deploy/k8s/base/internal-rpc-authority-publisher/authority-policy.json",import.meta.url),"utf8"));
const classification = JSON.parse(readFileSync(new URL("../../services/internal/control-plane/internal/app/service-identity-classification.json",import.meta.url),"utf8"));
test("control-plane policy preserves exact bindings and excludes STT continuation",() => {
  const policy=buildServicePolicy(source,classification);
  assert.equal(policy.bindings.length,380);
  assert.equal(policy.bindings.filter(b=>b.actor_mode==="USER_CREDENTIAL_REQUIRED").length,294);
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
