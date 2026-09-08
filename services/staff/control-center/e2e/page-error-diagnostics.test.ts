import { describe, expect, it } from "vitest";
import { createHash } from "node:crypto";
import { PageErrorDiagnostics } from "./page-error-diagnostics";
const origin = "https://kodex.test";
const hash = (text: string) => createHash("sha256").update(text).digest("hex");
describe("безопасные необработанные ошибки", () => {
  it("различает классы и сохраняет только digests и закрытые координаты", () => {
    const reporter = new PageErrorDiagnostics(origin);
    const message = "secret fixture token?cookie=opaque";
    reporter.beginStep();
    for (const name of ["Error", "TypeError"])
      reporter.observe(
        {
          name,
          message,
          stack: `${name}: ${message}\n    at privateFunction (https://kodex.test/assets/private.js?token=opaque:123:4)`,
        },
        1,
        7,
      );
    const evidence = reporter.snapshot();
    expect(evidence.events.map((event) => event.errorClass)).toEqual([
      "Error",
      "TypeError",
    ]);
    expect(evidence.events[0]).toMatchObject({
      messageSHA256: hash(message),
      page: 1,
      observedDocument: 7,
      stepSequence: 1,
      stage: "UI_STEP",
      category: "APPLICATION_ASSET",
      sourcePathSHA256: hash("/assets/private.js"),
      line: 123,
      column: 4,
      documentAttribution: "OBSERVATION_ONLY",
      digestScope: "FULL",
    });
    for (const secret of [
      message,
      "opaque",
      "privateFunction",
      "private.js",
      "https://",
      "cookie=",
    ])
      expect(JSON.stringify(evidence)).not.toContain(secret);
    expect(reporter.failed()).toBe(true);
  });
  it("не принимает произвольные имена, stage и forged stack как закрытый код", () => {
    const reporter = new PageErrorDiagnostics(origin);
    reporter.setStage("secret-stage");
    reporter.observe(
      {
        name: "TypeError secret",
        message: "ResizeObserver loop limit exceeded?token=private",
        stack: "secret at https://kodex.test/assets/private.js:10:2",
      },
      99,
      9000,
    );
    expect(reporter.snapshot().events[0]).toMatchObject({
      errorClass: "UNKNOWN",
      code: "UNKNOWN",
      stage: "UNKNOWN",
      category: "UNKNOWN",
      pageKnown: false,
      observedDocument: 0,
    });
    expect(JSON.stringify(reporter.snapshot())).not.toContain("secret");
  });
  it.each([
    ["f@https://kodex.test/src/private.vue:8:2", "DEV_MODULE"],
    ["f@https://outside.invalid/path?token=private:8:2", "EXTERNAL"],
    ["f@https://kodex.test/route?token=private:8:2", "APPLICATION_OTHER"],
  ])("классифицирует frame без вывода адреса %s", (stack, category) => {
    const reporter = new PageErrorDiagnostics(origin);
    reporter.observe({ stack }, 0, 1);
    expect(reporter.snapshot().events[0]?.category).toBe(category);
    expect(JSON.stringify(reporter.snapshot())).not.toContain("private");
  });
  it.each([
    "f@https://kodex.test/a.js:99999999:1",
    "f@https://kodex.test/a.js:1:0",
    "f@https://kodex.test/a.js:-1:3",
    "f@https://user:password@kodex.test/a.js:1:3",
    "f@javascript:private:1:3",
  ])("отклоняет недопустимые координаты/источники %s", (stack) => {
    const reporter = new PageErrorDiagnostics(origin);
    reporter.observe({ stack }, 0, 1);
    expect(reporter.snapshot().events[0]).toMatchObject({
      category: "UNKNOWN",
      line: 0,
      column: 0,
    });
  });
  it("ограничивает вход и явно отличает prefix digest", () => {
    const reporter = new PageErrorDiagnostics(origin);
    reporter.observe(
      { name: "Error", message: "x".repeat(5000), stack: "s".repeat(20000) },
      0,
      0,
    );
    expect(reporter.snapshot()).toMatchObject({ total: 1, overflow: 1 });
    expect(reporter.snapshot().events[0]).toMatchObject({
      digestScope: "BOUNDED_PREFIX",
      messageSHA256: hash("x".repeat(4096)),
      stackSHA256: hash("s".repeat(16384)),
      category: "UNKNOWN",
    });
    expect(reporter.failed()).toBe(true);
  });
  it("ограничивает количество и не выдаёт mutable snapshot", () => {
    const reporter = new PageErrorDiagnostics(origin);
    for (let n = 0; n < 100; n++) reporter.observe(new Error("fixture"), 0, 1);
    const snapshot = reporter.snapshot();
    expect(snapshot).toMatchObject({
      total: 100,
      overflow: 68,
      eventLimit: 32,
    });
    expect(snapshot.events).toHaveLength(32);
    const first = snapshot.events[0];
    if (!first) throw new Error("Missing fixture event");
    first.stage = "UNKNOWN";
    expect(reporter.snapshot().events[0]?.stage).toBe("PREFLIGHT");
    expect(Buffer.byteLength(JSON.stringify(snapshot))).toBeLessThan(24000);
  });
  it("поздняя ошибка не переименовывает завершённый шаг и не доказывает origin", () => {
    const reporter = new PageErrorDiagnostics(origin);
    reporter.beginStep();
    reporter.observe(new Error("first"), 0, 1);
    reporter.endStep();
    reporter.observe(new Error("late"), 0, 2);
    reporter.beginStep();
    reporter.observe(new Error("unknown origin"), 0, 2);
    expect(
      reporter
        .snapshot()
        .events.map((event) => [
          event.stepSequence,
          event.stage,
          event.documentAttribution,
        ]),
    ).toEqual([
      [1, "UI_STEP", "OBSERVATION_ONLY"],
      [0, "BETWEEN_STEPS", "OBSERVATION_ONLY"],
      [2, "UI_STEP", "OBSERVATION_ONLY"],
    ]);
  });
  it("не падает от getter/primitive и не подавляет ResizeObserver ошибку", () => {
    const reporter = new PageErrorDiagnostics(origin);
    reporter.observe(
      {
        get message() {
          throw new Error("private");
        },
      },
      0,
      0,
    );
    reporter.observe("private", 0, 0);
    reporter.observe(
      { name: "Error", message: "ResizeObserver loop limit exceeded" },
      0,
      1,
    );
    expect(reporter.snapshot().events[2]?.code).toBe("RESIZE_OBSERVER_LIMIT");
    expect(reporter.snapshot().total).toBe(3);
    expect(reporter.failed()).toBe(true);
    expect(JSON.stringify(reporter.snapshot())).not.toContain("private");
  });
  it("недостоверное время и переполнение наблюдателя закрыто отражаются", () => {
    const reporter = new PageErrorDiagnostics(origin);
    reporter.observe({}, 0, 0, Number.NaN);
    reporter.observationOverflow();
    expect(reporter.snapshot().events[0]?.elapsedMs).toBe(0);
    expect(reporter.snapshot().overflow).toBe(1);
    expect(reporter.failed()).toBe(true);
  });
});
