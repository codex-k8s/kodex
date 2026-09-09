import { describe, expect, it } from "vitest";
import { SyntheticFetchCorrelator } from "./synthetic-fetch-correlator";

const url = "https://kodex.test/api/v1/bootstrap";
const one = "11111111-1111-4111-8111-111111111111:1";
const two = "11111111-1111-4111-8111-111111111111:2";
describe("synthetic fetch identity", () => {
  it("navigation связывает START до goto с поздним network Request", () => {
    const observer = new SyntheticFetchCorrelator();
    const late = {},
      current = {};
    observer.observe({ phase: "start", id: one, url });
    observer.navigationStarted();
    observer.request(late, url, one);
    observer.observe({ phase: "start", id: two, url });
    observer.request(current, url, two);
    expect(observer.cancelled(late)).toBe(true);
    expect(observer.cancelled(current)).toBe(false);
  });
  it("последующий goto не переклассифицирует уже случившийся network FAIL", () => {
    const observer = new SyntheticFetchCorrelator();
    const failed = {};
    observer.observe({ phase: "start", id: one, url });
    observer.request(failed, url, one);
    observer.terminal(failed);
    observer.navigationStarted();
    expect(observer.cancelled(failed)).toBe(false);
  });
  it("navigation не принимает duplicate и несовпадающий URL", () => {
    const observer = new SyntheticFetchCorrelator();
    const first = {},
      duplicate = {},
      foreign = {};
    observer.observe({ phase: "start", id: one, url });
    observer.observe({ phase: "start", id: two, url });
    observer.navigationStarted();
    observer.request(first, url, one);
    observer.request(duplicate, url, one);
    observer.request(foreign, `${url}/other`, two);
    expect(observer.cancelled(first)).toBe(false);
    expect(observer.cancelled(duplicate)).toBe(false);
    expect(observer.cancelled(foreign)).toBe(false);
  });
  it.each([true, false])(
    "связывает receipt до/после request: %s",
    (receiptFirst) => {
      const observer = new SyntheticFetchCorrelator();
      const request = {};
      const start = () => observer.observe({ phase: "start", id: one, url });
      if (receiptFirst) start();
      observer.request(request, url, one);
      if (!receiptFirst) start();
      expect(observer.cancelled(request)).toBe(false);
      observer.observe({ phase: "abort", id: one, url });
      expect(observer.cancelled(request)).toBe(true);
    },
  );
  it("связывает конкурентные одинаковые URL, даже когда оба start пришли до request", () => {
    const observer = new SyntheticFetchCorrelator();
    const first = {},
      second = {};
    observer.observe({ phase: "start", id: one, url });
    observer.observe({ phase: "start", id: two, url });
    observer.request(second, url, two);
    observer.request(first, url, one);
    observer.observe({ phase: "abort", id: one, url });
    expect(observer.cancelled(first)).toBe(true);
    expect(observer.cancelled(second)).toBe(false);
  });
  it("поздний abort завершённого поколения не отменяет новый запрос", () => {
    const observer = new SyntheticFetchCorrelator();
    const old = {},
      current = {};
    observer.observe({ phase: "start", id: one, url });
    observer.request(old, url, one);
    observer.observe({ phase: "start", id: two, url });
    observer.request(current, url, two);
    observer.observe({ phase: "abort", id: one, url });
    expect(observer.cancelled(old)).toBe(true);
    expect(observer.cancelled(current)).toBe(false);
  });
  it("не принимает missing, malformed, foreign и duplicate network identity", () => {
    for (const id of [undefined, "unknown", two]) {
      const observer = new SyntheticFetchCorrelator();
      const request = {};
      observer.observe({ phase: "start", id: one, url });
      observer.observe({ phase: "abort", id: one, url });
      observer.request(request, url, id);
      expect(observer.cancelled(request)).toBe(false);
    }
    const observer = new SyntheticFetchCorrelator();
    const first = {},
      second = {};
    observer.observe({ phase: "start", id: one, url });
    observer.observe({ phase: "abort", id: one, url });
    observer.request(first, url, one);
    observer.request(second, url, one);
    expect(observer.cancelled(first)).toBe(false);
    expect(observer.cancelled(second)).toBe(false);
  });
  it("отклоняет другой URL, duplicate start и ошибку без AbortSignal", () => {
    for (const variant of ["url", "duplicate", "no-abort"] as const) {
      const observer = new SyntheticFetchCorrelator();
      const request = {};
      observer.request(request, url, one);
      observer.observe({ phase: "start", id: one, url });
      if (variant === "duplicate")
        observer.observe({ phase: "start", id: one, url });
      if (variant !== "no-abort")
        observer.observe({
          phase: "abort",
          id: one,
          url: variant === "url" ? `${url}/foreign` : url,
        });
      expect(observer.cancelled(request)).toBe(false);
    }
  });
  it("поздний start связывает ранний abort лишь после подтверждения обеих частей", () => {
    const observer = new SyntheticFetchCorrelator();
    const request = {};
    observer.request(request, url, one);
    observer.observe({ phase: "abort", id: one, url });
    expect(observer.cancelled(request)).toBe(false);
    observer.observe({ phase: "start", id: one, url });
    expect(observer.cancelled(request)).toBe(true);
  });
  it.each([true, false])(
    "принимает exact application body completion до/после request: %s",
    (bodyFirst) => {
      const observer = new SyntheticFetchCorrelator();
      const request = {};
      observer.observe({ phase: "start", id: one, url });
      if (bodyFirst) observer.observe({ phase: "body", id: one, url });
      observer.request(request, url, one);
      if (!bodyFirst) observer.observe({ phase: "body", id: one, url });
      expect(observer.bodyCompleted(request)).toBe(true);
    },
  );
  it.each(["headers", "body-error", "reject", "abort"] as const)(
    "не выдаёт phase=%s за завершённый body",
    (phase) => {
      const observer = new SyntheticFetchCorrelator();
      const request = {};
      observer.observe({ phase: "start", id: one, url });
      observer.observe({ phase, id: one, url });
      observer.request(request, url, one);
      expect(observer.bodyCompleted(request)).toBe(false);
    },
  );
  it("не принимает body с чужим URL или неоднозначной identity", () => {
    const foreign = new SyntheticFetchCorrelator();
    const foreignRequest = {};
    foreign.observe({ phase: "start", id: one, url });
    foreign.observe({ phase: "body", id: one, url: `${url}/foreign` });
    foreign.request(foreignRequest, url, one);
    expect(foreign.bodyCompleted(foreignRequest)).toBe(false);

    const duplicate = new SyntheticFetchCorrelator();
    const first = {};
    const second = {};
    duplicate.observe({ phase: "start", id: one, url });
    duplicate.observe({ phase: "body", id: one, url });
    duplicate.request(first, url, one);
    duplicate.request(second, url, one);
    expect(duplicate.bodyCompleted(first)).toBe(false);
    expect(duplicate.bodyCompleted(second)).toBe(false);
  });
  it.each(["body-error", "reject", "abort"] as const)(
    "не принимает body вместе с phase=%s ни в одном порядке",
    (phase) => {
      for (const bodyFirst of [true, false]) {
        const observer = new SyntheticFetchCorrelator();
        const request = {};
        observer.observe({ phase: "start", id: one, url });
        if (bodyFirst) observer.observe({ phase: "body", id: one, url });
        observer.observe({ phase, id: one, url });
        if (!bodyFirst) observer.observe({ phase: "body", id: one, url });
        observer.request(request, url, one);
        expect(observer.bodyCompleted(request)).toBe(false);
      }
    },
  );
  it.each([true, false])(
    "подтверждает exact body AbortError до/после abort receipt: %s",
    (bodyErrorFirst) => {
      const observer = new SyntheticFetchCorrelator();
      const request = {};
      observer.request(request, url, one);
      observer.observe({
        phase: "start",
        id: one,
        url,
        signalGeneration: 7,
      });
      observer.observe({
        phase: "headers",
        id: one,
        url,
        signalGeneration: 7,
      });
      const abort = () =>
        observer.observe({
          phase: "abort",
          id: one,
          url,
          signalGeneration: 7,
          signalAborted: true,
          reasonClass: "AbortError",
        });
      const bodyError = () =>
        observer.observe({
          phase: "body-error",
          id: one,
          url,
          signalGeneration: 7,
          signalAborted: true,
          reasonClass: "AbortError",
        });
      if (bodyErrorFirst) bodyError();
      abort();
      if (!bodyErrorFirst) bodyError();
      expect(observer.bodyAbortConfirmed(request)).toBe(true);
    },
  );
  it.each([
    "headers",
    "abort",
    "body-error",
    "signal",
    "reason",
    "generation",
    "zero-generation",
    "duplicate",
    "concurrent",
    "body",
    "reject",
  ] as const)("отклоняет неполный body abort proof: %s", (missing) => {
    const observer = new SyntheticFetchCorrelator();
    const request = {};
    observer.request(request, url, one);
    if (missing === "concurrent") observer.request({}, url, one);
    observer.observe({
      phase: "start",
      id: one,
      url,
      signalGeneration: missing === "zero-generation" ? 0 : 7,
    });
    if (missing === "duplicate")
      observer.observe({
        phase: "start",
        id: one,
        url,
        signalGeneration: 7,
      });
    if (missing !== "headers")
      observer.observe({
        phase: "headers",
        id: one,
        url,
        signalGeneration: missing === "zero-generation" ? 0 : 7,
      });
    if (missing !== "abort")
      observer.observe({
        phase: "abort",
        id: one,
        url,
        signalGeneration: missing === "zero-generation" ? 0 : 7,
        signalAborted: true,
        reasonClass: "AbortError",
      });
    if (missing !== "body-error")
      observer.observe({
        phase: "body-error",
        id: one,
        url,
        signalGeneration: missing === "generation" ? 8 : 7,
        signalAborted: missing !== "signal",
        reasonClass: missing === "reason" ? "TypeError" : "AbortError",
      });
    if (missing === "body")
      observer.observe({ phase: "body", id: one, url, signalGeneration: 7 });
    if (missing === "reject")
      observer.observe({ phase: "reject", id: one, url, signalGeneration: 7 });
    expect(observer.bodyAbortConfirmed(request)).toBe(false);
  });
});
