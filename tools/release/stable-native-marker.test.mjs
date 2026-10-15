import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import test from "node:test";

const image = process.env.KODEX_NODE_REGISTRY_FIXTURE_IMAGE;
const worker = readFileSync(new URL("./stable-native-marker-worker.sh", import.meta.url), "utf8");
const setup = `set -eu
root=/var/lib/rancher/k3s/server/manifests
mkdir -p "$root/metrics-server"
printf 'fixture source\\n' >"$root/coredns.yaml"
printf 'fixture metrics source\\n' >"$root/metrics-server/metrics-server-deployment.yaml"
chmod 0644 "$root/coredns.yaml" "$root/metrics-server/metrics-server-deployment.yaml"
worker=$1
plan=${"a".repeat(64)}
intent=${"b".repeat(64)}
source=$root/coredns.yaml
marker=$source.skip
staging=$root/.kodex-native-coredns-$plan.stage.skip
source_hash=$(sha256sum "$source" | cut -d ' ' -f 1)
source_metadata=$(stat -c '%u:%g:%a:%h:%d:%i:%s' "$source")
call() { sh -c "$worker" native-marker "$1" coredns "$plan" "$intent" "$source_hash" "$source_metadata" "${"${2:-}"}"; }
`;
function run(t, script) {
  assert.match(image, /^sha256:[a-f0-9]{64}$/);
  const name = "kodex-native-marker-fixture-" + randomUUID();
  try {
    return execFileSync("docker", ["run", "--rm", "--name", name, "--label", "kodex.dev/fixture=native-marker",
      "--network", "none", "--read-only", "--user", "0:0", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
      "--pids-limit", "32", "--memory", "64m", "--tmpfs", "/var/lib/rancher/k3s/server/manifests:rw,noexec,nosuid,nodev,size=1m,mode=700",
      "--entrypoint", "/bin/sh", image, "-ec", setup + script, "kodex-native-fixture", worker],
    { encoding: "utf8", timeout: 30000, stdio: ["ignore", "pipe", "pipe"] });
  } catch (error) {
    if (error.status === null) {
      try {
        if (execFileSync("docker", ["inspect", "--format", "{{.State.Running}}", name], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim() === "true")
          t.diagnostic("Unknown running exact fixture preserved: " + name);
      } catch { /* --rm мог уже завершить только этот fixture. */ }
    }
    throw Error("NATIVE_MARKER_FIXTURE_FAILED", { cause: error.status });
  }
}
const options = { skip: !image };

test("real marker worker publishes exclusively, inspects and removes only its receipt", options, t => {
  const output = run(t, `
receipt=$(call install)
test "$(call inspect "$receipt")" = "$receipt"
if call install >/dev/null 2>&1; then exit 21; fi
test "$(sha256sum "$source" | cut -d ' ' -f 1)" = "$source_hash"
test "$(call remove "$receipt")" = REMOVED
test "$(call inspect)" = ABSENT
test ! -e "$root/.kodex-stable-native-lock"
printf PASS
`);
  assert.equal(output, "PASS");
});

test("real marker worker rejects replaced inode, symlinks and unknown component", options, t => {
  assert.equal(run(t, `
receipt=$(call install)
mv "$marker" "$marker.old"
cp "$marker.old" "$marker"
chmod 0600 "$marker"
if call remove "$receipt" >/dev/null 2>&1; then exit 22; fi
test -e "$marker"
rm "$marker"
ln -s "$source" "$marker"
if call inspect >/dev/null 2>&1; then exit 23; fi
if sh -c "$worker" native-marker install foreign "$plan" "$intent" "$source_hash" "$source_metadata" >/dev/null 2>&1; then exit 24; fi
printf PASS
`), "PASS");
});

test("real marker crash at link publication is finalized by exact inode without reinstall", options, t => {
  assert.equal(run(t, `
printf 'kodex.stable-native-marker/v1\\ncoredns\\n%s\\n%s\\n' "$plan" "$intent" >"$staging"
chmod 0600 "$staging"
ln "$staging" "$marker"
observed=$(call inspect)
case "$observed" in LINKED\|*) ;; *) exit 25 ;; esac
receipt=$(call finalize "${"${observed#LINKED|}"}")
test ! -e "$staging"
test "$(stat -c '%h' "$marker")" = 1
test "$(call inspect "$receipt")" = "$receipt"
if call finalize "$receipt" >/dev/null 2>&1; then exit 26; fi
call remove "$receipt" >/dev/null
printf PASS
`), "PASS");
});

test("K3s source inode replacement with identical bytes does not invalidate marker identity", options, t => {
  assert.equal(run(t, `
receipt=$(call install)
cp "$source" "$source.restarted"
mv "$source.restarted" "$source"
test "$(call inspect "$receipt")" = "$receipt"
printf changed >"$source"
if call remove "$receipt" >/dev/null 2>&1; then exit 27; fi
test -e "$marker"
printf PASS
`), "PASS");
});

test("metrics marker is isolated and unknown lock is never broken automatically", options, t => {
  assert.equal(run(t, `
metrics=$root/metrics-server/metrics-server-deployment.yaml
metrics_hash=$(sha256sum "$metrics" | cut -d ' ' -f 1)
metrics_metadata=$(stat -c '%u:%g:%a:%h:%d:%i:%s' "$metrics")
receipt=$(sh -c "$worker" native-marker install metrics-server "$plan" "$intent" "$metrics_hash" "$metrics_metadata")
test ! -e "$source.skip"
sh -c "$worker" native-marker remove metrics-server "$plan" "$intent" "$metrics_hash" "$metrics_metadata" "$receipt" >/dev/null
mkdir -m 0700 "$root/.kodex-stable-native-lock"
if call install >/dev/null 2>&1; then exit 28; fi
if call inspect >/dev/null 2>&1; then exit 29; fi
test -d "$root/.kodex-stable-native-lock"
test ! -e "$marker"
printf PASS
`), "PASS");
});
