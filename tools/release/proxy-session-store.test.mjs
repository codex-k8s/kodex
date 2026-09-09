import { test } from "node:test";
import assert from "node:assert/strict";
import {
  resources,
  sessionStoreSecret,
  sessionStoreProxySpec,
  proxyImage,
  sessionStoreArgs,
  validateStoreDependencies,
} from "./proxy-session-store.mjs";
const fixture = () => ({
  kind: "Deployment",
  metadata: { name: "oauth2-control-center", namespace: "kodex-system" },
  spec: {
    replicas: 2,
    template: {
      spec: {
        containers: [
          {
            name: "oauth2-proxy",
            image: proxyImage,
            args: [
              "--cookie-expire=8h",
              "--cookie-refresh=1h",
              "--provider=keycloak-oidc",
              "--cookie-secure=true",
              "--cookie-httponly=true",
            ],
            env: [
              {
                name: "OAUTH2_PROXY_COOKIE_SECRET",
                valueFrom: {
                  secretKeyRef: {
                    name: "oauth2-control-center",
                    key: "cookie-secret",
                  },
                },
              },
            ],
          },
        ],
      },
    },
  },
});
test("переход сохраняет TTL/image и использует только ссылки на собственные credentials/CA", () => {
  const source = fixture(),
    before = structuredClone(source);
  const after = sessionStoreProxySpec(source);
  assert.deepEqual(source, before);
  const c = after.template.spec.containers[0];
  assert.deepEqual(c.args, [
    ...source.spec.template.spec.containers[0].args,
    ...sessionStoreArgs,
  ]);
  assert.equal(c.image, proxyImage);
  assert.equal(c.env.at(-1).value, undefined);
  assert.deepEqual(after.template.spec.volumes[0].secret.items, [
    { key: "ca.crt", path: "ca.crt" },
  ]);
  assert.equal(c.readinessProbe.httpGet.path, "/ready");
  assert.equal(c.livenessProbe.httpGet.path, "/ping");
});
for (const [name, change] of [
  ["foreign workload", (d) => (d.metadata.name = "other")],
  ["foreign namespace", (d) => (d.metadata.namespace = "production")],
  [
    "different image",
    (d) => (d.spec.template.spec.containers[0].image += "other"),
  ],
  [
    "altered TTL",
    (d) => (d.spec.template.spec.containers[0].args[0] = "--cookie-expire=9h"),
  ],
  [
    "prior store",
    (d) =>
      d.spec.template.spec.containers[0].args.push(
        "--session-store-type=cookie",
      ),
  ],
  [
    "inline password",
    (d) =>
      d.spec.template.spec.containers[0].env.push({
        name: "OAUTH2_PROXY_REDIS_PASSWORD",
        value: "synthetic",
      }),
  ],
  [
    "prior CA",
    (d) => (d.spec.template.spec.volumes = [{ name: "session-store-ca" }]),
  ],
])
  test(`небезопасный переход отклонён: ${name}`, () => {
    const d = fixture();
    change(d);
    assert.throws(() => sessionStoreProxySpec(d));
  });
test("ACL запрещает default/foreign keys/admin; пароли независимы, ACL содержит только hash", () => {
  const secret = sessionStoreSecret();
  assert.equal(secret.immutable, true);
  assert.equal(
    new Set(
      ["proxy", "probe", "backup"].map(
        (k) => secret.stringData[k + "-password"],
      ),
    ).size,
    3,
  );
  const acl = secret.stringData["users.acl"];
  assert.match(acl, /user default off/);
  assert.match(acl, /~_kodex_control_center_oauth2-\*/);
  assert.doesNotMatch(acl, /\+@all|\+flush|\+config|~\*/);
  for (const k of ["proxy", "probe", "backup"])
    assert.ok(!acl.includes(secret.stringData[k + "-password"]));
  assert.throws(() => sessionStoreSecret(() => "a".repeat(43)));
});
test("TLS/persistence/health/NetworkPolicy имеют собственного bounded owner", () => {
  const all = resources();
  const get = (k) => all.find((x) => x.kind === k);
  const cfg = get("ConfigMap").data["valkey.conf"];
  assert.match(cfg, /port 0\ntls-port 6379/);
  assert.match(cfg, /appendfsync always/);
  assert.match(cfg, /maxmemory-policy noeviction/);
  const sts = get("StatefulSet");
  assert.equal(sts.spec.replicas, 1);
  assert.equal(sts.spec.template.spec.automountServiceAccountToken, false);
  assert.match(
    sts.spec.template.spec.containers[0].image,
    /@sha256:[a-f0-9]{64}$/,
  );
  assert.equal(
    sts.spec.template.spec.containers[0].livenessProbe.exec.command.at(-1),
    "kill -0 1",
  );
  assert.equal(
    get("PersistentVolumeClaim").spec.resources.requests.storage,
    "1Gi",
  );
  const np = all.find(
    (x) =>
      x.kind === "NetworkPolicy" &&
      x.metadata.name === "proxy-session-store-boundary",
  );
  assert.deepEqual(np.spec.egress, []);
  assert.equal(np.spec.ingress.length, 1);
  assert.equal(
    np.spec.ingress[0].from[0].podSelector.matchLabels[
      "app.kubernetes.io/instance"
    ],
    "oauth2-control-center",
  );
  assert.equal(np.spec.ingress[0].ports[0].port, 6379);
  assert.match(get("ConfigMap").data["ops.sh"], /-h 127.0.0.1/);
});

const ownedDependencies = () =>
  resources().map((r, i) => ({
    ...r,
    metadata: {
      ...r.metadata,
      uid: `00000000-0000-4000-8000-${String(i).padStart(12, "0")}`,
    },
    ...(r.kind === "Certificate"
      ? { status: { conditions: [{ type: "Ready", status: "True" }] } }
      : {}),
  }));
test("dependency readback связывает все exact ownership/spec и допускает только server defaults", () => {
  const all = ownedDependencies();
  const sts = all.find((x) => x.kind === "StatefulSet");
  sts.spec.revisionHistoryLimit = 10;
  assert.equal(validateStoreDependencies(all).length, resources().length);
});
for (const [name, change] of [
  [
    "unknown TLS",
    (all) =>
      (all.find((x) => x.kind === "Certificate").spec.issuerRef.name =
        "foreign"),
  ],
  [
    "certificate not ready",
    (all) => (all.find((x) => x.kind === "Certificate").status.conditions = []),
  ],
  [
    "added plaintext port",
    (all) =>
      (all.find((x) => x.kind === "ConfigMap").data["valkey.conf"] +=
        "port 6379\n"),
  ],
  [
    "widened namespace ingress",
    (all) =>
      (all.find(
        (x) => x.kind === "NetworkPolicy",
      ).spec.ingress[0].from[0].namespaceSelector = {}),
  ],
  [
    "added sidecar",
    (all) =>
      all
        .find((x) => x.kind === "StatefulSet")
        .spec.template.spec.containers.push({ name: "foreign" }),
  ],
  ["missing resource", (all) => all.pop()],
])
  test(`dependency drift закрыто отклонён: ${name}`, () => {
    const all = ownedDependencies();
    change(all);
    assert.throws(() => validateStoreDependencies(all));
  });
