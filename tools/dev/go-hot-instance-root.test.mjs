import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

const script = fileURLToPath(new URL("./go-hot-instance-root.sh", import.meta.url));
const root = (name, uid) => execFileSync("sh", [script, name, uid], { encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
test("two replicas have different Air configs and output binaries while sharing Go cache", () => {
  const first = root("control-plane", "00000000-0000-4000-8000-000000000001");
  const second = root("control-plane", "00000000-0000-4000-8000-000000000002");
  assert.notEqual(first, second); assert.match(first, /^\/go\/build-cache\/runtime-control-plane-/);
  assert.equal(root("control-plane", ""), "/go/build-cache/runtime-control-plane");
});
test("runtime identity cannot escape cache path or interpolate shell", () => {
  for (const uid of ["../foreign", "$(id)", "foreign/name", "ABC", "a\nb"]) assert.throws(() => root("control-plane", uid));
  for (const name of ["../foreign", "a/b", "$(id)", "a b", ""]) assert.throws(() => root(name, ""));
});
