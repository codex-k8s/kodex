import { expect, test } from "vitest";
import { SessionRequestDiagnostics } from "./session-request-diagnostics";

test("диагностика связывает exact Request и не сохраняет URL/query/header/body/error text", () => {
  const observer = new SessionRequestDiagnostics("https://kodex.test");
  const first = {},
    second = {};
  observer.start(
    first,
    {
      url: "https://kodex.test/api/v1/session/ticket?ticket=private-marker",
      method: "POST",
      resourceType: "fetch",
      stage: "INITIAL_READY",
      tab: 0,
    },
    100,
  );
  observer.start(
    second,
    {
      url: "https://kodex.test/api/v1/session/ticket?ticket=other-private",
      method: "POST",
      resourceType: "fetch",
      stage: "NATURAL_RENEWAL",
      tab: 1,
    },
    200,
  );
  observer.failed(second, "READBACK", "failure private-marker", 450);
  observer.failed(first, "READBACK", "Load request cancelled", 500);
  const result = observer.snapshot();
  expect(result.requestsObserved).toBe(2);
  expect(observer.sequenceOf(first)).toBe(1);
  expect(observer.sequenceOf(second)).toBe(2);
  expect(observer.sequenceOf({})).toBeUndefined();
  expect(
    result.failures.map((value) => [
      value.requestSequence,
      value.tab,
      value.elapsedMs,
      value.code,
    ]),
  ).toEqual([
    [2, 1, 250, "UNKNOWN"],
    [1, 0, 400, "WEBKIT_CANCELLED"],
  ]);
  expect(
    result.failures.every(
      (value) =>
        value.route === "SESSION_TICKET" &&
        value.identityKnown &&
        /^[a-f0-9]{64}$/.test(value.errorSHA256),
    ),
  ).toBe(true);
  const serialized = JSON.stringify(result);
  for (const forbidden of [
    "private-marker",
    "other-private",
    "https://",
    "ticket=",
    "failure private",
  ])
    expect(serialized).not.toContain(forbidden);
  expect(result.failures[0]?.startedStage).toBe("NATURAL_RENEWAL");
  expect(result.failures[0]?.failedStage).toBe("READBACK");
});

test("unknown route/method/resource и overflow остаются ограниченными facts без ignore", () => {
  const observer = new SessionRequestDiagnostics("https://kodex.test");
  const request = {};
  observer.start(request, {
    url: "https://foreign.invalid/secret-ref",
    method: "private-method",
    resourceType: "private-resource",
    stage: "INITIAL_READY",
    tab: 8,
  });
  observer.failed(request, "READBACK", "NS_BINDING_ABORTED");
  observer.failed({}, "READBACK", "unknown");
  for (let index = 0; index < 35; index++)
    observer.failed({}, "READBACK", "unknown");
  const result = observer.snapshot();
  expect(result.failures).toHaveLength(32);
  expect(result.overflow).toBe(5);
  expect(result.failures[0]).toMatchObject({
    route: "FOREIGN_ORIGIN",
    method: "OTHER",
    resourceType: "other",
    tab: -1,
    code: "FIREFOX_ABORTED",
  });
  expect(result.failures[1]).toMatchObject({
    identityKnown: false,
    requestSequence: 0,
    route: "UNOBSERVED",
  });
  expect(JSON.stringify(result)).not.toContain("private-");
});

test("hot-reload revision имеет закрытую категорию без раскрытия pathname/query", () => {
  const observer = new SessionRequestDiagnostics("https://kodex.test");
  const request = {};
  observer.start(request, {
    url: "https://kodex.test/__kodex_dev_revision?private-marker",
    method: "GET",
    resourceType: "fetch",
    stage: "READBACK",
    tab: 0,
  });
  observer.failed(request, "READBACK", "NS_BINDING_ABORTED");
  expect(observer.snapshot().failures[0]).toMatchObject({
    route: "DEV_REVISION",
    method: "GET",
    resourceType: "fetch",
    code: "FIREFOX_ABORTED",
  });
  expect(JSON.stringify(observer.snapshot())).not.toContain("private-marker");
  expect(JSON.stringify(observer.snapshot())).not.toContain(
    "__kodex_dev_revision",
  );
});
