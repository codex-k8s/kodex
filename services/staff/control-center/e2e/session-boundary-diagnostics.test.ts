import { describe, expect, it } from "vitest";
import {
  safeSessionBoundary,
  sessionPreflight,
  SessionBoundaryDiagnostics,
} from "./session-boundary-diagnostics";

const fixture = {
  version: 3,
  generation: "private-generation",
  actor: "private-actor",
  csrf: "private-csrf",
  renewalMode: "BACKEND_REFRESH",
  serverTime: "2026-09-09T10:00:00Z",
  accessExpiresAt: "2026-09-09T10:05:00Z",
  expiresAt: "2026-09-09T10:15:00Z",
  absoluteExpiresAt: "2026-09-09T11:00:00Z",
  renewAfter: "2026-09-09T10:04:30Z",
};
describe("safe session boundary diagnostics", () => {
  it("оставляет относительные сроки, не identity и не secrets", () => {
    const result = safeSessionBoundary(fixture);
    expect(result).toEqual({
      version: 3,
      renewalMode: "BACKEND_REFRESH",
      accessRemainingMs: 300000,
      idleRemainingMs: 900000,
      absoluteRemainingMs: 3600000,
      renewInMs: 270000,
    });
    expect(JSON.stringify(result)).not.toMatch(
      /private|2026|actor|csrf|generation/,
    );
  });
  it("естественный refresh виден сменой version при прежней absolute границе", () => {
    const diagnostics = new SessionBoundaryDiagnostics();
    diagnostics.observe("PREFLIGHT", 200, fixture);
    diagnostics.observe("SESSION_RENEW", 200, {
      ...fixture,
      version: 4,
      serverTime: "2026-09-09T10:04:30Z",
      accessExpiresAt: "2026-09-09T10:09:30Z",
      expiresAt: "2026-09-09T10:19:30Z",
      renewAfter: "2026-09-09T10:09:00Z",
    });
    const events = diagnostics.snapshot().events;
    expect(events[1]?.boundary).toMatchObject({
      version: 4,
      absoluteRemainingMs: 3330000,
      accessRemainingMs: 300000,
      renewInMs: 270000,
    });
    expect(events[0]?.boundary?.version).toBe(3);
  });
  it("401 preflight и invalid metadata не превращаются в успешную границу", () => {
    const diagnostics = new SessionBoundaryDiagnostics();
    diagnostics.observe("PREFLIGHT", 401, { private: "content" });
    diagnostics.observe("SESSION_READ", 200, { ...fixture, serverTime: "bad" });
    expect(diagnostics.snapshot().events).toMatchObject([
      { status: 401, metadata: "NOT_APPLICABLE" },
      { status: 200, metadata: "INVALID" },
    ]);
    expect(
      diagnostics.snapshot().events.every((event) => !event.boundary),
    ).toBe(true);
    expect(() => safeSessionBoundary({ ...fixture, version: 0 })).toThrow();
    expect(() =>
      safeSessionBoundary({ ...fixture, renewalMode: "private" }),
    ).toThrow();
  });
  it("expired access отличается от idle/absolute и ticket не читается", () => {
    const diagnostics = new SessionBoundaryDiagnostics();
    diagnostics.observe("SESSION_READ", 200, {
      ...fixture,
      accessExpiresAt: "2026-09-09T09:59:59Z",
    });
    diagnostics.observe("SESSION_TICKET", 200, { ticket: "private-ticket" });
    expect(diagnostics.snapshot().events[0]?.boundary?.accessRemainingMs).toBe(
      -1000,
    );
    expect(diagnostics.snapshot().events[1]?.metadata).toBe("NOT_APPLICABLE");
    expect(JSON.stringify(diagnostics.snapshot())).not.toContain("private");
  });
  it("preflight закрывает401 и истёкшую policy, не запрещает законный backend refresh", () => {
    expect(() => sessionPreflight(401, fixture)).toThrow("not accepted");
    expect(() =>
      sessionPreflight(200, { ...fixture, expiresAt: fixture.serverTime }),
    ).toThrow("expired");
    expect(() =>
      sessionPreflight(200, {
        ...fixture,
        absoluteExpiresAt: fixture.serverTime,
      }),
    ).toThrow("expired");
    expect(() =>
      sessionPreflight(200, {
        ...fixture,
        renewalMode: "REAUTHENTICATION",
        accessExpiresAt: fixture.serverTime,
      }),
    ).toThrow("expired");
    expect(
      sessionPreflight(200, { ...fixture, accessExpiresAt: fixture.serverTime })
        .renewalMode,
    ).toBe("BACKEND_REFRESH");
  });
  it("body ACK в другом порядке не переставляет закреплённые response events", () => {
    const diagnostics = new SessionBoundaryDiagnostics();
    diagnostics.observe("SESSION_TICKET", 200, undefined, {
      sequence: 3,
      timestampUTC: "2026-09-09T10:00:03Z",
    });
    diagnostics.observe("SESSION_RENEW", 200, fixture, {
      sequence: 2,
      timestampUTC: "2026-09-09T10:00:02Z",
    });
    diagnostics.observe("SESSION_READ", 200, fixture, {
      sequence: 1,
      timestampUTC: "2026-09-09T10:00:01Z",
    });
    expect(
      diagnostics.snapshot().events.map((event) => event.sequence),
    ).toEqual([1, 2, 3]);
  });
  it("bounded events и возвращаемая копия сохраняют достоверность", () => {
    const diagnostics = new SessionBoundaryDiagnostics();
    for (let i = 0; i < 129; i++)
      diagnostics.observe("SESSION_READ", 200, fixture);
    expect(diagnostics.snapshot().overflow).toBe(1);
    const copied = diagnostics.snapshot().events[0]?.boundary;
    if (copied) copied.version = 999;
    expect(diagnostics.snapshot().events[0]?.boundary?.version).toBe(3);
  });
});
