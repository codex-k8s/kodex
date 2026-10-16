import assert from "node:assert/strict";
import test from "node:test";
import { bootUnits, canonicalObservedUnits } from "./edge-boot-recovery.mjs";
import { replacementUnits } from "./edge-rebind.mjs";
test("boot transition removes sockets target cycle without changing any network permissions or service", () => {
  const before = {};
  for (const i of [0, 1]) {
    before[`kodex-public-edge-https-${i}.socket`] = "# Kodex public edge plan fixture\n[Unit]\nAfter=network-online.target docker.service\n[Socket]\nListenStream=172.20.0.1:18443\nIPAddressDeny=any\nIPAddressAllow=172.20.0.2/32\n[Install]\nWantedBy=sockets.target\n";
    before[`kodex-public-edge-https-${i}.service`] = "# Kodex public edge plan fixture\n[Service]\nExecStart=/usr/lib/systemd/systemd-socket-proxyd 127.0.0.2:443\n";
  }
  const after = bootUnits(before);
  for (const [name, body] of Object.entries(after)) {
    if (name.endsWith(".service")) assert.equal(body, before[name]);
    else { assert.match(body, /DefaultDependencies=no/); assert.match(body, /Before=shutdown.target/);
      assert.match(body, /Conflicts=shutdown.target/); assert.match(body, /WantedBy=multi-user.target/);
      assert.equal(body.replace("DefaultDependencies=no\nBefore=shutdown.target\nConflicts=shutdown.target\n", "")
        .replace("WantedBy=multi-user.target", "WantedBy=sockets.target"), before[name]); }
  }
  assert.deepEqual(bootUnits(after), after);
  assert.throws(() => bootUnits({ ...before, foreign: "x" }), /SET_INVALID/);
});

test("automatic peer update accepts only canonical installed generations including partial prior replacement", () => {
  const source = { config: { platformHost: "platform.example.test", oidcHost: "sso.example.test", namespace: "kube-system",
    bridgeIP: "172.20.0.1", allowedPeers: ["172.20.0.2", "172.20.0.3"], backendIP: "127.0.0.2", relayPorts: [18443, 18444],
    upstreamServerName: "upstream.example.test", certResolver: "public" } };
  const peers = [0, 1].map(i => ({ name: `k3d-fixture-agent-${i}`, id: String(i + 1).repeat(64), peerIP: `172.20.0.${i + 2}`, networkID: "a".repeat(64) }));
  const old = replacementUnits(source, peers), newer = replacementUnits(source, peers.map((p, i) => ({ ...p, peerIP: `172.20.0.${i + 4}` })));
  const mixed = { ...old, "kodex-public-edge-https-1.service": newer["kodex-public-edge-https-1.service"] };
  assert.deepEqual(canonicalObservedUnits(source, peers, mixed), mixed);
  for (const change of [body => body + "ExecStart=/bin/foreign\n", body => body.replace("IPAddressDeny=any", "IPAddressDeny="),
    body => body.replace("/32", "/16"), body => body.replace("172.20.0.2", "8.8.8.8")]) {
    const bad = { ...mixed, "kodex-public-edge-https-0.socket": change(mixed["kodex-public-edge-https-0.socket"]) };
    assert.throws(() => canonicalObservedUnits(source, peers, bad));
  }
});
