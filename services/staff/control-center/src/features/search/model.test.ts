import { afterEach, describe, expect, it, vi } from "vitest";

import {
  canonicalSearchRoute,
  globalSearchDebounceMs,
  SearchCoordinator,
} from "@/features/search/model";
import type { SearchResult } from "@/shared/api/generated/openapi/types.gen";

function result(kind: SearchResult["kind"], ref: string): SearchResult {
  return {
    kind,
    ref,
    projectRef: "prj_project01",
    title: ref,
    subtitle: "",
    state: "ACTIVE",
    updatedAt: "2026-09-03T00:00:00Z",
  };
}

describe("global search model", () => {
  afterEach(() => vi.useRealTimers());

  it("запускает только последний запрос ровно через 500 ms", () => {
    vi.useFakeTimers();
    const search = vi.fn();
    const coordinator = new SearchCoordinator();
    coordinator.schedule("first", search);
    vi.advanceTimersByTime(globalSearchDebounceMs - 1);
    expect(search).not.toHaveBeenCalled();
    coordinator.schedule("agent", search);
    vi.advanceTimersByTime(globalSearchDebounceMs - 1);
    expect(search).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(search).toHaveBeenCalledOnce();
    expect(search).toHaveBeenCalledWith("agent");
  });

  it("строит canonical route для каждого публичного вида", () => {
    expect(canonicalSearchRoute(result("PROJECT", "prj_project01"))).toBe(
      "/projects/prj_project01",
    );
    expect(canonicalSearchRoute(result("AGENT", "agt_employee01"))).toBe(
      "/projects/prj_project01/agents/agt_employee01",
    );
    expect(canonicalSearchRoute(result("WORKFLOW", "wfl_process01"))).toBe(
      "/projects/prj_project01/workflows/wfl_process01",
    );
    expect(canonicalSearchRoute(result("RUN", "run_execution01"))).toBe(
      "/projects/prj_project01/runs/run_execution01",
    );
  });
});
