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
});
