import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const image = process.env.KODEX_FRONTEND_NGINX_FIXTURE_IMAGE;
const base = fileURLToPath(new URL("../../deploy/k8s/base/staff-control-center/", import.meta.url));

test("compiled nginx validates actual TLS config as nonroot without network or real credentials", { skip: !image }, () => {
  assert.match(image, /^sha256:[a-f0-9]{64}$/);
  const directory = mkdtempSync(join(tmpdir(), "kodex-frontend-nginx-fixture-"));
  try {
    execFileSync("openssl", ["req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:prime256v1", "-nodes",
      "-subj", "/CN=fixture", "-days", "1", "-keyout", join(directory, "fixture.key"), "-out", join(directory, "fixture.crt")],
    { stdio: ["ignore", "ignore", "ignore"], timeout: 10000 });
    // Это одноразовый тестовый ключ, не сертификат или credential среды.
    for (const name of ["fixture.key", "fixture.crt"]) chmodSync(join(directory, name), 0o444);
    writeFileSync(join(directory, "security-headers.conf"), readFileSync(base + "security-headers.conf", "utf8")
      .replaceAll("__KODEX_OIDC_ORIGIN__", "https://sso.example.test"), { mode: 0o444 });
    const mounts = [
      ["fixture.key", "/var/run/secrets/kodex/staff-control-center/backend-tls/tls.key"],
      ["fixture.crt", "/var/run/secrets/kodex/staff-control-center/backend-tls/tls.crt"],
      ["fixture.crt", "/var/run/secrets/kodex/staff-control-center/ingress-client-tls/ca.crt"],
      ["fixture.crt", "/var/run/config/kodex/staff-control-center/control-api-ca/ca.crt"],
      ["security-headers.conf", "/var/run/config/kodex/staff-control-center/runtime/security-headers.conf"],
    ];
    const output = execFileSync("docker", ["run", "--rm", "--pull=never", "--network=none", "--read-only", "--user", "101:101",
      "--cap-drop=ALL", "--security-opt=no-new-privileges", "--entrypoint", "nginx",
      "--add-host", "control-api-gateway.kodex-system.svc:127.0.0.1",
      "--tmpfs", "/var/cache/nginx:uid=101,gid=101", "--tmpfs", "/var/run/nginx:uid=101,gid=101",
      ...mounts.flatMap(([source, target]) => ["--mount", `type=bind,source=${join(directory, source)},target=${target},readonly`]),
      image, "-t"], { encoding: "utf8", timeout: 20000, stdio: ["ignore", "pipe", "pipe"] });
    assert.equal(output.trim(), "");
  } finally { rmSync(directory, { recursive: true, force: true }); }
});
