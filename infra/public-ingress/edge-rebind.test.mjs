import assert from "node:assert/strict";
import test from "node:test";
import { currentPeers, replacementUnits } from "./edge-rebind.mjs";

const source = { config: { platformHost: "platform.example.test", oidcHost: "sso.example.test", namespace: "kube-system",
  bridgeIP: "172.20.0.1", allowedPeers: ["172.20.0.4", "172.20.0.5"], backendIP: "127.0.0.2", relayPorts: [18443, 18444],
  upstreamServerName: "upstream.example.test", certResolver: "public" },
peers: [0, 1].map(i => ({ name: `k3d-fixture-agent-${i}`, id: String(i + 1).repeat(64), peerIP: `172.20.0.${i + 4}` })) };
function containers() { return new Map(source.peers.map((peer, i) => [peer.name, { Id: peer.id, State: { Running: true },
  NetworkSettings: { Networks: { fixture: { IPAddress: `172.20.0.${i + 2}`, Gateway: "172.20.0.1", NetworkID: "a".repeat(64) } } } }])); }

test("same node identities yield new exact peers and remove obsolete socket permissions", () => {
  const peers = currentPeers(source, containers()), units = replacementUnits(source, peers);
  assert.deepEqual(peers.map(p => p.peerIP), ["172.20.0.2", "172.20.0.3"]);
  for (const body of Object.values(units)) {
    assert.match(body, /IPAddressDeny=any/); assert.match(body, /IPAddressAllow=172\.20\.0\.2\/32/);
    assert.match(body, /IPAddressAllow=172\.20\.0\.3\/32/); assert.doesNotMatch(body, /IPAddressAllow=172\.20\.0\.[45]/);
  }
});

test("changed container, stopped node, ambiguous network and different bridge fail closed", () => {
  const mutations = [c => c.Id = "b".repeat(64), c => c.State.Running = false,
    c => c.NetworkSettings.Networks.fixture.Gateway = "172.21.0.1",
    c => c.NetworkSettings.Networks.extra = { ...c.NetworkSettings.Networks.fixture }];
  for (const mutate of mutations) { const values = containers(); mutate(values.values().next().value);
    assert.throws(() => currentPeers(source, values), /EDGE_REBIND_/); }
  const values = containers(); values.get(source.peers[1].name).NetworkSettings.Networks.fixture.NetworkID = "c".repeat(64);
  assert.throws(() => currentPeers(source, values), /NETWORK_DRIFT/);
});

test("loopback, public and duplicate peers cannot widen relay access", () => {
  for (const address of ["127.0.0.1", "8.8.8.8", "172.20.0.1", "172.20.0.3", "::1"]) {
    const values = containers(); values.get(source.peers[0].name).NetworkSettings.Networks.fixture.IPAddress = address;
    assert.throws(() => currentPeers(source, values));
  }
});
