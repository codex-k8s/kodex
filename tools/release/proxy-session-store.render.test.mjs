import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { sessionStoreArgs, proxyImage } from "./proxy-session-store.mjs";
test(
  "точный chart render использует внешний TLS store без скрытого Redis subchart",
  { skip: process.env.KODEX_PROXY_SESSION_RENDER_TEST !== "1" },
  () => {
    const archive = process.env.KODEX_OAUTH2_CHART_ARCHIVE;
    assert.ok(archive);
    assert.equal(
      createHash("sha256").update(readFileSync(archive)).digest("hex"),
      "47b44c66fdfc42677d307cf8891f3de29863414bf265f6bef2020c273ff7e839",
    );
    const directory = mkdtempSync(join(tmpdir(), "kodex-proxy-render-"));
    try {
      const base = readFileSync(
        new URL(
          "../../infra/management-surfaces/oauth2-proxy-values.yaml",
          import.meta.url,
        ),
        "utf8",
      )
        .replaceAll("__KODEX_OAUTH2_SECRET__", "oauth2-control-center")
        .replaceAll(
          "__KODEX_OAUTH2_COOKIE_NAME__",
          "_kodex_control_center_oauth2",
        )
        .replaceAll(
          "__KODEX_OIDC_ISSUER__",
          "https://identity.fixture.test/realms/kodex",
        )
        .replaceAll("__KODEX_SURFACE_ORIGIN__", "https://control.fixture.test")
        .replaceAll("__KODEX_ALLOWED_ROLE__", "kodex-owner")
        .replaceAll("__KODEX_SURFACE_HOST__", "control.fixture.test")
        .replaceAll("__KODEX_OIDC_CONNECT_IP__", "192.0.2.5")
        .replaceAll("__KODEX_OIDC_HOST__", "identity.fixture.test")
        .replaceAll("__KODEX_INGRESS_CLASS__", "traefik")
        .replaceAll("__KODEX_SURFACE_TLS_SECRET__", "fixture-tls");
      const file = join(directory, "values.yaml");
      writeFileSync(file, base, { mode: 0o600 });
      const rendered = execFileSync(
        "helm",
        [
          "template",
          "oauth2-control-center",
          archive,
          "-n",
          "kodex-system",
          "--set",
          "fullnameOverride=oauth2-control-center",
          "-f",
          file,
          "-f",
          new URL(
            "../../infra/management-surfaces/control-center-session-store-values.yaml",
            import.meta.url,
          ).pathname,
        ],
        { encoding: "utf8" },
      );
      const docs = JSON.parse(
        execFileSync("yq", ["eval-all", "-o=json", "[.]", "-"], {
          input: rendered,
          encoding: "utf8",
        }),
      );
      const deployment = docs.find((x) => x.kind === "Deployment");
      assert.ok(deployment);
      const c = deployment.spec.template.spec.containers[0];
      assert.equal(c.image, proxyImage);
      for (const arg of sessionStoreArgs) assert.ok(c.args.includes(arg), arg);
      assert.equal(
        c.env.filter((x) => x.name === "OAUTH2_PROXY_REDIS_PASSWORD").length,
        1,
      );
      assert.equal(c.readinessProbe.httpGet.path, "/ready");
      assert.equal(c.livenessProbe.httpGet.path, "/ping");
      assert.deepEqual(
        deployment.spec.template.spec.volumes.find(
          (x) => x.name === "session-store-ca",
        ).secret.items,
        [{ key: "ca.crt", path: "ca.crt" }],
      );
      assert.equal(docs.filter((x) => x.kind === "StatefulSet").length, 0);
      const baseResources = execFileSync(
        "kubectl",
        [
          "kustomize",
          new URL("../../deploy/k8s/base/proxy-session-store", import.meta.url)
            .pathname,
        ],
        { encoding: "utf8" },
      );
      assert.match(baseResources, /kind: StatefulSet/);
      assert.doesNotMatch(baseResources, /kind: Secret\n/);
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  },
);
