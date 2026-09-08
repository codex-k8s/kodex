import { describe, expect, it } from "vitest";
import { createHash } from "node:crypto";
import { ConsoleErrorDiagnostics } from "./console-error-diagnostics";
const hash = (text: string) => createHash("sha256").update(text).digest("hex");
const origin = "https://kodex.test";
describe("безопасная диагностика console.error", () => {
  it("сохраняет только digest и закрытые поля; query не входит в source digest", () => {
    const d = new ConsoleErrorDiagnostics(origin);
    d.beginStep();
    d.observe(
      {
        message: "TypeError: private-cookie",
        location: {
          url: `${origin}/src/private-user.ts?token=private-query#private-fragment`,
          line: 0,
          column: 7,
        },
      },
      1,
      2,
    );
    const s = d.snapshot();
    expect(s.events[0]).toMatchObject({
      errorClass: "TypeError",
      code: "UNKNOWN",
      classAttribution: "TEXT_ONLY",
      page: 1,
      observedDocument: 2,
      stepSequence: 1,
      stage: "UI_STEP",
      documentAttribution: "OBSERVATION_ONLY",
      sourceAttribution: "BROWSER_REPORTED",
      category: "DEV_MODULE",
      line: 0,
      column: 7,
      coordinatesKnown: true,
      coordinateBase: 0,
      messageSHA256: hash("TypeError: private-cookie"),
      sourcePathSHA256: hash("/src/private-user.ts"),
      sourceOriginSHA256: hash(origin),
    });
    expect(JSON.stringify(s)).not.toMatch(/private|https:|token=|fragment/);
    const first = s.events[0];
    if (!first) throw new Error("Missing fixture event");
    first.code = "tampered";
    expect(d.snapshot().events[0]?.code).toBe("UNKNOWN");
    expect(d.failed()).toBe(true);
  });
  it.each([
    "Service worker registration failed",
    "Control Center bootstrap failed",
  ])(
    "точный известный текст %s не является разрешением игнорировать",
    (message) => {
      const d = new ConsoleErrorDiagnostics(origin);
      d.observe({ message }, 0, 0);
      expect(d.snapshot().events[0]?.code).not.toBe("UNKNOWN");
      expect(d.failed()).toBe(true);
      d.observe({ message: message + " private" }, 0, 0);
      expect(d.snapshot().events[1]?.code).toBe("UNKNOWN");
    },
  );
  it.each([
    "https://user:private@kodex.test/path",
    "data:text/html,private",
    "javascript:private",
    "file:///private",
    "relative/private",
  ])("не выпускает неподдержанную location %s", (url) => {
    const d = new ConsoleErrorDiagnostics(origin);
    d.observe(
      { message: "private", location: { url, line: 1, column: 2 } },
      0,
      0,
    );
    expect(d.snapshot().events[0]).toMatchObject({
      category: "UNKNOWN",
      sourcePathSHA256: "",
      coordinatesKnown: false,
    });
    expect(JSON.stringify(d.snapshot())).not.toContain("private");
  });
  it("различает external и invalid coordinates, стадии и неизвестные поля", () => {
    const d = new ConsoleErrorDiagnostics(origin);
    d.setStage("arbitrary-private");
    d.observe(
      {
        message: "SecretException: private",
        location: {
          url: "https://external.test/private?token=x",
          line: -1,
          column: Infinity,
        },
      },
      100,
      -1,
    );
    expect(d.snapshot().events[0]).toMatchObject({
      errorClass: "UNKNOWN",
      stage: "UNKNOWN",
      pageKnown: false,
      page: 0,
      observedDocument: 0,
      category: "EXTERNAL",
      coordinatesKnown: false,
    });
    d.beginStep();
    d.endStep();
    d.observe({}, 0, 1);
    expect(d.snapshot().events[1]).toMatchObject({
      stage: "BETWEEN_STEPS",
      stepSequence: 0,
      messagePresent: false,
    });
    d.setStage("CLEANUP");
    d.observe({}, 0, 1);
    expect(d.snapshot().events[2]?.stage).toBe("CLEANUP");
  });
  it("закрыто обрабатывает getters и не сериализует произвольные объекты", () => {
    const d = new ConsoleErrorDiagnostics(origin);
    const value = Object.defineProperties(
      {},
      {
        message: {
          get() {
            throw new Error("private");
          },
        },
        location: {
          get() {
            throw new Error("private");
          },
        },
      },
    );
    expect(() => d.observe(value, 0, 0)).not.toThrow();
    expect(d.snapshot().events[0]).toMatchObject({
      messagePresent: false,
      category: "UNKNOWN",
    });
    expect(d.failed()).toBe(true);
  });
  it("ограничивает event/input/time/step budgets, любое переполнение блокирует PASS", () => {
    const d = new ConsoleErrorDiagnostics(origin);
    d.observe(
      {
        message: "x".repeat(4097),
        location: { url: origin + "/" + "s".repeat(4097), line: 1, column: 1 },
      },
      0,
      0,
      Infinity,
    );
    expect(d.snapshot()).toMatchObject({ overflow: 1 });
    expect(d.snapshot().events[0]).toMatchObject({
      digestScope: "BOUNDED_PREFIX",
      messageSHA256: hash("x".repeat(4096)),
      sourcePathSHA256: "",
      elapsedMs: 0,
    });
    for (let i = 0; i < 35; i++) d.observe({}, 0, 0);
    expect(d.snapshot()).toMatchObject({
      total: 36,
      overflow: 5,
      eventLimit: 32,
    });
    expect(d.snapshot().events).toHaveLength(32);
    const steps = new ConsoleErrorDiagnostics(origin);
    for (let i = 0; i < 10001; i++) steps.beginStep();
    expect(steps.failed()).toBe(true);
    expect(steps.snapshot().total).toBe(0);
  });
});
