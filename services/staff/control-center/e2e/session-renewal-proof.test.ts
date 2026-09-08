import { describe, expect, it } from "vitest";
import {
  observeFrame,
  renewalWindow,
  resumedAfterRenewal,
  sessionTiming,
  socketProof,
} from "./session-renewal-proof";
import { syntheticBrowserSession } from "./fixtures/browser-session";
const ready = (cursor: number) =>
  JSON.stringify({
    type: "SESSION_READY",
    streams: [{ streamKind: "PLATFORM", streamRef: "PLATFORM", cursor }],
  });
const resume = (cursor: number) =>
  JSON.stringify({ type: "SESSION_RESUME", platformAfterSequence: cursor });
describe("безопасное доказательство естественного продления", () => {
  it("ждёт server-owned renewAfter и не сохраняет identity или неожиданные поля", () => {
    const data = syntheticBrowserSession();
    expect(renewalWindow(data, 900_000).waitMs).toBe(300_000);
    expect(
      sessionTiming({ ...data, secret: "sensitive-sentinel" }),
    ).not.toHaveProperty("secret");
    expect(sessionTiming(data)).not.toHaveProperty("generation");
  });
  it.each([
    { renewAfter: "invalid" },
    { version: -1 },
    { renewalMode: "UNAVAILABLE" },
    { renewAfter: new Date(Date.now() - 1000).toISOString() },
    { absoluteExpiresAt: new Date(Date.now() + 310_000).toISOString() },
    { accessExpiresAt: new Date(Date.now() + 310_000).toISOString() },
    { expiresAt: new Date(Date.now() + 310_000).toISOString() },
  ])("закрыто отклоняет неподходящее окно %j", (change) => {
    expect(() =>
      renewalWindow({ ...syntheticBrowserSession(), ...change }, 900_000),
    ).toThrow();
  });
  it("не ждёт за пределами бюджета", () => {
    expect(() => renewalWindow(syntheticBrowserSession(), 300_000)).toThrow();
  });
  it("требует закрытия старого WS, нового READY и сохранения cursor", () => {
    const first = socketProof();
    observeFrame(first, resume(0), "sent");
    observeFrame(first, ready(7), "received");
    const second = socketProof();
    observeFrame(second, resume(7), "sent");
    observeFrame(second, ready(7), "received");
    expect(resumedAfterRenewal([first, second])).toBe(false);
    first.closed = true;
    expect(resumedAfterRenewal([first, second])).toBe(true);
    second.closed = true;
    expect(resumedAfterRenewal([first, second])).toBe(false);
  });
  it("не принимает один WS, регрессию cursor или SESSION_PROBLEM", () => {
    const first = {
      ...socketProof(),
      ready: 1,
      closed: true,
      readyCursors: [9],
    };
    const second = {
      ...socketProof(),
      ready: 1,
      resumeCursors: [0],
      readyCursors: [0],
    };
    expect(resumedAfterRenewal([first])).toBe(false);
    expect(resumedAfterRenewal([first, second])).toBe(false);
    second.resumeCursors = [9];
    second.readyCursors = [9];
    observeFrame(
      second,
      JSON.stringify({ type: "SESSION_PROBLEM", title: "sensitive-sentinel" }),
      "received",
    );
    expect(resumedAfterRenewal([first, second])).toBe(false);
    expect(JSON.stringify(second)).not.toContain("sensitive-sentinel");
  });
  it.each([
    "invalid sensitive-sentinel",
    "null",
    ready(-1),
    JSON.stringify({ type: "SESSION_READY", streams: [] }),
    "x".repeat(65_537),
  ])("повреждённый фрейм закрывает доказательство", (frame) => {
    const proof = socketProof();
    observeFrame(proof, frame, "received");
    expect(proof.malformed).toBe(1);
    expect(proof.ready).toBe(0);
    expect(JSON.stringify(proof)).not.toContain("sensitive-sentinel");
  });
  it("не сбрасывает последний наблюдавшийся cursor и допускает catch-up до READY", () => {
    const first = {
      ...socketProof(),
      ready: 1,
      closed: true,
      readyCursors: [7],
      maximumPlatformCursor: 9,
    };
    const second = {
      ...socketProof(),
      ready: 1,
      resumeCursors: [7],
      readyCursors: [7],
    };
    expect(resumedAfterRenewal([first, second])).toBe(false);
    second.resumeCursors = [9];
    second.readyCursors = [10];
    expect(resumedAfterRenewal([first, second])).toBe(true);
  });
  it("считает бизнес-события без содержимого и без заявления полноты", () => {
    const proof = socketProof();
    observeFrame(
      proof,
      JSON.stringify({
        type: "PLATFORM_INVALIDATED",
        cursor: 8,
        payload: "sensitive-sentinel",
      }),
      "received",
    );
    expect(proof.businessEvents).toBe(1);
    expect(JSON.stringify(proof)).not.toContain("sensitive-sentinel");
  });
});
