import { expect, it } from "vitest";
import { ReadNetworkCorrelator, type ReadSignalEvent } from "./ui-read-network";
const id = "11111111-1111-4111-8111-111111111111:1";
const address =
  "https://fixture.invalid/api/v1/projects?query=private-sentinel";
const event = (
  phase: ReadSignalEvent["phase"],
  at: number,
  intentional = true,
): ReadSignalEvent => ({ phase, at, intentional, id, address, method: "GET" });
it("requires exact native abort and rejected promise; retains raw failure without secrets", () => {
  const c = new ReadNetworkCorrelator<object>(),
    r = {};
  c.observe(event("start", 10, false));
  c.request(r, address, "GET", id);
  c.observe(event("abort", 20));
  c.failed(r, "cancelled", 21);
  expect(c.confirmed(r)).toBe(false);
  c.observe(event("reject", 22));
  expect(c.confirmed(r)).toBe(true);
  expect(c.snapshot()).toMatchObject({
    rawFailedRequests: 1,
    confirmedCancellations: 1,
    unexplainedFailures: 0,
  });
  expect(JSON.stringify(c.snapshot())).not.toContain("private-sentinel");
  expect(JSON.stringify(c.snapshot())).not.toContain(id);
  const details = JSON.stringify(c.safeDiagnostics());
  expect(details).not.toContain("private-sentinel");
  expect(details).not.toContain(id);
  expect(c.safeDiagnostics().failures[0]).toMatchObject({
    route: "PROJECT_CATALOG",
    method: "GET",
    exactCancellation: true,
    nativeIdentityKnown: true,
  });
});
it("does not accept timeout, late abort, duplicate identity, method mismatch or actual network failure", () => {
  for (const variant of [
    "timeout",
    "late",
    "duplicate",
    "method",
    "network",
    "reject",
  ]) {
    const c = new ReadNetworkCorrelator<object>(),
      r = {};
    c.observe(event("start", 10));
    c.request(r, address, variant === "method" ? "POST" : "GET", id);
    if (variant === "duplicate") c.request({}, address, "GET", id);
    c.observe(
      event("abort", variant === "late" ? 30 : 20, variant !== "timeout"),
    );
    c.observe(event("reject", 22, variant !== "reject"));
    c.failed(
      r,
      variant === "network" ? "net::ERR_CONNECTION_RESET" : "cancelled",
      21,
    );
    expect(c.confirmed(r), variant).toBe(false);
  }
});
it("navigation intent without a committed document never proves API cancellation", () => {
  const c = new ReadNetworkCorrelator<object>(),
    r = {};
  c.observe(event("start", 10));
  c.request(r, address, "GET", id);
  c.navigation("GOTO", 20);
  c.failed(r, "cancelled", 21);
  expect(c.confirmed(r)).toBe(false);
});
it("bounded overflow closes cancellation acceptance", () => {
  const c = new ReadNetworkCorrelator<object>();
  for (let i = 0; i < 4097; i++)
    c.observe({
      ...event("start", 10),
      id: id.replace(":1", ":" + String(i + 1)),
    });
  expect(c.snapshot()).toMatchObject({ signalEvents: 4096, overflow: 1 });
});

it("WebKit cancellation code still requires exact abort identity", () => {
  const c = new ReadNetworkCorrelator<object>(),
    r = {};
  c.request(r, address, "GET", id);
  c.failed(r, "Load request cancelled", 21);
  expect(c.confirmed(r)).toBe(false);
  c.observe(event("start", 10));
  c.observe(event("abort", 20));
  c.observe(event("reject", 22));
  expect(c.confirmed(r)).toBe(true);
});

it("checkpoints retain cumulative proof beyond 4096 completed asset and native requests", () => {
  const c = new ReadNetworkCorrelator<object>();
  let sequence = 0;
  for (let batch = 0; batch < 20; batch++) {
    for (let n = 0; n < 256; n++) {
      const r = {},
        next = id.replace(":1", ":" + String(++sequence));
      c.observe({ ...event("start", sequence), id: next });
      c.request(r, address, "GET", next);
      c.terminal(r, sequence + 1);
    }
    const checkpoint = c.checkpoint();
    expect(checkpoint.summary.overflow).toBe(0);
    expect(c.snapshot()).toMatchObject({
      retainedRequests: 0,
      retainedSignals: 0,
    });
  }
  expect(c.snapshot()).toMatchObject({
    observedRequests: 5120,
    signalEvents: 5120,
    overflow: 0,
    rawFailedRequests: 0,
    unexplainedFailures: 0,
  });
});

it("checkpoint freezes proven cancellation; later active overflow cannot rewrite it", () => {
  const c = new ReadNetworkCorrelator<object>(),
    r = {};
  c.observe(event("start", 10));
  c.request(r, address, "GET", id);
  c.observe(event("abort", 20));
  c.observe(event("reject", 22));
  c.failed(r, "cancelled", 21);
  expect(c.checkpoint().diagnostics.failures[0]?.exactCancellation).toBe(true);
  for (let i = 0; i < 4097; i++) c.request({}, address, "GET");
  expect(c.snapshot()).toMatchObject({
    overflow: 1,
    confirmedCancellations: 1,
    unexplainedFailures: 0,
  });
  expect(c.confirmed(r)).toBe(true);
});

it("active and late failures survive checkpoint, late proof cannot rehabilitate an archived failure", () => {
  const c = new ReadNetworkCorrelator<object>(),
    r = {};
  c.observe(event("start", 10));
  c.request(r, address, "GET", id);
  c.checkpoint();
  expect(c.snapshot().retainedRequests).toBe(1);
  c.failed(r, "cancelled", 21);
  const saved = c.checkpoint();
  expect(saved.diagnostics.failures[0]?.exactCancellation).toBe(false);
  c.observe(event("abort", 20));
  c.observe(event("reject", 22));
  expect(c.confirmed(r)).toBe(false);
  expect(c.snapshot()).toMatchObject({
    rawFailedRequests: 1,
    unexplainedFailures: 1,
  });
});

it("retired identity cannot be replayed and duplicate identity stays invalid after sibling retirement", () => {
  for (const duplicate of [false, true]) {
    const c = new ReadNetworkCorrelator<object>(),
      first = {},
      next = {};
    c.observe(event("start", 10));
    c.request(first, address, "GET", id);
    if (duplicate) c.request(next, address, "GET", id);
    c.terminal(first, 11);
    c.checkpoint();
    if (!duplicate) {
      c.observe(event("start", 12));
      c.request(next, address, "GET", id);
    }
    c.observe(event("abort", 20));
    c.observe(event("reject", 22));
    c.failed(next, "cancelled", 21);
    expect(c.checkpoint().diagnostics.failures[0]?.exactCancellation).toBe(
      false,
    );
    expect(c.snapshot().unexplainedFailures).toBe(1);
  }
});

it("checkpoints preserve every closed failure chunk without accumulating the per-chunk cap", () => {
  const c = new ReadNetworkCorrelator<object>();
  const chunks = [];
  for (let i = 0; i < 48; i++) {
    const r = {};
    c.request(r, address, "GET");
    c.failed(r, "net::ERR_CONNECTION_RESET");
    chunks.push(c.checkpoint());
  }
  expect(chunks.flatMap((chunk) => chunk.diagnostics.failures)).toHaveLength(
    48,
  );
  expect(
    new Set(
      chunks.flatMap((chunk) =>
        chunk.diagnostics.failures.map((failure) => failure.requestSequence),
      ),
    ).size,
  ).toBe(48);
  expect(c.snapshot()).toMatchObject({
    overflow: 0,
    rawFailedRequests: 48,
    unexplainedFailures: 48,
    retainedRequests: 0,
  });
  expect(JSON.stringify(chunks)).not.toContain("private-sentinel");
});
