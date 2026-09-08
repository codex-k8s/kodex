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
it("navigation snapshots exact pending identities including late request event, never prior failure or future request", () => {
  const c = new ReadNetworkCorrelator<object>(),
    r = {};
  c.observe(event("start", 10));
  c.navigation(20);
  c.request(r, address, "GET", id);
  c.failed(r, "NS_BINDING_ABORTED", 21);
  expect(c.confirmed(r)).toBe(true);
  const before = new ReadNetworkCorrelator<object>(),
    b = {};
  before.observe(event("start", 10));
  before.request(b, address, "GET", id);
  before.failed(b, "cancelled", 19);
  before.navigation(20);
  expect(before.confirmed(b)).toBe(false);
  const future = new ReadNetworkCorrelator<object>(),
    f = {};
  future.navigation(20);
  future.observe(event("start", 21));
  future.request(f, address, "GET", id);
  future.failed(f, "cancelled", 22);
  expect(future.confirmed(f)).toBe(false);
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
