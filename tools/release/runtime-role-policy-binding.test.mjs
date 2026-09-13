import test from "node:test";
import assert from "node:assert/strict";
import { planRuntimeRoleBinding, planRuntimeRoleBindingForRunner, runtimeRoleRunner, sameRuntimeRoleBindingPlan } from "./runtime-role-policy-binding.mjs";

const old = "kodex-image-admission-policy-old", active = "kodex-image-admission-policy-new";
const runner = `pull.kodex.works/kodex/agent-runner@sha256:${"a".repeat(64)}`;
const deployment = (name, key, value) => ({ spec: { template: { spec: { containers: [{ name, env: [{ name: key, value }] }] } } } });
function values() {
  return {
    controller: deployment("image-admission-controller", "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP", active),
    runtime: deployment("runtime-controller", "RUNTIME_CONTROLLER_DEFAULT_ROLE_IMAGE_REFERENCE", runner),
    policy: { apiVersion: "v1", kind: "ConfigMap", metadata: { name: active, namespace: "kodex-system" }, immutable: true, data: { nodeReadbackImage: runner } },
    binding: { apiVersion: "admissionregistration.k8s.io/v1", kind: "ValidatingAdmissionPolicyBinding", metadata: { name: "runtime-role-pod-exact-secret-projection", uid: "11111111-1111-4111-8111-111111111111", resourceVersion: "7" }, spec: { policyName: "runtime-role-pod-exact-secret-projection", validationActions: ["Deny"], paramRef: { name: old, namespace: "kodex-system", parameterNotFoundAction: "Deny" } } },
  };
}

test("план меняет только exact runtime binding и сохраняет Deny", () => { const v = values(), plan = planRuntimeRoleBinding(v.controller, v.runtime, v.policy, v.binding, "22222222-2222-4222-8222-222222222222"); assert.equal(plan.changed, true); assert.equal(plan.patch.at(-1).value.paramRef.name, active); assert.deepEqual(plan.patch.at(-1).value.validationActions, ["Deny"]); assert.equal(v.binding.spec.paramRef.name, old); });
test("несовпадающий runner и открытый binding отклоняются", () => { const v = values(); v.policy.data.nodeReadbackImage = runner.replace("a", "b"); assert.throws(() => planRuntimeRoleBinding(v.controller, v.runtime, v.policy, v.binding)); const x = values(); x.binding.spec.validationActions = ["Warn"]; assert.throws(() => planRuntimeRoleBinding(x.controller, x.runtime, x.policy, x.binding)); });
test("apply принимает неизменный plan с context и отвергает CAS drift", () => { const v = values(), current = planRuntimeRoleBinding(v.controller, v.runtime, v.policy, v.binding), saved = { ...current, context: "default" }; assert.doesNotThrow(() => sameRuntimeRoleBindingPlan(saved, current, "default")); current.resourceVersion = "8"; assert.throws(() => sameRuntimeRoleBindingPlan(saved, current, "default"), /PRECONDITION/); });
test("план может заранее связать policy с exact следующим runner", () => { const v = values(), next = runner.replace("a".repeat(64), "b".repeat(64)); v.policy.data.nodeReadbackImage = next; const plan = planRuntimeRoleBindingForRunner(v.controller, next, v.policy, v.binding); assert.equal(plan.runner, next); assert.equal(plan.patch.at(-1).value.paramRef.name, active); });
test("apply использует runner из сохранённого плана без повторного аргумента", () => { const v = values(), next = runner.replace("a".repeat(64), "b".repeat(64)); assert.equal(runtimeRoleRunner("apply", {}, v.runtime, { runner: next }), next); assert.throws(() => runtimeRoleRunner("apply", { "--runner-reference": next }, v.runtime, { runner: next }), /APPLY_ARGUMENTS_INVALID/); });
