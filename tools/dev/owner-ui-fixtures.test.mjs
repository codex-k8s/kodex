import assert from "node:assert/strict";
import test from "node:test";
import {
  ownerInputRequest,
  projectSecretInputProfile,
  projectSecretOwnerProof,
} from "./test-support/owner-ui-fixtures.mjs";

test("профили и вложенные proof не разделяют изменяемое состояние", () => {
  const profile = projectSecretInputProfile();
  const other = projectSecretInputProfile();
  assert.deepEqual(profile, other);
  assert.notEqual(profile, other);
  const first = projectSecretOwnerProof(profile);
  const second = projectSecretOwnerProof(profile);
  assert.deepEqual(first, second);
  first.environment.ready = false;
  first.binding.version++;
  first.assignedAgents.items[0].system = true;
  first.assignedAgents.items.push({ ref: "agt_foreign" });
  first.impact.consumers.push({ agentRef: "agt_foreign" });
  assert.deepEqual(second, projectSecretOwnerProof(other));
  profile.agentVersion++;
  assert.equal(
    projectSecretOwnerProof(profile).agentVersion,
    profile.agentVersion,
  );
  assert.equal(second.agentVersion, other.agentVersion);
  assert.equal(
    projectSecretInputProfile("https://fixture.invalid").pageURL,
    "https://fixture.invalid/projects/prj_synthetic/secrets",
  );
});

test("HTTP-оснастка сохраняет точные байты и полностью заменяет неверные поля", async () => {
  const body = Buffer.from("synthetic-body");
  const options = {
    host: "fixture.invalid",
    origin: "https://fixture.invalid",
    path: "/fixture",
    port: 23456,
    body,
  };
  const request = ownerInputRequest(options);
  assert.equal(request.socket.getProtocol(), "TLSv1.3");
  assert.equal(request.socket.remoteAddress, "127.0.0.1");
  assert.equal(request.socket.localAddress, "127.0.0.2");
  assert.equal(request.headers["content-length"], String(body.length));
  assert.equal(request.url, options.path);
  assert.equal(request.headers.host, "fixture.invalid:23456");
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  assert.deepEqual(Buffer.concat(chunks), body);
  const socket = { destroyed: true };
  const headers = { "content-length": "invalid" };
  const changed = ownerInputRequest({
    ...options,
    changes: { socket, headers },
  });
  assert.equal(changed.socket, socket);
  assert.equal(changed.headers, headers);
  assert.deepEqual(changed.rawHeaders, ["content-length", "invalid"]);
  assert.equal(changed.headers.host, undefined);
  assert.notEqual(ownerInputRequest(options).headers, request.headers);
});
