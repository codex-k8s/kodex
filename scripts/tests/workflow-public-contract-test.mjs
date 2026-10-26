// Публичная проверка: timeout 30s node --test scripts/tests/workflow-public-contract-test.mjs.
// Сверяет независимые HTTP/WebSocket схемы и логический ключ с грамматикой первого DAG-профиля.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = fileURLToPath(new URL("../../", import.meta.url));
const schemas = (kind) =>
  JSON.parse(
    execFileSync(
      "yq",
      [
        "-o=json",
        ".components.schemas",
        `${root}contracts/${kind}/control-api-gateway/v1/${kind}.yaml`,
      ],
      { timeout: 10000 },
    ),
  );
const http = schemas("openapi");
const websocket = schemas("asyncapi");

test("HTTP и WebSocket сохраняют один закрытый контракт истории результатов", () => {
  for (const name of ["WorkflowAcceptedResult", "WorkflowAttemptResult"]) {
    assert.deepEqual(http[name], websocket[name]);
    assert.equal(http[name].additionalProperties, false);
  }
  assert.deepEqual(
    http.RunNode.properties.workflowResults,
    websocket.RunNode.properties.workflowResults,
  );
});

test("короткие логические ключи согласованы в draft, опубликованном виде и зависимостях", () => {
  const keyRef = "#/components/schemas/WorkflowStepKey";
  for (const name of ["WorkflowStep", "WorkflowStepInput"]) {
    assert.equal(http[name].properties.ref.$ref, keyRef);
    assert.equal(http[name].properties.dependsOn.items.$ref, keyRef);
  }
  assert.equal(
    http.WorkflowResultRequirement.properties.subjectStepKey.$ref,
    keyRef,
  );
  assert.equal(http.WorkflowStepKey.minLength, 1);
  assert.equal(http.WorkflowStepKey.maxLength, 96);
  for (const flag of ["u", "v"]) {
    const pattern = new RegExp(http.WorkflowStepKey.pattern, flag);
    for (const key of ["a", "qa", "merge", "step-one_2"])
      assert.ok(pattern.test(key), key);
    for (const key of ["", "step/one", "step two", "шаг"])
      assert.equal(pattern.test(key), false, key);
  }
});
