import { describe, expect, it } from "vitest";
import { SyntheticFetchCorrelator } from "./synthetic-fetch-correlator";

const url = "https://kodex.test/api/v1/draft";
describe("synthetic fetch identity", () => {
  it.each([true, false])(
    "связывает receipt до/после request: %s",
    (receiptFirst) => {
      const observer = new SyntheticFetchCorrelator();
      const request = {};
      const start = () => observer.observe({ phase: "start", id: "one", url });
      if (receiptFirst) start();
      observer.request(request, url);
      if (!receiptFirst) start();
      expect(observer.cancelled(request)).toBe(false);
      observer.observe({ phase: "abort", id: "one", url });
      expect(observer.cancelled(request)).toBe(true);
    },
  );
  it("поздний abort завершённого поколения не отменяет новый запрос", () => {
    const observer = new SyntheticFetchCorrelator();
    const old = {},
      current = {};
    observer.observe({ phase: "start", id: "old", url });
    observer.request(old, url);
    observer.observe({ phase: "start", id: "current", url });
    observer.request(current, url);
    observer.observe({ phase: "abort", id: "old", url });
    expect(observer.cancelled(old)).toBe(true);
    expect(observer.cancelled(current)).toBe(false);
  });
  it("не принимает неоднозначные и неизвестные поколения", () => {
    const observer = new SyntheticFetchCorrelator();
    const first = {},
      second = {};
    observer.request(first, url);
    observer.request(second, url);
    observer.observe({ phase: "start", id: "one", url });
    observer.observe({ phase: "abort", id: "one", url });
    observer.observe({ phase: "abort", id: "unknown", url });
    expect(observer.cancelled(first)).toBe(false);
    expect(observer.cancelled(second)).toBe(false);
  });
});
